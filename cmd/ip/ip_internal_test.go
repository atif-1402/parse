// Tests for the ip helpers: interface detection, the block parser, and the
// route colorer.
package ip

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

const addrSample = `1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN group default qlen 1000
    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00
    inet 127.0.0.1/8 scope host lo
       valid_lft forever preferred_lft forever
2: enp2s0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc fq_codel state UP group default qlen 1000
    link/ether 0a:e0:af:c2:15:d8 brd 0a:e0:af:c2:15:d8
    inet 192.168.1.23/24 brd 192.168.1.255 scope global dynamic noprefixroute enp2s0
       valid_lft 64900sec preferred_lft 64900sec
`

func TestDetectRecognizesAddrOutput(t *testing.T) {
	if got := Detect(addrSample); got != "addr" {
		t.Errorf("Detect = %q, want addr", got)
	}
}

func TestDetectRecognizesRouteOutput(t *testing.T) {
	in := lines(
		"default via 192.168.1.1 dev enp2s0 proto dhcp src 192.168.1.23 metric 100",
		"192.168.1.0/24 dev enp2s0 proto kernel scope link src 192.168.1.23 metric 100",
	)
	if got := Detect(in); got != "route" {
		t.Errorf("Detect = %q, want route", got)
	}
}

func TestDetectIgnoresUnrelatedText(t *testing.T) {
	if got := Detect("hello\nworld\n"); got != "" {
		t.Errorf("claimed unrelated text: %q", got)
	}
}

func TestFieldFindsAValueInTheHeader(t *testing.T) {
	rest := " mtu 1500 qdisc fq_codel state UP group default"
	if got := field(rest, "state"); got != "UP" {
		t.Errorf("field(state) = %q, want UP", got)
	}
	if got := field(rest, "mtu"); got != "1500" {
		t.Errorf("field(mtu) = %q, want 1500", got)
	}
	if got := field(rest, "qdisc"); got != "fq_codel" {
		t.Errorf("field(qdisc) = %q, want fq_codel", got)
	}
	if got := field(rest, "absent"); got != "" {
		t.Errorf("field(absent) = %q, want empty", got)
	}
}

func TestStateColor(t *testing.T) {
	cases := []struct{ in, want string }{
		{"UP", green}, {"DOWN", red}, {"UNKNOWN", dim}, {"", ""},
	}
	for _, c := range cases {
		if got := stateColor(c.in); got != c.want {
			t.Errorf("stateColor(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLooksLikeRouteRejectsNonRoutes(t *testing.T) {
	if !looksLikeRoute("default via 192.168.1.1 dev enp2s0") {
		t.Error("a default route is a route")
	}
	if !looksLikeRoute("10.200.0.0/24 dev veth-host proto kernel") {
		t.Error("a network route is a route")
	}
	if looksLikeRoute("eth0 up") {
		t.Error("a link line is not a route")
	}
}
