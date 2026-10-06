// Tests for the lsof helpers: the heading lsof always prints, the coloring of
// a row's note and cells, and the arguments that mean the output is not a
// table at all. The rows below are lsof's own words, copied as it printed
// them — trailing spaces and empty cells included.
package lsof

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/atif-1402/parse/internal/tool"
)

func init() {
	tool.Paint = func(color, s string) string {
		if color == "" || s == "" {
			return s
		}
		return "<" + color + ">" + s + "</>"
	}
}

// strip takes the test's color markers back out, so a formatted row can be
// held against the row it came from.
var strip = regexp.MustCompile(`<(?:bold|dim|red|green|yellow|cyan)>|</>`)

func plain(text string) string { return strip.ReplaceAllString(text, "") }

func format(text string) string {
	var buf bytes.Buffer
	Format(&buf, text)
	return buf.String()
}

const (
	plainHeading = "COMMAND      PID    TID TASKCMD   USER   FD      TYPE             DEVICE  SIZE/OFF    NODE NAME"
	plainRow     = "systemd      769                  atif  cwd   unknown                                      /proc/769/cwd (readlink: Permission denied)"
	netHeading   = "COMMAND     PID USER  FD   TYPE  DEVICE SIZE/OFF NODE NAME"
	netRow       = "chromium  62754 atif 204u  IPv4  350953      0t0  UDP mdns.mcast.net:mdns "
	netStateRow  = "chromium  62799 atif  19u  IPv6 1838988      0t0  UDP anom:36726->[2600:1901:0:47fc::]:https (ESTABLISHED)"
	procHeading  = "COMMAND    PID USER  FD   TYPE             DEVICE SIZE/OFF    NODE NAME"
	memRow       = "bash    551284 atif mem    REG               0,28  1195144    7361 /usr/bin/bash (path dev=0,30)"
	emptySizeRow = "bash    551284 atif txt    REG               0,30             7361 /usr/bin/bash (path dev=0,30)"
	linksHeading = "COMMAND      PID USER  FD   TYPE             DEVICE  SIZE/OFF NLINK    NODE NAME"
	deletedRow   = "dbus-brok    784 atif   9u   REG                0,1   2097152     0      25 /memfd:dbus-broker-log (deleted)"
)

func TestDetectClaimsEveryTableHeading(t *testing.T) {
	for _, heading := range []string{
		plainHeading, netHeading, procHeading, linksHeading,
		"COMMAND     PID USER   FD      TYPE             DEVICE  SIZE/OFF    NODE NAME",
	} {
		if got := Detect(heading + "\n"); got != "lsof" {
			t.Errorf("Detect(heading) = %q, want lsof for %q", got, heading)
		}
	}
}

func TestDetectLeavesScriptsAndForeignTextAlone(t *testing.T) {
	for name, text := range map[string]string{
		"field mode": "p\n769\nf\ncwd\nn/proc/769/cwd\n",
		"terse pids": "769\n784\n",
		"free":       "              total        used        free      shared  buff/cache   available\n",
		"empty":      "\n\n",
	} {
		if got := Detect(text); got != "" {
			t.Errorf("Detect(%s) = %q, want \"\"", name, got)
		}
	}
}

func TestFormatPrintsEveryByteBack(t *testing.T) {
	text := strings.Join([]string{
		plainHeading, plainRow, netHeading, netRow, netStateRow, linksHeading, deletedRow,
	}, "\n") + "\n"
	if got := format(text); plain(got) != text {
		t.Errorf("Format moved a byte:\ngot  %q\nwant %q", plain(got), text)
	}
}

func TestFormatBoldensTheHeadingInPlace(t *testing.T) {
	got := format(netHeading)
	if plain(got) != netHeading {
		t.Fatalf("heading moved: %q", plain(got))
	}
	if !strings.Contains(got, "<bold>COMMAND</>") {
		t.Errorf("heading not bolded: %q", got)
	}
	if !strings.Contains(got, "<bold>NAME</>") {
		t.Errorf("last column not bolded: %q", got)
	}
}

func TestFormatColorsTheNoteAtTheEndOfARow(t *testing.T) {
	for _, tc := range []struct{ row, want string }{
		{netStateRow, "<green>(ESTABLISHED)</>"},
		{"x 1 a 0u IPv4 1 0t0 TCP :1 (LISTEN)", "<cyan>(LISTEN)</>"},
		{"x 1 a 0u IPv4 1 0t0 TCP :1 (UNCONNECTED)", "<dim>(UNCONNECTED)</>"},
		{deletedRow, "<yellow>(deleted)</>"},
		{plainRow, "<dim>(readlink: Permission denied)</>"},
		{memRow, "<dim>(path dev=0,30)</>"},
	} {
		if got := format(netHeading + "\n" + tc.row); !strings.Contains(got, tc.want) {
			t.Errorf("row %q\ngot  %q\nwant %q", tc.row, got, tc.want)
		}
	}
}

func TestFormatLeavesABracketedPathAlone(t *testing.T) {
	// A parenthesis in a name is a name, not one of lsof's remarks.
	row := "bash 551284 atif cwd DIR 0,54 520 354 /tmp/build (draft)/out"
	body := strings.SplitN(format(procHeading+"\n"+row), "\n", 2)[1]
	if strings.Contains(body, "<") {
		t.Errorf("colored a path as a note: %q", body)
	}
}

func TestFormatColorsCellsOfARowItCanRead(t *testing.T) {
	got := format(netHeading + "\n" + netRow)
	if !strings.Contains(got, "<cyan>IPv4</>") {
		t.Errorf("file type not colored: %q", got)
	}
	if !strings.Contains(got, "<bold>COMMAND</>") {
		t.Errorf("heading not colored on the same page: %q", got)
	}
	// The size column reads "0t0", an offset, and the node column reads the
	// protocol — both must pass for the row to be read at all.
	if !strings.Contains(got, "0t0  UDP") {
		t.Errorf("cells moved: %q", got)
	}
}

func TestFormatColorsFileDescriptorsOfARowItCanRead(t *testing.T) {
	if got := fdColor("mem"); got != dim {
		t.Errorf("fdColor(mem) = %q, want dim", got)
	}
	if got := format(procHeading + "\n" + memRow); !strings.Contains(got, "<dim>mem</>") {
		t.Errorf("memory mapping not dimmed: %q", got)
	}
}

func TestFormatFallsBackToTheNoteWhenACellIsMissing(t *testing.T) {
	// SIZE/OFF is empty on this row, so every cell after it is one column to
	// the left of where the heading puts it. The row must keep its note and
	// lose the cell color.
	got := format(procHeading + "\n" + emptySizeRow)
	if !strings.Contains(got, "<dim>(path dev=0,30)</>") {
		t.Errorf("note not colored: %q", got)
	}
	if strings.Contains(got, "<dim>txt</>") || strings.Contains(got, "<dim>REG</>") {
		t.Errorf("colored a shifted cell: %q", got)
	}
}

func TestFormatWithoutAHeadingIsLeftAlone(t *testing.T) {
	text := "p\n769\nf\ncwd\n"
	if got := format(text); got != text {
		t.Errorf("Format = %q, want %q", got, text)
	}
}

func TestHoldsReadsEachColumnItsOwnWay(t *testing.T) {
	for _, tc := range []struct {
		column, value string
		want          bool
	}{
		{"PID", "551284", true},
		{"PID", "atif", false},
		{"NODE", "354", true},
		{"NODE", "TCP", true},
		{"NODE", "/usr/bin/bash", false},
		{"SIZE/OFF", "1195144", true},
		{"SIZE/OFF", "0t0", true},
		{"SIZE/OFF", "500B", true},
		{"SIZE/OFF", "1.5K", true},
		{"SIZE/OFF", "TCP", false},
		{"DEVICE", "0,54", true},
		{"DEVICE", "350953", true},
		{"DEVICE", "/tmp", false},
		{"FD", "cwd", true},
		{"FD", "204u", true},
		{"FD", "mem", true},
		{"FD", "REG", false},
		{"USER", "atif", true},
		{"USER", "551284", false},
		{"TYPE", "IPv6", true},
		{"TYPE", "0,54", false},
		{"COMMAND", "anything", true},
	} {
		if got := holds(tc.column, tc.value); got != tc.want {
			t.Errorf("holds(%s, %q) = %v, want %v", tc.column, tc.value, got, tc.want)
		}
	}
}

func TestScriptFormRefusesFormsScriptsRead(t *testing.T) {
	for _, args := range [][]string{
		{"-F"}, {"-Fpcfn"}, {"-t"},
		{"+r"}, {"-r", "5"},
	} {
		if !scriptForm(args) {
			t.Errorf("scriptForm(%q) = false, want true", args)
		}
	}
	for _, args := range [][]string{
		nil, {"-i"}, {"-p", "1"}, {"-u", "atif"}, {"+L1"}, {"-nP", "-i"},
	} {
		if scriptForm(args) {
			t.Errorf("scriptForm(%q) = true, want false", args)
		}
	}
}

func TestNeedsTerminalOnlyForAListingThatNeverEnds(t *testing.T) {
	if !NeedsTerminal([]string{"+r"}) || !NeedsTerminal([]string{"-r", "5"}) {
		t.Error("a repeating listing must be handed the terminal")
	}
	if NeedsTerminal([]string{"-i"}) || NeedsTerminal([]string{"-F"}) {
		t.Error("an ordinary listing must not ask for the terminal")
	}
}
