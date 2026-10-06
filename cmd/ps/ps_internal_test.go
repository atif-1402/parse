// Tests for the ps helpers the end-to-end tests in ../../test cannot reach:
// the detectors, the column split, and the per-value coloring.
//
// Colors are replaced by readable tags, the same way ../../test does it.
package ps

import (
	"fmt"
	"io"
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
	tool.PrintTable = func(w io.Writer, headers []string, rows [][]tool.Cell) {
		fmt.Fprintln(w, strings.Join(headers, " | "))
	}
}

func lines(l ...string) string { return strings.Join(l, "\n") + "\n" }

func TestDetectNeedsThePsHeader(t *testing.T) {
	in := lines("USER         PID %CPU %MEM    VSZ   RSS TTY      STAT START   TIME COMMAND")
	if got := Detect(in); got != "aux" {
		t.Errorf("Detect = %q, want aux", got)
	}
	// A table from some other tool must not be claimed by ps.
	if got := Detect("NAME  SIZE  TYPE\nsda   111G  disk\n"); got != "" {
		t.Errorf("Detect claimed foreign output: %q", got)
	}
}

func TestPctColorFlagsBusyValues(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"45.2", yellow}, {"10.0", yellow},
		{"3.0", cyan}, {"1.5", cyan},
		{"0.0", ""}, {"0.9", ""},
	}
	for _, c := range cases {
		if got := pctColor(c.in); got != c.want {
			t.Errorf("pctColor(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStatColorSeparatesRunningFromStopped(t *testing.T) {
	if got := statColor("Rl+"); got != green {
		t.Errorf("a running process should be green, got %q", got)
	}
	if got := statColor("D"); got != red {
		t.Errorf("an uninterruptible process should be red, got %q", got)
	}
	if got := statColor("Ssl"); got != dim {
		t.Errorf("an idle state should stay quiet, got %q", got)
	}
}

func TestCommandIsDimmedNotDeleted(t *testing.T) {
	long := strings.Repeat("x", commandWidth+20)
	got := paintCommand(long)
	if len(got) < len(long) {
		t.Fatalf("paintCommand deleted text: %d chars in, %d out", len(long), len(got))
	}
	if !strings.Contains(got, "<dim>") {
		t.Errorf("expected a dimmed tail:\n%s", got)
	}
}

func TestShortCommandIsLeftAlone(t *testing.T) {
	if got := paintCommand("opencode"); got != "opencode" {
		t.Errorf("a short command should not be padded or colored: %q", got)
	}
}
