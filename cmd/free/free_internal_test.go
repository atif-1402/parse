// Tests for free: which forms are claimed, that the column grid free prints
// on survives whatever parse did to the numbers, and that the numbers carry a
// unit and a machine that is filling up says so in color.
package free

import (
	"bufio"
	"fmt"
	"strings"
	"testing"

	"github.com/atif-1402/parse/internal/tool"
)

// The tint is written as markers rather than escapes so a failure can be read.
func init() {
	tool.Paint = func(color, s string) string {
		if color == "" || s == "" {
			return s
		}
		return "<" + color + ">" + s + "</>"
	}
}

func lines(l ...string) string { return strings.Join(l, "\n") + "\n" }

// stripTags undoes the Paint override above.
func stripTags(s string) string {
	for _, c := range []string{red, yellow} {
		s = strings.ReplaceAll(s, "<"+c+">", "")
	}
	return strings.ReplaceAll(s, "</>", "")
}

// A form per option that changes the table, taken from a real free.
var tableForms = []struct {
	name string
	text string
}{
	{"free", `               total        used        free      shared  buff/cache   available
Mem:         8004464     5661088      454460      464540     2989260     2343376
Swap:       16009028      819024    15190004
`},
	{"free -h", `               total        used        free      shared  buff/cache   available
Mem:           7.6Gi       5.4Gi       443Mi       453Mi       2.9Gi       2.2Gi
Swap:           15Gi       799Mi        14Gi
`},
	{"free -w", `               total        used        free      shared     buffers       cache   available
Mem:         8004464     5661088      454460      464540          24     2989236     2343376
Swap:       16009028      819024    15190004
`},
	{"free -t", `               total        used        free      shared  buff/cache   available
Mem:         8004464     5661072      454460      464540     2989260     2343392
Swap:       16009028      819024    15190004
Total:      24013492     6480096    15644464
`},
	{"free -l", `               total        used        free      shared  buff/cache   available
Mem:         8004464     5661072      454460      464540     2989260     2343392
Low:         8004464     7550004      454460
High:              0           0           0
Swap:       16009028      819024    15190004
`},
	// The last value here is wider than the column it sits in, and free prints
	// it with a space rather than hard against the number before it.
	{"free -v", `               total        used        free      shared  buff/cache   available
Mem:         8004464     5663480      452052      464540     2989260     2340984
Swap:       16009028      819024    15190004
Comm:       20011260    27968576 18014398501524668
`},
	{"free -h -w -t", `               total        used        free      shared     buffers       cache   available
Mem:           7.6Gi       5.4Gi       437Mi       449Mi        24Ki       2.9Gi       2.2Gi
Swap:           15Gi       797Mi        14Gi
Total:          22Gi       6.2Gi        14Gi
`},
}

func TestDetectClaimsEveryFormThatPrintsTheTable(t *testing.T) {
	for _, c := range tableForms {
		if got := Detect(c.text); got != "free" {
			t.Errorf("Detect(%s) = %q, want free", c.name, got)
		}
	}
}

func TestDetectRefusesTheSingleLineFormAndForeignText(t *testing.T) {
	for name, text := range map[string]string{
		"free -L":    "SwapUse      816644 CachUse     2993844  MemUse     5631420 MemFree      472552\n",
		"free -L -h": "SwapUse       797Mi CachUse       2.9Gi  MemUse       5.4Gi MemFree       461Mi\n",
		"du -sh":     "229M\tproject/\n",
		"ls -l":      "-rw-r--r-- 1 atif atif 4096 file\n",
		"go.mod":     "module github.com/atif-1402/parse\n\ngo 1.24\n",
		"prose":      "The total used and free are what free prints, available too.\n",
		"empty":      "",
	} {
		if got := Detect(text); got != "" {
			t.Errorf("Detect(%s) = %q, want it claimed by nobody", name, got)
		}
	}
}

// TestPrintRowReproducesTheLinesFreePrints is the layout guarantee: put the
// cells free printed back through the printer and nothing moves. Every form
// above goes through it, so a later change to the grid fails here rather than
// quietly shifting a column under a reader's eye.
func TestPrintRowReproducesTheLinesFreePrints(t *testing.T) {
	for _, c := range tableForms {
		for _, line := range strings.Split(strings.TrimRight(c.text, "\n"), "\n") {
			label, cells := "", strings.Fields(line)
			if line[0] != ' ' {
				label = strings.TrimSuffix(cells[0], ":")
				cells = cells[1:]
			}
			var b strings.Builder
			w := bufio.NewWriter(&b)
			printRow(w, label, cells, "")
			w.Flush()
			if got := b.String(); got != line {
				t.Errorf("%s\n got: %q\nwant: %q", c.name, got, line)
			}
		}
	}
}

func TestFormatPutsAUnitOnTheBareNumbers(t *testing.T) {
	want := lines(
		`               total        used        free      shared  buff/cache   available`,
		"Mem:"+strings.Repeat(" ", 12)+"7.6G"+strings.Repeat(" ", 8)+"5.4G"+
			strings.Repeat(" ", 8)+"443M"+strings.Repeat(" ", 8)+"453M"+
			strings.Repeat(" ", 8)+"2.9G"+strings.Repeat(" ", 8)+"2.2G",
		"Swap:"+strings.Repeat(" ", 12)+"15G"+strings.Repeat(" ", 8)+"799M"+
			strings.Repeat(" ", 9)+"14G",
	)
	var b strings.Builder
	Format(&b, tableForms[0].text)
	if got := b.String(); got != want {
		t.Errorf("Format(free)\n got: %q\nwant: %q", got, want)
	}
}

// free's own human form already carries its unit: there is nothing to add, and
// the only change allowed is the tint — which on a healthy machine is nothing
// at all.
func TestFormatLeavesFreeHumanOutputByteIdentical(t *testing.T) {
	var b strings.Builder
	Format(&b, tableForms[1].text)
	if got := b.String(); got != tableForms[1].text {
		t.Errorf("Format(free -h)\n got: %q\nwant: %q", got, tableForms[1].text)
	}
}

// formatPlain runs Format with the tint switched off, so a colored run has an
// exact answer to be the same as.
func formatPlain(text string) string {
	tool.Paint = func(color, s string) string { return s }
	defer func() {
		tool.Paint = func(color, s string) string {
			if color == "" || s == "" {
				return s
			}
			return "<" + color + ">" + s + "</>"
		}
	}()
	var b strings.Builder
	Format(&b, text)
	return b.String()
}

func TestFormatTintsAMachineWhoseMemoryIsGoing(t *testing.T) {
	header := `               total        used        free      shared  buff/cache   available`
	for _, c := range []struct {
		used int
		want string
	}{
		{500000, ""},     // half: nothing to say
		{799999, ""},     // just under
		{800000, yellow}, // 80%
		{900000, red},    // 90%
		{1000000, red},   // the lot
	} {
		input := lines(header, fmt.Sprintf("Mem:     %10d %10d %10d", 1000000, c.used, 1000000-c.used))
		var b strings.Builder
		Format(&b, input)

		// The tint may wrap a value and nothing else: stripped of its colors
		// the line has to be what parse prints with no tint at all.
		if plain := stripTags(b.String()); plain != formatPlain(input) {
			t.Errorf("used %d: the tint moved a column:\n got: %q\nwant: %q", c.used, plain, formatPlain(input))
		}
		if c.want == "" {
			if strings.Contains(b.String(), "<") {
				t.Errorf("used %d: tinted %q, want the numbers plain", c.used, b.String())
			}
			continue
		}
		want := fmt.Sprintf("<%s>%s</>", c.want, human(fmt.Sprint(c.used)))
		if !strings.Contains(b.String(), want) {
			t.Errorf("used %d: no %s in %q", c.used, want, b.String())
		}
	}
}

// A row whose own total is zero has no percentage to show: free prints zeros
// for the tables that are not there.
func TestFormatSaysNothingAboutAnEmptyTotal(t *testing.T) {
	var b strings.Builder
	Format(&b, lines(
		`               total        used        free`,
		`High:              0           0           0`,
	))
	if strings.Contains(b.String(), "<") {
		t.Errorf("tinted an empty row: %q", b.String())
	}
}

func TestFormatLeavesTextThatIsNotFreeAlone(t *testing.T) {
	for name, text := range map[string]string{
		"go.mod":     "module github.com/atif-1402/parse\n\ngo 1.24\n",
		"du":         "229M\tproject/\n",
		"prose":      "Nothing here is a table.\n",
		"no newline": `               total        used        free      shared  buff/cache   available`,
	} {
		var b strings.Builder
		Format(&b, text)
		if got := b.String(); got != text {
			t.Errorf("Format(%s)\n got: %q\nwant: %q", name, got, text)
		}
	}
}

func TestScriptFormAndNeedsTerminal(t *testing.T) {
	for _, c := range []struct {
		args     []string
		script   bool
		terminal bool
	}{
		{nil, false, false},
		{[]string{"-h"}, false, false},
		{[]string{"-w", "-t"}, false, false},
		{[]string{"-l", "-v"}, false, false},
		{[]string{"-b"}, true, false},
		{[]string{"-m"}, true, false},
		{[]string{"--bytes"}, true, false},
		{[]string{"--mega"}, true, false},
		{[]string{"--si"}, true, false},
		{[]string{"-s", "1"}, true, true},
		{[]string{"-s=1"}, true, true},
		{[]string{"--seconds=1"}, true, true},
		{[]string{"-c", "3"}, true, true},
	} {
		if got := scriptForm(c.args); got != c.script {
			t.Errorf("scriptForm(%v) = %v, want %v", c.args, got, c.script)
		}
		if got := NeedsTerminal(c.args); got != c.terminal {
			t.Errorf("NeedsTerminal(%v) = %v, want %v", c.args, got, c.terminal)
		}
	}
}

func TestHumanMatchesFreeHumanForm(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"8004464", "7.6G"}, // what free -h prints as 7.6Gi
		{"5661088", "5.4G"}, // 5.4Gi
		{"454460", "443M"},  // 443Mi
		{"16009028", "15G"}, // 15Gi, truncated rather than rounded up
		{"1048575", "1G"},   // just short of a gibibyte: one unit up, not "1024M"
		{"1048576", "1G"},   // exactly one
		{"1023", "1023K"},   // below every larger unit
		{"0", "0K"},         // an empty table stays empty
		{"9", "9K"},         // one decimal below ten
	} {
		if got := human(c.in); got != c.want {
			t.Errorf("human(%s) = %q, want %q", c.in, got, c.want)
		}
	}
}
