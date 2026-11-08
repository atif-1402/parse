// Package mount formats the plain mount table. mount prints one line per
// mount with no alignment at all: the options are the widest thing on the
// screen and say almost the same thing on every row, and the filesystem
// type — the one word that says what this mount actually is — sits buried
// between "on" and "type". parse lines the rows up into a table instead:
// a column each for the device, the mountpoint, the filesystem type and
// the options, the type colored by where its data lives, the options
// quietened, and a read-only mount marked so it stands out.
package mount

import (
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/atif-1402/parse/internal/tool"
)

const (
	bold   = tool.Bold
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

// Format lines mount's rows up into a table: a heading, then one row per
// mount with a column each for the device, the mountpoint, the filesystem
// type and the options. Every field mount printed survives — only the
// words "on" and "type" and the padding are gone.
//
// When some line is not one of mount's rows, the table is abandoned and
// each line is painted on its own instead: an error that leaked onto
// stdout is never lined up as though it were a mount.
func Format(w io.Writer, text string) {
	lines := strings.Split(text, "\n")
	if last := len(lines) - 1; lines[last] == "" {
		lines = lines[:last]
	}
	rows := make([]mrow, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		r, ok := parseRow(line)
		if !ok {
			paintPlain(w, text)
			return
		}
		rows = append(rows, r)
	}
	if len(rows) == 0 {
		io.WriteString(w, text)
		return
	}
	writeTable(w, rows)
}

// mrow is one line of mount's output taken apart: everything the reader
// needs, with the glue words dropped and the `mount -l` label kept.
type mrow struct {
	dev, mnt, fs, opts, label string
}

// parseRow takes one line of mount's output apart, or reports that it is
// not one of mount's rows.
func parseRow(line string) (mrow, bool) {
	m := reMount.FindStringSubmatch(line)
	if m == nil {
		return mrow{}, false
	}
	return mrow{
		dev:   m[1],
		mnt:   m[2],
		fs:    m[3],
		opts:  m[4],
		label: strings.TrimSpace(m[5]),
	}, true
}

// cells is the row as columns. The label column only exists when some row
// actually has a label, so a plain `mount` never grows an empty one.
func (r mrow) cells(withLabel bool) []string {
	if withLabel {
		return []string{r.dev, r.mnt, r.fs, r.opts, r.label}
	}
	return []string{r.dev, r.mnt, r.fs, r.opts}
}

// writeTable prints the heading and one line per mount. Each column is as
// wide as its widest cell, two spaces sit between columns, and the last
// column is never padded, so no line ends in spaces.
func writeTable(w io.Writer, rows []mrow) {
	withLabel := false
	for _, r := range rows {
		if r.label != "" {
			withLabel = true
			break
		}
	}
	headers := []string{"DEVICE", "MOUNTPOINT", "FSTYPE", "OPTIONS"}
	if withLabel {
		headers = append(headers, "LABEL")
	}

	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len([]rune(h))
	}
	for _, r := range rows {
		for i, c := range r.cells(withLabel) {
			if n := len([]rune(c)); n > widths[i] {
				widths[i] = n
			}
		}
	}

	var sb strings.Builder
	writeCells(&sb, headers, widths, func(_ int, s string) string {
		return tool.PaintColor(bold, s)
	})
	for _, r := range rows {
		writeCells(&sb, r.cells(withLabel), widths, paintCell)
	}
	io.WriteString(w, sb.String())
}

// writeCells writes one line: every cell painted by paint and padded to
// its column, two spaces between columns and none after the last.
func writeCells(sb *strings.Builder, cells []string, widths []int, paint func(col int, s string) string) {
	for i, c := range cells {
		if i > 0 {
			sb.WriteString("  ")
		}
		sb.WriteString(paint(i, c))
		if i < len(cells)-1 {
			if pad := widths[i] - len([]rune(c)); pad > 0 {
				sb.WriteString(strings.Repeat(" ", pad))
			}
		}
	}
	sb.WriteString("\n")
}

// paintCell is the color of one column: the filesystem type by where its
// data lives and the options quietened with a read-only one marked. The
// device, the mountpoint and the label are the row's data and stay exactly
// as mount printed them.
func paintCell(col int, s string) string {
	switch col {
	case 2:
		if color := tool.FsColor(s); color != "" {
			return tool.PaintColor(color, s)
		}
	case 3:
		return paintOptions(s)
	}
	return s
}

// paintPlain is the fallback for text some line of which is not a mount
// row: every row is painted where it stands and anything else comes back
// untouched, so a claimed text is never half a table and half a lie.
func paintPlain(w io.Writer, text string) {
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			io.WriteString(w, "\n")
		}
		io.WriteString(w, row(line))
	}
}

// row colors one line of the mount table in place: the filesystem type by
// where its data lives, the options quietened with a read-only one marked.
// Everything else comes back byte for byte.
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
