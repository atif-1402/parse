// Package ip formats iproute2 output. `ip addr` spreads one interface over
// six indented lines, throws the flags into an angle-bracket wall, and repeats
// "valid_lft forever" for every address. The goal is the clarity of
// `ip -brief addr` while keeping the detail that makes the command worth
// running.
package ip

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/atif-1402/parse/internal/tool"
)

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

// Run implements `parse ip <args>`.
func Run(args []string) int {
	sub := subcommand(args)
	if sub == "" || !ipCommands[sub] {
		return tool.Passthrough("ip", args)
	}
	if isMachineOutput(args) {
		return tool.Passthrough("ip", args)
	}
	if isBrief(args) {
		// `ip -brief` is already a clean table. Leave it exactly as it is.
		return tool.Passthrough("ip", args)
	}
	text, code, started := tool.Capture("ip", args)
	if !started {
		return code
	}
	Format(os.Stdout, sub, text)
	return code
}

// Pipe formats piped ip output.
func Pipe(w io.Writer, text string) {
	text = tool.StripANSI(text)
	if sub := Detect(text); sub != "" {
		Format(w, sub, text)
		return
	}
	io.WriteString(w, text)
}

// subcommand returns the first non-flag argument, which is the ip subcommand.
func subcommand(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		switch a {
		case "addr", "a":
			return "addr"
		case "link", "l":
			return "link"
		case "route", "r":
			return "route"
		}
		return ""
	}
	return ""
}

var ipCommands = map[string]bool{"addr": true, "link": true, "route": true}

func isBrief(args []string) bool {
	for _, a := range args {
		if a == "-brief" || a == "-br" {
			return true
		}
	}
	return false
}

// isMachineOutput reports whether -json or -j was asked for, which must pass
// through untouched so scripts keep parsing it.
func isMachineOutput(args []string) bool {
	for _, a := range args {
		if a == "-j" || a == "-json" || a == "-json=short" || a == "-json=pretty" {
			return true
		}
	}
	return false
}

// ifaceRE matches an interface line: "2: enp2s0: <FLAGS> mtu 1500 ...".
var ifaceRE = regexp.MustCompile(`^(\d+):\s+([^:@]+)(@\S+)?:\s+<([^>]*)>(.*)$`)

// addrRE matches an "inet"/"inet6" line, capturing the address and its scope.
var addrRE = regexp.MustCompile(`^\s+(inet6?|inet6?)\s+(\S+)\s*(.*)$`)

// validRE matches the "valid_lft ... preferred_lft ..." lifetime line.
var validRE = regexp.MustCompile(`^\s+valid_lft\s+(\S+)\s+preferred_lft\s+(\S+)`)

// linkRE matches the "link/ether ..." hardware address line.
var linkRE = regexp.MustCompile(`^\s+link/(\S+)\s+([0-9a-f:]+)`)

// altnameRE matches the "altname ..." line.
var altnameRE = regexp.MustCompile(`^\s+altname\s+(\S+)`)

// Detect guesses which ip subcommand produced text.
func Detect(text string) string {
	lines := strings.SplitN(text, "\n", 12)
	addr, route := 0, 0
	for _, l := range lines {
		if ifaceRE.MatchString(l) {
			addr++
		}
		if looksLikeRoute(l) {
			route++
		}
	}
	if addr > 0 {
		return "addr"
	}
	if route >= 2 {
		return "route"
	}
	return ""
}

// looksLikeRoute matches a route line: an address or "default", then "via" or
// "dev" somewhere on the line.
func looksLikeRoute(l string) bool {
	if !strings.Contains(l, " dev ") && !strings.HasPrefix(l, "default") {
		return false
	}
	first := strings.Fields(l)
	if len(first) == 0 {
		return false
	}
	f := first[0]
	return f == "default" || strings.Contains(f, "/") || strings.Contains(f, ":")
}

// iface holds the facts parse keeps from one block of `ip addr` output.
type iface struct {
	name    string
	index   string
	ifIndex string
	state   string
	mtu     string
	qdisc   string
	mac     string
	loop    bool
	addrs   []string
	// finite records the addresses whose lease actually expires, which is
	// information `valid_lft forever` throws away.
	finite map[string]string
	alt    string
}

// Format writes the formatted ip report.
func Format(w io.Writer, sub, text string) {
	out := bufio.NewWriter(w)
	defer out.Flush()

	switch sub {
	case "addr", "link":
		formatAddr(out, text)
	case "route":
		formatRoute(out, text)
	default:
		io.WriteString(out, text)
	}
}

// formatAddr collapses each interface block into a short group.
func formatAddr(out *bufio.Writer, text string) {
	var cur *iface
	flush := func() {
		if cur != nil {
			printInterface(out, cur)
			cur = nil
		}
	}

	tool.EachLine(text, func(line string) {
		if m := ifaceRE.FindStringSubmatch(line); m != nil {
			flush()
			cur = newInterface(m)
			rest := m[5]
			cur.state = field(rest, "state")
			cur.mtu = field(rest, "mtu")
			cur.qdisc = field(rest, "qdisc")
			cur.loop = m[2] == "lo" || strings.Contains(m[4], "LOOPBACK")
			return
		}
		if cur == nil {
			fmt.Fprintln(out, line)
			return
		}
		if m := addrRE.FindStringSubmatch(line); m != nil {
			cur.addrs = append(cur.addrs, m[2])
			return
		}
		if m := validRE.FindStringSubmatch(line); m != nil && m[1] != "forever" {
			// Only a lifetime that ends is worth showing.
			cur.finite[cur.last()] = m[1]
			return
		}
		if m := linkRE.FindStringSubmatch(line); m != nil {
			cur.mac = m[2]
			return
		}
		if m := altnameRE.FindStringSubmatch(line); m != nil {
			cur.alt = m[1]
		}
	})
	flush()
}

func newInterface(m []string) *iface {
	return &iface{
		name:    m[2],
		index:   m[1],
		ifIndex: m[3],
		finite:  map[string]string{},
	}
}

func (i *iface) last() string {
	if len(i.addrs) == 0 {
		return ""
	}
	return i.addrs[len(i.addrs)-1]
}

// printInterface writes one interface: name, state and peer on the first line,
// details dimmed below, addresses last with an expiry note in a column of its
// own so a long IPv6 address cannot run into it.
func printInterface(w *bufio.Writer, i *iface) {
	peer := tool.PaintColor(dim, i.ifIndex)
	fmt.Fprintf(w, "%s %s %s\n",
		tool.PaintColor(bold, padRight(i.name, 18)),
		tool.PaintColor(bold+" "+stateColor(i.state), padLeft(i.state, 8)),
		peer)

	var details []string
	if i.mtu != "" {
		details = append(details, "mtu "+i.mtu)
	}
	if i.qdisc != "" {
		details = append(details, "qdisc "+i.qdisc)
	}
	if i.mac != "" {
		details = append(details, i.mac)
	}
	if i.alt != "" {
		details = append(details, "altname "+i.alt)
	}
	if len(details) > 0 {
		fmt.Fprintf(w, "  %s\n", tool.PaintColor(dim, strings.Join(details, "  ")))
	}

	// The expiry note goes at a fixed column, past the widest IPv6 address
	// with room to spare, so every note lines up down the page.
	const noteColumn = 46
	for _, a := range i.addrs {
		note := ""
		if exp, ok := i.finite[a]; ok {
			note = tool.PaintColor(yellow, "expires in "+exp)
		}
		fmt.Fprintf(w, "  %s%s\n", tool.PaintColor(bold, padRight(a, noteColumn)), note)
	}
	if len(i.addrs) > 0 {
		fmt.Fprintln(w)
	}
}

// stateColor tints the link state.
func stateColor(state string) string {
	switch state {
	case "UP":
		return green
	case "DOWN":
		return red
	case "UNKNOWN":
		return dim
	}
	return ""
}

// field returns the value that follows key in a space-separated line.
func field(rest, key string) string {
	f := strings.Fields(rest)
	for i := 0; i+1 < len(f); i++ {
		if f[i] == key {
			return f[i+1]
		}
	}
	return ""
}

// routeRE splits a route line into its destination and the rest.
var routeRE = regexp.MustCompile(`^(\S+)\s+(.*)$`)

// routeKeywordRE finds the dim-able keywords inside a route's tail.
var routeKeywordRE = regexp.MustCompile(`\b(via|dev|proto|src|metric|scope|table|linkdown)\b`)

// formatRoute colors a route table. The text is left alone; ip already aligns
// this one properly.
func formatRoute(out *bufio.Writer, text string) {
	tool.EachLine(text, func(line string) {
		if strings.TrimSpace(line) == "" {
			return
		}
		m := routeRE.FindStringSubmatch(line)
		if m == nil {
			fmt.Fprintln(out, line)
			return
		}
		dst := m[1]
		if dst == "default" {
			dst = tool.PaintColor(bold, dst)
		}
		tail := routeKeywordRE.ReplaceAllStringFunc(m[2], func(kw string) string {
			if kw == "linkdown" {
				return tool.PaintColor(yellow, kw)
			}
			return tool.PaintColor(dim, kw)
		})
		fmt.Fprintf(out, "%s %s\n", dst, tail)
	})
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func padLeft(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return strings.Repeat(" ", n-len(s)) + s
}
