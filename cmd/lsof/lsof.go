// Package lsof colors lsof output. lsof lays its own table out — every column
// is padded to the width of the widest value in it — so parse does not move a
// column. What the table has always needed is color: the socket state at the
// end of a network row, and the notes lsof appends after a path.
package lsof

import (
	"io"
	"os"
	"sort"
	"strings"

	"github.com/atif-1402/parse/internal/tool"
)

const (
	green  = tool.Green
	yellow = tool.Yellow
	cyan   = tool.Cyan
	dim    = tool.Dim
)

// Run implements `parse lsof <args>`.
func Run(args []string) int {
	if scriptForm(args) {
		return tool.Passthrough("lsof", args)
	}
	text, code, started := tool.Capture("lsof", args)
	if !started {
		return code
	}
	Format(os.Stdout, text)
	return code
}

// Pipe formats piped lsof output.
func Pipe(w io.Writer, text string) {
	Format(w, text)
}

// NeedsTerminal reports whether args ask lsof to print again, for as long as it
// runs. `lsof +r` never finishes, so it is handed the terminal as it is rather
// than captured block by block or sat behind a pager.
func NeedsTerminal(args []string) bool { return repeats(args) }

// scriptForm reports whether args asked for a form scripts read: `-F` prints
// one field per line with no table at all, `-t` prints pids and nothing else,
// and a repeating listing is read as it arrives. None of them is a table, so
// none of them is captured: passthrough keeps them streaming and keeps a pager
// out of a script's way.
func scriptForm(args []string) bool {
	return machine(args) || repeats(args)
}

func machine(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "-F") || a == "-t" {
			return true
		}
	}
	return false
}

func repeats(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "-r") || strings.HasPrefix(a, "+r") {
			return true
		}
	}
	return false
}

// Detect reports whether text looks like lsof output.
//
// Every table lsof prints opens with the same heading — COMMAND and PID first,
// NAME last, the owner of the process between them — and nothing else prints
// that combination. The field form (`-F`) and the terse one (`-t`) have no
// heading, so they are left to pass through untouched.
func Detect(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if isHeading(line) {
			return "lsof"
		}
		return ""
	}
	return ""
}

// isHeading reports whether line is lsof's column heading.
func isHeading(line string) bool {
	f := strings.Fields(line)
	if len(f) < 5 || f[0] != "COMMAND" || f[len(f)-1] != "NAME" {
		return false
	}
	hasPID, hasUSER := false, false
	for _, c := range f {
		hasPID = hasPID || c == "PID"
		hasUSER = hasUSER || c == "USER"
	}
	return hasPID && hasUSER
}

// Format gives lsof's rows their color and prints them back exactly where lsof
// put them. The heading is bolded where it already stands, so a column never
// ends up above a different one.
func Format(w io.Writer, text string) {
	lines := strings.Split(text, "\n")

	var names []string
	for _, line := range lines {
		if isHeading(line) {
			names = strings.Fields(line)
			break
		}
	}
	if names == nil {
		io.WriteString(w, text)
		return
	}

	for i, line := range lines {
		if i > 0 {
			io.WriteString(w, "\n")
		}
		if isHeading(line) {
			io.WriteString(w, heading(line))
			continue
		}
		io.WriteString(w, row(line, names))
	}
}

// heading bolds the column names without moving any of them: the padding
// between them is written as it came in, and only the words are painted.
func heading(line string) string {
	var sb strings.Builder
	at := 0
	for _, t := range tokens(line) {
		sb.WriteString(line[at:t[0]])
		sb.WriteString(tool.PaintColor(tool.Bold, line[t[0]:t[1]]))
		at = t[1]
	}
	sb.WriteString(line[at:])
	return sb.String()
}

// row colors one line of lsof's output: the note lsof has written at the end of
// the name, and — when the row's cells can be read with confidence — the file
// descriptor and the type of file they name.
func row(line string, names []string) string {
	marks := noteMarks(line)
	marks = append(marks, cellMarks(line, names)...)
	sort.Slice(marks, func(i, j int) bool { return marks[i].start < marks[j].start })

	var sb strings.Builder
	at := 0
	for _, m := range marks {
		if m.start < at {
			continue
		}
		sb.WriteString(line[at:m.start])
		sb.WriteString(tool.PaintColor(m.color, line[m.start:m.end]))
		at = m.end
	}
	sb.WriteString(line[at:])
	return sb.String()
}

// mark is a span of one line and the color it is painted with.
type mark struct {
	start, end int
	color      string
}

// noteMarks colors the parenthesis lsof has written at the end of a row: a
// socket's state, a file that has been unlinked under its opener, or one of
// lsof's own notes about a path it could not read. Anything else in brackets
// is left alone — it may be part of the name.
func noteMarks(line string) []mark {
	i := strings.LastIndex(line, " (")
	if i < 0 || !strings.HasSuffix(line, ")") {
		return nil
	}
	note := line[i+2 : len(line)-1]
	if note == "" {
		return nil
	}
	color := noteColor(note)
	if color == "" {
		return nil
	}
	return []mark{{i + 1, len(line), color}}
}

func noteColor(note string) string {
	if note == "deleted" {
		return yellow
	}
	if c := stateColor(note); c != "" {
		return c
	}
	if isLsofNote(note) {
		return dim
	}
	return ""
}

// stateColor is a socket's state, spelled the way lsof spells it. The colors
// match what parse does for the same states in `ss`.
func stateColor(state string) string {
	switch state {
	case "LISTEN":
		return cyan
	case "ESTABLISHED", "CONNECTED":
		return green
	case "TIME_WAIT", "CLOSE_WAIT", "FIN_WAIT_1", "FIN_WAIT_2", "LAST_ACK",
		"CLOSING", "SYN_SENT", "SYN_RECV", "CONNECTING", "SYNSENT":
		return yellow
	case "UNCONNECTED", "CLOSED", "CLOSE", "FREE", "IDLE":
		return dim
	}
	return ""
}

// isLsofNote reports whether note is one of lsof's own remarks about a path,
// rather than a name that happens to be bracketed.
func isLsofNote(note string) bool {
	for _, prefix := range []string{
		"path dev=", "readlink:", "stat:", "lstat:", "fstat:", "fstatfs:",
		"opendir:", "readdir:", "lsof:", "WARNING:",
	} {
		if strings.HasPrefix(note, prefix) {
			return true
		}
	}
	return false
}

// cellMarks colors a file descriptor and a file's type, but only when the
// row's cells can be put against the heading with no doubt. A row with an
// empty cell in it has fewer fields than the heading has columns, and reading
// it positionally would color the wrong word — so such a row keeps its notes
// and nothing else.
func cellMarks(line string, names []string) []mark {
	toks := tokens(line)
	if len(toks) < len(names)-1 {
		return nil
	}
	cells := toks[:len(names)-1]
	if !consistent(line, cells, names) {
		return nil
	}

	var marks []mark
	for _, c := range []struct {
		column string
		color  func(string) string
	}{
		{"FD", fdColor},
		{"TYPE", typeColor},
	} {
		i := indexOf(names, c.column)
		if i < 0 || i >= len(cells) {
			continue
		}
		text := line[cells[i][0]:cells[i][1]]
		if color := c.color(text); color != "" {
			marks = append(marks, mark{cells[i][0], cells[i][1], color})
		}
	}
	return marks
}

// consistent reports whether every cell before the name reads as the column
// the heading puts it under: a pid is a number, a device is a device number, a
// file type is one of the types lsof knows. A row that fails anywhere is a row
// with an empty cell in it, and is not read positionally.
func consistent(line string, cells [][2]int, names []string) bool {
	for i, name := range names[:len(names)-1] {
		if i >= len(cells) {
			return false
		}
		if !holds(name, line[cells[i][0]:cells[i][1]]) {
			return false
		}
	}
	return true
}

// holds reports whether value could be the contents of the named column.
func holds(column, value string) bool {
	switch column {
	case "PID", "PPID", "TID", "PGID", "NLINK", "UID":
		return numeric(value)
	case "NODE":
		// A file's node number, or — on a network row — the protocol the
		// socket speaks.
		return numeric(value) || isProtocol(value)
	case "DEVICE":
		return isDevice(value)
	case "SIZE/OFF", "OFFSET", "SZ":
		return isSize(value)
	case "FD":
		return isFD(value)
	case "USER":
		return isUser(value)
	case "TYPE":
		return isType(value)
	}
	return true
}

// isProtocol is the set of protocols lsof puts in a network row's NODE column.
// Without them every row of `lsof -i` would fail its own column check, since
// its node column carries "TCP" rather than a number.
func isProtocol(s string) bool {
	switch s {
	case "TCP", "UDP", "UDPLITE", "SCTP", "ICMP", "ICMPv6", "ICMPV6",
		"IP", "IPX", "RAW", "ICMP6":
		return true
	}
	return false
}

func numeric(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// isDevice accepts a device as lsof writes it: "0,54", "350953", or the
// pointer a unix socket's device column carries.
func isDevice(s string) bool {
	if s == "" || strings.Contains(s, " ") {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') ||
			c == ',' || c == 'x' {
			continue
		}
		return false
	}
	return true
}

// isSize accepts a size or an offset as lsof prints it: "1195144", "0t0", and
// the human sizes `-H` adds a unit to, "500B" and "1.5K".
func isSize(s string) bool {
	if s == "" {
		return false
	}
	if i := strings.IndexByte(s, 't'); i > 0 && numeric(s[:i]) && numeric(s[i+1:]) {
		return true
	}
	digits := s
	if last := s[len(s)-1]; (last < '0' || last > '9') && last != '.' {
		digits = s[:len(s)-1]
	}
	if i := strings.IndexByte(digits, '.'); i >= 0 {
		return i > 0 && numeric(digits[:i]) && numeric(digits[i+1:])
	}
	return numeric(digits)
}

// isFD accepts a descriptor as lsof names it: "0r", "204u", or one of the
// special names for a process's own files.
func isFD(s string) bool {
	switch s {
	case "cwd", "rtd", "txt", "mem", "DEL", "PTR", "FIFO":
		return true
	}
	if s == "" {
		return false
	}
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 || i == len(s) {
		return i > 0
	}
	switch s[i:] {
	case "r", "w", "u", "t":
		return true
	}
	return false
}

func isUser(s string) bool {
	if s == "" || numeric(s) {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '_' || c == '-' || c == '.' {
			continue
		}
		return false
	}
	return true
}

// isType is the set of file types lsof prints in its TYPE column. An unknown
// one makes the row unreadable positionally, which costs it color and nothing
// else.
func isType(s string) bool {
	switch s {
	case "REG", "DIR", "CHR", "BLK", "FIFO", "SOCK", "unknown", "LINK",
		"IPv4", "IPv6", "unix", "netlink", "a_inode", "PIPE", "DEL",
		"KQUEUE", "RTR", "PROC", "PSXSHM", "PSXSEM", "DOOR", "KSHM",
		"STR", "TPI", "UNDEF", "UNKN", "INODE", "XATTR":
		return true
	}
	return false
}

// fdColor marks the descriptors that are not descriptors of anything the
// reader opened: a memory mapping, or one whose file has gone.
func fdColor(s string) string {
	switch s {
	case "mem":
		return dim
	case "DEL":
		return yellow
	}
	return ""
}

// typeColor marks the file types that say where a row came from: sockets of
// any kind in cyan, and the type lsof falls back to when it could not read the
// file at all, which recedes.
func typeColor(s string) string {
	switch s {
	case "IPv4", "IPv6", "unix", "netlink":
		return cyan
	case "unknown":
		return dim
	}
	return ""
}

func indexOf(names []string, want string) int {
	for i, n := range names {
		if n == want {
			return i
		}
	}
	return -1
}

// tokens is every whitespace-separated run in line, with its offsets.
func tokens(line string) [][2]int {
	var out [][2]int
	at := 0
	for at < len(line) {
		for at < len(line) && isSpace(line[at]) {
			at++
		}
		start := at
		for at < len(line) && !isSpace(line[at]) {
			at++
		}
		if at > start {
			out = append(out, [2]int{start, at})
		}
	}
	return out
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}
