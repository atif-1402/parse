// Tests for the pager's decision logic: which invocations get a pager, which
// are left alone, and how the pager is chosen.
//
// The point of these tests is that every "no" is deliberate. A pager that opens
// when it should not is the difference between a tool people use and a tool
// people route around with `--no-pager`.
package main

import (
	"os"
	"strings"
	"testing"
)

// withTerm sets TERM for the duration of a test, since paging is refused on a
// dumb or missing terminal and the test runner may have neither.
func withTerm(t *testing.T, term string) {
	t.Helper()
	old, had := os.LookupEnv("TERM")
	os.Setenv("TERM", term)
	t.Cleanup(func() {
		if had {
			os.Setenv("TERM", old)
		} else {
			os.Unsetenv("TERM")
		}
	})
}

// noPagerEnv clears the pager variables so the test does not depend on whatever
// the developer has exported in their own shell.
func noPagerEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"PARSE_PAGER", "PAGER"} {
		k := k
		old, had := os.LookupEnv(k)
		os.Unsetenv(k)
		t.Cleanup(func() {
			if had {
				os.Setenv(k, old)
			} else {
				os.Unsetenv(k)
			}
		})
	}
}

func TestShouldPageTheFloodingCommands(t *testing.T) {
	withTerm(t, "xterm-256color")
	noPagerEnv(t)
	cases := []struct {
		name string
		tool string
		args []string
		want bool
	}{
		{"journalctl", "journalctl", nil, true},
		{"git log", "git", []string{"log"}, true},
		{"git log with a filter", "git", []string{"log", "--author=me"}, true},
		// A screenful is not a flood, and a pager that opens and closes again on
		// every invocation is worse than none. `less -F` is what settles that, by
		// printing short output straight through without ever taking the screen,
		// so parse no longer has to guess which tools are loud.
		{"ps", "ps", []string{"aux"}, true},
		{"du", "du", []string{"-sh"}, true},
		{"lsblk", "lsblk", nil, true},
		{"kubectl", "kubectl", []string{"get", "pods"}, true},
	}
	for _, c := range cases {
		if got := shouldPage(c.tool, c.args, false, true); got != c.want {
			t.Errorf("shouldPage(%s, tty) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestShouldPageNeverWhenOutputIsRedirected(t *testing.T) {
	withTerm(t, "xterm-256color")
	noPagerEnv(t)
	// This is the one that protects `parse journalctl > log.txt`.
	if shouldPage("journalctl", nil, false, false) {
		t.Error("paged output that was redirected to a file")
	}
}

// Ctrl-C has to reach the command. A pager in front of anything live quits
// instead, and leaves the writer writing into a closed pipe.
//
// This is the whole family, not just journalctl: `kubectl logs -f` and
// `systemctl watch` were being paged, because the pager only knew about
// journalctl's follow flags while the other tools decided to pass the terminal
// through long after the pager had already taken over stdout.
func TestShouldPageRefusesAnythingEndlessOrInteractive(t *testing.T) {
	withTerm(t, "xterm-256color")
	noPagerEnv(t)
	for _, c := range []struct {
		tool string
		args []string
	}{
		{"journalctl", []string{"-f"}},
		{"journalctl", []string{"-F"}},
		{"journalctl", []string{"--follow"}},
		{"journalctl", []string{"--follow-new"}},
		{"journalctl", []string{"-u", "nginx", "-f"}},
		{"kubectl", []string{"logs", "-f"}},
		{"kubectl", []string{"logs", "--follow"}},
		{"kubectl", []string{"-n", "prod", "logs", "-f"}},
		{"kubectl", []string{"logs", "-f", "pod/api-0"}},
		{"kubectl", []string{"get", "pods", "-w"}},
		{"kubectl", []string{"get", "pods", "--watch"}},
		{"kubectl", []string{"get", "pods", "--watch-only"}},
		{"kubectl", []string{"exec", "-it", "api-0", "sh"}},
		{"kubectl", []string{"attach", "-it", "api-0"}},
		{"systemctl", []string{"watch"}},
		{"systemctl", []string{"watch", "nginx.service"}},
		{"systemctl", []string{"monitor"}},
		{"systemctl", []string{"--no-pager", "watch"}},
		{"free", []string{"-s", "1"}},
		{"free", []string{"--seconds=1"}},
		{"free", []string{"-c", "3"}},
		{"lsof", []string{"+r"}},
		{"lsof", []string{"-r", "5"}},
		{"lsof", []string{"+r", "-i"}},
		{"findmnt", []string{"-p"}},
		{"findmnt", []string{"--poll"}},
		{"findmnt", []string{"-o", "TARGET", "--poll=umount"}},
		{"git", []string{"add", "-p"}},
		{"git", []string{"commit"}},
		{"git", []string{"-C", "/tmp/repo", "rebase", "-i"}},
	} {
		if shouldPage(c.tool, c.args, false, true) {
			t.Errorf("shouldPage(%s %q) = true, want false", c.tool, c.args)
		}
	}
}

// The mirror image, so the rule above cannot be met by refusing everything.
func TestShouldPageStillPagesTheOrdinaryCases(t *testing.T) {
	withTerm(t, "xterm-256color")
	noPagerEnv(t)
	for _, c := range []struct {
		tool string
		args []string
	}{
		{"journalctl", []string{"-n", "500"}},
		{"journalctl", []string{"-u", "nginx"}},
		{"kubectl", []string{"get", "pods"}},
		{"kubectl", []string{"logs", "--tail=50"}},
		{"kubectl", []string{"logs", "pod/api-0"}},
		{"systemctl", []string{"status", "nginx"}},
		{"systemctl", []string{"list-units"}},
		{"free", []string{"-h", "-w"}},
		{"lsof", []string{"-i"}},
		{"lsof", []string{"-p", "1"}},
		{"findmnt", []string{}},
		{"findmnt", []string{"-o", "TARGET,FSTYPE"}},
		{"findmnt", []string{"-T", "/tmp"}},
		// --pseudo filters the table, it does not watch it.
		{"findmnt", []string{"--pseudo"}},
		{"git", []string{"log"}},
		// -f is follow for `kubectl logs` but not for git log, where it means
		// nothing at all.
		{"git", []string{"log", "-f"}},
	} {
		if !shouldPage(c.tool, c.args, false, true) {
			t.Errorf("shouldPage(%s %q) = false, want true", c.tool, c.args)
		}
	}
}

// parse's own output is not a command's output. Paging `parse -h` means reading
// the help text also means finding and pressing `q`.
func TestParseOwnOutputIsNeverPaged(t *testing.T) {
	for _, name := range []string{"-h", "--help", "help", "-l", "--list", "-v", "--version", "nosuchtool", "gti", ""} {
		if supported(name) {
			t.Errorf("supported(%q) = true, so a pager would be opened for parse's own output", name)
		}
	}
	// The gate must not be so wide that nothing pages, and a tool added later
	// has to page without anyone remembering to widen this list.
	for _, tool := range tools {
		if !supported(tool) {
			t.Errorf("supported(%q) = false, so %q would never be paged", tool, tool)
		}
	}
}

// The usage text is the documentation for the paging rules, so it is checked
// the same way the rest of the help text is.
func TestUsageDescribesUniversalPaging(t *testing.T) {
	if strings.Contains(usage, "paging (journalctl, git)") {
		t.Error("usage still says only journalctl and git page")
	}
	for _, want := range []string{"more than a screen", "Ctrl-C"} {
		if !strings.Contains(usage, want) {
			t.Errorf("usage does not mention %q, so the paging rules are misdescribed", want)
		}
	}
}

func TestShouldPageHonorsEveryOptOut(t *testing.T) {
	withTerm(t, "xterm-256color")
	noPagerEnv(t)
	if !shouldPage("journalctl", nil, false, true) {
		t.Fatal("baseline did not page, so the opt-outs prove nothing")
	}
	if shouldPage("journalctl", nil, true, true) {
		t.Error("ignored --no-pager")
	}
	// The tools have a --no-pager of their own, which reaches parse as one of
	// their arguments rather than as a parse flag.
	if shouldPage("journalctl", []string{"--no-pager"}, false, true) {
		t.Error("ignored the tool's own --no-pager")
	}
}

func TestShouldPageRefusesADumbTerminal(t *testing.T) {
	noPagerEnv(t)
	for _, term := range []string{"", "dumb"} {
		os.Setenv("TERM", term)
		if shouldPage("journalctl", nil, false, true) {
			t.Errorf("paged on TERM=%q, where less cannot draw", term)
		}
	}
}

func TestShouldPageRefusesWhenNoPagerExists(t *testing.T) {
	withTerm(t, "xterm-256color")
	t.Setenv("PARSE_PAGER", "definitely-not-a-real-pager-xyz")
	if shouldPage("journalctl", nil, false, true) {
		t.Error("paged through a pager that does not exist")
	}
}

func TestPagerCommandDefaultsToLess(t *testing.T) {
	noPagerEnv(t)
	name, flags, err := pagerCommand()
	if err != nil {
		t.Fatalf("pagerCommand: %v", err)
	}
	if !strings.HasSuffix(name, "less") {
		t.Errorf("pager = %q, want less", name)
	}
	// -R keeps the colors, -F skips the pager when it all fits, -X leaves the
	// scrollback alone.
	for _, want := range []string{"-R", "-F", "-X"} {
		if !hasAnyFlag(flags, want) {
			t.Errorf("flags %q missing %s", flags, want)
		}
	}
}

func TestPagerCommandTakesTheUsersChoice(t *testing.T) {
	noPagerEnv(t)
	t.Setenv("PARSE_PAGER", "more -d")
	name, flags, err := pagerCommand()
	if err != nil {
		t.Fatalf("pagerCommand: %v", err)
	}
	if !strings.HasSuffix(name, "more") {
		t.Errorf("pager = %q, want more", name)
	}
	// less's flags must not be handed to something that has never heard of them.
	if hasAnyFlag(flags, "-R") {
		t.Errorf("flags %q: less flags given to more", flags)
	}
	if len(flags) != 1 || flags[0] != "-d" {
		t.Errorf("flags = %q, want the user's own [-d]", flags)
	}
}

func TestPagerCommandKeepsFlagsTheUserAlreadyGave(t *testing.T) {
	noPagerEnv(t)
	t.Setenv("PAGER", "less -S")
	_, flags, err := pagerCommand()
	if err != nil {
		t.Fatalf("pagerCommand: %v", err)
	}
	if !hasAnyFlag(flags, "-R") {
		t.Errorf("flags %q: -R not added, so the colors would be dropped", flags)
	}
	// -S was asked for and must survive.
	if !hasAnyFlag(flags, "-S") {
		t.Errorf("flags %q: the user's -S was dropped", flags)
	}
}

func TestHasAnyFlagReadsBundles(t *testing.T) {
	flags := []string{"-FRX", "-S"}
	for _, letter := range []string{"-F", "-R", "-X", "-S"} {
		if !hasAnyFlag(flags, letter) {
			t.Errorf("hasAnyFlag(%q, %q) = false, want true", flags, letter)
		}
	}
	if hasAnyFlag([]string{"-S"}, "-R") {
		t.Error("hasAnyFlag found an R that is not there")
	}
	// A long option is not a bundle of single letters.
	if hasAnyFlag([]string{"--no-pager"}, "-p") {
		t.Error("hasAnyFlag read --no-pager as a bundle containing p")
	}
}

// Nothing is excluded for being unremarkable: `less -F` makes a pager that has
// nothing to show print straight through. The only commands left out are the
// ones that never finish or that read the keyboard.
func TestEveryCommandIsACandidateForPaging(t *testing.T) {
	withTerm(t, "xterm-256color")
	noPagerEnv(t)
	for _, tool := range []string{"journalctl", "git", "ps", "du", "findmnt", "free", "lsof", "ss", "lsblk", "ip", "find", "systemctl", "kubectl"} {
		if !shouldPage(tool, nil, false, true) {
			t.Errorf("shouldPage(%s, tty) = false, want true", tool)
		}
	}
}

func TestShouldPageNeverInFrontOfAnInteractiveCommand(t *testing.T) {
	withTerm(t, "xterm-256color")
	noPagerEnv(t)
	// These read the keyboard a line at a time and expect it to arrive as they
	// go, so a pager between them and the terminal would swallow it.
	for _, c := range []struct {
		args []string
		want bool
	}{
		{[]string{"add", "-p"}, false},
		{[]string{"add", "--patch"}, false},
		{[]string{"commit"}, false},
		{[]string{"rebase", "-i"}, false},
		{[]string{"stash", "-p"}, false},
		{[]string{"commit", "-m", "msg"}, true},
		{[]string{"log"}, true},
		{[]string{"diff"}, true},
	} {
		if got := shouldPage("git", c.args, false, true); got != c.want {
			t.Errorf("shouldPage(git %v) = %v, want %v", c.args, got, c.want)
		}
	}
}

func TestFinishIsSafeToCallTwice(t *testing.T) {
	// run() reaches finish through a defer, and a nil pager is the normal case
	// when nothing was started. Neither may panic or close anything twice.
	var p *pager
	p.finish()
	p = &pager{done: true}
	p.finish()
}
