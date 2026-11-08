// Package gcc formats gcc and clang compiler diagnostics. A compiler prints
// one diagnostic per problem with verbose snippets and buries the count at
// the end. parse groups diagnostics by file, puts the count summary up
// front, paints severity by kind, and shows the full snippet block — source
// line, caret, and expansion — dimmed under each diagnostic.
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

// reCaret matches a continuation line: `      |             ~^     ~~~~~~~~~~~~`.
var reCaret = regexp.MustCompile(`^\s*\|`)

// reSummary matches the compiler's own `N errors generated.` tail line.
var reSummary = regexp.MustCompile(`^\d+ (error|warning)s? generated\.$`)

// diag is one parsed compiler diagnostic.
type diag struct {
	file, line, col, sev, msg string
	snippet                   string
}

// fileBlock is one file's diagnostics, grouped under a single path.
type fileBlock struct {
	file  string
	diags []diag
	errs  int
	warns int
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

// Format parses the compiler output into diagnostics grouped by file and
// prints them: a count summary up front, then per file the path once bolded,
// followed by line:col + colored severity + message with the dimmed snippet
// block underneath. Text with no diagnostic headers passes through unchanged.
func Format(w io.Writer, text string) {
	lines := strings.Split(text, "\n")
	diags := parseDiags(lines)
	if len(diags) == 0 {
		io.WriteString(w, text)
		return
	}

	blocks := groupByFile(diags)
	errs, warns := 0, 0
	for _, b := range blocks {
		errs += b.errs
		warns += b.warns
	}

	var sb strings.Builder
	sb.WriteString(summary(errs, warns))
	sb.WriteString("\n")
	writeBlocks(&sb, blocks)
	io.WriteString(w, sb.String())
}

// summary prints the count summary, omitting zero counts so "2 errors"
// reads better than "2 errors, 0 warnings".
func summary(errs, warns int) string {
	parts := []string{}
	if errs > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", errs, plural(errs, "error")))
	}
	if warns > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", warns, plural(warns, "warning")))
	}
	if len(parts) == 0 {
		return "0 problems"
	}
	return strings.Join(parts, ", ")
}

// groupByFile groups diagnostics by file path, preserving order.
func groupByFile(diags []diag) []fileBlock {
	index := map[string]int{}
	blocks := []fileBlock{}
	for _, d := range diags {
		if i, ok := index[d.file]; ok {
			b := &blocks[i]
			b.diags = append(b.diags, d)
			switch d.sev {
			case "error", "fatal error":
				b.errs++
			case "warning":
				b.warns++
			}
		} else {
			index[d.file] = len(blocks)
			var b fileBlock
			b.file = d.file
			b.diags = []diag{d}
			switch d.sev {
			case "error", "fatal error":
				b.errs = 1
			case "warning":
				b.warns = 1
			}
			blocks = append(blocks, b)
		}
	}
	return blocks
}

// writeBlocks writes one file block per file, grouped under a bold path.
func writeBlocks(sb *strings.Builder, blocks []fileBlock) {
	for i, b := range blocks {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(tool.PaintColor(bold, b.file))
		sb.WriteString("\n")
		for _, d := range b.diags {
			sb.WriteString("  ")
			sb.WriteString(tool.PaintColor(bold, d.line+":"+d.col))
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
	}
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

// parseDiags splits lines into diagnostic blocks. Each block starts with a
// header matching reDiag and gathers the following snippet lines — source,
// caret, and expansion — until the next header or a blank line. The `In
// function` context line and the compiler's own `N errors generated.` tail
// are dropped.
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
