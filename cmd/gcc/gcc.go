// Package gcc formats gcc and clang compiler diagnostics. A compiler prints
// one diagnostic per problem — header, source snippets, carets — and buries
// the error count at the end. parse groups each diagnostic under its
// file:line:col, paints the severity, puts the count summary up front, and
// dims the snippets so the actual problem stands out. This is a
// reformation, not a recoloring: gcc already colors its own output when it
// sees a terminal; parse changes the shape of it.
package gcc

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/atif-1402/parse/internal/tool"
)

const (
	bold   = tool.Bold
	red    = tool.Red
	yellow = tool.Yellow
	cyan   = tool.Cyan
	dim    = tool.Dim
)

// reDiag matches a compiler diagnostic header: `file:line:col: severity: message`.
var reDiag = regexp.MustCompile(`^(.+?):(\d+):(\d+):\s*(fatal error|error|warning|note|remark):\s*(.*)$`)

// reSnippet matches a source snippet line: `    5 |     printf("hello\n"`.
var reSnippet = regexp.MustCompile(`^\s*\d+\s*\|`)

// reCaret matches a caret pointer line: `      |           ~         ^`.
var reCaret = regexp.MustCompile(`^\s*\|`)

// reSummary matches the compiler's own `N errors generated.` tail line.
var reSummary = regexp.MustCompile(`^\d+ (error|warning)s? generated\.$`)

// diag is one parsed compiler diagnostic.
type diag struct {
	file, line, col, sev, msg string
	snippet                   string
}

// Run implements `parse gcc <args>` and `parse clang <args>`.
//
// gcc and clang write diagnostics to stderr, so we capture stderr rather than
// stdout. Anything gcc prints on stdout (preprocessor output with -E, etc.)
// still reaches the terminal directly.
func Run(bin string, args []string) int {
	cmd := exec.Command(bin, args...)
	cmd.Stdin = os.Stdin
	var out bytes.Buffer
	cmd.Stderr = &out
	err := cmd.Run()
	code := tool.ExitCode(err)
	text := out.String()
	if text == "" {
		return code
	}
	Format(os.Stdout, text)
	return code
}

// Pipe formats piped gcc or clang output.
func Pipe(w io.Writer, text string) {
	Format(w, text)
}

// Detect reports whether text looks like gcc or clang diagnostics. The
// header line — `file:line:col: severity: message` — is distinctive: no
// other tool parse knows prints a path, two line numbers, and a severity
// keyword all on one line. gcc often opens with an `In function` context
// line, so every line is checked until one matches.
func Detect(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if reDiag.MatchString(line) {
			return "gcc"
		}
	}
	return ""
}

// Format parses the compiler output into diagnostics and prints them as a
// grouped list: a summary line up front (compilers hide it at the bottom),
// then each diagnostic with its location, colored severity, message, and the
// dimmed source snippet below. Text with no diagnostic headers passes
// through unchanged.
func Format(w io.Writer, text string) {
	lines := strings.Split(text, "\n")
	diags := parseDiags(lines)
	if len(diags) == 0 {
		io.WriteString(w, text)
		return
	}

	var sb strings.Builder
	errs, warns := 0, 0
	for _, d := range diags {
		switch d.sev {
		case "error", "fatal error":
			errs++
		case "warning":
			warns++
		}
	}
	fmt.Fprintf(&sb, "%d %s, %d %s\n", errs, plural(errs, "error"), warns, plural(warns, "warning"))
	sb.WriteString(strings.Repeat("─", 45) + "\n")

	for i, d := range diags {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(tool.PaintColor(bold, d.file+":"+d.line+":"+d.col))
		sb.WriteString("  ")
		sb.WriteString(tool.PaintColor(sevColor(d.sev), d.sev))
		sb.WriteString("  ")
		sb.WriteString(d.msg)
		sb.WriteString("\n")
		if d.snippet != "" {
			sb.WriteString(tool.PaintColor(dim, strings.TrimRight(d.snippet, "\n")))
			sb.WriteString("\n")
		}
	}
	io.WriteString(w, sb.String())
}

// parseDiags splits lines into diagnostic blocks. Each block starts with a
// header matching reDiag and gathers the following snippet and caret lines
// until the next header or a blank line.
func parseDiags(lines []string) []diag {
	var out []diag
	var cur *diag
	flush := func() {
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}
	for _, line := range lines {
		if m := reDiag.FindStringSubmatch(line); m != nil {
			flush()
			cur = &diag{
				file: m[1], line: m[2], col: m[3],
				sev: m[4], msg: m[5],
			}
		} else if cur != nil && (reSnippet.MatchString(line) || reCaret.MatchString(line)) {
			cur.snippet += strings.TrimRight(line, " ") + "\n"
		} else if cur != nil && strings.TrimSpace(line) == "" {
			flush()
		} else if cur != nil {
			flush()
		}
	}
	flush()
	return out
}

// plural returns word or word+"s" depending on n.
func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// sevColor returns the paint color for a severity keyword.
func sevColor(s string) string {
	switch s {
	case "error", "fatal error":
		return red
	case "warning":
		return yellow
	case "note", "remark":
		return cyan
	default:
		return ""
	}
}
