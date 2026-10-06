// Tests for parse's own flags, which main_test.go cannot reach from the
// cmd package tests.
package main

import (
	"strings"
	"testing"
)

func TestParseArgsKeepsGitArgs(t *testing.T) {
	for _, c := range []struct {
		args []string
		want []string
	}{
		{[]string{"git", "log", "-n", "5"}, []string{"git", "log", "-n", "5"}},
		{[]string{"--color=always", "git", "log"}, []string{"git", "log"}},
		{[]string{"--color", "never", "git", "status"}, []string{"git", "status"}},
		// Once a tool is named, --color belongs to the tool. git has its own
		// --color, and parse swallowing it meant `parse git log --color always`
		// ate both words instead of letting git act on them.
		{[]string{"git", "--color=never", "log"}, []string{"git", "--color=never", "log"}},
		{[]string{"git", "log", "--color", "always"}, []string{"git", "log", "--color", "always"}},
		// the systemd tools take the same route
		{[]string{"systemctl", "--user", "status"}, []string{"systemctl", "--user", "status"}},
		{[]string{"journalctl", "--user", "-n", "5"}, []string{"journalctl", "--user", "-n", "5"}},
		{[]string{"--color=always", "journalctl", "-f"}, []string{"journalctl", "-f"}},
		// git's own flags must survive untouched
		{[]string{"git", "-c", "color.ui=always", "log"}, []string{"git", "-c", "color.ui=always", "log"}},
	} {
		_, got, err := parseArgs(c.args)
		if err != nil {
			t.Errorf("parseArgs(%q) failed: %v", c.args, err)
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("parseArgs(%q) = %q, want %q", c.args, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("parseArgs(%q) = %q, want %q", c.args, got, c.want)
				break
			}
		}
	}
}

func TestParseArgsDefaultsToAuto(t *testing.T) {
	o, _, err := parseArgs([]string{"git", "status"})
	if err != nil {
		t.Fatal(err)
	}
	if o.colorMode != "auto" {
		t.Errorf("default color mode = %q, want auto", o.colorMode)
	}
}

func TestParseArgsRejectsBadColor(t *testing.T) {
	for _, args := range [][]string{
		{"--color=sometimes", "git", "log"},
		{"--color"},
	} {
		if _, _, err := parseArgs(args); err == nil {
			t.Errorf("parseArgs(%q) should have failed", args)
		}
	}
}

func TestColorMode(t *testing.T) {
	// auto is decided by whether stdout is a terminal, which is not the case
	// under `go test`, so auto must report false here.
	for _, c := range []struct {
		mode string
		want bool
	}{
		{"always", true},
		{"never", false},
		{"auto", false},
	} {
		if got := (options{colorMode: c.mode}).color(); got != c.want {
			t.Errorf("color(%q) = %v, want %v", c.mode, got, c.want)
		}
	}
}

func TestNoArgsMessagePointsAtHelp(t *testing.T) {
	// Bare `parse` must say what it wants rather than dumping the whole usage.
	for _, want := range []string{"parse git status", "| parse", "parse -h", "parse --help"} {
		if !strings.Contains(noArgs, want) {
			t.Errorf("noArgs does not mention %q:\n%s", want, noArgs)
		}
	}
	if strings.Contains(noArgs, "Examples") {
		t.Error("noArgs should not carry an examples section")
	}
}

func TestUsageListsEveryToolAndHasNoExamples(t *testing.T) {
	for _, want := range []string{"parse <tool> <args>", "| parse", "--color", "--version"} {
		if !strings.Contains(usage, want) {
			t.Errorf("usage does not mention %q:\n%s", want, usage)
		}
	}
	// Every supported tool is named, so a user does not have to guess whether
	// parse handles the command they just ran.
	for _, tool := range tools {
		if !strings.Contains(usage, tool) {
			t.Errorf("usage does not list the tool %q:\n%s", tool, usage)
		}
	}
	if strings.Contains(usage, "Examples") {
		t.Errorf("usage should not carry an examples section:\n%s", usage)
	}
}

func TestEveryToolIsDispatched(t *testing.T) {
	// The usage text, the dispatch table and the tools list must not drift
	// apart: a tool named in one and missing from the others is a bug.
	for _, tool := range tools {
		if !strings.Contains(usage, tool) {
			t.Errorf("tool %q is dispatched but not listed in usage", tool)
		}
	}
}

// A command parse does not know how to format is refused by name rather than
// run half-way: the direct path exists for the tools parse can improve, and
// the list is one flag away for anyone who has lost track of them. The piped
// path is the other way round — it shows any command's output.
func TestDirectRunOfAnUnsupportedCommandSaysSo(t *testing.T) {
	out, errOut, code := runBinary(t, "", "echo", "hello")
	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if out != "" {
		t.Errorf("an unsupported command wrote to stdout: %q", out)
	}
	if want := "parse: echo is not supported yet\n"; errOut != want {
		t.Errorf("stderr = %q, want %q", errOut, want)
	}
	for _, tool := range tools {
		if strings.Contains(errOut, tool) {
			t.Errorf("the error lists %q, which was not asked for:\n%s", tool, errOut)
		}
	}
}

// And the list is still one flag away, so nothing is actually hidden by
// dropping it from the error.
func TestTheToolListIsStillReachable(t *testing.T) {
	out, _, code := runBinary(t, "", "-l")
	if code != 0 {
		t.Errorf("exit %d, want 0", code)
	}
	for _, tool := range tools {
		if !strings.Contains(out, tool) {
			t.Errorf("%s is missing from `parse -l`:\n%s", tool, out)
		}
	}
}

// TestToolListText guards the contract of `parse -l`: every supported command
// appears, and each line says something concrete about what parse changes, so
// the list is useful on its own instead of just echoing the usage text.
func TestToolListText(t *testing.T) {
	out := toolListText()

	for _, tool := range tools {
		if !strings.Contains(out, tool) {
			t.Errorf("%s is missing from the tool list:\n%s", tool, out)
		}
	}

	for _, tool := range toolList {
		want := "  " + tool.name
		found := false
		for _, l := range strings.Split(out, "\n") {
			if !strings.HasPrefix(l, want) {
				continue
			}
			found = true
			if strings.TrimSpace(strings.TrimPrefix(l, want)) == "" {
				t.Errorf("%s is listed with no description:\n%s", tool.name, out)
			}
		}
		if !found {
			t.Errorf("%s has no line of its own in the list:\n%s", tool.name, out)
		}
	}
}

// A tool list that grew or shrank without tools being updated would mean
// `parse ls` starts failing while parse still advertises ls.
func TestToolsMatchesToolList(t *testing.T) {
	if len(tools) != len(toolList) {
		t.Errorf("tools has %d entries, toolList has %d", len(tools), len(toolList))
	}
	for i, name := range tools {
		if toolList[i].name != name {
			t.Errorf("entry %d: tools says %q, toolList says %q", i, name, toolList[i].name)
		}
		if toolList[i].what == "" {
			t.Errorf("%s has no description", name)
		}
	}
}

// -w is parse's own flag for the two-column diff. It has to be claimed for the
// piped form, where there is no git argument list, and left alone once a tool
// has been named, so `parse git diff -w` keeps working through git's own path.
func TestSideBySideFlagIsClaimedOnlyBeforeATool(t *testing.T) {
	for _, in := range [][]string{{"-w"}, {"--side-by-side"}} {
		o, rest, err := parseArgs(in)
		if err != nil {
			t.Fatalf("parseArgs(%v): %v", in, err)
		}
		if !o.sideBySide {
			t.Errorf("parseArgs(%v) did not claim the flag", in)
		}
		if len(rest) != 0 {
			t.Errorf("parseArgs(%v) left %v behind", in, rest)
		}
	}

	o, rest, err := parseArgs([]string{"git", "diff", "-w"})
	if err != nil {
		t.Fatal(err)
	}
	if o.sideBySide {
		t.Error("-w after a tool must not be claimed by parse")
	}
	if strings.Join(rest, " ") != "git diff -w" {
		t.Errorf("rest is %v, want the arguments untouched", rest)
	}
}

func TestUsageMentionsSideBySide(t *testing.T) {
	for _, want := range []string{"-w", "--side-by-side"} {
		if !strings.Contains(usage, want) {
			t.Errorf("usage does not mention %q:\n%s", want, usage)
		}
	}
}
