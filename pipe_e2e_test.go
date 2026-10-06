package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// runBinary builds parse and runs it with the given stdin and arguments,
// returning stdout, stderr and the exit code. Testing the real binary is the
// only way to cover the paths that read os.Stdin or write to os.Stderr, which is
// most of what changed in this area.
func runBinary(t *testing.T, stdin string, args ...string) (string, string, int) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain")
	}
	bin := t.TempDir() + "/parse"
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("building parse: %v", err)
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running parse: %v", err)
	}
	return out.String(), errb.String(), code
}

// Empty input is a normal answer. `git diff` on a clean tree prints nothing, and
// parse used to answer that with "no input on stdin" plus exit 1, which told the
// user their command had failed when it had succeeded. This also broke any
// script that checked $?, over a clean tree.
func TestEmptyPipeIsSilentAndSucceeds(t *testing.T) {
	for _, stdin := range []string{"", "\n"} {
		out, errOut, code := runBinary(t, stdin)
		if code != 0 {
			t.Errorf("stdin %q: exit %d, want 0", stdin, code)
		}
		if out != stdin {
			t.Errorf("stdin %q: stdout %q, want it unchanged", stdin, out)
		}
		if strings.Contains(errOut, "no input") {
			t.Errorf("stdin %q: complained about empty input: %q", stdin, errOut)
		}
	}
}

// A misspelled command is bash's error to report, and bash reports it on stderr
// where the user can already see it. parse sees an empty pipe and has nothing to
// add, so it must add nothing.
func TestEmptyPipeAfterAFailedCommandStaysQuiet(t *testing.T) {
	_, errOut, code := runBinary(t, "")
	if code != 0 {
		t.Errorf("exit %d, want 0", code)
	}
	if errOut != "" {
		t.Errorf("parse added its own commentary to an empty pipe: %q", errOut)
	}
}

// A real diff still formats, so the empty-pipe fix did not break the common path.
func TestNonEmptyPipeStillFormats(t *testing.T) {
	in := "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+new\n"
	out, errOut, code := runBinary(t, in)
	if code != 0 {
		t.Errorf("exit %d, want 0", code)
	}
	if errOut != "" {
		t.Errorf("unexpected stderr: %q", errOut)
	}
	if !strings.Contains(out, "-old") || !strings.Contains(out, "+new") {
		t.Errorf("the diff body was lost: %q", out)
	}
	if strings.Contains(out, "diff --git") {
		t.Errorf("the boilerplate should have been replaced: %q", out)
	}
}
