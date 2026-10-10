package docker

import (
	"strings"
	"testing"
)

// dfRow renders one table (header + rows) the way docker pads it: every cell
// in a column starts at the same rune offset, with at least two spaces
// between columns so dfColumnStarts can find the seams. Padding is counted in
// runes because dfCells slices rows by rune.
func dfRow(header []string, rows [][]string) string {
	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = len([]rune(h)) + 2
	}
	for _, row := range rows {
		for i, c := range row {
			if n := len([]rune(c)) + 2; n > widths[i] {
				widths[i] = n
			}
		}
	}
	line := func(cells []string) string {
		var b strings.Builder
		for i, c := range cells {
			b.WriteString(c)
			if i < len(cells)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-len([]rune(c))))
			}
		}
		return b.String()
	}
	var b strings.Builder
	b.WriteString(line(header))
	b.WriteByte('\n')
	for _, row := range rows {
		b.WriteString(line(row))
		b.WriteByte('\n')
	}
	return b.String()
}

// The verbose fixture carries every shape the layout must handle: a dangling
// image, an unused image, a used one, a used volume, an unused volume, a
// running container, a stopped one with size, two empty containers, an
// all-zero SHARED SIZE column (so it hides), an all-zero LOCAL VOLUMES column
// (so it hides), and a build cache section with no rows.
func systemDFVerboseFixture() string {
	images := dfRow(
		[]string{"REPOSITORY", "TAG", "IMAGE ID", "CREATED", "SIZE", "SHARED SIZE", "UNIQUE SIZE", "CONTAINERS"},
		[][]string{
			{"<none>", "<none>", "deadbeef1234", "2 days ago", "76.9MB", "0B", "62.98MB", "0"},
			{"nginx", "latest", "f9ea18bfa4fa", "4 days ago", "243MB", "0B", "242.7MB", "5"},
			{"busybox", "1.36", "73aaf090f3d8", "3 years ago", "6.68MB", "0B", "6.679MB", "0"},
		},
	)
	containers := dfRow(
		[]string{"CONTAINER ID", "IMAGE", "COMMAND", "LOCAL VOLUMES", "SIZE", "CREATED", "STATUS", "NAMES"},
		[][]string{
			{"aaaabbbbcccc", "nginx", `"/docker-en…"`, "0", "8.19kB", "3 hours ago", "Up 3 hours (unhealthy)", "fx-run"},
			{"dddeeefff000", "alpine", `"sleep 10"`, "0", "2.5MB", "5 hours ago", "Exited (1) 4 hours ago", "fx-big"},
			{"111122223333", "alpine", `"sleep 1"`, "0", "0B", "5 hours ago", "Created", "fx-z1"},
			{"444455556666", "alpine", `"sleep 2"`, "0", "0B", "5 hours ago", "Exited (0) 1 hour ago", "fx-z2"},
		},
	)
	volumes := dfRow(
		[]string{"VOLUME NAME", "LINKS", "SIZE"},
		[][]string{
			{"fx-vol-used", "1", "52.43MB"},
			{"fx-vol-unused", "0", "0B"},
		},
	)
	cache := dfRow(
		[]string{"CACHE ID", "CACHE TYPE", "SIZE", "CREATED", "LAST USED", "USAGE", "SHARED"},
		nil,
	)
	return "Images space usage:\n\n" + images +
		"\nContainers space usage:\n\n" + containers +
		"\nLocal Volumes space usage:\n\n" + volumes +
		"\nBuild cache usage: 0B\n\n" + cache
}

func formatDF(t *testing.T, text string) string {
	t.Helper()
	var sb strings.Builder
	Format(&sb, "df", text)
	return sb.String()
}

// The size normalizer is the spine of every number this formatter prints:
// docker's decimal sizes come back in one style, at four significant digits,
// and anything it cannot read is left alone.
func TestNormalizeSize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"0B", "0B"},
		{"kB", "kB"},
		{"MB", "MB"},
		{"GB", "GB"},
		{"21.8kB", "21.8kB"},
		{"13.99MB", "13.99MB"},
		{"1.5GB", "1.5GB"},
		{"1.2GB", "1.2GB"},
		{"40.96kB", "40.96kB"},
		{"7.762kB", "7.762kB"},
		{"243MB", "243MB"},
		{"63MB", "63MB"},
		{"82.17MB (23%)", "82.17MB (23%)"},
		{"0B (0%)", "0B (0%)"},
		{"13900kB", "13.9MB"},
		{"1500000kB", "1.5GB"},
		{"500B", "500B"},
		{"134.66MB", "134.7MB"},
	}
	for _, c := range cases {
		if got := normalizeSize(c.in); got != c.want {
			t.Errorf("normalizeSize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A column is hidden only when every row in it is empty or zero — SHARED SIZE
// all 0B disappears, a column of false/true flags does not, and a half-zero
// column stays.
func TestDFHidesZeroColumns(t *testing.T) {
	headers := []string{"REPOSITORY", "SIZE", "SHARED SIZE", "UNIQUE SIZE", "CONTAINERS"}
	rows := [][]string{
		{"nginx", "243MB", "0B", "242.7MB", "5"},
		{"busybox", "6.68MB", "0B", "6.679MB", "0"},
	}
	gotHeaders, gotRows := hideZeroColumns(headers, rows)
	want := []string{"REPOSITORY", "SIZE", "UNIQUE SIZE", "CONTAINERS"}
	if strings.Join(gotHeaders, ",") != strings.Join(want, ",") {
		t.Errorf("headers = %v, want %v", gotHeaders, want)
	}
	for _, row := range gotRows {
		if len(row) != len(want) {
			t.Errorf("row %v does not match kept columns %v", row, want)
		}
	}

	// A column of non-numeric flags is never zero.
	headers = []string{"CACHE ID", "SHARED"}
	rows = [][]string{{"abc", "false"}, {"def", "true"}}
	gotHeaders, _ = hideZeroColumns(headers, rows)
	if len(gotHeaders) != 2 {
		t.Errorf("the SHARED flag column was hidden: %v", gotHeaders)
	}

	// An all-zero numeric column hides.
	headers = []string{"VOLUME NAME", "LOCAL VOLUMES"}
	rows = [][]string{{"vol1", "0"}, {"vol2", "0"}}
	gotHeaders, _ = hideZeroColumns(headers, rows)
	if len(gotHeaders) != 1 || gotHeaders[0] != "VOLUME NAME" {
		t.Errorf("all-zero LOCAL VOLUMES was not hidden: %v", gotHeaders)
	}
}

// Detection looks at the first non-empty line and nothing else — the same
// rule psHeader follows — so a df-shaped table appearing later in someone
// else's text is never claimed.
func TestSystemDFDetectsByFirstLine(t *testing.T) {
	plain := "TYPE            TOTAL     ACTIVE    SIZE      RECLAIMABLE\nImages          8         4         351.5MB   82.17MB (23%)\n"
	if got := Detect(plain); got != "df" {
		t.Errorf("plain df: Detect = %q, want df", got)
	}
	verbose := "Images space usage:\n\nREPOSITORY    TAG       IMAGE ID       SIZE\nnginx         latest    f9ea18bfa4fa   243MB\n"
	if got := Detect(verbose); got != "df" {
		t.Errorf("verbose df: Detect = %q, want df", got)
	}
	// The df header is real but is not the first non-empty line.
	notFirst := "free - used memory\nTYPE            TOTAL     ACTIVE    SIZE      RECLAIMABLE\n"
	if got := Detect(notFirst); got != "" {
		t.Errorf("df header below the first line: Detect = %q, want it claimed by nobody", got)
	}
	if got := Detect("hello world\n"); got != "" {
		t.Errorf("Detect(garbage) = %q, want \"\"", got)
	}
	if got := Detect(""); got != "" {
		t.Errorf("Detect(empty) = %q, want \"\"", got)
	}
}

// The verbose layout end to end: summary first with build cache included,
// tables sorted by size with state and name as tiebreaks, all-zero columns
// hidden, tags on the right rows, the status column split the way ps splits
// it, empty containers collapsed to one dim line, an empty build cache as
// one dim line, and a dim rule with a blank line before each section.
func TestSystemDFVerboseLayout(t *testing.T) {
	raw := formatDF(t, systemDFVerboseFixture())
	out := stripTags(raw)

	wantSummary := "~images 326.6MB · volumes 52.43MB · ~containers 2.508MB · ~build cache 0B · reclaimable 83.58MB"
	if !strings.HasPrefix(out, wantSummary+"\n") {
		t.Errorf("first line = %q, want %q", strings.SplitN(out, "\n", 2)[0], wantSummary)
	}

	if strings.Contains(out, "SHARED SIZE") {
		t.Error("SHARED SIZE was all 0B and should have been hidden")
	}
	if strings.Contains(out, "LOCAL VOLUMES") {
		t.Error("LOCAL VOLUMES was all 0 and should have been hidden")
	}

	imagesAt := strings.Index(out, "f9ea18bfa4fa")
	noneAt := strings.Index(out, "deadbeef1234")
	busyAt := strings.Index(out, "73aaf090f3d8")
	if imagesAt < 0 || noneAt < 0 || busyAt < 0 {
		t.Fatalf("an image row is missing:\n%s", raw)
	}
	if !(imagesAt < noneAt && noneAt < busyAt) {
		t.Errorf("images are not sorted by SIZE desc: nginx@%d <none>@%d busybox@%d", imagesAt, noneAt, busyAt)
	}

	if !strings.Contains(out, "dangling unused") {
		t.Errorf("the <none> image was not tagged dangling unused:\n%s", raw)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "f9ea18bfa4fa") && (strings.Contains(line, "dangling") || strings.Contains(line, "unused")) {
			t.Errorf("the used image was tagged: %q", line)
		}
		if strings.HasPrefix(strings.TrimSpace(line), "fx-vol-used") && strings.Contains(line, "unused") {
			t.Errorf("the linked volume was tagged unused: %q", line)
		}
		if strings.HasPrefix(strings.TrimSpace(line), "fx-vol-unused") && !strings.Contains(line, "unused") {
			t.Errorf("the unlinked volume was not tagged: %q", line)
		}
	}

	// The containers table answers "which containers take space": NAME,
	// STATE, SIZE. The ps columns are gone from this block (the Images
	// table keeps its own CREATED column).
	containersBlock := out
	if i := strings.Index(out, "── Containers"); i >= 0 {
		containersBlock = out[i:]
		if j := strings.Index(containersBlock, "── Local Volumes"); j >= 0 {
			containersBlock = containersBlock[:j]
		}
	}
	head := strings.Split(containersBlock, "\n")
	if len(head) < 2 || !strings.Contains(head[1], "NAME") || !strings.Contains(head[1], "STATE") || !strings.Contains(head[1], "SIZE") {
		t.Errorf("the containers table is not NAME/STATE/SIZE:\n%s", raw)
	}
	for _, gone := range []string{"CONTAINER ID", "COMMAND", "CREATED", "STATUS", "NAMES"} {
		if strings.Contains(containersBlock, gone) {
			t.Errorf("the containers table still shows %q:\n%s", gone, raw)
		}
	}
	bigAt := strings.Index(out, "fx-big")
	runAt := strings.Index(out, "fx-run")
	if bigAt < 0 || runAt < 0 || bigAt > runAt {
		t.Errorf("containers are not sorted by SIZE desc: big@%d run@%d", bigAt, runAt)
	}
	if !strings.Contains(out, "running 3h") {
		t.Errorf("the running status was not split like ps:\n%s", raw)
	}
	if !strings.Contains(out, "exited 1 4h ago") {
		t.Errorf("the stopped status was not split like ps:\n%s", raw)
	}
	if !strings.Contains(out, "2 containers at 0B (1 stopped, 1 created)") {
		t.Errorf("the empty containers did not collapse into one line:\n%s", raw)
	}

	if !strings.Contains(out, "\n\n── Images") {
		t.Errorf("the Images section has no blank line and dim rule:\n%q", out)
	}
	if !strings.Contains(out, "build cache: empty") {
		t.Errorf("the empty build cache has no dim line:\n%s", raw)
	}
	if strings.Contains(out, "CACHE ID") {
		t.Errorf("the empty build cache printed its table:\n%s", raw)
	}
}

// This machine's docker keeps every image tagged, so the dangling tag is
// proven against fake text: a <none> in either REPOSITORY or TAG marks the
// image dangling, and only an image with no containers gets unused.
func TestSystemDFDanglesNoneImages(t *testing.T) {
	verbose := "Images space usage:\n\n" +
		dfRow(
			[]string{"REPOSITORY", "TAG", "IMAGE ID", "CREATED", "SIZE", "SHARED SIZE", "UNIQUE SIZE", "CONTAINERS"},
			[][]string{
				{"<none>", "latest", "aaaabbbbcccc", "1 day ago", "10MB", "0B", "10MB", "2"},
				{"busybox", "<none>", "ddddeeeeffff", "1 day ago", "5MB", "0B", "5MB", "0"},
			},
		)
	out := stripTags(formatDF(t, verbose))
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(line, "aaaabbbbcccc"):
			if !strings.Contains(line, "dangling") || strings.Contains(line, "unused") {
				t.Errorf("the <none> repository image: line = %q, want dangling only", line)
			}
		case strings.Contains(line, "ddddeeeeffff"):
			if !strings.Contains(line, "dangling unused") {
				t.Errorf("the <none> tag image: line = %q, want dangling unused", line)
			}
		}
	}
}

// The plain summary table: one table, sorted by SIZE, an all-zero Build
// Cache row dropped in favour of the same dim line, and a percentage on
// every RECLAIMABLE cell — the cache row's own included. Text that does not
// parse — a bare header, or nothing df-like at all — comes back untouched.
func TestSystemDFPlainSummaryAndPassthrough(t *testing.T) {
	plain := "TYPE            TOTAL     ACTIVE    SIZE      RECLAIMABLE\n" +
		"Images          8         4         351.5MB   82.17MB (23%)\n" +
		"Containers      18        7         40.96kB   0B (0%)\n" +
		"Local Volumes   3         1         52.43MB   0B (0%)\n" +
		"Build Cache     0         0         0B        0B\n"
	out := stripTags(formatDF(t, plain))

	wantSummary := "images 351.5MB · volumes 52.43MB · containers 40.96kB · build cache 0B · reclaimable 82.17MB"
	if !strings.HasPrefix(out, wantSummary+"\n") {
		t.Errorf("first line = %q, want %q", strings.SplitN(out, "\n", 2)[0], wantSummary)
	}
	if strings.Contains(out, "Build Cache") {
		t.Errorf("the empty Build Cache row should have been dropped:\n%s", out)
	}
	if !strings.Contains(out, "build cache: empty") {
		t.Errorf("no dim line for the empty build cache:\n%s", out)
	}
	// The summary spells the words in lower case; the table rows below it
	// keep docker's capitalisation, so these three point at table rows only.
	imagesAt := strings.Index(out, "Images")
	volAt := strings.Index(out, "Local Volumes")
	ctrAt := strings.Index(out, "Containers")
	if imagesAt < 0 || volAt < 0 || ctrAt < 0 || imagesAt > volAt || volAt > ctrAt {
		t.Errorf("rows are not sorted by SIZE desc:\n%s", out)
	}

	headerOnly := "TYPE            TOTAL     ACTIVE    SIZE      RECLAIMABLE\n"
	if got := formatDF(t, headerOnly); got != headerOnly {
		t.Errorf("a bare header was rewritten:\n got: %q\nwant: %q", got, headerOnly)
	}
	garbage := "nothing df about this\n"
	if got := formatDF(t, garbage); got != garbage {
		t.Errorf("garbage was rewritten:\n got: %q\nwant: %q", got, garbage)
	}
}

// Docker prints a percentage on every RECLAIMABLE cell but the build cache
// row's. The cache row gets one too, computed from its own numbers.
func TestSystemDFPlainCachePercent(t *testing.T) {
	plain := "TYPE            TOTAL     ACTIVE    SIZE      RECLAIMABLE\n" +
		"Build Cache     2         0         60MB      10MB\n"
	out := stripTags(formatDF(t, plain))
	if !strings.Contains(out, "10MB (17%)") {
		t.Errorf("the build cache row has no percentage:\n%s", out)
	}
}

// One state, two views: the plain summary (which trusts docker's own
// RECLAIMABLE column) and the -v summary (which recomputes reclaimable from
// the tables) must agree, down to the byte of the summary line.
func TestSystemDFPlainAndVerboseSummariesAgree(t *testing.T) {
	plain := "TYPE            TOTAL     ACTIVE    SIZE      RECLAIMABLE\n" +
		"Images          2         1         120MB     20MB (17%)\n" +
		"Containers      2         1         3MB       0B (0%)\n" +
		"Local Volumes   2         1         7MB       2MB (29%)\n" +
		"Build Cache     2         0         60MB      10MB\n"
	verbose := "Images space usage:\n\n" +
		dfRow(
			[]string{"REPOSITORY", "TAG", "IMAGE ID", "CREATED", "SIZE", "SHARED SIZE", "UNIQUE SIZE", "CONTAINERS"},
			[][]string{
				{"busybox", "latest", "aaaaaaaaaaaa", "1 day ago", "100MB", "0B", "100MB", "3"},
				{"alpine", "latest", "bbbbbbbbbbbb", "1 day ago", "20MB", "0B", "20MB", "0"},
			},
		) +
		"\nContainers space usage:\n\n" +
		dfRow(
			[]string{"CONTAINER ID", "IMAGE", "COMMAND", "LOCAL VOLUMES", "SIZE", "CREATED", "STATUS", "NAMES"},
			[][]string{
				{"cccccccccccc", "busybox", `"sh"`, "0", "3MB", "1 hour ago", "Up 1 hour", "run-1"},
				{"dddddddddddd", "alpine", `"sh"`, "0", "0B", "1 hour ago", "Created", "empty-1"},
			},
		) +
		"\nLocal Volumes space usage:\n\n" +
		dfRow(
			[]string{"VOLUME NAME", "LINKS", "SIZE"},
			[][]string{
				{"vol-used", "1", "5MB"},
				{"vol-free", "0", "2MB"},
			},
		) +
		"\nBuild cache usage: 60MB\n\n" +
		dfRow(
			[]string{"CACHE ID", "CACHE TYPE", "SIZE", "CREATED", "LAST USED", "USAGE", "SHARED"},
			[][]string{
				{"eeeeeeeeeeee", "regular", "50MB", "1 hour ago", "1 hour ago", "2", "true"},
				{"ffffffffffff", "regular", "10MB", "1 hour ago", "1 hour ago", "1", "false"},
			},
		)

	plainOut := stripTags(formatDF(t, plain))
	verboseOut := stripTags(formatDF(t, verbose))
	plainSummary := strings.SplitN(plainOut, "\n", 2)[0]
	verboseSummary := strings.SplitN(verboseOut, "\n", 2)[0]
	want := "images 120MB · build cache 60MB · volumes 7MB · containers 3MB · reclaimable 32MB"
	wantVerbose := "~images 120MB · ~build cache 60MB · volumes 7MB · ~containers 3MB · reclaimable 32MB"
	if plainSummary != want {
		t.Errorf("plain summary = %q, want %q", plainSummary, want)
	}
	if verboseSummary != wantVerbose {
		t.Errorf("verbose summary = %q, want %q\n%s", verboseSummary, wantVerbose, verboseOut)
	}
	// The pipe marks its sums; underneath the ~s the numbers agree with the
	// plain table docker itself printed for the same state.
	if stripped := strings.ReplaceAll(verboseSummary, "~", ""); stripped != plainSummary {
		t.Errorf("the two views disagree beyond the ~ marks:\n plain: %q\n   -v: %q", plainSummary, stripped)
	}

	// The direct path can afford a second, plain run: its exact totals
	// replace the sums, ~ and all.
	SetDFExactSummary(plainSummary)
	defer SetDFExactSummary("")
	exact := strings.SplitN(stripTags(formatDF(t, verbose)), "\n", 2)[0]
	if exact != plainSummary {
		t.Errorf("direct-mode summary = %q, want the exact plain line %q", exact, plainSummary)
	}
}

// The same input must print the same table every time: size, then state,
// then name is a total order, never a map's whim.
func TestSystemDFIsDeterministic(t *testing.T) {
	first := formatDF(t, systemDFVerboseFixture())
	for i := 0; i < 2; i++ {
		if got := formatDF(t, systemDFVerboseFixture()); got != first {
			t.Fatalf("run %d differs from the first:\n--- first ---\n%s\n--- run %d ---\n%s", i+2, first, i+2, got)
		}
	}
}

// Equal sizes fall back to state — running first, then problems, then clean
// exits — and then to the name, so two healthy containers of the same size
// always list in name order.
func TestSystemDFSortTiebreaks(t *testing.T) {
	verbose := "Containers space usage:\n\n" +
		dfRow(
			[]string{"CONTAINER ID", "IMAGE", "COMMAND", "LOCAL VOLUMES", "SIZE", "CREATED", "STATUS", "NAMES"},
			[][]string{
				{"a1", "alpine", `"sh"`, "0", "2MB", "1 hour ago", "Exited (0) 1 hour ago", "c-clean"},
				{"a2", "alpine", `"sh"`, "0", "2MB", "1 hour ago", "Up 1 hour", "c-run-b"},
				{"a3", "alpine", `"sh"`, "0", "2MB", "1 hour ago", "Up 1 hour", "c-run-a"},
				{"a4", "alpine", `"sh"`, "0", "2MB", "1 hour ago", "Restarting (1) 1 second ago", "c-problem"},
			},
		)
	out := stripTags(formatDF(t, verbose))
	runA := strings.Index(out, "c-run-a")
	runB := strings.Index(out, "c-run-b")
	problem := strings.Index(out, "c-problem")
	clean := strings.Index(out, "c-clean")
	if runA < 0 || runB < 0 || problem < 0 || clean < 0 {
		t.Fatalf("a container row is missing:\n%s", out)
	}
	if !(runA < runB && runB < problem && problem < clean) {
		t.Errorf("tiebreak order is wrong: runA@%d runB@%d problem@%d clean@%d", runA, runB, problem, clean)
	}
}

// The collapsed line counts only exited as stopped; paused, restarting and
// created are their own words, so a reader never has to guess what the
// empties are doing.
func TestSystemDFZeroContainerLineCountsStates(t *testing.T) {
	verbose := "Containers space usage:\n\n" +
		dfRow(
			[]string{"CONTAINER ID", "IMAGE", "COMMAND", "LOCAL VOLUMES", "SIZE", "CREATED", "STATUS", "NAMES"},
			[][]string{
				{"z1", "alpine", `"sh"`, "0", "0B", "1 hour ago", "Exited (1) 1 hour ago", "z-fail"},
				{"z2", "alpine", `"sh"`, "0", "0B", "1 hour ago", "Exited (0) 1 hour ago", "z-clean"},
				{"z3", "alpine", `"sh"`, "0", "0B", "1 hour ago", "Up 5 minutes (Paused)", "z-paused"},
				{"z4", "alpine", `"sh"`, "0", "0B", "1 hour ago", "Restarting (1) 2 seconds ago", "z-restart"},
				{"z5", "alpine", `"sh"`, "0", "0B", "1 hour ago", "Created", "z-created"},
			},
		)
	out := stripTags(formatDF(t, verbose))
	if !strings.Contains(out, "5 containers at 0B (2 stopped, 1 paused, 1 restarting, 1 created)") {
		t.Errorf("the zero-container line miscounted the states:\n%s", out)
	}
}

// A GB-range volume sorts first and keeps its unit: 1.2GB prints as 1.2GB,
// not as 1200MB and not behind the megabyte rows.
func TestSystemDFGBVolumeSortsFirst(t *testing.T) {
	verbose := "Local Volumes space usage:\n\n" +
		dfRow(
			[]string{"VOLUME NAME", "LINKS", "SIZE"},
			[][]string{
				{"vol-mid", "1", "500MB"},
				{"vol-big", "1", "1.2GB"},
				{"vol-small", "1", "40MB"},
			},
		)
	out := stripTags(formatDF(t, verbose))
	if !strings.Contains(out, "1.2GB") {
		t.Errorf("the big volume did not print as 1.2GB:\n%s", out)
	}
	big := strings.Index(out, "vol-big")
	mid := strings.Index(out, "vol-mid")
	small := strings.Index(out, "vol-small")
	if big < 0 || mid < 0 || small < 0 || !(big < mid && mid < small) {
		t.Errorf("volumes are not sorted by size desc:\n%s", out)
	}
}

// The -v build cache is one dim line with its totals, not a table; --all
// asks for the rows behind that line.
func TestSystemDFBuildCacheCollapsesUnlessAll(t *testing.T) {
	verbose := "Build cache usage: 60MB\n\n" +
		dfRow(
			[]string{"CACHE ID", "CACHE TYPE", "SIZE", "CREATED", "LAST USED", "USAGE", "SHARED"},
			[][]string{
				{"cafe00000001", "regular", "50MB", "1 hour ago", "1 hour ago", "2", "true"},
				{"cafe00000002", "regular", "10MB", "1 hour ago", "1 hour ago", "1", "false"},
			},
		)
	out := stripTags(formatDF(t, verbose))
	if !strings.Contains(out, "build cache: 2 entries, 60MB, 10MB reclaimable") {
		t.Errorf("the build cache did not collapse to its line:\n%s", out)
	}
	if strings.Contains(out, "CACHE ID") {
		t.Errorf("the collapsed build cache still printed rows:\n%s", out)
	}

	SetDFShowAll(true)
	defer SetDFShowAll(false)
	out = stripTags(formatDF(t, verbose))
	if !strings.Contains(out, "cafe00000001") || !strings.Contains(out, "cafe00000002") {
		t.Errorf("--all did not show the cache rows:\n%s", out)
	}
	if strings.Contains(out, "build cache: 2 entries") {
		t.Errorf("--all kept the collapsed line as well:\n%s", out)
	}
}

// A narrow terminal gets a wrapped summary and rightmost columns dropped,
// never a table wider than the screen.
func TestSystemDFNarrowTerminal(t *testing.T) {
	t.Setenv("COLUMNS", "60")
	out := stripTags(formatDF(t, systemDFVerboseFixture()))
	for _, line := range strings.Split(out, "\n") {
		if displayLen(line) > 60 {
			t.Errorf("line is %d columns, wider than COLUMNS=60: %q", displayLen(line), line)
		}
	}
	// The widest table is the images one; its rightmost columns go first.
	// TAGS and CONTAINERS cannot fit 60 columns with everything before them.
	imagesBlock := out
	if i := strings.Index(out, "── Images"); i >= 0 {
		imagesBlock = out[i:]
		if j := strings.Index(imagesBlock, "── Containers"); j >= 0 {
			imagesBlock = imagesBlock[:j]
		}
	}
	for _, gone := range []string{"TAGS", "UNIQUE SIZE", "SHARED SIZE", "CONTAINERS"} {
		if strings.Contains(imagesBlock, gone) {
			t.Errorf("the images table kept %q at COLUMNS=60:\n%s", gone, out)
		}
	}
	if !strings.Contains(out, "\n  ~build cache 0B · reclaimable 83.58MB") {
		t.Errorf("the summary did not wrap with an indent:\n%s", out)
	}
}
