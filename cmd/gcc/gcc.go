// Package gcc formats gcc and clang diagnostic output. A diagnostic is a
// block: a `file:line:col: error:` header, then source snippets with caret
// pointers, then the next header. Every line looks alike on a terminal, so
// parse paints the file:line anchor bold, the severity by kind, and dims the
// source snippets and context lines so the actual error stands out.
package gcc

import (
	"io"
	"os"
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

// reDiag is one diagnostic header: `file:line:col: severity: message`.
// gcc and clang both spell it this way, clang adds `remark:` too.
var reDiag = regexp.MustCompile(`^(.+):(\d+):(\d+): (fatal error|error|warning|note|remark): (.*)$`)

// reContext is the `file: In function 'name':` line that opens a group of
// diagnostics from one function.
var reContext = regexp.MustCompile(`^.+?: In function '.*':.*$`)

// reSnippet is a source snippet line: `    5 |     printf("hello\n"`.
var reSnippet = regexp.MustCompile(`^\s*\d+ \|`)

// reCaret is a caret pointer line: `      |           ~         ^`.
var reCaret = regexp.MustCompile(`^\s*\|`)

// reSummary is the `2 warnings generated.` tail line.
var reSummary = regexp.MustCompile(`^\d+ (warning|error)s? generated\.$`)

// Run implements `parse gcc <args>` and `parse clang <args>`.
func Run(bin string, args []string) int {
	text, code, started := tool.Capture(bin, args)
	if !started {
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
// line or a command echo, so every line is checked until one matches.
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

// Format paints each line of the diagnostic output. Diagnostic headers get
// a bold file:line anchor and a colored severity; source snippets, carets,
// context lines and summaries are dimmed; everything else passes through.
func Format(w io.Writer, text string) {
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			io.WriteString(w, "\n")
		}
		io.WriteString(w, row(line))
	}
}

// row paints one line of compiler output.
func row(line string) string {
	switch {
	case reSummary.MatchString(line):
		return tool.PaintColor(dim, line)
	case reContext.MatchString(line):
		return tool.PaintColor(dim, line)
	case reSnippet.MatchString(line) || reCaret.MatchString(line):
		return tool.PaintColor(dim, line)
	case reDiag.MatchString(line):
		return paintDiag(line)
	default:
		return line
	}
}

// paintDiag splits one diagnostic header into its parts and paints them:
// the file:line:col anchor bold, the severity by kind, the message plain.
// Group spans: [0:2] whole match, [2:4] file, [4:6] line, [6:8] col,
// [8:10] severity, [10:12] message; literal ": " separators sit between.
func paintDiag(line string) string {
	m := reDiag.FindStringSubmatchIndex(line)
	if m == nil {
		return line
	}
	var sb strings.Builder
	sb.WriteString(line[:m[2]])                            // nothing (anchored at ^)
	sb.WriteString(tool.PaintColor(bold, line[m[2]:m[7]])) // file:line:col bold
	sb.WriteString(line[m[7]:m[8]])                        // ": " after col
	sb.WriteString(paintSeverity(line[m[8]:m[9]]))         // severity colored
	sb.WriteString(line[m[9]:m[10]])                       // ": " after severity
	sb.WriteString(line[m[10]:])                           // message plain
	return sb.String()
}

// paintSeverity colors the severity keyword by kind.
func paintSeverity(s string) string {
	switch s {
	case "fatal error", "error":
		return tool.PaintColor(red, s)
	case "warning":
		return tool.PaintColor(yellow, s)
	case "note", "remark":
		return tool.PaintColor(cyan, s)
	default:
		return s
	}
}
