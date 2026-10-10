package docker

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/atif-1402/parse/internal/tool"
)

var (
	// reDFSectionTitle matches the section titles docker system df -v prints:
	// "Images space usage:", "Containers space usage:", "Local Volumes space
	// usage:", "Build cache usage: 119.3MB". Table headers and rows always
	// contain a run of two or more spaces, which the title never does.
	reDFSectionTitle = regexp.MustCompile(`^[A-Za-z][A-Za-z ]* usage:`)

	// reSizeToken matches the leading size of a df cell: "0B", "21.8kB",
	// "351.5MB", "82.17MB (23%)" — docker's sizes are decimal (kB = 1000).
	reSizeToken = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?[kMGT]?B`)

	rePlainNum = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

	// rePaintTag strips the <dim>…</> tags the test stubs emit, so widths are
	// right in tests and in a real terminal alike.
	rePaintTag = regexp.MustCompile(`</?(?:bold|dim|red|green|yellow|cyan|)>|</>`)
)

// dfShowAll is the CLI's --all, the one flag that changes what this
// formatter prints: with it, `docker system df -v` shows the build cache
// rows instead of collapsing them to a line. Docker itself rejects the flag,
// so Run strips it before calling docker and records it here.
var dfShowAll bool

// SetDFShowAll records whether the build cache rows must be shown. Called
// from Run when --all or -a appears after `system df`.
func SetDFShowAll(v bool) { dfShowAll = v }

// dfExactSummary, when set, replaces the computed -v summary line. Only the
// direct path can fill it: it runs a plain `docker system df` beside the -v
// one, because summing -v rows double-counts shared layers and the plain
// table already carries docker's own exact totals.
var dfExactSummary string

// SetDFExactSummary records the summary line from a plain `docker system
// df` run, or "" to fall back to the computed one.
func SetDFExactSummary(s string) { dfExactSummary = s }

// dfSummaryFromText renders the summary line for df text, or "" when the
// text is not df. The direct path uses it on a plain run's output.
func dfSummaryFromText(text string) string {
	sections := parseSystemDF(text)
	if len(sections) == 0 {
		return ""
	}
	return dfSummary(sections)
}

// looksLikeSystemDF reports whether text is `docker system df` output, plain
// or -v. The first non-empty line decides, the same rule psHeader follows:
// a df-shaped table appearing later in text that belongs to another tool must
// not be stolen from it.
func looksLikeSystemDF(text string) bool {
	first := dfFirstLine(text)
	if first == "" {
		return false
	}
	if dfIsSectionTitle(first) {
		return true
	}
	return strings.Contains(first, "TYPE") && strings.Contains(first, "TOTAL") && strings.Contains(first, "SIZE")
}

func dfFirstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func dfIsSectionTitle(line string) bool {
	return reDFSectionTitle.MatchString(line) && !strings.Contains(line, "  ")
}

type dfSection struct {
	title      string
	headers    []string
	rows       [][]string
	starts     []int
	cacheEmpty bool

	// zeroContainers counts the -v containers rows dropped because their
	// SIZE is 0B; they collapse into one dim line instead of the table.
	// zeroStates breaks those down by state: only exited is "stopped".
	zeroContainers int
	zeroStates     map[string]int

	// stateRanks orders containers rows after the size sort: running first,
	// then problems, then clean exits. Aligned with rows.
	stateRanks []int
}

func parseSystemDF(text string) []dfSection {
	first := dfFirstLine(text)
	if first == "" || !looksLikeSystemDF(text) {
		return nil
	}
	lines := strings.Split(text, "\n")
	if dfIsSectionTitle(first) {
		return parseDFSections(lines)
	}
	return parseDFSummary(lines)
}

func parseDFSummary(lines []string) []dfSection {
	var sec dfSection
	seenHeader := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !seenHeader {
			sec.starts = dfColumnStarts(line)
			if sec.starts == nil {
				return nil
			}
			sec.headers = headerFields(line, sec.starts)
			seenHeader = true
			continue
		}
		if cells := dfCells(line, sec.starts, len(sec.headers)); cells != nil {
			sec.rows = append(sec.rows, cells)
		}
	}
	if !seenHeader {
		return nil
	}
	return []dfSection{sec}
}

func parseDFSections(lines []string) []dfSection {
	var sections []dfSection
	var cur dfSection
	flush := func() {
		if cur.title != "" || cur.headers != nil {
			sections = append(sections, cur)
		}
		cur = dfSection{}
	}
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if dfIsSectionTitle(t) {
			flush()
			cur.title = dfTitleOf(t)
			continue
		}
		if cur.headers == nil {
			cur.starts = dfColumnStarts(t)
			if cur.starts == nil {
				continue
			}
			cur.headers = headerFields(t, cur.starts)
			continue
		}
		if cells := dfCells(t, cur.starts, len(cur.headers)); cells != nil {
			cur.rows = append(cur.rows, cells)
		}
	}
	flush()
	return sections
}

func dfTitleOf(line string) string {
	t := line
	if idx := strings.Index(t, " usage:"); idx >= 0 {
		t = t[:idx]
	}
	t = strings.TrimSpace(t)
	t = strings.TrimSuffix(t, " space")
	return strings.TrimSpace(t)
}

// dfCells slices one row against the column starts its own header defines, so
// a COMMAND cell containing its own spacing is still one cell.
func dfCells(line string, starts []int, n int) []string {
	runes := []rune(line)
	cells := make([]string, n)
	anyValue := false
	for i := 0; i < n && i < len(starts); i++ {
		lo := starts[i]
		if lo >= len(runes) {
			continue
		}
		hi := len(runes)
		if i+1 < len(starts) && starts[i+1] < hi {
			hi = starts[i+1]
		}
		if lo >= hi {
			continue
		}
		cells[i] = strings.TrimSpace(string(runes[lo:hi]))
		if cells[i] != "" {
			anyValue = true
		}
	}
	if !anyValue {
		return nil
	}
	return cells
}

func normalizeSize(s string) string {
	raw := strings.TrimSpace(s)
	m := reSizeToken.FindString(raw)
	if m == "" {
		return s
	}
	b, ok := sizeBytes(raw)
	if !ok {
		return s
	}
	return formatBytes(b) + strings.TrimPrefix(raw, m)
}

func sizeBytes(s string) (float64, bool) {
	m := reSizeToken.FindString(strings.TrimSpace(s))
	if m == "" {
		return 0, false
	}
	unit := "B"
	if len(m) >= 2 {
		switch m[len(m)-2] {
		case 'k', 'M', 'G', 'T':
			unit = m[len(m)-2:]
		}
	}
	num, err := strconv.ParseFloat(strings.TrimSuffix(m, unit), 64)
	if err != nil {
		return 0, false
	}
	var mult float64
	switch unit {
	case "B":
		mult = 1
	case "kB":
		mult = 1e3
	case "MB":
		mult = 1e6
	case "GB":
		mult = 1e9
	case "TB":
		mult = 1e12
	default:
		return 0, false
	}
	return num * mult, true
}

func formatBytes(b float64) string {
	if b < 1000 {
		return strconv.FormatInt(int64(b), 10) + "B"
	}
	units := []string{"kB", "MB", "GB", "TB"}
	u := -1
	for b >= 1000 && u < len(units)-1 {
		b /= 1000
		u++
	}
	s := roundSig4(b)
	if f, err := strconv.ParseFloat(s, 64); err == nil && f >= 1000 && u < len(units)-1 {
		s = roundSig4(b / 1000)
		u++
	}
	return s + units[u]
}

// roundSig4 renders v with four significant digits, the precision docker's
// own sizes carry: 134.66 → "134.7", 21.8 → "21.8", 63 → "63".
func roundSig4(v float64) string {
	if v == 0 {
		return "0"
	}
	decimals := 3 - int(math.Floor(math.Log10(math.Abs(v))))
	if decimals < 0 {
		decimals = 0
	}
	p := math.Pow(10, float64(decimals))
	return strconv.FormatFloat(math.Round(v*p)/p, 'f', -1, 64)
}

func isZeroCell(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return true
	}
	if b, ok := sizeBytes(t); ok {
		return b == 0
	}
	if rePlainNum.MatchString(t) {
		f, err := strconv.ParseFloat(t, 64)
		return err == nil && f == 0
	}
	return false
}

func isNumericCell(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return false
	}
	if _, ok := sizeBytes(t); ok {
		return true
	}
	return rePlainNum.MatchString(t)
}

// hideZeroColumns drops a column when every row in it is empty or zero —
// data-driven, so SHARED SIZE disappears only when it is actually all 0B.
func hideZeroColumns(headers []string, rows [][]string) ([]string, [][]string) {
	keep := make([]bool, len(headers))
	for i := range headers {
		for _, row := range rows {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			if !isZeroCell(cell) {
				keep[i] = true
				break
			}
		}
	}
	var newHeaders []string
	for i, h := range headers {
		if keep[i] {
			newHeaders = append(newHeaders, h)
		}
	}
	newRows := make([][]string, len(rows))
	for r, row := range rows {
		cells := make([]string, 0, len(newHeaders))
		for i := range headers {
			if keep[i] {
				cell := ""
				if i < len(row) {
					cell = row[i]
				}
				cells = append(cells, cell)
			}
		}
		newRows[r] = cells
	}
	return newHeaders, newRows
}

func appendDFTags(sec *dfSection) {
	repoI, tagI, contI := indexOf(sec.headers, "REPOSITORY"), indexOf(sec.headers, "TAG"), indexOf(sec.headers, "CONTAINERS")
	if repoI >= 0 && tagI >= 0 && contI >= 0 {
		sec.headers = append(sec.headers, "TAGS")
		for ri, row := range sec.rows {
			var tags []string
			if row[repoI] == "<none>" || row[tagI] == "<none>" {
				tags = append(tags, "dangling")
			}
			if isZeroCell(row[contI]) {
				tags = append(tags, "unused")
			}
			sec.rows[ri] = append(row, strings.Join(tags, " "))
		}
		return
	}
	nameI, linksI := indexOf(sec.headers, "VOLUME NAME"), indexOf(sec.headers, "LINKS")
	if nameI >= 0 && linksI >= 0 {
		sec.headers = append(sec.headers, "TAGS")
		for ri, row := range sec.rows {
			tags := ""
			if isZeroCell(row[linksI]) {
				tags = "unused"
			}
			sec.rows[ri] = append(row, tags)
		}
	}
}

// dfStateRank orders a container's state for the sort: running first, then
// problems (created, paused, restarting, dead, unhealthy, nonzero exits),
// then clean exits.
func dfStateRank(s ContainerStatus) int {
	switch s.State {
	case "running":
		if s.Health == "unhealthy" {
			return 1
		}
		return 0
	case "exited":
		if s.ExitCode == 0 {
			return 2
		}
		return 1
	default:
		return 1
	}
}

// buildContainersSection reshapes the -v containers table into the three
// columns that answer "which containers take space": NAME, STATE and SIZE.
// Empty rows collapse into one dim line counted in zeroContainers.
func buildContainersSection(sec *dfSection) {
	nameI := indexOf(sec.headers, "NAMES")
	sizeI := indexOf(sec.headers, "SIZE")
	statusI := indexOf(sec.headers, "STATUS")
	if nameI < 0 || sizeI < 0 || statusI < 0 {
		return
	}
	cell := func(row []string, i int) string {
		if i < len(row) {
			return row[i]
		}
		return ""
	}
	var rows [][]string
	var ranks []int
	zero := 0
	var states map[string]int
	for _, row := range sec.rows {
		sz := cell(row, sizeI)
		st := parseStatusFromText(cell(row, statusI))
		if isZeroCell(sz) {
			zero++
			if states == nil {
				states = make(map[string]int)
			}
			states[st.State]++
			continue
		}
		ranks = append(ranks, dfStateRank(st))
		rows = append(rows, []string{cell(row, nameI), formatState(st), sz})
	}
	sec.headers = []string{"NAME", "STATE", "SIZE"}
	sec.rows = rows
	sec.stateRanks = ranks
	sec.zeroContainers = zero
	sec.zeroStates = states
}

func transformDFSection(sec *dfSection) {
	for i, h := range sec.headers {
		if !strings.Contains(h, "SIZE") && h != "RECLAIMABLE" {
			continue
		}
		for ri := range sec.rows {
			if i < len(sec.rows[ri]) {
				sec.rows[ri][i] = normalizeSize(sec.rows[ri][i])
			}
		}
	}
	if indexOf(sec.headers, "CONTAINER ID") >= 0 {
		buildContainersSection(sec)
	} else {
		appendDFTags(sec)
	}
	if sec.title == "" {
		tI, totI := indexOf(sec.headers, "TYPE"), indexOf(sec.headers, "TOTAL")
		if tI >= 0 && totI >= 0 {
			var kept [][]string
			for _, row := range sec.rows {
				if strings.Contains(row[tI], "Build Cache") && isZeroCell(row[totI]) {
					sec.cacheEmpty = true
					continue
				}
				kept = append(kept, row)
			}
			sec.rows = kept
		}
		// Docker prints a percentage on every RECLAIMABLE cell except the
		// build cache row's; one row with and one without reads as a bug,
		// so the cache row gets its own.
		if rI := indexOf(sec.headers, "RECLAIMABLE"); rI >= 0 {
			if sizeI := indexOf(sec.headers, "SIZE"); sizeI >= 0 {
				for ri, row := range sec.rows {
					if tI < 0 || tI >= len(row) || rI >= len(row) || sizeI >= len(row) {
						continue
					}
					if !strings.Contains(row[tI], "Build Cache") {
						continue
					}
					if strings.Contains(row[rI], "(") {
						continue
					}
					sz, _ := sizeBytes(row[sizeI])
					r, ok := sizeBytes(row[rI])
					if !ok || sz <= 0 {
						continue
					}
					sec.rows[ri][rI] = fmt.Sprintf("%s (%d%%)", row[rI], int(math.Round(r/sz*100)))
				}
			}
		}
	}
	sec.headers, sec.rows = hideZeroColumns(sec.headers, sec.rows)
	dfSortSection(sec)
}

// dfNameColumn picks the column that breaks a size tie, per section type.
func dfNameColumn(sec *dfSection) (int, []int) {
	if i := indexOf(sec.headers, "NAME"); i >= 0 {
		return i, nil
	}
	if i := indexOf(sec.headers, "NAMES"); i >= 0 {
		return i, nil
	}
	if i := indexOf(sec.headers, "VOLUME NAME"); i >= 0 {
		return i, nil
	}
	if i := indexOf(sec.headers, "CACHE ID"); i >= 0 {
		return i, nil
	}
	if i := indexOf(sec.headers, "TYPE"); i >= 0 {
		return i, nil
	}
	repoI, tagI := indexOf(sec.headers, "REPOSITORY"), indexOf(sec.headers, "TAG")
	if repoI >= 0 && tagI >= 0 {
		return repoI, []int{tagI}
	}
	return -1, nil
}

func dfRowCell(row []string, i int) string {
	if i >= 0 && i < len(row) {
		return row[i]
	}
	return ""
}

// dfSortSection orders rows by size descending, then state (running,
// problems, clean exits), then name, so the same input always prints the
// same table.
func dfSortSection(sec *dfSection) {
	sizeI := indexOf(sec.headers, "SIZE")
	if sizeI < 0 || len(sec.rows) < 2 {
		return
	}
	nameI, extraI := dfNameColumn(sec)
	ranked := len(sec.stateRanks) == len(sec.rows)
	type keyed struct {
		row  []string
		rank int
		size float64
		name string
	}
	keyedRows := make([]keyed, len(sec.rows))
	for i, row := range sec.rows {
		size, _ := sizeBytes(dfRowCell(row, sizeI))
		name := dfRowCell(row, nameI)
		for _, j := range extraI {
			name += " " + dfRowCell(row, j)
		}
		rank := 0
		if ranked {
			rank = sec.stateRanks[i]
		}
		keyedRows[i] = keyed{row: row, rank: rank, size: size, name: name}
	}
	sort.SliceStable(keyedRows, func(a, b int) bool {
		if keyedRows[a].size != keyedRows[b].size {
			return keyedRows[a].size > keyedRows[b].size
		}
		if keyedRows[a].rank != keyedRows[b].rank {
			return keyedRows[a].rank < keyedRows[b].rank
		}
		return keyedRows[a].name < keyedRows[b].name
	})
	for i, kr := range keyedRows {
		sec.rows[i] = kr.row
		if ranked {
			sec.stateRanks[i] = kr.rank
		}
	}
}

func dfAligns(headers []string, rows [][]string) []string {
	aligns := make([]string, len(headers))
	for i := range headers {
		right := len(rows) > 0
		for _, row := range rows {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			if !isNumericCell(cell) {
				right = false
				break
			}
		}
		if right {
			aligns[i] = "right"
		} else {
			aligns[i] = "left"
		}
	}
	return aligns
}

// dfComponent is one named total on the summary line. approx marks a total
// built by summing -v rows, which double-counts shared layers; the reader
// sees it behind a ~ so it is never mistaken for docker's own number.
type dfComponent struct {
	name   string
	bytes  float64
	approx bool
}

// dfSummaryLine prints the components largest first, reclaimable last — the
// one number the reader wants after the totals.
func dfSummaryLine(comps []dfComponent, reclaim float64) string {
	sort.SliceStable(comps, func(i, j int) bool {
		if comps[i].bytes != comps[j].bytes {
			return comps[i].bytes > comps[j].bytes
		}
		return comps[i].name < comps[j].name
	})
	parts := make([]string, 0, len(comps)+1)
	for _, c := range comps {
		prefix := ""
		if c.approx {
			prefix = "~"
		}
		parts = append(parts, fmt.Sprintf("%s%s %s", prefix, c.name, formatBytes(c.bytes)))
	}
	parts = append(parts, fmt.Sprintf("reclaimable %s", formatBytes(reclaim)))
	return strings.Join(parts, " · ")
}

func dfSummary(sections []dfSection) string {
	var reclaim float64
	if len(sections) > 0 && sections[0].title == "" {
		sec := sections[0]
		sizeI := indexOf(sec.headers, "SIZE")
		tI, rI := indexOf(sec.headers, "TYPE"), indexOf(sec.headers, "RECLAIMABLE")
		var images, containers, volumes, cache float64
		for _, row := range sec.rows {
			sz, _ := sizeBytes(dfRowCell(row, sizeI))
			switch {
			case tI < 0:
			case strings.Contains(dfRowCell(row, tI), "Image"):
				images += sz
			case strings.Contains(dfRowCell(row, tI), "Container"):
				containers += sz
			case strings.Contains(dfRowCell(row, tI), "Volume"):
				volumes += sz
			case strings.Contains(dfRowCell(row, tI), "Build Cache"):
				cache += sz
			}
			if r, ok := sizeBytes(dfRowCell(row, rI)); ok {
				reclaim += r
			}
		}
		comps := []dfComponent{{name: "images", bytes: images}, {name: "build cache", bytes: cache}, {name: "volumes", bytes: volumes}, {name: "containers", bytes: containers}}
		return dfSummaryLine(comps, reclaim)
	}
	var images, containers, volumes, cache float64
	for _, sec := range sections {
		sizeI := indexOf(sec.headers, "SIZE")
		if sizeI < 0 {
			continue
		}
		contI, linksI := indexOf(sec.headers, "CONTAINERS"), indexOf(sec.headers, "LINKS")
		// Images call the column SHARED SIZE; the build cache calls it SHARED.
		sharedI := indexOf(sec.headers, "SHARED SIZE")
		if sharedI < 0 {
			sharedI = indexOf(sec.headers, "SHARED")
		}
		for _, row := range sec.rows {
			sz, _ := sizeBytes(dfRowCell(row, sizeI))
			switch sec.title {
			case "Images":
				images += sz
				// An image with a container keeps its layers; one without
				// offers SIZE minus what it shares with other images —
				// a shared layer is reclaimable once, not per sharer.
				if contI >= 0 && isZeroCell(dfRowCell(row, contI)) {
					shared, _ := sizeBytes(dfRowCell(row, sharedI))
					if sz-shared > 0 {
						reclaim += sz - shared
					}
				}
			case "Containers":
				containers += sz
			case "Local Volumes":
				volumes += sz
				if linksI >= 0 && isZeroCell(dfRowCell(row, linksI)) {
					reclaim += sz
				}
			case "Build cache":
				cache += sz
				// An entry still shared into a later build is in use; the
				// rest can be pruned.
				if sharedI >= 0 && strings.EqualFold(dfRowCell(row, sharedI), "false") {
					reclaim += sz
				}
			}
		}
	}
	// Volumes do not share layers, so their sum is docker's own number;
	// the other three double-count shared layers and are marked approx.
	comps := []dfComponent{
		{name: "images", bytes: images, approx: true},
		{name: "build cache", bytes: cache, approx: true},
		{name: "volumes", bytes: volumes},
		{name: "containers", bytes: containers, approx: true},
	}
	return dfSummaryLine(comps, reclaim)
}

func displayLen(s string) int {
	return len([]rune(rePaintTag.ReplaceAllString(tool.StripANSI(s), "")))
}

func padDF(s string, width int, align string) string {
	v := displayLen(s)
	if v >= width {
		return s
	}
	spaces := strings.Repeat(" ", width-v)
	if align == "right" {
		return spaces + s
	}
	return s + spaces
}

func dfRule(title string) string {
	limit := 40
	if w := dfTermWidth(); w > 0 && w < limit {
		limit = w
	}
	r := "── " + title + " "
	if n := limit - len([]rune(r)); n > 0 {
		r += strings.Repeat("─", n)
	}
	return paintDim(r)
}

// dfTermWidth is the terminal's COLUMNS, or 0 when unset or unreadable.
func dfTermWidth() int {
	n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("COLUMNS")))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// dfFitTable drops rightmost columns — the least important first, TAGS and
// identifiers before names and sizes — until the table fits the terminal.
func dfFitTable(headers []string, rows [][]string, width int) ([]string, [][]string) {
	if width <= 0 {
		return headers, rows
	}
	total := func(hs []string) int {
		w := 0
		for i, h := range hs {
			col := displayLen(h)
			for _, row := range rows {
				if i < len(row) && displayLen(row[i]) > col {
					col = displayLen(row[i])
				}
			}
			if i > 0 {
				w += 2
			}
			w += col
		}
		return w
	}
	for len(headers) > 2 && total(headers) > width {
		headers = headers[:len(headers)-1]
		for r := range rows {
			if len(rows[r]) > len(headers) {
				rows[r] = rows[r][:len(headers)]
			}
		}
	}
	return headers, rows
}

// dfWrapSummary breaks the summary across lines at " · " when the terminal
// is too narrow for it on one line.
func dfWrapSummary(summary string, width int) string {
	if width <= 0 {
		return summary
	}
	parts := strings.Split(summary, " · ")
	var lines []string
	cur := ""
	for _, p := range parts {
		switch {
		case cur == "":
			cur = p
		case displayLen(cur)+3+displayLen(p) <= width:
			cur += " · " + p
		default:
			lines = append(lines, cur)
			cur = "  " + p
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return strings.Join(lines, "\n")
}

func renderDFTable(headers []string, rows [][]string, aligns []string) string {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = displayLen(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && displayLen(cell) > widths[i] {
				widths[i] = displayLen(cell)
			}
		}
	}
	var buf bytes.Buffer
	var head strings.Builder
	for i, h := range headers {
		if i > 0 {
			head.WriteString(colGap)
		}
		head.WriteString(padDF(h, widths[i], aligns[i]))
	}
	buf.WriteString(paintBold(strings.TrimRight(head.String(), " ")))
	buf.WriteByte('\n')
	for _, row := range rows {
		var line strings.Builder
		for i, cell := range row {
			if i > 0 {
				line.WriteString(colGap)
			}
			p := padDF(cell, widths[i], aligns[i])
			if i < len(headers) && headers[i] == "TAGS" {
				p = paintDim(p)
			}
			line.WriteString(p)
		}
		buf.WriteString(strings.TrimRight(line.String(), " "))
		buf.WriteByte('\n')
	}
	return buf.String()
}

// dfZeroContainersLine is the one dim line the dropped empty containers
// collapse into, broken down by state: only exited is "stopped"; paused,
// restarting and created are counted separately because they are none of
// the reader's business to guess at.
func dfZeroContainersLine(sec *dfSection) string {
	n := sec.zeroContainers
	unit := "containers"
	if n == 1 {
		unit = "container"
	}
	line := fmt.Sprintf("%d %s at 0B", n, unit)
	var notes []string
	for _, s := range []struct{ state, word string }{
		{"exited", "stopped"},
		{"paused", "paused"},
		{"restarting", "restarting"},
		{"created", "created"},
		{"dead", "dead"},
	} {
		if c := sec.zeroStates[s.state]; c > 0 {
			notes = append(notes, fmt.Sprintf("%d %s", c, s.word))
		}
	}
	if len(notes) > 0 {
		line += " (" + strings.Join(notes, ", ") + ")"
	}
	return paintDim(line)
}

// dfCacheStats totals a -v build cache section: entries, their size, and the
// size of the entries no later build still shares.
func dfCacheStats(sec *dfSection) (entries int, size, reclaim float64) {
	sizeI := indexOf(sec.headers, "SIZE")
	sharedI := indexOf(sec.headers, "SHARED")
	entries = len(sec.rows)
	for _, row := range sec.rows {
		sz, _ := sizeBytes(dfRowCell(row, sizeI))
		size += sz
		if sharedI >= 0 && strings.EqualFold(dfRowCell(row, sharedI), "false") {
			reclaim += sz
		}
	}
	return
}

// dfCacheBlock is the whole -v build cache display: one dim line with the
// totals, unless --all asked for the rows behind them.
func dfCacheBlock(sec *dfSection) string {
	entries, size, reclaim := dfCacheStats(sec)
	if entries == 0 {
		return paintDim("build cache: empty") + "\n"
	}
	if !dfShowAll {
		return paintDim(fmt.Sprintf("build cache: %d entries, %s, %s reclaimable",
			entries, formatBytes(size), formatBytes(reclaim))) + "\n"
	}
	var b strings.Builder
	b.WriteString(dfRule(sec.title))
	b.WriteByte('\n')
	headers, rows := dfFitTable(sec.headers, sec.rows, dfTermWidth())
	b.WriteString(renderDFTable(headers, rows, dfAligns(headers, rows)))
	return b.String()
}

func formatSystemDF(w io.Writer, text string) {
	sections := parseSystemDF(text)
	if len(sections) == 0 {
		io.WriteString(w, text)
		return
	}
	summary := dfSummary(sections)
	if dfExactSummary != "" {
		summary = dfExactSummary
	}
	for i := range sections {
		transformDFSection(&sections[i])
	}
	if sections[0].title == "" && len(sections[0].rows) == 0 && !sections[0].cacheEmpty {
		io.WriteString(w, text)
		return
	}
	width := dfTermWidth()
	var blocks []string
	for i := range sections {
		sec := &sections[i]
		if sec.title == "Build cache" {
			blocks = append(blocks, dfCacheBlock(sec))
			continue
		}
		if len(sec.rows) == 0 {
			if sec.zeroContainers > 0 {
				blocks = append(blocks, dfZeroContainersLine(sec)+"\n")
			}
			continue
		}
		var b strings.Builder
		if sec.title != "" {
			b.WriteString(dfRule(sec.title))
			b.WriteByte('\n')
		}
		headers, rows := dfFitTable(sec.headers, sec.rows, width)
		b.WriteString(renderDFTable(headers, rows, dfAligns(headers, rows)))
		if sec.zeroContainers > 0 {
			b.WriteString(dfZeroContainersLine(sec) + "\n")
		}
		blocks = append(blocks, b.String())
		if sec.cacheEmpty {
			blocks = append(blocks, paintDim("build cache: empty")+"\n")
		}
	}
	var buf bytes.Buffer
	buf.WriteString(paintDim(dfWrapSummary(summary, width)))
	buf.WriteString("\n")
	for _, b := range blocks {
		buf.WriteByte('\n')
		buf.WriteString(b)
	}
	io.WriteString(w, buf.String())
}
