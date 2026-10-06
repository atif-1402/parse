// Package free formats free output. Two things make the default form hard to
// read: the numbers are counts of kibibytes with nothing saying so, so 8004464
// has to be read as "7.6G" from memory, and nothing on the screen says how
// full the machine is, so used has to be held against total by eye.
package free

import (
	"bufio"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/atif-1402/parse/internal/tool"
)

const (
	red    = tool.Red
	yellow = tool.Yellow
)

// Run implements `parse free <args>`.
func Run(args []string) int {
	if scriptForm(args) {
		return tool.Passthrough("free", args)
	}
	text, code, started := tool.Capture("free", args)
	if !started {
		return code
	}
	Format(os.Stdout, text)
	return code
}

// Pipe formats piped free output.
func Pipe(w io.Writer, text string) {
	Format(w, text)
}

// NeedsTerminal reports whether args ask free to keep printing. `free -s 1`
// prints a block every second until it is interrupted, so it is handed the
// terminal as it is: capturing it would hold every block back until the end,
// and a pager would sit in front of a screen that is meant to move.
func NeedsTerminal(args []string) bool { return hasAny(args, repeating) }

// scriptForm reports whether args asked for a form scripts read. A pinned unit
// turns free's numbers into plain counts of that unit, which parse must not
// reinterpret, and a repeating output is read as it arrives rather than as one
// block of text.
func scriptForm(args []string) bool {
	return hasAny(args, pinned) || hasAny(args, repeating)
}

// pinned are the flags that choose the unit free counts in. The text they
// produce is identical in shape to the default form, so the choice is only
// visible on the command line — which is where these are checked.
var pinned = []string{
	"-b", "-k", "-m", "-g",
	"--bytes", "--kilo", "--mega", "--giga", "--tera", "--peta",
	"--kibi", "--mebi", "--gibi", "--tebi", "--pebi", "--si",
}

// repeating are the flags that make free print again, with or without a count
// to stop at.
var repeating = []string{"-s", "-c", "--seconds", "--count"}

func hasAny(args []string, flags []string) bool {
	for _, a := range args {
		for _, f := range flags {
			if a == f || strings.HasPrefix(a, f+"=") {
				return true
			}
		}
	}
	return false
}

// Detect reports whether text looks like free output.
//
// free is told apart by its header, which every form except `free -L` prints:
// total, used and free in that order, ending in available. The values say
// nothing about which form produced them — `free -m` and `free -b` print the
// same header over different units — so the header identifies the tool and the
// unit is taken from the command line, where it is visible.
func Detect(text string) string {
	for _, line := range strings.SplitN(text, "\n", 4) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if columns(line) == nil {
			return ""
		}
		return "free"
	}
	return ""
}

// columns returns the header's field names, or nil when line is not free's
// header. The first three names and the last one are what free prints in every
// table form, and nothing else prints that combination.
func columns(line string) []string {
	f := strings.Fields(line)
	if len(f) < 6 || f[0] != "total" || f[1] != "used" || f[2] != "free" ||
		f[len(f)-1] != "available" {
		return nil
	}
	return f
}

// Format gives free's bare numbers a unit and tints the row whose memory is
// filling up. The headings and the columns they head come back byte for byte:
// free already aligns them, and a column that moves is a column a reader has
// to find again.
func Format(w io.Writer, text string) {
	lines := strings.Split(text, "\n")

	// The header is whatever the first non-blank line turns out to be. Text
	// that opens differently is not free's and goes back as it came in.
	head := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if columns(line) != nil {
			head = i
		}
		break
	}
	if head < 0 {
		io.WriteString(w, text)
		return
	}

	out := bufio.NewWriter(w)
	defer out.Flush()
	for i, line := range lines {
		if i > 0 {
			out.WriteByte('\n')
		}
		if i == head {
			printRow(out, "", strings.Fields(line), "")
			continue
		}
		label, cells, ok := dataRow(line)
		if !ok {
			out.WriteString(line)
			continue
		}
		// The tint is worked out before a unit is added: it compares free's own
		// numbers, not the rounded ones printed back at the reader.
		color := fill(cells)
		if unitless(cells) {
			for j := range cells {
				cells[j] = human(cells[j])
			}
		}
		printRow(out, label, cells, color)
	}
}

// dataRow reads a label and its values. free's rows begin at the left margin
// with the label; a line that does not is the header or something else.
func dataRow(line string) (label string, cells []string, ok bool) {
	if line == "" || line[0] == ' ' {
		return "", nil, false
	}
	f := strings.Fields(line)
	if len(f) < 2 || !strings.HasSuffix(f[0], ":") {
		return "", nil, false
	}
	return strings.TrimSuffix(f[0], ":"), f[1:], true
}

// unitless reports whether a row's numbers carry no unit, which is free's
// default form. free -h prints the same table with a suffix on every value.
func unitless(cells []string) bool {
	if len(cells) == 0 || cells[0] == "" {
		return false
	}
	last := cells[0][len(cells[0])-1]
	return last >= '0' && last <= '9'
}

// printRow writes one line back the way free lays it out: the label, colon and
// all, in a field of eight, then every value right-aligned in twelve. That is
// the grid free itself prints on, so a line nothing was changed about comes
// back identical, and a value shortened by a unit stays in its own column
// instead of dragging the ones to its right along with it.
//
// A value wider than its field is separated by a space rather than pushed
// against the one before it, which is what free does for the very long numbers
// `free -v` prints. The line is written without its newline, so a table free
// did not end with a newline does not gain one.
func printRow(w *bufio.Writer, label string, cells []string, color string) {
	if label != "" {
		label += ":"
	}
	if len(label) < 8 {
		label += strings.Repeat(" ", 8-len(label))
	}
	w.WriteString(label)
	for i, c := range cells {
		if len(c) >= 12 {
			w.WriteString(" " + c)
			continue
		}
		pad := strings.Repeat(" ", 12-len(c))
		if i == 1 && color != "" {
			w.WriteString(pad + tool.PaintColor(color, c))
			continue
		}
		w.WriteString(pad + c)
	}
}

// fill tints the used column by how much of the row's own total is gone. free
// defines used as total minus available, so this is the percentage a reader
// would work out anyway — only shown rather than calculated, and only as color
// so the numbers stay the numbers.
//
// Memory sits in the tens of percent on any healthy machine, so the thresholds
// are higher than a disk's: 80 to warn, 90 to say so plainly.
func fill(cells []string) string {
	if len(cells) < 2 {
		return ""
	}
	total, ok := quantity(cells[0])
	if !ok || total <= 0 {
		return ""
	}
	used, ok := quantity(cells[1])
	if !ok {
		return ""
	}
	switch ratio := used / total; {
	case ratio >= 0.9:
		return red
	case ratio >= 0.8:
		return yellow
	}
	return ""
}

// quantity reads a free value as a number of bytes. The bare form counts
// kibibytes and the human form carries its own suffix ("7.6Gi", "318Mi"), and
// only the ratio between two values from the same row is used, so the unit has
// to be right for both and wrong for neither.
func quantity(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	i := 0
	for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
		i++
	}
	if i == 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0, false
	}
	switch s[i:] {
	case "", "K", "Ki":
		return v * 1024, true
	case "M", "Mi":
		return v * 1024 * 1024, true
	case "G", "Gi":
		return v * 1024 * 1024 * 1024, true
	case "T", "Ti":
		return v * 1024 * 1024 * 1024 * 1024, true
	case "P", "Pi":
		return v * 1024 * 1024 * 1024 * 1024 * 1024, true
	case "B":
		return v, true
	}
	return 0, false
}

// human renders a count of kibibytes the way free's own -h does: the largest
// unit that fits, one decimal below ten, and none above it. 8004464 comes out
// as 7.6G, which is what `free -h` calls 7.6Gi.
//
// It truncates rather than rounds, as free does, so `parse free` and
// `free -h` print the same number for the same machine.
func human(s string) string {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}
	units := []string{"K", "M", "G", "T", "P", "E"}
	i := 0
	// 1023.5 rather than 1024 so a value just under a unit does not come back
	// as "1024M" after it is printed.
	for v >= 1023.5 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if v >= 10 {
		return strconv.FormatFloat(math.Trunc(v), 'f', 0, 64) + units[i]
	}
	one := strconv.FormatFloat(math.Round(v*10)/10, 'f', 1, 64)
	return strings.TrimSuffix(one, ".0") + units[i]
}
