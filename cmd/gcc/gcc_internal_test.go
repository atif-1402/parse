// Tests for the gcc formatter: the summary goes up front, the file:line:col
// anchor is bold, the severity is colored, and the source snippets are dimmed.
// This is a reformation — the output is restructured, not byte-for-byte.
// Samples are gcc and clang's own output, copied as they printed them.
package gcc

import (
	"bytes"
	"strings"
	"testing"

	"github.com/atif-1402/parse/internal/tool"
)

func init() {
	tool.Paint = func(color, s string) string {
		if color == "" || s == "" {
			return s
		}
		return "<" + color + ">" + s + "</>"
	}
}

func format(text string) string {
	var buf bytes.Buffer
	Format(&buf, text)
	return buf.String()
}

const gccSample = `/home/atif/testing/gcc-test/syntax-error.c: In function 'main':
/home/atif/testing/gcc-test/syntax-error.c:5:21: error: expected ')' before 'return'
    5 |     printf("hello\n"
      |           ~         ^
      |                     )
    6 |     return 0;
      |     ~~~~~~          
/home/atif/testing/gcc-test/syntax-error.c:6:14: error: expected ';' before '}' token
    6 |     return 0;
      |              ^
      |              ;
    7 | }
      | ~
`

const gccWarningSample = `/home/atif/testing/gcc-test/warnings.c: In function 'main':
/home/atif/testing/gcc-test/warnings.c:6:14: warning: format '%d' expects argument of type 'int', but argument 2 has type 'char *' [-Wformat=]
    6 |     printf("%d\n", "not an int");
      |             ~^     ~~~~~~~~~~~~
      |              |     |
      |              int   char *
      |             %s
/home/atif/testing/gcc-test/warnings.c:5:9: warning: unused variable 'unused' [-Wunused-variable]
    5 |     int unused = 42;
      |         ^~~~~~
`

const clangSample = `/home/atif/testing/gcc-test/warnings.c:6:20: warning: format specifies type 'int' but the argument has type 'char *' [-Wformat]
    6 |     printf("%d\n", "not an int");
      |             ~~     ^~~~~~~~~~~~
      |             %s
/home/atif/testing/gcc-test/warnings.c:5:9: warning: unused variable 'unused' [-Wunused-variable]
    5 |     int unused = 42;
      |         ^~~~~~
2 warnings generated.
`

const clangErrorSample = `/home/atif/testing/gcc-test/syntax-error.c:6:5: error: expected ')'
    6 |     return 0;
      |     ^
syntax-error.c:5:11: note: to match this '('
    5 |     printf("hello\n"
      |           ^
1 error generated.
`

const linkerSample = `/usr/bin/ld: /tmp/cc2CmFu2.o: in function ` + "`" + `main':
link-error.c:(.text+0xa): undefined reference to ` + "`" + `helper'
collect2: error: ld returned 1 exit status
`

func TestDetectClaimsDiagnosticHeaders(t *testing.T) {
	for name, text := range map[string]string{
		"gcc error":   gccSample,
		"gcc warning": gccWarningSample,
		"clang":       clangSample,
		"clang note":  clangErrorSample,
		// A single diagnostic line is still compiler output.
		"one line": "/home/atif/x.c:3:10: error: expected ';' before '}' token\n",
	} {
		if got := Detect(text); got != "gcc" {
			t.Errorf("%s: Detect = %q, want gcc", name, got)
		}
	}
}

func TestDetectLeavesOtherOutputAlone(t *testing.T) {
	for name, text := range map[string]string{
		"make echo": "make: Entering directory '/home/atif/testing/gcc-test'\n",
		"make run":  "cc syntax-error.c -o syntax-error\n",
		"prose":     "syntax-error.c:5: not a diagnostic\n",
		// A file:line without a severity keyword is not a diagnostic header.
		"no severity": "/home/atif/x.c:5:10: \n",
		"empty":       "",
		"blank":       "\n\n",
	} {
		if got := Detect(text); got != "" {
			t.Errorf("%s: Detect = %q, want \"\"", name, got)
		}
	}
}

func TestFormatPrintsSummaryUpFront(t *testing.T) {
	got := format(gccSample)
	if !strings.Contains(got, "2 errors, 0 warnings") {
		t.Errorf("summary missing:\n%s", got)
	}
	// Notes don't count as errors or warnings.
	note := format(clangErrorSample)
	if !strings.Contains(note, "1 error, 0 warnings") {
		t.Errorf("clang summary missing:\n%s", note)
	}
	// The compiler's own `N errors generated.` tail is replaced by our summary.
	if strings.Contains(got, "errors generated.") {
		t.Errorf("compiler's tail summary should not appear:\n%s", got)
	}
}

func TestFormatColorsSeverities(t *testing.T) {
	got := format(gccSample)
	if !strings.Contains(got, "<red>error</>") {
		t.Errorf("error not red:\n%s", got)
	}
	got = format(gccWarningSample)
	if !strings.Contains(got, "<yellow>warning</>") {
		t.Errorf("warning not yellow:\n%s", got)
	}
	note := format(clangErrorSample)
	if !strings.Contains(note, "<cyan>note</>") {
		t.Errorf("note not cyan:\n%s", note)
	}
}

func TestFormatBoldsFileLineCol(t *testing.T) {
	got := format(gccSample)
	for _, want := range []string{
		"<bold>/home/atif/testing/gcc-test/syntax-error.c:5:21</>",
		"<bold>/home/atif/testing/gcc-test/syntax-error.c:6:14</>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

func TestFormatDimsSnippets(t *testing.T) {
	got := format(gccSample)
	if !strings.Contains(got, "<dim>") {
		t.Errorf("snippets not dimmed:\n%s", got)
	}
	// The snippet content survives inside the dim tag.
	if !strings.Contains(got, `printf("hello\n"`) {
		t.Errorf("snippet content missing:\n%s", got)
	}
	if !strings.Contains(got, `return 0;`) {
		t.Errorf("second snippet missing:\n%s", got)
	}
}

func TestFormatDropsInFunctionContext(t *testing.T) {
	got := format(gccSample)
	if strings.Contains(got, "In function") {
		t.Errorf("In function context should not appear — file:line:col is enough:\n%s", got)
	}
}

func TestFormatKeepsMessagePlain(t *testing.T) {
	got := format(gccSample)
	if !strings.Contains(got, "<red>error</>  expected ')' before 'return'") {
		t.Errorf("message not plain after severity:\n%s", got)
	}
	got = format(gccWarningSample)
	if !strings.Contains(got, "<yellow>warning</>  format '%d' expects argument of type 'int'") {
		t.Errorf("warning message not plain:\n%s", got)
	}
}

func TestFormatLeavesNonDiagnosticTextAlone(t *testing.T) {
	// make's own error lines.
	makeErr := "make: *** [Makefile:10: syntax] Error 1\n"
	if got := format(makeErr); got != makeErr {
		t.Errorf("make error was repainted:\n%s", got)
	}
	// A bare file:line without severity is not a diagnostic.
	if got := format("syntax-error.c:5: not a diagnostic\n"); got != "syntax-error.c:5: not a diagnostic\n" {
		t.Errorf("prose was repainted:\n%s", got)
	}
	// Linker output doesn't match the diagnostic header shape.
	if got := format(linkerSample); got != linkerSample {
		t.Errorf("linker output was reformatted:\n%s", got)
	}
	// Diagnostics are still extracted when mixed with a command echo.
	text := "cc syntax-error.c -o syntax-error\n" + gccSample
	got := format(text)
	if !strings.Contains(got, "2 errors, 0 warnings") {
		t.Errorf("summary missing from mixed text:\n%s", got)
	}
	if strings.Contains(got, "cc syntax-error.c -o syntax-error") {
		t.Errorf("command echo should be dropped:\n%s", got)
	}
}

func TestFormatPrintsEveryByteBackForSingleDiag(t *testing.T) {
	// A text with exactly one diagnostic and no context lines — every
	// byte that matters survives: file:line:col, severity, message, snippet.
	text := "/home/atif/x.c:3:10: error: expected ';' before '}' token\n    3 |     return 0\n      |              ^\n"
	got := format(text)
	if !strings.Contains(got, "<bold>/home/atif/x.c:3:10</>") {
		t.Errorf("file:line:col not bold:\n%s", got)
	}
	if !strings.Contains(got, "<red>error</>") {
		t.Errorf("error not red:\n%s", got)
	}
	if !strings.Contains(got, "expected ';' before '}' token") {
		t.Errorf("message missing:\n%s", got)
	}
	if !strings.Contains(got, "<dim>") {
		t.Errorf("snippet not dimmed:\n%s", got)
	}
}
