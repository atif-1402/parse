// Tests for the ss helpers: the header detector, the process-name split and
// the state coloring.
package ss

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

func TestDetectNeedsTheSsHeader(t *testing.T) {
	in := lines("Netid State  Recv-Q Send-Q   Local Address:Port  Peer Address:Port Process")
	if got := Detect(in); got != "ss" {
		t.Errorf("Detect = %q, want ss", got)
	}
	// A similar-looking table from another tool must not be claimed.
	if got := Detect("Proto Recv-Q Send-Q Local Address\nudp   0      0      0.0.0.0:53\n"); got != "" {
		t.Errorf("claimed foreign output: %q", got)
	}
}

func TestParseRowPullsOutTheProcessName(t *testing.T) {
	in := `udp   UNCONN 0      0      224.0.0.251:5353  224.0.0.251:5353 users:(("chromium",pid=3568,fd=63))`
	r := parseRow(in)
	if r.process != "chromium" {
		t.Errorf("process = %q, want chromium", r.process)
	}
	if strings.Contains(r.rest, "users:") {
		t.Errorf("the users:(...) field should be out of the address column: %q", r.rest)
	}
	if r.netid != "udp" || r.state != "UNCONN" {
		t.Errorf("netid/state = %q/%q, want udp/UNCONN", r.netid, r.state)
	}
}

func TestParseRowKeepsBothAddresses(t *testing.T) {
	r := parseRow("tcp   ESTAB 0      0      10.0.0.1:22  10.0.0.2:5555")
	if !strings.Contains(r.rest, "10.0.0.1:22") || !strings.Contains(r.rest, "10.0.0.2:5555") {
		t.Errorf("lost an address column: %q", r.rest)
	}
}

func TestStateColor(t *testing.T) {
	cases := []struct{ in, want string }{
		{"ESTAB", green}, {"LISTEN", cyan}, {"UNCONN", dim},
		{"SYN-SENT", yellow}, {"", ""},
	}
	for _, c := range cases {
		if got := stateColor(c.in); got != c.want {
			t.Errorf("stateColor(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatSplitsTheCollidingHeader(t *testing.T) {
	in := lines(
		"Netid State  Recv-Q Send-Q   Local Address:Port  Peer Address:PortProcess",
		"udp   UNCONN 0      0      10.0.0.1:53  0.0.0.0:*",
	)
	var sb strings.Builder
	Format(&sb, in)
	got := sb.String()
	if strings.Contains(got, "PortProcess") {
		t.Errorf("header still collides:\n%s", got)
	}
	if !strings.Contains(got, "Process") {
		t.Errorf("the Process heading should survive:\n%s", got)
	}
}
