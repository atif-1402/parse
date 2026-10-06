// Package ps formats ps output. The table ps already prints is fine; what it
// is not fine with is a COMMAND column that runs to hundreds of characters of
// browser flags, burying the pid and user you were scanning for.
package ps

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/atif-1402/parse/internal/tool"
)

// Colors and the table cell come from the shared plumbing. They are aliased
// here so the formatting code below reads like ordinary parse code.
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

// commandWidth is how much of COMMAND is shown before the rest is dimmed. The
// text is never deleted, only dimmed, so a flag the reader is hunting for is
// still on the line and still findable with a wider terminal.
const commandWidth = 60

// psCommands are the subcommands and option styles parse formats.
var psCommands = map[string]bool{
	"aux": true, "auxwww": true, "ef": true, "-e": true, "ax": true, "a": true,
}

// Run implements `parse ps <args>`.
func Run(args []string) int {
	if len(args) == 0 {
		return tool.Passthrough("ps", args)
	}
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			// --sort, -o, --ppid, -u and friends change the columns, and
			// parse does not know every ps layout yet.
			return tool.Passthrough("ps", args)
		}
		break
	}
	if !psCommands[args[0]] {
		return tool.Passthrough("ps", args)
	}
	text, code, started := tool.Capture("ps", args)
	if !started {
		return code
	}
	Format(os.Stdout, text)
	return code
}

// Pipe formats piped ps output.
func Pipe(w io.Writer, text string) {
	Format(w, text)
}

// headerRE matches the ps table header. It is the anchor for Detect: no header
// means this is not ps output, and parse leaves it alone.
var headerRE = regexp.MustCompile(`^USER\s+PID\s+%CPU\s+%MEM`)

// Detect reports whether text looks like a ps table, and returns "" when it
// does not. It is deliberately strict: a false positive would mangle output
// from another tool.
func Detect(text string) string {
	head := firstLines(text, 3)
	if !headerRE.MatchString(head) {
		return ""
	}
	return "aux"
}

// firstLines returns at most n lines of text, newline stripped.
func firstLines(text string, n int) string {
	lines := strings.SplitN(strings.TrimRight(text, "\n"), "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// Format writes the formatted ps table.
func Format(w io.Writer, text string) {
	out := bufio.NewWriter(w)
	defer out.Flush()

	eachRow(out, text)
}

// eachRow writes the header followed by one row per process.
func eachRow(w *bufio.Writer, text string) {
	rows := 0
	tool.EachLine(text, func(line string) {
		if strings.TrimSpace(line) == "" {
			return
		}
		rows++
		if headerRE.MatchString(line) {
			fmt.Fprintf(w, "%s %s %s %s %s %s\n",
				tool.PaintColor(bold, pad("USER", 8)),
				tool.PaintColor(bold, pad("PID", 7)),
				tool.PaintColor(bold, pad("%CPU", 5)),
				tool.PaintColor(bold, pad("%MEM", 5)),
				tool.PaintColor(bold, pad("STAT", 5)),
				tool.PaintColor(bold, "COMMAND"))
			return
		}
		formatRow(w, line)
	})
	if rows == 0 {
		io.WriteString(w, text)
	}
}

// psColumns are the leading fixed-width columns of a `ps aux` row. COMMAND is
// whatever is left, and is the only field wide enough to run away.
const psColumns = 10

// formatRow prints one process. The first 10 columns are whitespace-separated
// and fixed; COMMAND is the remainder.
func formatRow(w *bufio.Writer, line string) {
	fields := strings.Fields(line)
	if len(fields) < psColumns {
		io.WriteString(w, line+"\n")
		return
	}
	// ps aux columns, in order: USER PID %CPU %MEM VSZ RSS TTY STAT START TIME
	// COMMAND. TIME may be two fields ("0:28" is one, "172:33" is one), so
	// COMMAND is everything from psColumns on rather than a single field.
	user, pid := fields[0], fields[1]
	cpu, mem := fields[2], fields[3]
	stat := ""
	if len(fields) > 7 {
		stat = fields[7]
	}
	command := strings.TrimSpace(strings.Join(fields[psColumns:], " "))

	// Pad before painting: escape codes inside a padded field would throw the
	// columns off in a real terminal.
	fmt.Fprintf(w, "%s %s %s %s %s %s\n",
		tool.PaintColor(dim, pad(user, 8)),
		tool.PaintColor(bold, pad(pid, 7)),
		tool.PaintColor(pctColor(cpu), pad(cpu, 5)),
		tool.PaintColor(pctColor(mem), pad(mem, 5)),
		tool.PaintColor(statColor(stat), pad(stat, 5)),
		paintCommand(command))
}

// paintCommand shows the first commandWidth characters bright and dims the
// rest, so the text stays on the line and nothing is lost.
func paintCommand(s string) string {
	if len(s) <= commandWidth {
		return s
	}
	return tool.PaintColor("", s[:commandWidth]) + tool.PaintColor(dim, s[commandWidth:])
}

// pad right-aligns s in a field n wide.
func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return strings.Repeat(" ", n-len(s)) + s
}

// pctColor highlights the numbers a reader scans for: a busy CPU and a large
// memory share. Under one percent stays uncolored so the loud ones stand out.
func pctColor(s string) string {
	v, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
	if err != nil {
		return ""
	}
	switch {
	case v >= 10:
		return yellow
	case v >= 1:
		return cyan
	}
	return ""
}

// statColor tints the process state. Running gets green, stopped and defunct
// get attention, the idle states stay quiet.
func statColor(stat string) string {
	switch {
	case strings.Contains(stat, "R"):
		return green
	case strings.Contains(stat, "D"):
		return red
	case strings.Contains(stat, "T"):
		return yellow
	}
	return dim
}
