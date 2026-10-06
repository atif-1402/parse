// Tests for the parts of the git package the end-to-end tests in
// ../../test cannot reach: the unexported helpers, and the output that no
// pipe signature identifies.
//
// Colors are replaced by readable tags, the same way ../../test does it.
package git

import (
	"fmt"
	"io"
	"regexp"
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
	tool.PrintTable = func(w io.Writer, headers []string, rows [][]tool.Cell) {
		fmt.Fprintln(w, strings.Join(headers, " | "))
		for _, row := range rows {
			parts := make([]string, len(row))
			for i, c := range row {
				parts[i] = tool.Paint(c.Color, c.Text)
			}
			fmt.Fprintln(w, strings.Join(parts, " | "))
		}
	}
}

// lines joins lines with newlines and adds the trailing newline git prints.
func lines(l ...string) string {
	return strings.Join(l, "\n") + "\n"
}

// ---------------------------------------------------------------------------
// needsTerminal: what must reach git with the terminal attached
// ---------------------------------------------------------------------------

func TestNeedsTerminal(t *testing.T) {
	tests := []struct {
		sub  string
		args []string
		want bool
	}{
		// new commands
		{"checkout", []string{"main"}, false},
		{"checkout", []string{"-b", "feature"}, false},
		{"checkout", []string{"--", "file.go"}, false},
		{"clean", []string{"-fd"}, false},
		{"clean", []string{"-i"}, true},
		{"clean", []string{"--interactive"}, true},
		{"stash", []string{}, false},
		{"stash", []string{"push"}, false},
		{"stash", []string{"pop", "--index"}, false},
		{"stash", []string{"-p"}, true},
		{"stash", []string{"push", "--patch"}, true},
		{"revert", []string{"HEAD"}, false},
		{"revert", []string{"--no-edit", "HEAD"}, false},
		{"revert", []string{"--edit", "HEAD"}, true},
		{"revert", []string{"-e", "HEAD"}, true},
		{"revert", []string{"--continue"}, true},
		{"revert", []string{"--abort"}, true},
		{"cherry-pick", []string{"HEAD"}, false},
		{"cherry-pick", []string{"-n", "HEAD"}, false},
		{"cherry-pick", []string{"--skip"}, true},
		{"blame", []string{"main.go"}, false},
		{"remote", []string{"-v"}, false},
		// existing behavior must not change
		{"add", []string{"-p"}, true},
		{"add", []string{"file.go"}, false},
		{"commit", []string{"-m", "msg"}, false},
		{"commit", []string{}, true},
		{"tag", []string{"-a", "v1"}, true},
		{"tag", []string{"-a", "-m", "note", "v1"}, false},
		{"push", []string{"origin", "main"}, false},
	}
	for _, tt := range tests {
		if got := needsTerminal(tt.sub, tt.args); got != tt.want {
			t.Errorf("needsTerminal(%q, %q) = %v, want %v", tt.sub, tt.args, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// detectGit: guessing the command behind piped output
// ---------------------------------------------------------------------------

func TestDetectGit(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"status", "On branch main\n", "status"},
		{"diff", "diff --git a/main.go b/main.go\n@@ -1 +1 @@\n-a\n+b\n", "diff"},
		{"log default", "commit abc1234def5678abc1234def5678abc1234def56\n", "log"},
		{"log oneline", "abc1234 Fix login bug\n", "log"},
		{"branch", "* main\n  feature\n", "branch"},
		{"stash list", "stash@{0}: WIP on main: abc1234 Fix bug\n", "stash"},
		{"remote -v", "origin\tgit@github.com:alice/parse.git (fetch)\n", "remote"},
		{"blame", "abc1234 (Alice 2026-10-03 17:22:04 +0530 1) package main\n", "blame"},
		{"blame boundary", "^abc1234 (Alice 2026-10-03 17:22:04 +0530 1) code\n", "blame"},
		{"blame uncommitted", "00000000 (Not Committed Yet 2026-10-03 17:22:04 +0530 1) code\n", "blame"},
		{"unknown", "hello world\n", ""},
	}
	for _, tt := range tests {
		if got := Detect(tt.in); got != tt.want {
			t.Errorf("%s: Detect(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}

// A blame line looks exactly like a `git log --oneline` line, so it must be
// recognized as blame before the log check gets a chance to claim it.
func TestDetectGitPrefersBlameOverLog(t *testing.T) {
	in := "abc1234 (Alice 2026-10-03 17:22:04 +0530 1) package main\n"
	if got := Detect(in); got != "blame" {
		t.Errorf("Detect(%q) = %q, want blame", in, got)
	}
}

// ---------------------------------------------------------------------------
// formatRemote
// ---------------------------------------------------------------------------

func TestFormatRemoteNames(t *testing.T) {
	var sb strings.Builder
	if !formatRemote(&sb, "origin\nupstream\n") {
		t.Fatal("formatRemote returned false for a list of names")
	}
	want := "<yellow>origin</>\n<yellow>upstream</>\n"
	if sb.String() != want {
		t.Errorf("got %q, want %q", sb.String(), want)
	}
}

func TestFormatRemoteRejectsWhatItCannotIdentify(t *testing.T) {
	for _, in := range []string{
		"", // remote add prints nothing
		"fatal: not a git repository (or any of the parent directories): .git\n",
		"* remote origin\n  Fetch URL: git@github.com:alice/parse.git\n", // remote show
		"error: Could not read from remote repository.\n",
	} {
		var sb strings.Builder
		if formatRemote(&sb, in) {
			t.Errorf("formatRemote(%q) = true, want false", in)
		}
		if sb.String() != "" {
			t.Errorf("formatRemote(%q) wrote %q, want nothing", in, sb.String())
		}
	}
}

// ---------------------------------------------------------------------------
// small helpers
// ---------------------------------------------------------------------------

func TestSyncOf(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Your branch is up to date with 'origin/main'.", "up to date with origin/main"},
		{"Your branch is ahead of 'origin/main' by 2 commits.", "ahead of origin/main by 2 commits"},
		{"Your branch and 'origin/main' have diverged,", "diverged from origin/main"},
		{"Your branch is on a branch yet to be born.", "on a branch yet to be born"},
		{"On branch main", ""},
	}
	for _, tt := range tests {
		if got := syncOf(tt.in); got != tt.want {
			t.Errorf("syncOf(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSplitStashMsg(t *testing.T) {
	tests := []struct{ in, branch, msg string }{
		{"WIP on main: abc1234 Fix bug", "main", "abc1234 Fix bug"},
		{"On feature: my note", "feature", "my note"},
		{"WIP on main with no colon", "", "main with no colon"},
		{"plain message", "", "plain message"},
	}
	for _, tt := range tests {
		branch, msg := splitStashMsg(tt.in)
		if branch != tt.branch || msg != tt.msg {
			t.Errorf("splitStashMsg(%q) = %q, %q; want %q, %q", tt.in, branch, msg, tt.branch, tt.msg)
		}
	}
}

func TestSplitBlameWho(t *testing.T) {
	author, date := splitBlameWho("Alice                  2026-10-03 17:22:04 +0530")
	if author != "Alice" || date != "                  2026-10-03 17:22:04 +0530" {
		t.Errorf("got %q, %q; the padding must survive", author, date)
	}
	author, date = splitBlameWho("Not Committed Yet      2026-10-03 17:22:08 +0530")
	if author != "Not Committed Yet" || date != "      2026-10-03 17:22:08 +0530" {
		t.Errorf("got %q, %q", author, date)
	}
}

// TestGraphPrefix pins down the one tricky bit of the graph formatter: telling
// a graph line apart from a diff line or an indented commit subject, both of
// which start with a space.
func TestGraphPrefix(t *testing.T) {
	for _, c := range []struct{ line, want string }{
		{"* abc1234 msg", "* "},
		{"| * abc1234 msg", "| * "},
		{"| Author: Alice", "| "},
		{"| ", "| "},
		{"\\ 02f4c2f merge", "\\ "},
		{"+ added diff line", ""},
		{"- removed diff line", ""},
		{" src/a.go | 2 +-", ""},
		{"    commit subject", ""},
		{"", ""},
	} {
		if got := graphPrefix(c.line); got != c.want {
			t.Errorf("graphPrefix(%q) = %q, want %q", c.line, got, c.want)
		}
	}
}

func TestStatusCodeColor(t *testing.T) {
	for _, c := range []struct{ xy, want string }{
		{"??", red},
		{"!!", dim},
		{"UU", "bold red"},
		{"AA", "bold red"},
		{" D", red},
		{"D ", red},
		{"A ", green},
		{"??", red},
		{"M ", yellow},
		{" T", yellow},
		{"  ", ""},
	} {
		if got := statusCodeColor(c.xy); got != c.want {
			t.Errorf("statusCodeColor(%q) = %q, want %q", c.xy, got, c.want)
		}
	}
}

// The commands below have no distinctive marker a pipe could be recognized by,
// so they are only formatted when parse ran the command itself. These tests
// call formatGit directly with the subcommand, the way cmd.Git does.

func gitOut(sub, in string) string {
	var sb strings.Builder
	Format(&sb, sub, in)
	return strings.TrimRight(sb.String(), "\n")
}

func TestFormatSubmoduleStatus(t *testing.T) {
	in := lines(
		" 35fd7468bc6f794d2ed5a21f8721bd3ff7ecb5a7 mysub (heads/main)",
		"+913e86cd2deadc10259457932e7f86e265303368 other (v1.0)",
	)
	if got, want := gitOut("submodule", in), lines(
		" <yellow>35fd7468bc6f794d2ed5a21f8721bd3ff7ecb5a7</> <cyan>mysub</> <dim>(heads/main)</>",
		"<green>+</><yellow>913e86cd2deadc10259457932e7f86e265303368</> <cyan>other</> <dim>(v1.0)</>",
	); strings.TrimRight(got, "\n") != strings.TrimRight(want, "\n") {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatRemoteShow(t *testing.T) {
	in := lines(
		"* remote origin",
		"  Fetch URL: /tmp/origin.git",
		"  Remote branches:",
		"    main    tracked",
	)
	if got, want := gitOut("remote", in), lines(
		"<bold>REMOTE</>  <cyan>origin</>",
		"  <cyan>Fetch URL:</> /tmp/origin.git",
		"<bold>Remote branches:</>",
		"<dim>    main    tracked</>",
	); strings.TrimRight(got, "\n") != strings.TrimRight(want, "\n") {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// A formatter must never claim git's refusal to run: nothing in "fatal: ..."
// looks like a commit or a path, so it would come out with no color at all.
func TestGitErrorsAreColored(t *testing.T) {
	// only commands parse still runs itself; the rest pass through, where git
	// prints and colors its own messages
	for _, sub := range []string{"blame", "annotate", "submodule", "grep", "log", "status", "branch", "remote", "stash", "shortlog", "reflog", "tag"} {
		in := lines("fatal: No names found, cannot describe anything.")
		if got, want := gitOut(sub, in), "<red>fatal: No names found, cannot describe anything.</>"; got != want {
			t.Errorf("%s: got %q, want %q", sub, got, want)
		}
	}
}

func TestIsGitError(t *testing.T) {
	for _, c := range []struct {
		text string
		want bool
	}{
		{"fatal: bad revision\n", true},
		{"error: pathspec did not match\n", true},
		{"\nfatal: bad revision\n", true},
		{"commit abc1234 message\n", false},
		{"", false},
		{"  feature\n", false},
	} {
		if got := isGitError(c.text); got != c.want {
			t.Errorf("isGitError(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

// --- decorations and merges -------------------------------------------------
//
// A log's decorations say where HEAD is and which branches and tags point at
// which commit. Dropping them left `parse git log` with less information than
// the git it replaced.

func TestLogKeepsDecorations(t *testing.T) {
	text := "commit 2ac556b862378c8c44d9529951c952e08dde037a (HEAD -> master, tag: v2)\n" +
		"Author: t <t@t>\n" +
		"Date:   Sun Oct 4 02:12:27 2026 +0530\n" +
		"\n" +
		"    newest\n"
	commits, unknown := parseLog(text)
	if unknown != 0 || len(commits) != 1 {
		t.Fatalf("parseLog gave %d commits, %d unknown", len(commits), unknown)
	}
	if commits[0].deco != "HEAD -> master, tag: v2" {
		t.Errorf("decoration lost, got %q", commits[0].deco)
	}
}

func TestOnelineKeepsDecorations(t *testing.T) {
	hash, deco, msg, ok := onelineCommit("2ac556b (HEAD -> master) Merge branch 'side'")
	if !ok {
		t.Fatal("not recognized as an oneline commit")
	}
	if deco != "HEAD -> master" {
		t.Errorf("decoration lost, got %q", deco)
	}
	if msg != "Merge branch 'side'" {
		t.Errorf("message should not include the decoration, got %q", msg)
	}
	if hash != "2ac556b" {
		t.Errorf("hash wrong: %q", hash)
	}
}

// A decoration in parentheses belongs to git itself, so it has no message
// ambiguity: everything after it is the subject.
func TestOnelineDecorationOnly(t *testing.T) {
	_, deco, msg, ok := onelineCommit("2ac556b (tag: v1.0)")
	if !ok || deco != "tag: v1.0" || msg != "" {
		t.Errorf("got deco=%q msg=%q ok=%v", deco, msg, ok)
	}
}

func TestOnelineWithoutDecorationStillWorks(t *testing.T) {
	hash, deco, msg, ok := onelineCommit("b88be03 first commit")
	if !ok || deco != "" || msg != "first commit" || hash != "b88be03" {
		t.Errorf("got hash=%q deco=%q msg=%q ok=%v", hash, deco, msg, ok)
	}
}

// A merge commit must not look like a linear one.
func TestMergeCommitKeepsItsParents(t *testing.T) {
	text := "commit 2ac556b (HEAD -> master)\n" +
		"Merge: 9a6cb6503363baa2b81dbbaa7474990704b9f706 d792d382a291b4933915d3a16f15414e4243d247\n" +
		"Author: t <t@t>\n" +
		"Date:   Sun Oct 4 02:12:27 2026 +0530\n" +
		"\n" +
		"    Merge branch 'side'\n"
	commits, _ := parseLog(text)
	if len(commits) != 1 {
		t.Fatalf("got %d commits", len(commits))
	}
	if !strings.Contains(messageWithMerge(commits[0]), "merge of 9a6cb65, d792d38") {
		t.Errorf("merge parents lost: %q", messageWithMerge(commits[0]))
	}
}

func TestNonMergeMessageIsUnchanged(t *testing.T) {
	if got := messageWithMerge(commit{message: "plain"}); got != "plain" {
		t.Errorf("got %q", got)
	}
}

// git hides decorations when it sees a pipe, which is how parse runs it, so
// parse has to ask for them or show less than a terminal would.
func TestWithDecorateAsksGitForThem(t *testing.T) {
	got := withDecorate([]string{"log"})
	if len(got) != 2 || got[1] != "--decorate=short" {
		t.Errorf("got %v", got)
	}
	for _, args := range [][]string{
		{"log", "--no-decorate"},
		{"log", "--decorate=full"},
		{"log", "--pretty=oneline"},
		{"log", "--format=%h %s"},
		{"log", "-p"},
		{"log", "--graph"},
		{"log", "--stat"},
		{"status"},
		{"diff"},
		{"branch"},
	} {
		if got := withDecorate(args); len(got) != len(args) {
			t.Errorf("withDecorate(%v) changed it to %v", args, got)
		}
	}
}

// --- git diff ---------------------------------------------------------------
//
// A diff is the one place where a summary earns its keep: before the fix, four
// boilerplate lines per file stood between the reader and the change, and the
// file name scrolled away from the hunk it belonged to.

const twoFileDiff = `diff --git a/app.py b/app.py
index d9b2d36..d96b5f3 100644
--- a/app.py
+++ b/app.py
@@ -1,5 +1,5 @@ def handler(event):
 alpha
-bravo
+BRAVO
 charlie
diff --git a/new.txt b/new.txt
new file mode 100644
index 0000000..3e75765
--- /dev/null
+++ b/new.txt
@@ -0,0 +1 @@
+hello
`

func TestDiffCountsAndSplitsFiles(t *testing.T) {
	files, ok := parseDiff(twoFileDiff)
	if !ok || len(files) != 2 {
		t.Fatalf("parseDiff gave %d files, ok=%v", len(files), ok)
	}
	if files[0].path != "app.py" || files[0].added != 1 || files[0].removed != 1 {
		t.Errorf("first file wrong: %+v", files[0])
	}
	if files[1].path != "new.txt" || files[1].kind != "added" {
		t.Errorf("second file wrong: %+v", files[1])
	}
}

// The four boilerplate lines are the noise this replaces; none of them should
// reach the output.
func TestDiffDropsBoilerplate(t *testing.T) {
	var sb strings.Builder
	if !formatDiff(&sb, twoFileDiff) {
		t.Fatal("formatDiff refused a real diff")
	}
	out := sb.String()
	for _, noise := range []string{"diff --git", "index d9b2d36", "--- a/app.py", "+++ b/new.txt"} {
		if strings.Contains(out, noise) {
			t.Errorf("%q should have been replaced by the summary:\n%s", noise, out)
		}
	}
	for _, want := range []string{"app.py", "new.txt", "+BRAVO", "-bravo", "charlie"} {
		if !strings.Contains(out, want) {
			t.Errorf("lost %q:\n%s", want, out)
		}
	}
}

// Context lines are the code, so they must survive verbatim.
func TestDiffKeepsContextLines(t *testing.T) {
	var sb strings.Builder
	formatDiff(&sb, twoFileDiff)
	// " hello" is an added line, not context, so it is colored and no longer
	// starts with a space. It is checked by TestDiffDropsBoilerplate.
	for _, want := range []string{" alpha", " charlie"} {
		if !strings.Contains(sb.String(), want) {
			t.Errorf("context line %q was lost or altered:\n%s", want, sb.String())
		}
	}
}

func TestDiffNamesBothSidesOfARename(t *testing.T) {
	in := "diff --git c/f.txt i/renamed.txt\nsimilarity index 75%\nrename from f.txt\n" +
		"rename to renamed.txt\nindex de98044..d68dd40 100644\n@@ -1,3 +1,4 @@\n a\n+d\n"
	files, ok := parseDiff(in)
	if !ok || len(files) != 1 {
		t.Fatalf("got %d files", len(files))
	}
	if files[0].from != "f.txt" || files[0].path != "renamed.txt" {
		t.Errorf("rename lost: %+v", files[0])
	}
	if name := diffName(files[0]); !strings.Contains(name, "f.txt") || !strings.Contains(name, "renamed.txt") {
		t.Errorf("name should show both ends, got %q", name)
	}
	if strings.Contains(files[0].lines[0], "similarity") {
		t.Errorf("similarity index is implied by the kind, got %q", files[0].lines[0])
	}
}

func TestDiffKindForTheAwkwardCases(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"deleted", "diff --git a/x b/x\ndeleted file mode 100644\nindex a..b 100644\n", "deleted"},
		{"mode", "diff --git a/x b/x\nold mode 100644\nnew mode 100755\n", "mode 100755"},
	}
	for _, c := range cases {
		files, ok := parseDiff(c.in)
		if !ok || len(files) != 1 {
			t.Fatalf("%s: got %d files", c.name, len(files))
		}
		if got := kindText(files[0]); got != c.want {
			t.Errorf("%s: kindText gave %q, want %q", c.name, got, c.want)
		}
	}
}

// Binary content has no hunks, and saying so beats printing an empty block.
func TestDiffBinaryFile(t *testing.T) {
	in := "diff --git a/blob.bin b/blob.bin\nnew file mode 100644\nBinary files a/blob.bin and b/blob.bin differ\n"
	files, _ := parseDiff(in)
	if !files[0].binary {
		t.Error("binary file not recognized")
	}
	var sb strings.Builder
	formatDiff(&sb, in)
	if !strings.Contains(sb.String(), "binary contents not shown") {
		t.Errorf("no binary note:\n%s", sb.String())
	}
}

// Text that is not a diff must be refused, not turned into an empty table.
func TestParseDiffRefusesNonDiff(t *testing.T) {
	for _, in := range []string{"", "hello world\n", "fatal: not a git repository\n"} {
		if _, ok := parseDiff(in); ok {
			t.Errorf("parseDiff accepted %q", in)
		}
	}
	if formatDiff(io.Discard, "just some text\n") {
		t.Error("formatDiff claimed plain text")
	}
}

// git's own --stat lines are not diff headers and must not become a file.
func TestParseDiffIgnoresTrailingStat(t *testing.T) {
	in := twoFileDiff + " 2 files changed, 3 insertions(+), 1 deletion(-)\n"
	files, _ := parseDiff(in)
	if len(files) != 2 {
		t.Errorf("the stat line was counted as a file: %d", len(files))
	}
}

// --- side-by-side -----------------------------------------------------------
//
// The point of the two-column view is that the old line sits beside the new one
// it became, so the pairing is what these tests pin down.

// `git show | parse -w` and `parse git show -w` must give the same answer. The
// show case checked for -w separately from the diff case and forgot, so the
// piped form quietly came out one column while the direct form came out two.
func TestShowHonorsSideBySideFromThePipe(t *testing.T) {
	in := lines(
		"commit abc1234def5678abc1234def5678abc1234def56",
		"Author: Alice <alice@example.com>",
		"Date:   Sat Oct 3 10:12:45 2026 +0530",
		"",
		"    Fix login bug",
		"",
		"diff --git a/main.go b/main.go",
		"--- a/main.go",
		"+++ b/main.go",
		"@@ -1,3 +1,3 @@",
		" package main",
		"-old line",
		"+new line",
	)

	var piped, one strings.Builder
	sbsAlways = true // as `parse -w` sets it for piped input
	Format(&piped, Detect(in), in)
	sbsAlways = false
	Format(&one, Detect(in), in)

	got := stripTags(piped.String())
	if !strings.Contains(got, "│") {
		t.Errorf("`git show | parse -w` came out one column:\n%s", got)
	}
	if !strings.Contains(got, "old line") || !strings.Contains(got, "new line") {
		t.Errorf("the two-column view lost the lines it was showing:\n%s", got)
	}
	if stripTags(one.String()) == got {
		t.Errorf("-w made no difference between the piped and direct forms:\n%s", got)
	}
}

func TestSideBySidePairsOldWithNew(t *testing.T) {
	in := lines(
		"diff --git a/app.py b/app.py",
		"index 111aaaa..222bbbb 100644",
		"--- a/app.py",
		"+++ b/app.py",
		"@@ -1,3 +1,3 @@",
		" keep",
		"-def old():",
		"+def new():",
		" tail",
	)
	var sb strings.Builder
	if !sideBySide(&sb, in, 100) {
		t.Fatal("sideBySide refused a real diff")
	}
	out := stripTags(sb.String())
	row := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "def old") || strings.Contains(l, "def new") {
			row = l
			break
		}
	}
	// The old line and the new one have to be on the same row, with the
	// separator between them. The +/- markers are gone: the columns and the
	// colors say which side is which.
	if !strings.Contains(row, "def old():") || !strings.Contains(row, "def new():") ||
		!strings.Contains(row, "│") {
		t.Errorf("old and new are not side by side: %q\n%s", row, out)
	}
	if strings.Index(row, "def old") > strings.Index(row, "def new") {
		t.Errorf("the old version should be on the left: %q", row)
	}
}

// Line numbers let you jump straight to a line in your editor, so each side is
// numbered independently and a line that exists on only one side has a blank
// number on the other.
func TestSideBySideNumbersBothSides(t *testing.T) {
	in := lines(
		"diff --git a/f.txt b/f.txt",
		"index 111aaaa..222bbbb 100644",
		"@@ -10,2 +10,3 @@",
		" ctx",
		"-gone",
		"+added",
	)
	files, _ := parseDiff(in)
	rows := collectSbsRows(files)[0]
	for _, r := range rows {
		if r.left == "ctx" {
			if r.leftNo != 10 || r.rightNo != 10 {
				t.Errorf("context line should be 10 on both sides, got %d/%d", r.leftNo, r.rightNo)
			}
		}
		if r.left == "gone" && r.right == "added" {
			if r.leftNo != 11 {
				t.Errorf("removed line is old line 11, got %d", r.leftNo)
			}
			if r.rightNo != 11 {
				t.Errorf("added line is new line 11, got %d", r.rightNo)
			}
		}
	}
}

// A run of two removals followed by two additions must not number the second
// removal as 13 and 14: the counters have already moved past both by the time
// the run is flushed.
func TestSideBySideNumbersLongRunsFromWhereTheyStart(t *testing.T) {
	in := lines(
		"diff --git a/f.txt b/f.txt",
		"index 111aaaa..222bbbb 100644",
		"@@ -1,4 +1,4 @@",
		"-one",
		"-two",
		"-three",
		"+uno",
		"+dos",
		"+tres",
	)
	files, _ := parseDiff(in)
	var rows []sbsRow
	for _, r := range collectSbsRows(files)[0] {
		if !strings.HasPrefix(r.left, "@@") { // the hunk header is not a row
			rows = append(rows, r)
		}
	}
	want := []struct {
		l, r   string
		ln, rn int
	}{
		{"one", "uno", 1, 1},
		{"two", "dos", 2, 2},
		{"three", "tres", 3, 3},
	}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d", len(rows), len(want))
	}
	for i, w := range want {
		got := rows[i]
		if got.left != w.l || got.right != w.r || got.leftNo != w.ln || got.rightNo != w.rn {
			t.Errorf("row %d: got %+v, want %s/%s at %d/%d", i, got, w.l, w.r, w.ln, w.rn)
		}
	}
}

// An added-only file has nothing on the left, and a deleted-only file nothing on
// the right. Neither may show a line number for the side that has no line.
func TestSideBySideOneSidedFiles(t *testing.T) {
	added := lines(
		"diff --git a/new.txt b/new.txt",
		"new file mode 100644",
		"index 0000000..222bbbb",
		"@@ -0,0 +1,2 @@",
		"+first",
		"+second",
	)
	files, _ := parseDiff(added)
	for _, r := range collectSbsRows(files)[0] {
		if r.leftNo == -1 { // the hunk header
			continue
		}
		if r.left != "" && r.leftNo != 0 {
			t.Errorf("a new file has no old lines, got left %q at %d", r.left, r.leftNo)
		}
		if r.right != "" && r.rightNo == 0 {
			t.Errorf("added line %q lost its number", r.right)
		}
	}

	deleted := lines(
		"diff --git a/gone.txt b/gone.txt",
		"deleted file mode 100644",
		"index 111aaaa..0000000",
		"@@ -1,2 +0,0 @@",
		"-first",
		"-second",
	)
	files, _ = parseDiff(deleted)
	for _, r := range collectSbsRows(files)[0] {
		if r.leftNo == -1 { // the hunk header
			continue
		}
		if r.right != "" && r.rightNo != 0 {
			t.Errorf("a deleted file has no new lines, got right %q at %d", r.right, r.rightNo)
		}
	}
}

// A long line must be cut with a visible marker, not silently: the tail of a
// line is usually where the interesting part is.
func TestTruncateMarksWhatItCut(t *testing.T) {
	long := strings.Repeat("x", 100)
	got := truncate(long, 20)
	if utf8Len(got) != 20 {
		t.Errorf("width %d, wanted 20", utf8Len(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("no marker: %q", got)
	}
	if truncate("short", 20) != "short" {
		t.Error("a line that fits must not be touched")
	}
}

// A double-width character occupies two columns, so padding has to count it as
// two or the columns drift apart.
func TestWideCharsCountAsTwoColumns(t *testing.T) {
	if got := utf8Len("日本語"); got != 6 {
		t.Errorf("utf8Len(日本語) = %d, want 6", got)
	}
	if got := utf8Len("ab日本"); got != 6 {
		t.Errorf("utf8Len(ab日本) = %d, want 6", got)
	}
	if got := utf8Len(stripTags("<red>日本</>")); got != 4 {
		t.Errorf("color codes must not be counted, got %d", got)
	}
}

func TestSideBySideRespectsTheGivenWidth(t *testing.T) {
	in := lines(
		"diff --git a/f.txt b/f.txt",
		"index 111aaaa..222bbbb 100644",
		"@@ -1,1 +1,1 @@",
		"-before",
		"+after",
	)
	for _, width := range []int{120, 80, 60} {
		var sb strings.Builder
		sideBySide(&sb, in, width)
		for _, l := range strings.Split(strings.TrimRight(sb.String(), "\n"), "\n") {
			if strings.HasPrefix(l, "@@") || strings.HasPrefix(l, "f.txt") {
				continue
			}
			if n := utf8Len(stripTags(l)); n > width {
				t.Errorf("width %d: line is %d wide: %q", width, n, stripTags(l))
			}
		}
	}
}

// Too narrow for two honest columns, so it falls back to one rather than
// pretending.
// A terminal too narrow for two honest columns gets one instead of a lie.
func TestSideBySideFallsBackWhenTooNarrow(t *testing.T) {
	in := lines(
		"diff --git a/f.txt b/f.txt",
		"index 111aaaa..222bbbb 100644",
		"@@ -1,1 +1,1 @@",
		"-before",
		"+after",
	)
	var sb strings.Builder
	sideBySide(&sb, in, 24)
	if strings.Contains(stripTags(sb.String()), "│") {
		t.Errorf("a 24-column terminal should not get two columns:\n%s", sb.String())
	}
	if !strings.Contains(stripTags(sb.String()), "before") || !strings.Contains(stripTags(sb.String()), "after") {
		t.Errorf("the one-column fallback lost content:\n%s", sb.String())
	}
}

// parse's own flag must never reach git, which would refuse to run.
func TestWantSideBySideStripsParseFlag(t *testing.T) {
	for _, in := range [][]string{
		{"diff", "-w"},
		{"diff", "--side-by-side"},
		{"-w", "diff", "--cached"},
	} {
		on, keep := wantSideBySide(in)
		if !on {
			t.Errorf("wantSideBySide(%v) said no", in)
		}
		for _, a := range keep {
			if a == "-w" || a == "--side-by-side" {
				t.Errorf("%q would have been passed to git: %v", a, keep)
			}
		}
	}
	if on, keep := wantSideBySide([]string{"diff", "--stat"}); on || len(keep) != 2 {
		t.Errorf("a plain diff should be left alone, got on=%v keep=%v", on, keep)
	}
}

// -w means "ignore whitespace" to every git command except the ones that lay out
// a diff, and stripping it everywhere silently changed what those commands did
// with nothing on screen to show it.
func TestWantSideBySideLeavesOtherCommandsAlone(t *testing.T) {
	for _, in := range [][]string{
		{"log", "-w"},
		{"add", "-p", "-w"},
		{"status", "-w"},
		{"grep", "-w", "TODO"},
	} {
		if _, keep := wantSideBySide(in); len(keep) != len(in) {
			t.Errorf("wantSideBySide(%v) dropped arguments, kept %v", in, keep)
		}
	}
}

// A value that happens to read "-w" is not a flag. `git commit -m -w` sets the
// commit message to the word "-w", and eating that would commit the wrong thing.
func TestWantSideBySideDoesNotEatFlagValues(t *testing.T) {
	for _, in := range [][]string{
		{"diff", "-m", "-w"},
		{"diff", "--message", "-w"},
		{"diff", "--unified", "-w"},
		// After a bare "--" every word is a path, so "-w" here is a file name.
		{"diff", "--", "-w"},
	} {
		_, keep := wantSideBySide(in)
		joined := strings.Join(keep, " ")
		if !strings.Contains(joined, "-w") {
			t.Errorf("wantSideBySide(%v) ate a path or flag value, kept %v", in, keep)
		}
	}
}

// reColorTag matches the markers the test hook adds, so width and layout
// assertions read the text a terminal would show.
var reColorTag = regexp.MustCompile(`</?(?:bold )?(?:dim|red|green|yellow|cyan)?>`)

func stripTags(s string) string {
	return reColorTag.ReplaceAllString(s, "")
}

// A global git option and a subcommand flag can share a letter. In
// `git -C dir commit` the -C is the directory, not --reuse-message, so the
// commit is still interactive and must not be handed a pager.
func TestNeedsTerminalIgnoresGlobalOptions(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"bare commit", []string{"commit"}, true},
		{"commit after -C", []string{"-C", "/tmp/repo", "commit"}, true},
		{"commit after --git-dir", []string{"--git-dir", "/tmp/repo/.git", "commit"}, true},
		{"commit with a message", []string{"-C", "/tmp/repo", "commit", "-m", "msg"}, false},
		{"commit reusing a message", []string{"-C", "/tmp/repo", "commit", "-C", "HEAD"}, false},
		{"add -p after -C", []string{"-C", "/tmp/repo", "add", "-p"}, true},
		{"add after -C", []string{"-C", "/tmp/repo", "add", "big.txt"}, false},
		{"log after -C", []string{"-C", "/tmp/repo", "log"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NeedsTerminal(tt.args); got != tt.want {
				t.Errorf("NeedsTerminal(%q) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}
