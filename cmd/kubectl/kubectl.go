// Package kubectl formats the two kubectl reports that are actually hard to
// read: the resource table `get` prints, where the one column that answers the
// question (STATUS) sits in the middle of a wide row, and the key/value block
// `describe` prints, which spreads the answer over eighty lines and buries it
// under two container hashes nobody can use.
//
// Everything else kubectl prints is either already clear or meant for a program,
// and passes through untouched: `logs`, `top`, `events` and `cluster-info` read
// fine as they are, the json, yaml, name, template and jsonpath printers are
// machine formats, and `get --watch` never ends so it needs the terminal.
package kubectl

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/atif-1402/parse/internal/tool"
)

// Colors and the table cell come from the shared plumbing. They are aliased
// here so the formatting code below reads like ordinary parse code.
const (
	bold   = tool.Bold
	dim    = tool.Dim
	red    = tool.Red
	green  = tool.Green
	yellow = tool.Yellow
	cyan   = tool.Cyan
)

// Cell is one table value with an optional color name.
type Cell = tool.Cell

// kubectlCommands are the only two subcommands parse formats. Everything else
// runs untouched, which covers the mutating verbs (apply, delete, scale, ...),
// the ones that already read clearly (logs, top, events, cluster-info,
// api-resources, config view) and the ones that need a terminal (--follow,
// --watch).
var kubectlCommands = map[string]bool{
	"get":      true, // a wide table with STATUS buried in the middle
	"describe": true, // eighty lines of key/value with the answer scattered through it
}

// machineModes lists the -o modes that are read by a program rather than a
// person. This is kubectl's own list rather than tool.IsMachineOutput, which is
// the systemd set: the two tools barely overlap. `wide` is deliberately absent,
// because it is the same table with more columns and the formatter works from
// whatever header it is handed.
var machineModes = map[string]bool{
	"json": true, "yaml": true, "kyaml": true, "name": true,
	"go-template": true, "go-template-file": true,
	"template": true, "templatefile": true,
	"jsonpath": true, "jsonpath-as-json": true, "jsonpath-file": true,
	"custom-columns": true, "custom-columns-file": true,
}

// valueFlags are the kubectl flags that take a separate value. A word after one
// of these is never the subcommand, because `kubectl -n prod get pods` puts
// "prod" exactly where a subcommand would go.
var valueFlags = map[string]bool{
	"-n": true, "--namespace": true,
	"-o": true, "--output": true,
	"-l": true, "--selector": true, "--field-selector": true,
	"--context": true, "--cluster": true, "--kubeconfig": true,
	"--sort-by": true, "--template": true,
	"-f": true, "--filename": true, "-k": true, "--kustomize": true,
	"--as": true, "--as-group": true, "--user": true,
	"--server": true, "-s": true, "--token": true,
	"--request-timeout": true, "--cache-dir": true,
	"--chunk-size": true, "--limit": true, "--password": true,
}

// Run implements `parse kubectl <args>`: run kubectl, format its output, and
// return kubectl's exit code.
func Run(args []string) int {
	sub := kubectlSubcommand(args)
	if !kubectlCommands[sub] || !formattable(args) {
		return tool.Passthrough("kubectl", args)
	}
	data, code, started := tool.Capture("kubectl", args)
	if !started {
		return code
	}
	out := bufio.NewWriter(os.Stdout)
	Format(out, sub, tool.StripANSI(data))
	out.Flush()
	return code
}

// kubectlSubcommand returns the first argument that is not a flag, skipping the
// values of flags that take one.
func kubectlSubcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			return a
		}
		if valueFlags[a] {
			i++
		}
	}
	return ""
}

// NeedsTerminal reports whether this invocation needs the terminal untouched,
// which is what keeps the pager out of the way. See cmd.NeedsTerminal.
func NeedsTerminal(args []string) bool {
	switch kubectlSubcommand(args) {
	case "exec", "attach":
		// These hand the terminal to a shell inside a pod. The keyboard, and
		// the raw mode it needs in order to work, are the program here rather
		// than decoration around it.
		return true
	case "logs":
		return followsLog(args)
	}
	return watches(args)
}

// followsLog reports whether `kubectl logs` was asked to keep streaming.
func followsLog(args []string) bool {
	for _, a := range args {
		if a == "-f" || a == "--follow" {
			return true
		}
	}
	return false
}

// watches reports whether a watch was asked for. A watch prints changes as they
// happen and never ends by itself, so a pager in front of it would trap Ctrl-C
// in the pager.
func watches(args []string) bool {
	for _, a := range args {
		switch a {
		case "-w", "--watch", "--watch-only":
			return true
		}
	}
	return false
}

// formattable reports whether these particular flags still leave output that is
// meant for a person to read.
func formattable(args []string) bool {
	// -o custom-columns=NAME:.metadata.name arrives as the whole string, so the
	// mode has to be cut off before it can be looked up.
	mode := tool.OutputMode(args)
	if i := strings.IndexAny(mode, "=,;"); i >= 0 {
		mode = mode[:i]
	}
	if machineModes[mode] || tool.IsMachineOutput(mode) {
		return false
	}
	// A template's result is whatever the template said, not a table.
	if tool.HasLongFlag(args, "--template") {
		return false
	}
	// Without a header there is nothing to align to.
	if tool.HasLongFlag(args, "--no-headers") {
		return false
	}
	// A watch never ends. Handing it to the terminal is what lets the user stop
	// it, the same call `journalctl -f` gets.
	return !watches(args)
}

// ---------------------------------------------------------------------------
// Detection
// ---------------------------------------------------------------------------

// reHeader matches a kubectl get header: two or more runs of capitals, digits
// and punctuation, separated by spaces.
var reHeader = regexp.MustCompile(`^[A-Z][A-Z0-9:/()%.-]*( +[A-Z][A-Z0-9:/()%.-]*)+$`)

// getColumns are the column names kubectl's tables use. Requiring one of them
// is what keeps this from claiming some other tool's capitalised header.
var getColumns = map[string]bool{
	"READY": true, "STATUS": true, "AGE": true, "RESTARTS": true,
	"UP-TO-DATE": true, "AVAILABLE": true, "CLUSTER-IP": true,
	"EXTERNAL-IP": true, "PORT(S)": true, "VERSION": true, "ROLES": true,
	"MINUTES": true, "SCHEDULE": true, "COMPLETIONS": true, "DURATION": true,
	"CONTAINERS": true, "IMAGES": true, "SELECTOR": true, "REVISION": true,
	"CAPACITY": true, "ACCESSIBLE": true, "KIND": true, "RECLAIM": true,
	"STORAGECLASS": true, "PROVISIONER": true, "RECLAIM POLICY": true,
}

// Detect reports whether text is kubectl output, and which report it is.
func Detect(text string) string {
	lines := strings.Split(text, "\n")
	first := tool.FirstNonEmpty(lines)
	switch {
	case looksLikeDescribe(first, lines):
		return "describe"
	case looksLikeGet(lines):
		return "get"
	}
	return ""
}

// looksLikeDescribe: every resource kubectl describes opens with its name, and
// the block that follows is all "Key: value". Requiring several of them keeps a
// stray line that happens to start with "Name:" from claiming the stream.
func looksLikeDescribe(first string, lines []string) bool {
	if !strings.HasPrefix(first, "Name:") {
		return false
	}
	keys := 0
	for _, l := range lines {
		if reDescribeKey.MatchString(l) {
			keys++
			if keys >= 3 {
				return true
			}
		}
	}
	return false
}

// looksLikeGet: a header row of capitals that opens with NAME and contains a
// column only kubectl prints.
//
// The NAME-first rule is what separates this from lsblk, whose header is also
// capitals opening with NAME; the column whitelist is what keeps TYPE out of
// that list, because lsblk has a TYPE column too.
func looksLikeGet(lines []string) bool {
	headers := 0
	first := true
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if !reHeader.MatchString(l) {
			continue
		}
		fields := strings.Fields(l)
		if fields[0] != "NAME" {
			continue
		}
		headers++
		if first {
			first = false
			kubernetes := false
			for _, f := range fields[1:] {
				if getColumns[f] {
					kubernetes = true
				}
			}
			if !kubernetes {
				return false
			}
		}
	}
	// `kubectl get all` prints one table per resource kind, each with its own
	// header. There is no single set of columns to lay out, so it is left alone.
	return headers == 1
}

// ---------------------------------------------------------------------------
// Formatting
// ---------------------------------------------------------------------------

// Format writes text as the report sub describes. Text that does not have the
// shape sub promises is written through byte for byte.
func Format(w io.Writer, sub, text string) {
	switch sub {
	case "get":
		if !formatGet(w, text) {
			io.WriteString(w, text)
		}
	case "describe":
		if !formatDescribe(w, text) {
			io.WriteString(w, text)
		}
	default:
		io.WriteString(w, text)
	}
}

// formatGet lays the resource table out in aligned columns with the status
// tinted. It returns false when the text is not a table after all.
func formatGet(w io.Writer, text string) bool {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	header := -1
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			header = i
			break
		}
	}
	if header < 0 || !reHeader.MatchString(lines[header]) {
		return false
	}
	names, offsets := parseHeader(lines[header])
	if len(names) < 2 {
		return false
	}

	var rows [][]Cell
	for _, l := range lines[header+1:] {
		if strings.TrimSpace(l) == "" {
			continue
		}
		cells := sliceRow(l, offsets)
		if len(cells) != len(names) || !aligned(l, offsets) {
			// The row does not line up with the header, so the offsets cannot be
			// trusted. Writing the original text is the safe answer.
			return false
		}
		row := make([]Cell, len(cells))
		for i, v := range cells {
			row[i] = Cell{Text: v, Color: cellColor(names[i], v)}
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		// A header with nothing under it is kubectl saying there is nothing
		// here, which needs no layout.
		return false
	}
	tool.PrintTable(w, names, rows)
	return true
}

// parseHeader splits a header row into its column names and the offset each one
// starts at. The offsets are what the rows are sliced with, because a cell can
// contain spaces (`kubectl get events` has a MESSAGE column) and cannot be found
// by splitting on whitespace.
//
// Columns are separated by two or more spaces, not one. kubectl lays its tables
// out with tabwriter padding of three, so a real gap between columns is always
// at least that, while the space inside a two-word column name is exactly one.
// That is the only way to tell `NOMINATED NODE` and `READINESS GATES` in a
// `-o wide` header apart from two separate columns.
func parseHeader(line string) ([]string, []int) {
	var names []string
	var offsets []int
	for i := 0; i < len(line); {
		for i < len(line) && line[i] == ' ' {
			i++
		}
		if i >= len(line) {
			break
		}
		start := i
		for i < len(line) && line[i] != ' ' {
			i++
		}
		// Take the rest of the name, one word at a time, while only a single
		// space separates it from the next word.
		for i+1 < len(line) && line[i] == ' ' && line[i+1] != ' ' {
			i++
			for i < len(line) && line[i] != ' ' {
				i++
			}
		}
		names = append(names, line[start:i])
		offsets = append(offsets, start)
	}
	return names, offsets
}

// sliceRow cuts one row into cells at the offsets taken from the header.
// kubectl pads its cells to line up with the header, so the offsets from the
// header row address the data rows as well.
func sliceRow(line string, offsets []int) []string {
	cells := make([]string, len(offsets))
	for i, off := range offsets {
		if off >= len(line) {
			continue
		}
		end := len(line)
		if i+1 < len(offsets) && offsets[i+1] < end {
			end = offsets[i+1]
		}
		if end <= off {
			continue
		}
		cells[i] = strings.TrimSpace(line[off:end])
	}
	return cells
}

// aligned reports whether a data row starts its cells where the header does.
// kubectl pads every cell out to the column width, so the space in front of each
// column is where the header says it is. A row that disagrees has been reflowed
// by something else, and slicing it on the header's offsets would cut words in
// half, which is worse than not formatting at all.
func aligned(line string, offsets []int) bool {
	for i, off := range offsets {
		if i == 0 {
			// The first column always starts at the left margin.
			if off != 0 || (len(line) > 0 && line[0] == ' ') {
				return false
			}
			continue
		}
		if off >= len(line) {
			// The row stops short of this column, so there is nothing to check.
			continue
		}
		if line[off-1] != ' ' {
			return false
		}
	}
	return true
}

// cellColor tints one cell of a resource table according to its column.
func cellColor(column, value string) string {
	switch column {
	case "NAME":
		return cyan
	case "STATUS":
		return statusColor(value)
	case "READY":
		return readyColor(value)
	case "RESTARTS":
		if value != "" && value != "0" {
			return yellow
		}
		return dim
	case "AGE", "CREATED", "START TIME", "LAST SEEN", "TIMESTAMP":
		return dim
	case "IP":
		return cyan
	case "VERSION":
		return dim
	}
	return ""
}

// statusColor tints a status word. The list is kubectl's own vocabulary for pod
// phases, node conditions and container states.
func statusColor(s string) string {
	switch s {
	case "Running", "Active", "Bound", "Available", "Ready", "Succeeded",
		"Completed", "Healthy", "Established", "True", "Yes":
		return green
	case "Pending", "ContainerCreating", "Terminating", "Progressing",
		"Initializing", "Unknown", "Warning", "Unschedulable":
		return yellow
	case "Error", "Failed", "False", "CrashLoopBackOff", "ImagePullBackOff",
		"ErrImagePull", "Evicted", "OOMKilled", "NotReady", "Unhealthy",
		"BackOff", "DeadlineExceeded":
		return red
	}
	return ""
}

// readyColor tints a "1/2" ready count: all of them is fine, none of them is not.
func readyColor(s string) string {
	ready, total, ok := strings.Cut(s, "/")
	if !ok || total == "" {
		return ""
	}
	switch {
	case ready == total:
		return green
	case ready == "0" || total == "0":
		return red
	}
	return yellow
}

// reDescribeKey matches a "Key:" at the start of a describe line.
//
// The key has to be capitalised words joined by single spaces or hyphens, which
// is what kubectl's own keys look like ("Start Time:", "QoS Class:",
// "TokenExpirationSeconds:"). It is also what keeps a wrapped event message
// such as "fit failure on node (node-6ta5): Node didn't have..." from being read
// as a key, because that starts lower case and carries brackets.
var reDescribeKey = regexp.MustCompile(`^(\s*)([A-Z][A-Za-z0-9]*(?:[ -][A-Za-z0-9]+)*?:)(\s*)(\S.*)?$`)

// reHash spots the two lines of every describe block that are pure noise: the
// container ID and the image digest.
var reHash = regexp.MustCompile(`^(containerd|docker|cri-o)://|@sha256:`)

// formatDescribe dims the keys and tints the values that say what state
// something is in, so Status, Ready, State and the Conditions no longer have to
// be hunted for in eighty lines.
func formatDescribe(w io.Writer, text string) bool {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) == 0 || !strings.HasPrefix(tool.FirstNonEmpty(lines), "Name:") {
		return false
	}
	out := bufio.NewWriter(w)
	defer out.Flush()

	// section tracks which of describe's two little tables we are inside, because
	// the column worth tinting sits in a different place in each.
	section := ""
	for _, l := range lines {
		if s, ok := describeSection(l); ok {
			section = s
		} else if isSectionHeading(l) {
			section = ""
		}
		fmt.Fprintln(out, describeLine(l, section))
	}
	return true
}

// describeSection reports which of describe's tables a heading opens, and which
// of its columns holds the state: Conditions lists the condition name first and
// its True/False second, Events lists the Normal/Warning first.
func describeSection(line string) (string, bool) {
	switch strings.TrimSpace(line) {
	case "Conditions:":
		return "conditions", true
	case "Events:":
		return "events", true
	}
	return "", false
}

// isSectionHeading reports whether line is a key at the left margin, which ends
// whatever table was open.
func isSectionHeading(line string) bool {
	return strings.HasPrefix(line, " ") == false && reDescribeKey.MatchString(line)
}

// describeLine formats one line of a describe block.
func describeLine(line, section string) string {
	m := reDescribeKey.FindStringSubmatch(line)
	if m == nil {
		return describeRow(line, section)
	}
	indent, key, gap, value := m[1], m[2], m[3], m[4]

	// The keys at the left margin are the section headings a reader scans for,
	// so they are the only ones made bold.
	keyColor := dim
	if indent == "" {
		keyColor = bold
	}
	var b strings.Builder
	b.WriteString(indent)
	b.WriteString(tool.PaintColor(keyColor, key))
	// A key with no value is a heading, and kubectl leaves it bare. Adding the
	// gap anyway would leave a trail of spaces at the end of the line.
	if value == "" {
		return b.String()
	}
	// The gap is dropped and re-made, because the value may be painted and a
	// painted gap would drag colour codes into the alignment.
	b.WriteString(strings.Repeat(" ", maxInt(1, len(gap))))
	b.WriteString(describeValue(value))
	return b.String()
}

// describeRow handles the lines inside describe that are not key/value: the
// wrapped continuations under Labels and Tolerations, and the little tables
// under Conditions and Events.
func describeRow(line, section string) string {
	if section == "" {
		return line
	}
	// The header ("Type  Reason  Age") and the dashes under it are not rows.
	if reHeader.MatchString(strings.TrimSpace(line)) || isDashes(line) {
		return line
	}
	column := 0
	if section == "conditions" {
		column = 1
	}
	return paintField(line, column)
}

// paintField tints the nth run of non-space characters on a line, leaving every
// space exactly where it was so the surrounding columns stay put.
func paintField(line string, column int) string {
	seen, i := 0, 0
	for i < len(line) {
		for i < len(line) && line[i] == ' ' {
			i++
		}
		if i >= len(line) {
			break
		}
		start := i
		for i < len(line) && line[i] != ' ' {
			i++
		}
		if seen == column {
			color := statusColor(line[start:i])
			if color == "" {
				return line
			}
			return line[:start] + tool.PaintColor(color, line[start:i]) + line[i:]
		}
		seen++
	}
	return line
}

// isDashes reports whether line is the "----  ----" separator kubectl prints
// under a table header.
func isDashes(line string) bool {
	t := strings.TrimSpace(line)
	if t == "" {
		return false
	}
	return strings.Trim(t, "- ") == ""
}

// describeValue tints a value by what it says, rather than by which key it came
// from, because the same words appear under Status, State, Ready, Reason and
// the Conditions table.
func describeValue(v string) string {
	if v == "" {
		return ""
	}
	switch v {
	case "<none>", "<nil>":
		return tool.PaintColor(dim, v)
	case "Running", "True", "Active", "Bound", "Available", "Ready", "Yes",
		"Succeeded", "Completed", "Healthy", "Established":
		return tool.PaintColor(green, v)
	case "Pending", "Unknown", "Progressing", "ContainerCreating",
		"Terminating", "Warning", "Initializing":
		return tool.PaintColor(yellow, v)
	case "Failed", "Error", "False", "Critical", "Unhealthy", "NotReady",
		"CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "Evicted",
		"OOMKilled", "BackOff", "DeadlineExceeded", "Unschedulable":
		return tool.PaintColor(red, v)
	}
	if reHash.MatchString(v) {
		return tool.PaintColor(dim, v)
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
