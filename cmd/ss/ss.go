// Package ss formats ss output. The header ss prints runs "Peer Address:Port"
// straight into "Process" with no space between them, so the two columns read
// as one run-on token, and the process name is buried at the end of a long
// address column.
package ss

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

// Run implements `parse ss <args>`.
func Run(args []string) int {
	if tool.OutputMode(args) != "" {
		// --json and friends are read by scripts, not by people.
		return tool.Passthrough("ss", args)
	}
	text, code, started := tool.Capture("ss", args)
	if !started {
		return code
	}
	Format(os.Stdout, text)
	return code
}

// Pipe formats piped ss output.
func Pipe(w io.Writer, text string) {
	Format(w, text)
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag || strings.HasPrefix(a, flag+"=") {
			return true
		}
	}
	return false
}

// headerRE matches the ss header, which is how Detect recognizes its output.
var headerRE = regexp.MustCompile(`^Netid\s+State\s+Recv-Q\s+Send-Q`)

// Detect reports whether text looks like ss output.
func Detect(text string) string {
	for _, line := range strings.SplitN(text, "\n", 4) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if headerRE.MatchString(line) {
			return "ss"
		}
		return ""
	}
	return ""
}

// usersRE matches the trailing "users:(("name",pid=123,fd=4))" field.
var usersRE = regexp.MustCompile(`\susers:\(\("?([^",]*).*?\)\)\s*$`)

// row is one parsed ss line.
type row struct {
	netid, state, rest, process string
}

// Format writes the ss table with the process column separated out and the
// state colored.
func Format(w io.Writer, text string) {
	out := bufio.NewWriter(w)
	defer out.Flush()

	header := ""
	var rows []row
	matched := false

	tool.EachLine(text, func(line string) {
		if strings.TrimSpace(line) == "" {
			return
		}
		if headerRE.MatchString(line) {
			matched = true
			header = line
			return
		}
		rows = append(rows, parseRow(line))
	})
	if !matched {
		io.WriteString(out, text)
		return
	}

	// The process name goes in a column past the widest address tail, so it
	// lines up down the page and never collides with a port. Deriving the
	// offset from the data is more reliable than trusting ss's own header
	// widths, which do not line up with the rows it prints.
	width := 0
	for _, r := range rows {
		if n := len(r.netid) + len(r.state) + len(r.rest); n > width {
			width = n
		}
	}

	if header != "" {
		printHeader(out, header, width)
	}
	for _, r := range rows {
		// Pad before painting, so the escape codes never sit inside the
		// padding and the columns still line up in a real terminal.
		line := fmt.Sprintf("%s %s %s",
			tool.PaintColor(dim, r.netid)+strings.Repeat(" ", maxInt(1, 6-len(r.netid))),
			tool.PaintColor(stateColor(r.state), r.state)+strings.Repeat(" ", maxInt(1, 8-len(r.state))),
			r.rest)
		if r.process != "" {
			line += strings.Repeat(" ", maxInt(2, width-len(line)+2)) +
				tool.PaintColor(dim, r.process)
		}
		fmt.Fprintln(out, line)
	}
}

// parseRow splits an ss line into its two named columns, the address tail and
// the process name ss buries at the end of that tail.
func parseRow(line string) row {
	process := ""
	if m := usersRE.FindStringSubmatch(line); m != nil {
		process = m[1]
		line = usersRE.ReplaceAllString(line, "")
	}
	line = strings.TrimRight(line, " ")

	fields := strings.Fields(line)
	if len(fields) < 2 {
		return row{rest: line}
	}
	netid, state := fields[0], fields[1]
	return row{
		netid:   netid,
		state:   state,
		rest:    strings.TrimLeft(line, netid+" "+state),
		process: process,
	}
}

// printHeader rewrites the colliding header so "Port" and "Process" are
// separate columns, and pads the address part to the width the rows use.
func printHeader(out *bufio.Writer, line string, width int) {
	// "Peer Address:PortProcess" is two headings run together; split them.
	fixed := strings.Replace(line, "Peer Address:PortProcess", "Peer Address:Port", 1)
	// Keep ss's own column headers and spacing; only split the collision and
	// drop the duplicate Netid/State pair that the rows already print.
	head := "      " + strings.TrimSpace(fixed)
	if n := len(head); n < width {
		head += strings.Repeat(" ", width-n)
	}
	fmt.Fprintln(out, tool.PaintColor(bold, head+"  Process"))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// stateColor tints the connection state.
func stateColor(state string) string {
	switch state {
	case "ESTAB":
		return green
	case "LISTEN":
		return cyan
	case "UNCONN":
		return dim
	case "SYN-SENT", "FIN-WAIT", "CLOSE-WAIT":
		return yellow
	}
	return ""
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
