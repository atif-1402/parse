// Tests for the gcc formatter: the diagnostic header shape, the colors it
// gets, and the lines it dims. Samples are gcc and clang's own output,
// copied as they printed them.
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

// strip takes the test's color markers back out, so a formatted line can be
// held against the line it came from.
func strip(s string) string {
	for _, tag := range []string{"<bold>", "<dim>", "<red>", "<green>", "<yellow>", "<cyan>", "</>"} {
		s = strings.ReplaceAll(s, tag, "")
	}
	return s
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

func TestFormatPaintsDiagnosticHeaders(t *testing.T) {
	got := format(gccSample)
	// The file:line:col anchor is bold; the severity is colored.
	for _, want := range []string{
		"<bold>/home/atif/testing/gcc-test/syntax-error.c:5:21</>",
		"<red>error</>",
		"<bold>/home/atif/testing/gcc-test/syntax-error.c:6:14</>",
		"<red>error</>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	// The message after the severity stays plain.
	if !strings.Contains(got, "<red>error</>: expected ')' before 'return'") {
		t.Errorf("message was repainted:\n%s", got)
	}
}

func TestFormatColorsWarningsAndNotes(t *testing.T) {
	got := format(gccWarningSample)
	if !strings.Contains(got, "<yellow>warning</>") {
		t.Errorf("warning not yellow:\n%s", got)
	}
	note := format(clangErrorSample)
	if !strings.Contains(note, "<cyan>note</>") {
		t.Errorf("note not cyan:\n%s", note)
	}
}

func TestFormatDimsSnippetsAndContext(t *testing.T) {
	got := format(gccSample)
	// Source snippets and caret lines are dimmed; context lines too.
	for _, want := range []string{
		"<dim>    5 |     printf(\"hello\\n\"</>",
		"<dim>      |           ~         ^</>",
		"<dim>/home/atif/testing/gcc-test/syntax-error.c: In function 'main':</>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

func TestFormatDimsSummary(t *testing.T) {
	got := format(clangSample)
	if !strings.Contains(got, "<dim>2 warnings generated.</>") {
		t.Errorf("summary not dimmed:\n%s", got)
	}
}

func TestFormatPrintsEveryByteBack(t *testing.T) {
	for name, text := range map[string]string{
		"gcc error":   gccSample,
		"gcc warning": gccWarningSample,
		"clang":       clangSample,
		"clang note":  clangErrorSample,
		"linker":      linkerSample,
	} {
		if got := format(text); strip(got) != text {
			t.Errorf("%s moved a byte:\ngot  %q\nwant %q", name, strip(got), text)
		}
	}
}

func TestFormatLeavesNonDiagnosticLinesAlone(t *testing.T) {
	// A make command echo mixed with a diagnostic: the echo passes through,
	// only the diagnostic header gets painted.
	text := "cc syntax-error.c -o syntax-error\n" + gccSample
	got := format(text)
	if !strings.Contains(got, "cc syntax-error.c -o syntax-error") {
		t.Errorf("command echo was repainted:\n%s", got)
	}
	if !strings.Contains(got, "<red>error</>") {
		t.Errorf("diagnostic was not painted:\n%s", got)
	}
	// make's own error lines are not diagnostics.
	makeErr := "make: *** [Makefile:10: syntax] Error 1\n"
	if got := format(makeErr); got != makeErr {
		t.Errorf("make error was repainted:\n%s", got)
	}
}
