// Package findmnt colors findmnt output. findmnt has already lined every
// column up, so parse moves none of them. What the table needs is color: the
// tree that says which mount sits under which is structure rather than data,
// the options column is the widest thing on the screen and says almost the
// same thing on every row, and two facts deserve to stand out — which
// filesystems are on a disk, and which are mounted read only.
package findmnt

import (
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/atif-1402/parse/internal/tool"
)

const (
	red    = tool.Red
	yellow = tool.Yellow
	cyan   = tool.Cyan
	dim    = tool.Dim
)

// Run implements `parse findmnt <args>`.
func Run(args []string) int {
	if !capturable(args) {
		return tool.Passthrough("findmnt", args)
	}
	text, code, started := tool.Capture("findmnt", args)
	if !started {
		return code
	}
	Format(os.Stdout, text)
	return code
}

// capturable reports whether findmnt's output can be collected in full and
// formatted afterwards. Machine forms must not be rewritten at all, and a
// listing that never ends would never reach the formatter: Capture waits for
// the tool to exit, and `findmnt --poll` does not. Such a command line is
// handed the terminal as it is instead, which is what keeps the events
// streaming by in findmnt's own colors.
func capturable(args []string) bool {
	return !scriptForm(args) && !NeedsTerminal(args)
}

// Pipe formats piped findmnt output.
func Pipe(w io.Writer, text string) {
	Format(w, text)
}

// NeedsTerminal reports whether args ask findmnt to watch the mount table and
// print as it goes. `findmnt --poll` never finishes, so it is handed the
// terminal as it is rather than captured block by block or sat behind a pager.
func NeedsTerminal(args []string) bool { return polls(args) }

// scriptForm reports whether args asked for a form scripts read: JSON, the
// shell-friendly pairs, and the raw table, which separates its fields with one
// space and no padding at all. None of them is a table parse may line up, so
// none of them is captured: passthrough keeps the bytes a script counted on.
func scriptForm(args []string) bool {
	for _, a := range args {
		switch a {
		case "-J", "--json", "-P", "--pairs", "-y", "--shell", "-r", "--raw":
			return true
		}
	}
	return false
}

// polls reports whether args ask findmnt to keep printing. The short form
// takes an optional list and the long one an optional `=`, and `-p` shares no
// prefix with any other option findmnt has, so an exact match is enough.
func polls(args []string) bool {
	for _, a := range args {
		if a == "-p" || strings.HasPrefix(a, "--poll") {
			return true
		}
	}
	return false
}

// columns are the names findmnt prints as a heading, taken from its own
// `--list-columns`. A line made up entirely of them is the one line that
// introduces every table it prints, in every form but the raw one.
var columns = map[string]bool{
	"TARGET": true, "ACTION": true, "AVAIL": true, "FREQ": true, "FSROOT": true,
	"FSTYPE": true, "FS-OPTIONS": true, "ID": true, "INO.AVAIL": true,
	"INO.TOTAL": true, "INO.USED": true, "INO.USE%": true, "LABEL": true,
	"MAJ:MIN": true, "OLD-OPTIONS": true, "OLD-TARGET": true, "OPTIONS": true,
	"OPT-FIELDS": true, "PARENT": true, "PARTLABEL": true, "PARTUUID": true,
	"PASSNO": true, "PROPAGATION": true, "SIZE": true, "SOURCE": true,
	"SOURCES": true, "TID": true, "UNIQ-ID": true, "USED": true, "USE%": true,
	"UUID": true, "VFS-OPTIONS": true,
}

// Detect reports whether text looks like findmnt output.
//
// findmnt's heading is a row of its own column names, and no other table
// parse knows is made of those names: `df` spells them in lower case, and
// systemctl's are its own. Column names alone are not enough, though: lsblk
// prints FSTYPE, SIZE, UUID and friends too, so a heading made only of the
// names the two tools share is left to pass through untouched. The heading is
// the only thing asked about, because `-n` prints none and leaves the text to
// pass through on its own.
func Detect(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if isHeading(line) {
			return "findmnt"
		}
		return ""
	}
	return ""
}

// lsblkColumns is the shared vocabulary: every name that appears in both
// findmnt's `--list-columns` and lsblk's. A heading spelled only from these
// names is exactly the shape lsblk prints for a column selection of its own
// (`lsblk -o FSTYPE,SIZE`), which findmnt would otherwise claim and paint
// with the wrong palette. The set is closed by construction: any heading
// holding even one name outside it — TARGET, SOURCE, USE%, OPTIONS, any of
// the inode columns — is a table lsblk is incapable of printing, because
// these are all the lsblk columns findmnt shares.
var lsblkColumns = map[string]bool{
	"ID": true, "FSTYPE": true, "LABEL": true, "MAJ:MIN": true,
	"PARTLABEL": true, "PARTUUID": true, "SIZE": true, "UUID": true,
}

// isHeading reports whether line is findmnt's column heading.
//
// Every column must be one of findmnt's own, and not all of them may belong
// to the shared vocabulary above: a table findmnt and lsblk could both print
// belongs to neither, and the text passes through as it came in.
func isHeading(line string) bool {
	f := strings.Fields(line)
	if len(f) == 0 {
		return false
	}
	shared := true
	for _, c := range f {
		if !columns[c] {
			return false
		}
		if !lsblkColumns[c] {
			shared = false
		}
	}
	return !shared
}

// Format gives findmnt's rows their color and prints them back exactly where
// findmnt put them. The heading is bolded where it already stands, so no
// column ends up above a different one.
func Format(w io.Writer, text string) {
	lines := strings.Split(text, "\n")

	head := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if isHeading(line) {
			head = i
		}
		break
	}
	// No heading means no columns to read by.
	if head < 0 {
		io.WriteString(w, text)
		return
	}

	names := strings.Fields(lines[head])
	starts := columnStarts(lines, head, len(names))
	if starts == nil {
		// The fields are not padded into columns at all — raw output
		// separates them with one space — so no cell of it can be found, and
		// the text is printed as it came in.
		io.WriteString(w, text)
		return
	}

	for i, line := range lines {
		if i > 0 {
			io.WriteString(w, "\n")
		}
		switch {
		case i == head:
			io.WriteString(w, paintHeading(line))
		case strings.TrimSpace(line) == "":
			io.WriteString(w, line)
		default:
			io.WriteString(w, row(line, names, starts))
		}
	}
}

// columnStarts is where each of the table's columns begins, or nil when the
// lines do not form a table at all.
//
// The boundary between two columns is a run of positions that is a space in
// every line of the table at once, and there is no other kind: a position
// inside a column is protected by the heading's own name, which fills the
// start of the column, and no row can have its gap in the same place as
// another's unless the gap really is between columns. A column right-aligns
// its values (`SIZE`, `USE%`) while its heading sits at the right edge with
// them, so this finds the leftmost value of the column, which is the edge a
// reader's eye uses.
//
// Positions are counted in characters, not bytes, because that is the unit
// findmnt lines its columns up by: the branch it draws in front of a target
// is one column wide however many bytes it takes to spell it.
func columnStarts(lines []string, head, count int) []int {
	width := 0
	space := map[int]bool{}
	for _, line := range lines[head:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		rs := []rune(strings.TrimRight(line, " "))
		if len(rs) > width {
			width = len(rs)
			for p := 0; p < width; p++ {
				if _, seen := space[p]; !seen {
					space[p] = true
				}
			}
		}
		for p, r := range rs {
			if r != ' ' {
				space[p] = false
			}
		}
	}

	starts := []int{0}
	for p := 0; p < width; p++ {
		if space[p] {
			// A gutter: the column after it begins where the spaces stop.
			for p < width && space[p] {
				p++
			}
			if p >= width {
				return nil
			}
			starts = append(starts, p)
		}
	}
	if len(starts) != count {
		return nil
	}
	return starts
}

// paintHeading bolds the column names without moving any of them: the padding
// between them is written as it came in, and only the words are painted.
func paintHeading(line string) string {
	rs := []rune(line)
	var sb strings.Builder
	at := 0
	for _, t := range words(rs) {
		sb.WriteString(string(rs[at:t[0]]))
		sb.WriteString(tool.PaintColor(tool.Bold, string(rs[t[0]:t[1]])))
		at = t[1]
	}
	sb.WriteString(string(rs[at:]))
	return sb.String()
}

// words is every whitespace-separated run in rs, with its offsets.
func words(rs []rune) [][2]int {
	var out [][2]int
	at := 0
	for at < len(rs) {
		for at < len(rs) && (rs[at] == ' ' || rs[at] == '\t') {
			at++
		}
		start := at
		for at < len(rs) && rs[at] != ' ' && rs[at] != '\t' {
			at++
		}
		if at > start {
			out = append(out, [2]int{start, at})
		}
	}
	return out
}

// mark is a span of one line, in characters, and the color it is painted with.
type mark struct {
	start, end int
	color      string
}

// row colors one line of findmnt's output. Each cell is found by the column
// the heading puts it under, so a mountpoint holding a space and a value
// pushed to the right of its column change nothing.
func row(line string, names []string, starts []int) string {
	rs := []rune(line)
	var marks []mark
	for i, name := range names {
		if starts[i] >= len(rs) {
			continue
		}
		end := len(rs)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		if end > len(rs) {
			end = len(rs)
		}
		cell := rs[starts[i]:end]
		lead, tail := 0, len(cell)
		for lead < tail && cell[lead] == ' ' {
			lead++
		}
		for tail > lead && cell[tail-1] == ' ' {
			tail--
		}
		if lead == tail {
			continue
		}
		marks = append(marks, cellMarks(name, starts[i]+lead, string(cell[lead:tail]), len(names))...)
	}
	sort.Slice(marks, func(i, j int) bool { return marks[i].start < marks[j].start })

	var sb strings.Builder
	at := 0
	for _, m := range marks {
		if m.start < at {
			continue
		}
		sb.WriteString(string(rs[at:m.start]))
		sb.WriteString(tool.PaintColor(m.color, string(rs[m.start:m.end])))
		at = m.end
	}
	sb.WriteString(string(rs[at:]))
	return sb.String()
}

// cellMarks is the color of one named column: the tree inside a target, a
// filesystem's type, the options, and how full the filesystem is.
func cellMarks(name string, start int, text string, columns int) []mark {
	switch {
	case name == "TARGET":
		if end := treeEnd(text); end > 0 {
			return []mark{{start, start + end, dim}}
		}
	case name == "FSTYPE":
		if color := fsColor(text); color != "" {
			return []mark{{start, start + len([]rune(text)), color}}
		}
	case strings.HasSuffix(name, "OPTIONS") && columns > 1:
		// The options are the widest column on the screen and repeat
		// themselves on nearly every row, so every spelling of them — OPTIONS,
		// FS-OPTIONS, VFS-OPTIONS — is quietened, except for the one option
		// worth noticing, a filesystem mounted read only.
		if text == "ro" || strings.HasPrefix(text, "ro,") {
			return []mark{
				{start, start + len([]rune("ro")), yellow},
				{start + len([]rune("ro")), start + len([]rune(text)), dim},
			}
		}
		return []mark{{start, start + len([]rune(text)), dim}}
	case strings.HasSuffix(name, "USE%"):
		if color := percentColor(text); color != "" {
			return []mark{{start, start + len([]rune(text)), color}}
		}
	}
	return nil
}

// treeEnd is the length of the drawing at the start of a target: the branch
// findmnt has printed to show which mount this one hangs from. Both the box
// drawing and the `--ascii` spellings are covered, along with the spaces
// between them.
func treeEnd(s string) int {
	rs := []rune(s)
	at := 0
	for at < len(rs) {
		switch rs[at] {
		case '\u2502', '\u251c', '\u2514', '\u2500', '|', '`', '-', ' ':
			at++
		default:
			return at
		}
	}
	return at
}

// fsColor names a filesystem by where its data lives: a type backed by a disk
// is cyan, the kernel's own plumbing is dim, and anything else — a network
// share, an overlay, a type this list has never heard of — keeps its color,
// because being unsure is not the same as being noise.
func fsColor(s string) string {
	switch s {
	case "btrfs", "ext2", "ext3", "ext4", "f2fs", "xfs", "zfs", "bcachefs",
		"vfat", "exfat", "ntfs", "ntfs3", "reiserfs", "jfs", "nilfs2",
		"hfs", "hfsplus", "minix", "udf", "iso9660", "squashfs", "erofs":
		return cyan
	case "proc", "sysfs", "devtmpfs", "devpts", "ramfs", "tmpfs", "cgroup",
		"cgroup2", "securityfs", "debugfs", "tracefs", "pstore", "bpf",
		"configfs", "fusectl", "autofs", "binfmt_misc", "mqueue", "hugetlbfs",
		"efivarfs", "binderfs", "nsfs", "rpc_pipefs", "selinuxfs":
		return dim
	}
	return ""
}

// percentColor is how full a filesystem is, at the thresholds `parse free`
// uses for the same number: 80% and over says so in yellow, 90% in red.
func percentColor(s string) string {
	if !strings.HasSuffix(s, "%") {
		return ""
	}
	n, err := strconv.Atoi(strings.TrimSuffix(s, "%"))
	if err != nil {
		return ""
	}
	switch {
	case n >= 90:
		return red
	case n >= 80:
		return yellow
	}
	return ""
}
