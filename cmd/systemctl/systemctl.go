// Package systemctl formats systemctl output. Only the report-like
// subcommands are touched; anything that changes the state of the machine
// runs untouched.
package systemctl

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/atif-1402/parse/cmd/journalctl"
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

// systemctlCommands are the systemctl subcommands parse formats. Everything
// else passes through untouched, which covers both the mutating verbs
// (start, stop, enable, daemon-reload, ...) and the ones that already read
// clearly (show, cat, is-enabled).
var systemctlCommands = map[string]bool{
	"status":            true, // one block of labels, all equally grey today
	"list-units":        true, // five padded columns
	"list-unit-files":   true, // three padded columns
	"is-active":         true, // one word that deserves to be red
	"is-failed":         true,
	"list-dependencies": true, // a recursive tree of unit names
}

// Systemctl implements `parse systemctl <args>`: run systemctl, format its
// output, and return systemctl's exit code.
func Run(args []string) int {
	if !systemctlCommands[systemctlSubcommand(args)] || !formattableSystemctl(args) {
		return tool.Passthrough("systemctl", args)
	}
	data, code, started := tool.Capture("systemctl", args)
	if !started {
		return code
	}

	out := bufio.NewWriter(os.Stdout)
	Format(out, systemctlSubcommand(args), tool.StripANSI(data))
	out.Flush()
	return code
}

// systemctlSubcommand returns the first argument that is not a flag or a flag
// value, so `systemctl --user status foo` resolves to status.
// NeedsTerminal reports whether this invocation needs the terminal untouched,
// which is what keeps the pager out of the way. See cmd.NeedsTerminal.
//
// systemctl watch redraws the state of every unit as it changes, and monitor
// tails journal messages as they arrive. Neither ends on its own, so both are
// handed the terminal the way `journalctl -f` is.
func NeedsTerminal(args []string) bool {
	switch systemctlSubcommand(args) {
	case "watch", "monitor":
		return true
	}
	return false
}

func systemctlSubcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-o" || a == "--output" || a == "--state" || a == "--type" ||
			a == "--pattern" || a == "--job-mode" || a == "--wall" {
			i++ // skip this flag's value
			continue
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		return a
	}
	return ""
}

func formattableSystemctl(args []string) bool {
	if tool.IsMachineOutput(tool.OutputMode(args)) {
		return false
	}
	return !tool.HasLongFlag(args, "--value") // bare values, meant for scripts
}

// ---------------------------------------------------------------------------
// Formatting
// ---------------------------------------------------------------------------

func Format(w io.Writer, sub, text string) {
	switch sub {
	case "status":
		formatUnitStatus(w, text)
	case "list-units":
		formatListUnits(w, text)
	case "list-unit-files":
		formatUnitFiles(w, text)
	case "is-active", "is-failed":
		formatUnitState(w, text)
	case "list-dependencies":
		formatDependencies(w, text)
	}
}

// --- list-dependencies ----------------------------------------------------

// reDependency matches one line of the dependency tree. The bullet and the
// tree characters are leading decoration that carries no information, and the
// indentation is what makes the output a wall.
var reDependency = regexp.MustCompile(`^(\s*)(●\s+)?([├└│─\s]*)(.*)$`)

// formatDependencies dims the tree and the bullet so the unit names are the
// only bright thing left, and colors a unit that is not active.
func formatDependencies(w io.Writer, text string) {
	tool.EachLine(text, func(line string) {
		if strings.TrimSpace(line) == "" {
			return
		}
		indent, bullet, tree, name := "", "", "", ""
		if m := reDependency.FindStringSubmatch(line); m != nil {
			indent, bullet, tree, name = m[1], m[2], m[3], m[4]
		} else {
			name = line
		}
		if strings.TrimSpace(name) == "" {
			fmt.Fprintln(w, line)
			return
		}
		// A unit name carries a type suffix; targets are plain names.
		fmt.Fprintf(w, "%s%s%s%s\n",
			indent,
			tool.PaintColor(dim, bullet),
			tool.PaintColor(dim, tree),
			tool.PaintColor(unitNameColor(name), name))
	})
}

// looksLikeDependencyTree reports whether text is a list-dependencies tree:
// unit names one per line, each indented under the last, with a bullet or a
// tree character in front of it.
func looksLikeDependencyTree(lines []string) bool {
	indented := 0
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if strings.HasPrefix(l, " ") && (strings.Contains(l, "●") || strings.ContainsAny(l, "├└│─")) {
			indented++
		}
	}
	// A dependency tree is at least a root plus a couple of children.
	return indented >= 3
}

// unitNameColor bolds a leaf unit and dims a .target row, so the structure of
// the tree reads without counting the tree characters.
func unitNameColor(name string) string {
	switch {
	case strings.HasSuffix(name, ".target"):
		return bold
	case strings.Contains(name, ".service"), strings.Contains(name, ".socket"),
		strings.Contains(name, ".path"), strings.Contains(name, ".timer"),
		strings.Contains(name, ".device"), strings.Contains(name, ".mount"):
		return cyan
	}
	return ""
}

// --- status ---------------------------------------------------------------

// reStatusHeader matches the leading "● unit.service - description" line.
var reStatusHeader = regexp.MustCompile(`^(●\s+)?(\S+)(?:\s+-\s+(.*))?$`)

// reStatusLabel matches "     Active: active (running) since ...".
var reStatusLabel = regexp.MustCompile(`^(\s+)([A-Za-z][A-Za-z ]*?):(\s+)(.*)$`)

// reStatusTree matches a cgroup child, "├─1066 /usr/lib/at-spi-bus-launcher".
var reStatusTree = regexp.MustCompile(`^(\s*[├└│]─)(\d+)(\s+)(.*)$`)

// reUnitsListed matches the "12 loaded units listed." footer, which systemctl
// indents to line up with the unit column.
var reUnitsListed = regexp.MustCompile(`^\s*\d+ (loaded )?units? listed\.$`)

// statusDimLabels are the labels whose value is reference detail rather than
// the point of the report. Dimming them is the main reason to read a status
// block at all: Active and the description stop competing with a PID and a
// 32-character invocation id.
var statusDimLabels = map[string]bool{
	"Invocation": true, "Tasks": true, "Memory": true, "CPU": true,
	"CGroup": true, "Control PID": true, "TriggeredBy": true,
	"CPUUsage": true, "MemoryCurrent": true, "TasksCurrent": true,
}

func formatUnitStatus(w io.Writer, text string) {
	// An error message is not a report: it has no labels to color, so it is
	// painted red rather than left as an unexplained line.
	if !strings.Contains(text, "Active:") && !strings.Contains(text, "●") && looksLikeSystemctlError(text) {
		fmt.Fprintln(w, tool.PaintColor(red, strings.TrimRight(text, "\n")))
		return
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, line := range lines {
		// The unit header is the first line, or any line starting a new block.
		if i == 0 || strings.HasPrefix(line, "● ") {
			if s, ok := colorStatusHeader(line, activeState(lines, i)); ok {
				fmt.Fprintln(w, s)
				continue
			}
		}
		if s, ok := colorStatusLine(line); ok {
			fmt.Fprintln(w, s)
			continue
		}
		if s, ok := journalctl.ColorLine(line); ok {
			// A failed unit ends with recent log lines, indented to align
			// under the labels. Format them like journal output.
			fmt.Fprintln(w, s)
			continue
		}
		if reUnitsListed.MatchString(line) {
			fmt.Fprintln(w, tool.PaintColor(dim, line))
			continue
		}
		fmt.Fprintln(w, line)
	}
}

// activeState returns the state of the unit block starting at index start, so
// the bullet on the first line can be colored from a fact printed further down.
func activeState(lines []string, start int) string {
	for j := start + 1; j < len(lines); j++ {
		if strings.HasPrefix(lines[j], "● ") {
			break // next unit's block
		}
		m := reStatusLabel.FindStringSubmatch(lines[j])
		if m == nil || strings.TrimSpace(m[2]) != "Active" {
			continue
		}
		if f := strings.Fields(m[4]); len(f) > 0 {
			return f[0]
		}
	}
	return ""
}

// colorStatusHeader paints the "● foo.service - Foo daemon" line. The bullet
// is colored from the state printed further down the block, which is why
// formatUnitStatus looks ahead before calling this.
//
// A header without a bullet (systemctl --plain) is only accepted when the
// first word looks like a unit name, so that "Unit foo.service could not be
// found." is not mistaken for one.
func colorStatusHeader(line, state string) (string, bool) {
	m := reStatusHeader.FindStringSubmatch(line)
	if m == nil {
		return line, false
	}
	unit, desc := m[2], m[3]
	if m[1] == "" && !strings.Contains(unit, ".") {
		return line, false
	}
	out := ""
	if m[1] != "" {
		out = tool.PaintColor(unitStateColor(state), "●") + " "
	}
	out += tool.PaintColor(bold, unit)
	if desc != "" {
		out += " - " + desc
	}
	return out, true
}

func colorStatusLine(line string) (string, bool) {
	if m := reStatusTree.FindStringSubmatch(line); m != nil {
		// Dim the gutter and the pid, keep the command readable.
		return tool.PaintColor(dim, m[1]+m[2]+m[3]) + m[4], true
	}

	if m := reStatusLabel.FindStringSubmatch(line); m != nil {
		label, value := m[2], m[4]
		out := m[1] + tool.PaintColor(dim, label+":") + m[3]
		switch {
		case label == "Active":
			out += paintFirstWord(unitStateColor, value)
		case label == "Loaded":
			out += paintFirstWord(loadStateColor, value)
		case statusDimLabels[label]:
			out += tool.PaintColor(dim, value)
		default:
			out += value
		}
		return out, true
	}
	return line, false
}

// paintFirstWord colors only the first word of a value, which is where systemd
// puts the state, leaving any parenthesised detail after it alone.
func paintFirstWord(color func(string) string, value string) string {
	word := firstWord(value)
	if word == "" {
		return value
	}
	return tool.PaintColor(color(word), word) + value[len(word):]
}

func firstWord(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

func looksLikeSystemctlError(text string) bool {
	for _, s := range []string{"could not be found", "Failed to", "Unknown", "not been loaded"} {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}

// --- list-units and list-unit-files ---------------------------------------

// reUnitNameHex matches the "\x2d" systemd puts in unit names derived from
// paths, where a literal hyphen would be ambiguous. Only hyphens are decoded:
// it is the one escape that actually shows up in unit names, and decoding
// arbitrary \xNN could turn into a space in the middle of a column.
var reUnitNameHex = regexp.MustCompile(`\\x2d`)

func unescapeUnitName(s string) string {
	return reUnitNameHex.ReplaceAllString(s, "-")
}

func formatListUnits(w io.Writer, text string) {
	headers := []string{"UNIT", "LOAD", "ACTIVE", "SUB", "DESCRIPTION"}
	var rows [][]Cell
	var tail []string
	seen := false
	tool.EachLine(text, func(line string) {
		f := strings.Fields(line)
		switch {
		case reUnitsListed.MatchString(line):
			tail = append(tail, line)
		case len(f) >= 3 && f[0] == "UNIT" && f[1] == "LOAD":
			seen = true
		case len(f) >= 4:
			seen = true
			desc := ""
			if len(f) > 4 {
				desc = strings.Join(f[4:], " ")
			}
			rows = append(rows, []Cell{
				{Text: unescapeUnitName(f[0]), Color: bold},
				{Text: f[1], Color: dim},
				{Text: f[2], Color: unitStateColor(f[2])},
				{Text: f[3], Color: dim},
				{Text: desc, Color: ""},
			})
		case len(f) == 0 && !seen:
			fmt.Fprintln(w)
		default:
			tail = append(tail, line)
		}
	})

	tool.PrintTable(w, headers, rows)
	for _, l := range tail {
		fmt.Fprintln(w, tool.PaintColor(dim, l))
	}
}

func formatUnitFiles(w io.Writer, text string) {
	headers := []string{"UNIT FILE", "STATE", "PRESET"}
	var rows [][]Cell
	var tail []string
	seen := false
	tool.EachLine(text, func(line string) {
		f := strings.Fields(line)
		switch {
		case reUnitsListed.MatchString(line):
			tail = append(tail, line)
		case len(f) >= 3 && f[0] == "UNIT" && f[1] == "FILE":
			seen = true
		case len(f) >= 2:
			seen = true
			preset := ""
			if len(f) > 2 {
				preset = strings.Join(f[2:], " ")
			}
			rows = append(rows, []Cell{
				{Text: unescapeUnitName(f[0]), Color: bold},
				{Text: f[1], Color: unitStateColor(f[1])},
				{Text: preset, Color: dim},
			})
		case len(f) == 0 && !seen:
			fmt.Fprintln(w)
		default:
			tail = append(tail, line)
		}
	})

	tool.PrintTable(w, headers, rows)
	for _, l := range tail {
		fmt.Fprintln(w, tool.PaintColor(dim, l))
	}
}

// --- is-active and is-failed ----------------------------------------------

func formatUnitState(w io.Writer, text string) {
	tool.EachLine(text, func(line string) {
		fmt.Fprintln(w, tool.PaintColor(unitStateColor(strings.TrimSpace(line)), line))
	})

}

// --- shared colors --------------------------------------------------------

// unitStateColor colors a state word: green when the unit is working, red when
// it is broken, yellow while it is changing, dim when the state is not
// something you need to act on.
func unitStateColor(state string) string {
	switch state {
	case "active", "enabled", "enabled-runtime", "linked", "linked-runtime":
		return green
	case "activating", "deactivating", "reloading":
		return yellow
	case "failed", "failed-runtime", "masked", "masked-runtime", "error", "bad":
		return red
	}
	return dim
}

// loadStateColor colors the word after "Loaded:".
func loadStateColor(state string) string {
	switch state {
	case "loaded":
		return green
	case "not-found", "masked", "error", "bad":
		return red
	}
	return dim
}

// ---------------------------------------------------------------------------
// Piped input
// ---------------------------------------------------------------------------

// SystemctlPipe formats piped systemctl output (`systemctl ... | parse`),
// guessing which command produced it.
func SystemctlPipe(w io.Writer, text string) {
	text = tool.StripANSI(text)
	Format(w, Detect(text), text)
}

// detectSystemctl recognizes the fixed shapes systemctl prints. It returns ""
// when nothing matches, so unrelated piped text is left alone.
func Detect(text string) string {
	lines := strings.Split(text, "\n")
	first := tool.FirstNonEmpty(lines)
	switch {
	case fieldsAre(first, "UNIT", "LOAD", "ACTIVE"):
		return "list-units"
	case fieldsAre(first, "UNIT", "FILE", "STATE"):
		return "list-unit-files"
	case strings.HasPrefix(first, "● "):
		return "status"
	case looksLikeDependencyTree(lines):
		return "list-dependencies"
	case looksLikeSystemctlError(first):
		// "Unit foo.service could not be found." and friends.
		return "status"
	}
	// `is-active` prints one state word and nothing else.
	if isUnitState(strings.TrimSpace(first)) && strings.TrimSpace(tool.FirstNonEmpty(lines)) == strings.TrimSpace(lastNonEmpty(lines)) {
		return "is-active"
	}
	// `status --plain` drops the bullet, so look for the labels instead.
	for _, l := range lines {
		m := reStatusLabel.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		switch strings.TrimSpace(m[2]) {
		case "Active", "Loaded", "Main PID", "CGroup":
			return "status"
		}
	}
	return ""
}

func fieldsAre(line string, want ...string) bool {
	got := strings.Fields(line)
	if len(got) < len(want) {
		return false
	}
	for i, w := range want {
		if got[i] != w {
			return false
		}
	}
	return true
}

func isUnitState(s string) bool {
	switch s {
	case "active", "inactive", "failed", "activating", "deactivating",
		"reloading", "enabled", "disabled", "masked", "static", "generated",
		"transient", "linked", "alias", "indirect", "maintenance", "refreshing":
		return true
	}
	return false
}

func lastNonEmpty(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return lines[i]
		}
	}
	return ""
}
