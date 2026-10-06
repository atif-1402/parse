// Tests for the du helpers: the row regex, the unit conversion, and the
// magnitude thresholds.
package du

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

func TestDetectNeedsSizeThenPath(t *testing.T) {
	if got := Detect("22972\tproject/\n"); got != "du" {
		t.Errorf("Detect = %q, want du", got)
	}
	// `ls -l` starts with a word, not a number, so du must not claim it.
	if got := Detect("-rw-r--r-- 1 atif atif 4096 file\n"); got != "" {
		t.Errorf("claimed foreign output: %q", got)
	}
}

func TestHumanSizeAddsUnitsToBareCounts(t *testing.T) {
	// du's default unit is 1K blocks, so a bare number means kilobytes.
	if got := humanSize("22972", ""); got != "22972K" {
		t.Errorf("humanSize(22972, \"\") = %q, want 22972K", got)
	}
	if got := humanSize("23", "M"); got != "23M" {
		t.Errorf("humanSize(23, M) = %q, want 23M", got)
	}
}

func TestParseSizeAgreesWithHumanSize(t *testing.T) {
	// The two must round-trip, or magnitudeColor compares kilobytes against
	// byte thresholds and nothing is ever highlighted.
	cases := []struct {
		in    string
		sizes [2]string
	}{
		{"492M", [2]string{"492M", ""}},
		{"104K", [2]string{"104K", ""}},
	}
	for _, c := range cases {
		if got := parseSize(c.in); got <= 0 {
			t.Errorf("parseSize(%q) = %v, want a positive byte count", c.in, got)
		}
	}
	if parseSize("1M") != 1024*1024 {
		t.Errorf("parseSize(1M) = %v, want 1MiB", parseSize("1M"))
	}
	if parseSize("1K") != 1024 {
		t.Errorf("parseSize(1K) = %v, want 1KiB", parseSize("1K"))
	}
}

func TestMagnitudeColorThresholds(t *testing.T) {
	cases := []struct{ in, want string }{
		{"2.0G", red},
		{"492M", yellow},
		{"50M", cyan},
		{"104K", ""},
		{"28", ""},
	}
	for _, c := range cases {
		if got := magnitudeColor(c.in); got != c.want {
			t.Errorf("magnitudeColor(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// du's size column is followed by a tab or several spaces, never by one space.
// Prose that begins with a number must not be claimed: rewriting "3 days ago"
// as "3K days ago" is worse than leaving it alone.
func TestDetectLeavesProseAlone(t *testing.T) {
	for _, in := range []string{
		"3 days ago\n",
		"2 files remaining\n",
		"9 /some/path\n",
		"5 items\n",
	} {
		if Detect(in) != "" {
			t.Errorf("du claimed prose %q", strings.TrimSpace(in))
		}
	}
}

// Real du output, in both the block and human forms, still has to be claimed.
func TestDetectClaimsRealDu(t *testing.T) {
	for _, in := range []string{
		"22972\t/home/dev/src/\n",
		"23M\t/home/dev/src\n",
		"    16M  /home/dev/Projects/coldlock\n",
		"4\t./a\n8\t./b\n",
	} {
		if Detect(in) != "du" {
			t.Errorf("du failed to claim %q", strings.TrimSpace(in))
		}
	}
}
