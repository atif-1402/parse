// Package find formats find output. Bare `find` prints one path per line with
// no columns at all, which parse cannot improve. The form worth fixing is
// `find -ls`, whose eleven positional fields force the reader to count columns
// to find the size or the owner.
package find

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

// Run implements `parse find <args>`.
//
// Only -ls is formatted. Bare find and -printf already produce whatever shape
// the reader asked for, and -printf formats itself.
func Run(args []string) int {
	if !hasLs(args) || hasPrintf(args) {
		return tool.Passthrough("find", args)
	}
	text, code, started := tool.Capture("find", args)
	if !started {
		return code
	}
	Format(os.Stdout, text)
	return code
}

// Pipe formats piped find output.
func Pipe(w io.Writer, text string) {
	Format(w, text)
}

func hasLs(args []string) bool {
	for _, a := range args {
		if a == "-ls" || a == "-lsa" || strings.HasPrefix(a, "-ls") {
			return true
		}
	}
	return false
}

func hasPrintf(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "-printf") || strings.HasPrefix(a, "--printf") {
			return true
		}
	}
	return false
}

// lsRE matches one `find -ls` row: inode, block count, mode, links, owner,
// group, size, date, then the path. The path may contain spaces, so it is
// everything after the date.
var lsRE = regexp.MustCompile(`^\s*(\d+)\s+(\d+)\s+(.+?)\s+(\d+)\s+(\S+)\s+(\S+)\s+(\d+)\s+(\S+\s+\S+\s+\S+)\s+(.*)$`)

// Detect reports whether text looks like `find -ls` output.
func Detect(text string) string {
	for _, line := range strings.SplitN(text, "\n", 6) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if lsRE.MatchString(line) {
			return "ls"
		}
		return ""
	}
	return ""
}

// Format writes the eleven positional fields as a named table.
func Format(w io.Writer, text string) {
	out := bufio.NewWriter(w)
	defer out.Flush()

	matched := false
	tool.EachLine(text, func(line string) {
		if strings.TrimSpace(line) == "" {
			return
		}
		m := lsRE.FindStringSubmatch(line)
		if m == nil {
			fmt.Fprintln(out, line)
			return
		}
		if !matched {
			printHeader(out)
		}
		matched = true
		printRow(out, m)
	})
	if !matched {
		io.WriteString(out, text)
	}
}

// printRow prints one file. MODE is expanded to the familiar rwxr-xr-x form
// because the raw mode's leading type character is what makes it unreadable.
func printRow(w *bufio.Writer, m []string) {
	inode, links, mode, owner, group, size, date, path := m[1], m[4], m[3], m[5], m[6], m[7], m[8], m[9]

	fmt.Fprintln(w,
		tool.PaintColor(dim, padLeft(inode, 7))+"  ",
		tool.PaintColor(dim, padLeft(links, 5))+"  ",
		tool.PaintColor(bold+" "+modeColor(mode), padRight(mode, 11))+"  ",
		tool.PaintColor(cyan, padRight(owner, 8))+"  ",
		tool.PaintColor(cyan, padRight(group, 8))+"  ",
		tool.PaintColor(magnitudeColor(size), padLeft(humanSize(size), 7))+"  ",
		tool.PaintColor(dim, padRight(date, 16))+"  ",
		pathColor(mode, path))
}

// printHeader prints the column names.
func printHeader(w *bufio.Writer) {
	fmt.Fprintln(w,
		tool.PaintColor(bold, padLeft("INODE", 7))+"  ",
		tool.PaintColor(bold, padLeft("LINKS", 5))+"  ",
		tool.PaintColor(bold, padRight("MODE", 11))+"  ",
		tool.PaintColor(bold, padRight("OWNER", 8))+"  ",
		tool.PaintColor(bold, padRight("GROUP", 8))+"  ",
		tool.PaintColor(bold, padLeft("SIZE", 7))+"  ",
		tool.PaintColor(bold, padRight("MODIFIED", 16))+"  ",
		tool.PaintColor(bold, "PATH"))
}

// expandMode turns "-rw-r--r--" into "rw-r--r--", dropping the leading type
// character, which expandModeColor still uses to tell files from directories.
func expandMode(mode string) string {
	if len(mode) > 0 && !strings.ContainsAny(mode[:1], "-dlbcps") {
		return mode
	}
	if len(mode) > 1 {
		return mode[1:]
	}
	return mode
}

// modeColor tints the type so directories and executables stand out.
func modeColor(mode string) string {
	switch {
	case strings.HasPrefix(mode, "d"):
		return cyan
	case isExecutable(mode):
		return green
	}
	return ""
}

// isExecutable reports whether any execute bit is set.
func isExecutable(mode string) bool {
	if len(mode) < 10 {
		return false
	}
	return strings.Contains(mode[3:10], "x")
}

// pathColor bolds directories, greens executables and leaves the rest alone.
func pathColor(mode, path string) string {
	switch {
	case strings.HasPrefix(mode, "d"):
		return tool.PaintColor(cyan, path)
	case isExecutable(mode):
		return tool.PaintColor(green, path)
	}
	return path
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

// magnitudeColor highlights big files so they are easy to spot in a long list.
func magnitudeColor(size string) string {
	v, err := parseInt(size)
	if err != nil {
		return ""
	}
	switch {
	case v >= 100*1024*1024:
		return yellow
	case v >= 10*1024*1024:
		return cyan
	}
	return ""
}

// humanSize renders a byte count the way ls -h does, so sizes in a find -ls
// listing are comparable at a glance.
func humanSize(size string) string {
	v, err := parseInt(size)
	if err != nil {
		return size
	}
	const unit = 1024
	if v < unit {
		return size
	}
	div, exp := int64(unit), 0
	for n := v / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	suffixes := []string{"K", "M", "G", "T", "P"}
	val := float64(v) / float64(div)
	// one decimal, but drop it when it would just be a trailing zero
	s := fmt.Sprintf("%.1f", val)
	if strings.HasSuffix(s, ".0") {
		s = s[:len(s)-2]
	}
	return s + suffixes[exp]
}

func parseInt(s string) (int64, error) {
	var v int64
	_, err := fmt.Sscanf(s, "%d", &v)
	return v, err
}
