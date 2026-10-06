// Tests for the piped-input contract: what parse must not touch, and what it
// must stay quiet about.
//
// The rule the whole file leans on is that output parse does not understand is
// passed through byte for byte. These tests are mostly about the ways that
// promise was being broken: machine-readable git formats being rewritten into
// nonsense, and an empty pipe being reported as an error.
package test

import (
	"strings"
	"testing"

	"github.com/atif-1402/parse/cmd"
)

// throughCmd runs text through the full dispatcher, which is what `<tool> | parse`
// uses. Unlike format() in git_test.go this does not go straight to git.Pipe, so
// it also proves that no other tool claims the text first.
func throughCmd(text string) string {
	var sb strings.Builder
	cmd.Pipe(&sb, text)
	return sb.String()
}

// passthrough asserts that text comes back completely unchanged. Anything less
// is a bug: a reader who piped machine-readable output expects the same bytes.
func passthrough(t *testing.T, name, text string) {
	t.Helper()
	if got := throughCmd(text); got != text {
		t.Errorf("%s was rewritten\n--- got ---\n%q\n--- want ---\n%q", name, got, text)
	}
}

// The machine-readable git formats. Each of these is a script's input, and
// rewriting one is worse than not formatting it at all.
//
// --numstat was the worst of them: the du detector claimed it, because its rows
// are "3\t2\tcmd/main.go" and du reads "3<TAB>anything" as a size and a path.
// The added/removed counts came back as sizes: "3K  2\tcmd/main.go".
func TestMachineFormatsSurviveIntact(t *testing.T) {
	passthrough(t, "numstat", lines(
		"3\t2\tcmd/main.go",
		"0\t1\tREADME.md",
		"12\t0\tnew.go",
	))
	passthrough(t, "numstat binary", lines(
		"-\t-\tlogo.png",
		"2\t3\told name.txt",
	))
	passthrough(t, "raw", lines(
		":100644 100644 abc1234 def5678 M\tcmd/main.go",
		":000000 100644 0000000 9917ab0 A\t.gitignore",
	))
	passthrough(t, "name-only", lines(
		"cmd/main.go",
		"README.md",
	))
	passthrough(t, "name-status", lines(
		"M\tcmd/main.go",
		"A\t.gitignore",
		"R100\told.go\tnew.go",
	))
	passthrough(t, "shortstat", lines(
		" 1 file changed, 2 insertions(+), 1 deletion(-)",
	))
	passthrough(t, "diff --stat", lines(
		" cmd/main.go | 4 ++--",
		" 1 file changed, 2 insertions(+), 2 deletions(-)",
	))
}

// --raw is safe above even though it looks like a diff, because its rows start
// with a colon and never with "diff --git".

// --word-diff opens with the same `diff --git` header a patch does but has no
// hunks, so it used to be accepted as a diff and printed under a fake "+0 -0"
// header with the interesting part left unformatted.
func TestWordDiffIsNotMistakenForAPatch(t *testing.T) {
	passthrough(t, "word-diff", lines(
		"diff --git a/main.go b/main.go",
		"index 111aaaa..222bbbb 100644",
		"--- a/main.go",
		"+++ b/main.go",
		"@@ -1,3 +1,3 @@",
		"[-old line-]{+new line+}",
	))
}

// Prose that happens to begin with the words `diff --git` is not a diff. This is
// what the README of a project explaining its own diff format would contain.
func TestProseBeginningWithDiffGitIsNotADiff(t *testing.T) {
	passthrough(t, "prose", lines(
		"diff --git is described in the docs, and the a/ b/ prefixes matter.",
		"See the notes file for details.",
	))
}

// Empty input is a normal answer, not a failure. `git diff` on a clean tree
// prints nothing, `git status --porcelain` prints nothing when nothing is
// staged, `git stash list` prints nothing when there are no stashes. parse used
// to print "no input on stdin" and exit 1, which told the user their command had
// failed when it had succeeded and there was simply nothing to report.
func TestEmptyInputIsSilent(t *testing.T) {
	if got := throughCmd(""); got != "" {
		t.Errorf("empty input produced %q, want nothing at all", got)
	}
}

// Whitespace-only input is passed through untouched, byte for byte, rather than
// swallowed or reported. A command that prints a bare newline printed a bare
// newline, and rewriting that would be the same class of bug as rewriting
// --numstat: parse changed output it did not understand. What matters is that
// none of it produces a complaint on stderr.
func TestWhitespaceOnlyInputIsPassedThroughNotComplainedAbout(t *testing.T) {
	for _, text := range []string{"\n", "  ", "\n\n", " \t\n"} {
		if got := throughCmd(text); got != text {
			t.Errorf("input %q became %q, want it unchanged", text, got)
		}
	}
}

// CRLF input used to keep its \r, so a hunk header and the lines under it
// disagreed about where a line ended and the block rendered as one long line.
func TestCRLFDiffIsNormalized(t *testing.T) {
	in := "diff --git a/main.go b/main.go\r\n" +
		"--- a/main.go\r\n" +
		"+++ b/main.go\r\n" +
		"@@ -1 +1 @@\r\n" +
		"-old\r\n" +
		"+new\r\n"
	got := throughCmd(in)
	if strings.Contains(got, "\r") {
		t.Errorf("a carriage return survived into the output: %q", got)
	}
	if !strings.Contains(got, "-old") || !strings.Contains(got, "+new") {
		t.Errorf("the diff body was lost: %q", got)
	}
}

// Foreign color is the user's own: they asked for `grep --color=always`, so
// parse has no business removing it. This is the other half of the contract
// above, and it is what makes the "byte for byte" promise complete.
func TestUnrecognizedColorIsLeftAlone(t *testing.T) {
	passthrough(t, "colored grep", "\x1b[31mred\x1b[0m\n\x1b[1mbold\x1b[0m\n")
}

// Ordinary files that happen to be piped in must not be claimed by a
// formatter. A go.mod is not a du listing just because it has numbers in it.
func TestOrdinaryTextIsNotClaimed(t *testing.T) {
	passthrough(t, "go.mod", lines(
		"module github.com/user/coldlock",
		"",
		"go 1.22.0",
		"",
		"require (",
		"\tgithub.com/spf13/cobra v1.8.0",
		")",
	))
	passthrough(t, "markdown prose", lines(
		"# coldlock",
		"",
		"Hard-lock distraction blocker for Linux. Block sites two ways.",
	))
}

// df formats its own table, so parse has nothing to add and must not claim
// either of its shapes. The pipe is where that choice is settled: there is no
// command line to read a flag from, so it rests on the text alone.
func TestDfOutputIsLeftAlone(t *testing.T) {
	passthrough(t, "df -h", lines(
		"Filesystem        Size  Used Avail Use% Mounted on",
		"dev               3.8G    0B  3.8G   0% /dev",
		"/dev/mapper/root 109.8G 68.8G 38.4G 65% /",
	))
	passthrough(t, "df -P", lines(
		"Filesystem     1024-blocks     Used Available Capacity Mounted on",
		"dev              3886860        0   3886860       0% /dev",
		"/dev/mapper/root 115104768 72152624  40286608      65% /",
	))
	passthrough(t, "df -i", lines(
		"Filesystem        Inodes IUsed   IFree IUse% Mounted on",
		"dev                971715   2129  949586   1% /dev",
		"/dev/mapper/root         0      0       0    - /",
	))
}

// free is claimed on the strength of its header alone: the text cannot say
// which form printed it, so the unit is assumed to be free's own default, the
// way du's is.
func TestFreeIsClaimedInThePipe(t *testing.T) {
	got := throughCmd(freeSample)
	if !strings.Contains(got, "7.6G") {
		t.Errorf("free was not claimed:\n%s", got)
	}
}

// lsof is claimed on the strength of its heading: COMMAND first, NAME last,
// the owner of the process between them. Everything else about the text — the
// arrow in an address, the bracket at the end — is left to the formatter.
func TestLsofIsClaimedInThePipe(t *testing.T) {
	got := throughCmd(lsofSample)
	if !strings.Contains(got, "<bold>COMMAND</>") {
		t.Errorf("lsof was not claimed:\n%q", got)
	}
	if !strings.Contains(got, "<green>(ESTABLISHED)</>") {
		t.Errorf("the socket state was not marked:\n%q", got)
	}
}

// `-F` prints one field per line with no heading to claim it by, and `-t`
// prints pids and nothing else. Neither is a table, and rewriting either is
// worse than leaving it alone.
func TestLsofScriptFormsAreLeftAlone(t *testing.T) {
	passthrough(t, "lsof -F", lines(
		"p",
		"769",
		"f",
		"cwd",
		"n/proc/769/cwd",
	))
	passthrough(t, "lsof -t", lines("769", "784"))
}

// `free -L` prints one line of name-and-value pairs with no header to claim it
// by, and no unit parse can safely add.
func TestFreeSingleLineFormIsLeftAlone(t *testing.T) {
	passthrough(t, "free -L", lines(
		"SwapUse      816644 CachUse     2993844  MemUse     5631420 MemFree      472552",
	))
}

// findmnt is claimed on the strength of its heading: its own column names in
// capitals, which df — the table a user is most likely to pipe beside it —
// spells in lower case.
func TestFindmntIsClaimedInThePipe(t *testing.T) {
	got := throughCmd(findmntSample)
	if !strings.Contains(got, "<bold>TARGET</>") {
		t.Errorf("findmnt was not claimed:\n%q", got)
	}
	if !strings.Contains(got, "<dim>├─</>") {
		t.Errorf("the mount tree was not marked:\n%q", got)
	}
}

// Raw output separates its fields with one space and no padding, so there are
// no columns to read by; JSON and output with no heading have nothing to claim
// them by at all. None of them may be rewritten.
func TestFindmntMachineFormsAreLeftAlone(t *testing.T) {
	passthrough(t, "findmnt -r", lines(
		"TARGET SOURCE FSTYPE OPTIONS",
		"/proc proc proc rw,nosuid,nodev,noexec,relatime",
		"/sys sysfs sysfs rw,nosuid,nodev,noexec,relatime",
	))
	passthrough(t, "findmnt -n", lines(
		"/       /dev/mapper/root[/@] btrfs rw,relatime,compress=zstd:3",
		"├─/proc proc proc rw,nosuid,nodev,noexec,relatime",
	))
	passthrough(t, "findmnt -J", lines(
		`{`,
		`    "filesystems": [`,
		`        { "target": "/", "fstype": "btrfs" }`,
		`    ]`,
		`}`,
	))
}

// lsblk's own column selection prints a heading made entirely of names
// findmnt also uses, which is a table neither tool may claim: lsblk refuses
// it for lacking NAME, and findmnt must refuse it too rather than paint
// another tool's output with its palette. The text passes through as it came.
func TestLsblkColumnSelectionIsLeftAlone(t *testing.T) {
	passthrough(t, "lsblk -o FSTYPE,SIZE", lines(
		"FSTYPE        SIZE",
		"            111.8G",
		"vfat            2G",
		"crypto_LUKS 109.8G",
		"btrfs       109.8G",
	))
	passthrough(t, "lsblk -o LABEL,UUID", lines(
		"LABEL                              UUID",
		"                                  1234-5678",
		"root                        abcd-ef01-2345",
	))
	// The df-style heading holds SOURCE, USE% and TARGET — names lsblk does
	// not have — so findmnt's own -D form is still claimed.
	got := throughCmd(findmntDfSample)
	if !strings.Contains(got, "<bold>SOURCE</>") {
		t.Errorf("findmnt -D was not claimed:\n%q", got)
	}
}
