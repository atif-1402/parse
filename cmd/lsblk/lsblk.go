// Package lsblk formats lsblk output. The device tree is drawn with box
// characters that carry no information, and a device with several mountpoints
// prints them on their own lines, aligned to the column but with nothing tying
// them back to the device above.
package lsblk

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

// Run implements `parse lsblk <args>`.
func Run(args []string) int {
	for _, a := range args {
		if a == "-J" || a == "--json" || a == "-O" || a == "--output" || a == "-P" || a == "--pairs" {
			// machine-readable forms must stay byte-identical
			return tool.Passthrough("lsblk", args)
		}
		if strings.HasPrefix(a, "--json") || strings.HasPrefix(a, "--output") {
			return tool.Passthrough("lsblk", args)
		}
	}
	text, code, started := tool.Capture("lsblk", args)
	if !started {
		return code
	}
	Format(os.Stdout, text)
	return code
}

// Pipe formats piped lsblk output.
func Pipe(w io.Writer, text string) {
	Format(w, text)
}

// headerRE matches the lsblk header.
var headerRE = regexp.MustCompile(`^NAME\s+MAJ:MIN`)

// Detect reports whether text looks like lsblk output.
func Detect(text string) string {
	for _, line := range strings.SplitN(text, "\n", 4) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if headerRE.MatchString(line) {
			return "lsblk"
		}
		return ""
	}
	return ""
}

// treeChars are the box-drawing characters lsblk uses to draw the device
// tree. They are dimmed rather than removed so the shape still reads.
const treeChars = "├└─│"

// Format writes the lsblk table with dimmed tree characters and mountpoints
// indented under the device they belong to.
func Format(w io.Writer, text string) {
	out := bufio.NewWriter(w)
	defer out.Flush()

	matched := false
	// nameWidth is where the NAME column ends, taken from the header so the
	// continuation lines for mountpoints can be indented under their device.
	nameWidth := 10
	tool.EachLine(text, func(line string) {
		if strings.TrimSpace(line) == "" {
			return
		}
		if headerRE.MatchString(line) {
			matched = true
			if w := len(strings.Fields(line)[0]); w > nameWidth {
				nameWidth = w
			}
			fmt.Fprintln(out, tool.PaintColor(bold, dimTree(line)))
			return
		}
		matched = true
		printRow(out, line, nameWidth)
	})
	if !matched {
		io.WriteString(out, text)
	}
}

// printRow prints one device, or a wrapped mountpoint line belonging to the
// device above it.
func printRow(out *bufio.Writer, line string, nameWidth int) {
	if isContinuation(line) {
		// A mountpoint with no device of its own: indent it under the device
		// it belongs to instead of leaving it floating at the column edge.
		fmt.Fprintf(out, "%s%s\n", strings.Repeat(" ", nameWidth+1), tool.PaintColor(cyan, line))
		return
	}
	fmt.Fprintln(out, colorRow(line))
}

// isContinuation reports whether a line is a wrapped mountpoint: it starts at
// the mountpoint column and holds no device fields.
func isContinuation(line string) bool {
	if strings.ContainsAny(line, treeChars) {
		return false
	}
	f := strings.Fields(line)
	if len(f) == 0 {
		return false
	}
	// A device row always begins with a tree character or a bare name; a
	// continuation begins with an absolute mount path.
	return strings.HasPrefix(f[0], "/")
}

// colorRow tints the device row: tree characters dim, TYPE colored, device
// name bold.
func colorRow(line string) string {
	// dim the tree characters first, then color what remains
	out := strings.Builder{}
	for _, r := range line {
		if strings.ContainsRune(treeChars, r) {
			out.WriteString(tool.PaintColor(dim, string(r)))
			continue
		}
		out.WriteRune(r)
	}
	return colorFields(out.String())
}

// colorFields tints the TYPE column and the device name.
func colorFields(line string) string {
	// The device name is the first field, tree characters included.
	trimmed := strings.TrimLeft(line, treeChars)
	lead := line[:len(line)-len(trimmed)]
	if trimmed == "" {
		return line
	}
	name, rest := trimmed, ""
	if i := strings.IndexAny(trimmed, " \t"); i >= 0 {
		name, rest = trimmed[:i], trimmed[i:]
	}
	return lead + tool.PaintColor(bold, name) + typeColor(rest)
}

// typeColor tints the TYPE value in place, keeping the original spacing so
// the columns stay aligned.
func typeColor(rest string) string {
	for _, t := range []struct{ name, color string }{
		{"disk", bold},
		{"part", dim},
		{"crypt", cyan},
		{"lvm", cyan},
		{"raid1", cyan},
		{"rom", red},
	} {
		rest = replaceWord(rest, t.name, t.color)
	}
	return rest
}

// replaceWord colors one whole word. It only matches a word bounded by
// whitespace, so "disk" never colors the "disk" inside a longer device name.
func replaceWord(s, word, color string) string {
	var out strings.Builder
	for _, f := range strings.SplitAfter(s, " ") {
		trimmed := strings.TrimRight(f, " ")
		if trimmed == word {
			out.WriteString(tool.PaintColor(color, word))
			out.WriteString(strings.Repeat(" ", len(f)-len(trimmed)))
			continue
		}
		out.WriteString(f)
	}
	return out.String()
}

// dimTree dims the tree characters in the header row too, for symmetry.
func dimTree(line string) string {
	out := strings.Builder{}
	for _, r := range line {
		if strings.ContainsRune(treeChars, r) {
			out.WriteString(tool.PaintColor(dim, string(r)))
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}
