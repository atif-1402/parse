// Package mount formats the plain mount table. mount prints one line per
// mount with no alignment at all: the options are the widest thing on the
// screen and say almost the same thing on every row, and the filesystem
// type — the one word that says what this mount actually is — sits buried
// between "on" and "type". parse moves no bytes: the device and the
// mountpoint keep their places as the row's anchor, the filesystem type is
// colored by where its data lives, the options are quietened, and a
// read-only mount is marked so it stands out.
package mount

import (
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/atif-1402/parse/internal/tool"
)

const (
	yellow = tool.Yellow
	dim    = tool.Dim
)

// reMount is one row of mount's table: the device, the word "on", the
// mountpoint, the word "type", the filesystem type, and the options in
// brackets. The mountpoint may hold spaces, because a share mounted at
// "/mnt/Backup Share" is real, and the optional tail at the end is the
// label `mount -l` prints after the closing bracket.
var reMount = regexp.MustCompile(`^(\S+) on (.+) type (\S+) \((.*)\)( .*)?$`)

// Run implements `parse mount <args>`.
func Run(args []string) int {
	text, code, started := tool.Capture("mount", args)
	if !started {
		return code
	}
	Format(os.Stdout, text)
	return code
}

// Pipe formats piped mount output.
func Pipe(w io.Writer, text string) {
	Format(w, text)
}

// Detect reports whether text looks like the plain mount table. The row
// shape — `device on /path type fs (options)` — is mount's own: findmnt
// opens with a heading of column names, df's rows carry neither " on " nor
// " type " nor brackets, and no log parse knows prints brackets at the end
// of every line. Only the first line with something on it is asked about,
// because that is the shape every row of the table shares.
func Detect(text string) string {
	first := tool.FirstNonEmpty(strings.Split(text, "\n"))
	if reMount.MatchString(first) {
		return "mount"
	}
	return ""
}

// Format paints mount's rows and prints them back exactly where mount put
// them. A line that is not one of mount's rows — an error that leaked
// onto stdout, a label in a shape this regex never met — is written back
// untouched, so a claimed text is never half painted and half wrong.
func Format(w io.Writer, text string) {
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			io.WriteString(w, "\n")
		}
		io.WriteString(w, row(line))
	}
}

// row colors one line of the mount table. The line is rebuilt from the
// regex's spans, so everything outside the filesystem type and the
// options — device, " on ", mountpoint, " type ", brackets, label — comes
// back byte for byte.
func row(line string) string {
	idx := reMount.FindStringSubmatchIndex(line)
	if idx == nil {
		return line
	}
	fStart, fEnd := idx[6], idx[7]
	oStart, oEnd := idx[8], idx[9]

	var sb strings.Builder
	sb.WriteString(line[:fStart])
	if color := tool.FsColor(line[fStart:fEnd]); color != "" {
		sb.WriteString(tool.PaintColor(color, line[fStart:fEnd]))
	} else {
		sb.WriteString(line[fStart:fEnd])
	}
	sb.WriteString(line[fEnd:oStart])
	sb.WriteString(paintOptions(line[oStart:oEnd]))
	sb.WriteString(line[oEnd:])
	return sb.String()
}

// paintOptions quiets the options, which repeat on nearly every row, and
// marks the one worth noticing: a filesystem mounted read only, spelled
// the same way findmnt marks its OPTIONS column.
func paintOptions(opts string) string {
	if opts == "ro" || strings.HasPrefix(opts, "ro,") {
		if rest := opts[len("ro"):]; rest != "" {
			return tool.PaintColor(yellow, "ro") + tool.PaintColor(dim, rest)
		}
		return tool.PaintColor(yellow, "ro")
	}
	return tool.PaintColor(dim, opts)
}
