// Package du formats du output. Two things make du hard to read: the default
// form prints raw block counts with no unit, so 22972 has to be read as
// "kilobytes" from memory, and --max-depth prints parents and children in one
// flat list with nothing showing which is which.
package du

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

// Run implements `parse du <args>`.
func Run(args []string) int {
	if tool.OutputMode(args) != "" || hasFlag(args, "--bytes") {
		// -b and --apparent-size are asked for in machine terms on purpose.
		return tool.Passthrough("du", args)
	}
	text, code, started := tool.Capture("du", args)
	if !started {
		return code
	}
	Format(os.Stdout, text)
	return code
}

// Pipe formats piped du output.
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

// duRE matches "22972\tpath/" and the human form "23M\tpath/". The size is
// either a plain block count or a suffixed number.
//
// The gap after the size is deliberately restricted to a tab or two or more
// spaces. du right-aligns the size in a column and starts the path below it, so
// it always leaves that much space, whereas ordinary prose uses a single space.
// Matching on one space made du claim text that had nothing to do with it and
// rewrite it: "3 days ago" came out as "3K days ago".
//
// The path may not be a number followed by a tab. That rules out git's
// --numstat, whose rows are three tab-separated numbers, "3\t2\tcmd/main.go".
// See numstatRE below.
var duRE = regexp.MustCompile(`^\s*([0-9.]+)([KMGTPE]?)(\t+|  +)(.*)$`)

// numstatRE matches a git --numstat row, or any line whose second
// tab-separated field is a bare count. du used to claim these and rewrite the
// counts as sizes, so "3\t2\tcmd/main.go" came out as "3K  2\tcmd/main.go",
// which silently destroyed the added/removed numbers the reader came for.
//
// du is run before git on purpose (its shape is more specific), so the only way
// to keep --numstat intact is for du to decline it here. A du path that is a
// bare number followed by a tab is not a thing du produces.
var numstatRE = regexp.MustCompile(`^\s*[0-9.]+\t(?:[0-9]+\t|-[0-9]+\t|$)`)

// Detect reports whether text looks like du output.
func Detect(text string) string {
	for _, line := range strings.SplitN(text, "\n", 4) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if numstatRE.MatchString(line) {
			return ""
		}
		if duRE.MatchString(line) {
			return "du"
		}
		return ""
	}
	return ""
}

// Format writes du output with human units and, for --max-depth, children
// indented under the parent they belong to.
func Format(w io.Writer, text string) {
	out := bufio.NewWriter(w)
	defer out.Flush()

	matched := false
	tool.EachLine(text, func(line string) {
		if strings.TrimSpace(line) == "" {
			return
		}
		m := duRE.FindStringSubmatch(line)
		if m == nil {
			fmt.Fprintln(out, line)
			return
		}
		matched = true
		printRow(out, m)
	})
	if !matched {
		io.WriteString(out, text)
	}
}

// printRow prints one entry. du marks a directory with a trailing slash and
// indents the children it recursed into, so the hierarchy is already in the
// text; parse keeps that indent and makes the size the thing you see first.
func printRow(w *bufio.Writer, m []string) {
	size, suffix, path := m[1], m[2], m[4]

	isDir := strings.HasSuffix(path, "/")
	name := strings.TrimSuffix(path, "/")
	if isDir {
		name = tool.PaintColor(bold, name) + "/"
	}

	text := humanSize(size, suffix)
	fmt.Fprintf(w, "%s  %s\n", tool.PaintColor(magnitudeColor(text), padLeft(text, 6)), name)
}

// magnitudeColor highlights the entries a reader is looking for. The
// thresholds are in bytes; parseSize returns bytes, so they line up.
func magnitudeColor(s string) string {
	v := parseSize(s)
	switch {
	case v >= 1024*1024*1024:
		return red
	case v >= 100*1024*1024:
		return yellow
	case v >= 10*1024*1024:
		return cyan
	}
	return ""
}

// humanSize renders a size with a unit. du's plain form is in 1K blocks by
// default, so a bare number is kilobytes unless the caller asked otherwise.
func humanSize(size, suffix string) string {
	v, err := strconv.ParseFloat(size, 64)
	if err != nil {
		return size
	}
	switch suffix {
	case "":
		// plain du counts 1K blocks unless -b was passed, which we do not format
		return fmt.Sprintf("%.0fK", v)
	case "K", "M", "G", "T", "P", "E":
		return trimZero(size) + suffix
	}
	return size
}

// trimZero drops a trailing zero decimal, so 1.5G does not print as 1.50G.
func trimZero(s string) string {
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimSuffix(s, ".")
	}
	return s
}

// parseSize converts a rendered size back to bytes. The human form carries its
// unit ("492M"); the bare form is du's default, which is 1K blocks.
func parseSize(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimRight(s, "KMGTPE"), 64)
	if err != nil {
		return 0
	}
	switch {
	case strings.HasSuffix(s, "K"):
		return v * 1024
	case strings.HasSuffix(s, "M"):
		return v * 1024 * 1024
	case strings.HasSuffix(s, "G"):
		return v * 1024 * 1024 * 1024
	case strings.HasSuffix(s, "T"):
		return v * 1024 * 1024 * 1024 * 1024
	}
	// no unit: du counted 1K blocks
	return v * 1024
}

func padLeft(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return strings.Repeat(" ", n-len(s)) + s
}
