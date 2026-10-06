// Package test checks parse's git formatting end to end through the exported
// git.Pipe, using the same text real git commands print.
//
// It calls git.Pipe rather than cmd.Pipe on purpose: cmd.Pipe guesses which
// tool produced the text and falls back to raw passthrough when nothing
// matches, while these tests want git's formatter exercised directly.
//
// Colors are replaced by readable tags so expectations are easy to read:
// Paint("green", "x") becomes "<green>x</>". The table printer is replaced by
// a simple " | " joiner, so these tests check content and color choices, not
// column padding.
package test

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/atif-1402/parse/cmd"
	"github.com/atif-1402/parse/cmd/git"
	"github.com/atif-1402/parse/internal/tool"
)

func init() {
	paint := func(color, s string) string {
		if color == "" || s == "" {
			return s
		}
		return "<" + color + ">" + s + "</>"
	}
	printTable := func(w io.Writer, headers []string, rows [][]tool.Cell) {
		fmt.Fprintln(w, strings.Join(headers, " | "))
		for _, row := range rows {
			parts := make([]string, len(row))
			for i, c := range row {
				parts[i] = paint(c.Color, c.Text)
			}
			fmt.Fprintln(w, strings.Join(parts, " | "))
		}
	}
	cmd.SetHooks(paint, printTable)
}

// format runs piped text through parse and returns the result without the
// trailing newline.
func format(input string) string {
	var sb strings.Builder
	git.Pipe(&sb, input)
	return strings.TrimRight(sb.String(), "\n")
}

// lines joins lines with newlines and adds the trailing newline git prints.
func lines(l ...string) string {
	return strings.Join(l, "\n") + "\n"
}

func want(l ...string) string {
	return strings.Join(l, "\n")
}

func check(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("output mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// ---------------------------------------------------------------------------
// git log
// ---------------------------------------------------------------------------

func TestLogDefaultFormatBecomesTable(t *testing.T) {
	in := lines(
		"commit abc1234def5678abc1234def5678abc1234def56",
		"Author: Alice <alice@example.com>",
		"Date:   Sat Oct 3 10:12:45 2026 +0530",
		"",
		"    Fix login bug",
		"",
		"commit def5678abc1234def5678abc1234def5678abc12",
		"Author: Bob <bob@example.com>",
		"Date:   Fri Oct 2 09:00:00 2026 +0000",
		"",
		"    Add parser support",
	)
	check(t, format(in), want(
		"COMMIT | AUTHOR | DATE | MESSAGE",
		"<yellow>abc1234</> | <cyan>Alice</> | 2026-10-03 | Fix login bug",
		"<yellow>def5678</> | <cyan>Bob</> | 2026-10-02 | Add parser support",
	))
}

func TestLogOnelineBecomesTwoColumnTable(t *testing.T) {
	in := lines(
		"abc1234 Fix login bug",
		"def5678 Add parser support",
	)
	check(t, format(in), want(
		"COMMIT | MESSAGE",
		"<yellow>abc1234</> | Fix login bug",
		"<yellow>def5678</> | Add parser support",
	))
}

// `git log -p` and `git show` both open with a commit header and then a patch.
// The patch used to keep every line of git's boilerplate; now it goes through
// the same formatter as `git diff`, so both spellings come out the same shape.
func TestShowFormatsHeaderAndPatch(t *testing.T) {
	in := lines(
		"commit abc1234def5678abc1234def5678abc1234def56",
		"Author: Alice <alice@example.com>",
		"Date:   Sat Oct 3 10:12:45 2026 +0530",
		"",
		"    Fix login bug",
		"",
		"diff --git a/main.go b/main.go",
		"index 111aaaa..222bbbb 100644",
		"--- a/main.go",
		"+++ b/main.go",
		"@@ -1,3 +1,3 @@",
		" package main",
		"-old line",
		"+new line",
	)
	check(t, format(in), want(
		"<yellow>abc1234</>  <cyan>Alice</>  <dim>2026-10-03</>",
		"Fix login bug",
		"",
		"<bold cyan>main.go</>  <dim>+1 -1</>",
		"<dim>@@ -1,3 +1,3 @@</>",
		" package main",
		"<red>-old line</>",
		"<green>+new line</>",
	))
}

// ---------------------------------------------------------------------------
// git status
// ---------------------------------------------------------------------------

func TestStatusGroupsFilesBySection(t *testing.T) {
	in := lines(
		"On branch main",
		"Your branch is ahead of 'origin/main' by 1 commit.",
		"  (use \"git push\" to publish your local commits)",
		"",
		"Changes to be committed:",
		"  (use \"git restore --staged <file>...\" to unstage)",
		"\tnew file:   foo.go",
		"",
		"Changes not staged for commit:",
		"  (use \"git add <file>...\" to update what will be committed)",
		"\tmodified:   main.go",
		"",
		"Untracked files:",
		"  (use \"git add <file>...\" to include in what will be committed)",
		"\tnotes.txt",
	)
	check(t, format(in), want(
		"<bold>BRANCH</>  <cyan>main</>  <yellow>ahead of origin/main by 1 commit</>",
		"",
		"<bold green>STAGED (1)</>",
		"  <green>new file</>  foo.go",
		"",
		"<bold red>UNSTAGED (1)</>",
		"  <red>modified</>  main.go",
		"",
		"<bold red>UNTRACKED (1)</>",
		"  <red>notes.txt</>",
	))
}

func TestStatusCleanTree(t *testing.T) {
	in := lines(
		"On branch main",
		"Your branch is up to date with 'origin/main'.",
		"",
		"nothing to commit, working tree clean",
	)
	check(t, format(in), want(
		"<bold>BRANCH</>  <cyan>main</>  <green>up to date with origin/main</>",
		"<green>clean - nothing to commit</>",
	))
}

func TestStatusDivergedBranch(t *testing.T) {
	in := lines(
		"On branch main",
		"Your branch and 'origin/main' have diverged,",
		"and have 1 and 2 different commits each, respectively.",
	)
	check(t, format(in), want(
		"<bold>BRANCH</>  <cyan>main</>  <yellow>diverged from origin/main (1 ahead, 2 behind)</>",
	))
}

func TestStatusDetachedHead(t *testing.T) {
	in := lines(
		"HEAD detached at abc1234",
		"nothing to commit, working tree clean",
	)
	check(t, format(in), want(
		"<bold>BRANCH</>  <cyan>HEAD detached at abc1234</>",
		"<green>clean - nothing to commit</>",
	))
}

// ---------------------------------------------------------------------------
// git diff
// ---------------------------------------------------------------------------

// A diff replaces git's four boilerplate lines with one line carrying the file
// name and how much moved, because the name used to scroll away from the hunk it
// belonged to. One file needs no summary table, which would just repeat the name.
func TestDiffSummarizesOneFileWithoutATable(t *testing.T) {
	in := lines(
		"diff --git a/main.go b/main.go",
		"index 111aaaa..222bbbb 100644",
		"--- a/main.go",
		"+++ b/main.go",
		"@@ -1,3 +1,3 @@",
		" package main",
		"-old line",
		"+new line",
	)
	check(t, format(in), want(
		"<bold cyan>main.go</>  <dim>+1 -1</>",
		"<dim>@@ -1,3 +1,3 @@</>",
		" package main",
		"<red>-old line</>",
		"<green>+new line</>",
	))
}

// Several files, one header line each. A summary table used to sit above these
// and was removed: it named every file twice before any hunk.
func TestDiffListsSeveralFilesWithoutATable(t *testing.T) {
	in := lines(
		"diff --git a/app.py b/app.py",
		"index 111aaaa..222bbbb 100644",
		"--- a/app.py",
		"+++ b/app.py",
		"@@ -1,2 +1,2 @@",
		"-bravo",
		"+BRAVO",
		"diff --git a/new.txt b/new.txt",
		"new file mode 100644",
		"index 0000000..3e75765",
		"--- /dev/null",
		"+++ b/new.txt",
		"@@ -0,0 +1 @@",
		"+hello",
	)
	check(t, format(in), want(
		"<bold cyan>app.py</>  <dim>+1 -1</>",
		"<dim>@@ -1,2 +1,2 @@</>",
		"<red>-bravo</>",
		"<green>+BRAVO</>",
		"",
		"<bold cyan>new.txt</>  <green>new file</>  <dim>+1 -0</>",
		"<dim>@@ -0,0 +1 @@</>",
		"<green>+hello</>",
	))
}

// The hunk header's trailing text is the enclosing function, which is the only
// part of the line worth reading.
func TestDiffKeepsTheSectionHeading(t *testing.T) {
	in := lines(
		"diff --git a/main.go b/main.go",
		"index 111aaaa..222bbbb 100644",
		"@@ -1,3 +1,3 @@ func handler(event):",
		" old",
		"+new",
	)
	got := format(in)
	if !strings.Contains(got, "<cyan>func handler(event):</>") {
		t.Errorf("section heading lost or not highlighted:\n%s", got)
	}
	if !strings.Contains(got, "<dim>@@ -1,3 +1,3 @@ </>") {
		t.Errorf("hunk range should be dimmed:\n%s", got)
	}
}

// A big change should be visible in the summary without opening the hunk.
func TestDiffColorsLargeChangesMoreProminently(t *testing.T) {
	big := "diff --git a/big.go b/big.go\nindex 111aaaa..222bbbb 100644\n@@ -1,200 +1,200 @@\n"
	for i := 0; i < 60; i++ {
		big += "-gone\n+new\n"
	}
	small := "diff --git a/small.go b/small.go\nindex 111aaaa..222bbbb 100644\n@@ -1,2 +1,2 @@\n-gone\n+new\n"

	got := format(big + small)
	if !strings.Contains(got, "<bold cyan>big.go</>  <bold yellow>+60 -60</>") {
		t.Errorf("a 120-line change should stand out:\n%s", got)
	}
	if !strings.Contains(got, "<bold cyan>small.go</>  <dim>+1 -1</>") {
		t.Errorf("a 2-line change should stay quiet:\n%s", got)
	}
}

func TestDiffstat(t *testing.T) {
	in := lines(
		" main.go | 3 ++-",
		" 1 file changed, 2 insertions(+), 1 deletion(-)",
	)
	check(t, format(in), want(
		" main.go | 3 <green>++</><red>-</>",
		"<bold> 1 file changed</>, <green>2 insertions(+)</>, <red>1 deletion(-)</>",
	))
}

// ---------------------------------------------------------------------------
// git branch
// ---------------------------------------------------------------------------

func TestBranchList(t *testing.T) {
	in := lines(
		"* main",
		"  feature",
		"  remotes/origin/main",
	)
	check(t, format(in), want(
		"* <green>main</>",
		"  feature",
		"  <red>remotes/origin/main</>",
	))
}

func TestBranchVerbose(t *testing.T) {
	in := lines(
		"* main    abc1234 Fix login bug",
		"  feature def5678 Other work",
	)
	check(t, format(in), want(
		"* <green>main</>    <yellow>abc1234</> Fix login bug",
		"  feature <yellow>def5678</> Other work",
	))
}

// ---------------------------------------------------------------------------
// everything else
// ---------------------------------------------------------------------------

func TestCommitOutput(t *testing.T) {
	in := lines(
		"[main abc1234] Fix login bug",
		" 1 file changed, 2 insertions(+), 1 deletion(-)",
		" create mode 100644 foo.go",
	)
	check(t, format(in), want(
		"<yellow>[main abc1234]</> Fix login bug",
		"<bold> 1 file changed</>, <green>2 insertions(+)</>, <red>1 deletion(-)</>",
		"<green> create mode 100644 foo.go</>",
	))
}

func TestErrorsAreRed(t *testing.T) {
	in := lines("fatal: not a git repository (or any of the parent directories): .git")
	check(t, format(in), want(
		"<red>fatal: not a git repository (or any of the parent directories): .git</>",
	))
}

func TestPushRefUpdates(t *testing.T) {
	in := lines(
		"To github.com:alice/parse.git",
		" * [new branch]      feature -> feature",
		"   abc1234..def5678  main -> main",
		" ! [rejected]        old -> old (non-fast-forward)",
	)
	check(t, format(in), want(
		"<dim>To github.com:alice/parse.git</>",
		"<green> * [new branch]      feature -> feature</>",
		"<yellow>   abc1234..def5678  main -> main</>",
		"<red> ! [rejected]        old -> old (non-fast-forward)</>",
	))
}

func TestExistingColorCodesAreStripped(t *testing.T) {
	in := "\x1b[32mhello\x1b[0m\n"
	check(t, format(in), "hello")
}

// ---------------------------------------------------------------------------
// git checkout
// ---------------------------------------------------------------------------

func TestCheckoutBranchWithSync(t *testing.T) {
	in := lines(
		"Switched to branch 'main'",
		"Your branch is up to date with 'origin/main'.",
	)
	check(t, format(in), want(
		"<green>Switched to branch 'main'</>",
		"<green>up to date with origin/main</>",
	))
}

func TestCheckoutNewBranch(t *testing.T) {
	in := lines(
		"branch 'feature' set up to track 'origin/feature'.",
		"Switched to a new branch 'feature'",
	)
	check(t, format(in), want(
		"<dim>branch 'feature' set up to track 'origin/feature'.</>",
		"<green>Switched to a new branch 'feature'</>",
	))
}

func TestCheckoutFromDetachedHead(t *testing.T) {
	in := lines(
		"Previous HEAD position was 5b5408c Add main",
		"Switched to branch 'main'",
	)
	check(t, format(in), want(
		"<dim>Previous HEAD position was 5b5408c Add main</>",
		"<green>Switched to branch 'main'</>",
	))
}

func TestCheckoutDetachedAdviceIsDimmed(t *testing.T) {
	in := lines(
		"Note: switching to 'HEAD~1'.",
		"",
		"You are in 'detached HEAD' state. You can look around, make experimental",
		"changes and commit them, and you can discard any commits you make in this",
		"state without impacting any branches by switching back to a branch.",
		"",
		"Turn off this advice by setting config variable advice.detachedHead to false",
		"",
		"HEAD is now at 5b5408c Add main",
	)
	check(t, format(in), want(
		"<yellow>Note: switching to 'HEAD~1'.</>",
		"",
		"<dim>You are in 'detached HEAD' state. You can look around, make experimental</>",
		"<dim>changes and commit them, and you can discard any commits you make in this</>",
		"<dim>state without impacting any branches by switching back to a branch.</>",
		"",
		"<dim>Turn off this advice by setting config variable advice.detachedHead to false</>",
		"",
		"<green>HEAD is now at 5b5408c Add main</>",
	))
}

// ---------------------------------------------------------------------------
// git stash
// ---------------------------------------------------------------------------

func TestStashListBecomesTable(t *testing.T) {
	in := lines(
		"stash@{0}: WIP on main: 0c123be Fix login bug",
		"stash@{1}: WIP on main: def5678 Add parser support",
	)
	check(t, format(in), want(
		"STASH | COMMIT | BRANCH | MESSAGE",
		"<yellow>stash@{0}</> | <yellow>0c123be</> | <cyan>main</> | Fix login bug",
		"<yellow>stash@{1}</> | <yellow>def5678</> | <cyan>main</> | Add parser support",
	))
}

func TestStashListWithoutCommitDropsThatColumn(t *testing.T) {
	in := lines(
		"stash@{0}: On main: my custom stash",
		"stash@{1}: On feature: older note",
	)
	check(t, format(in), want(
		"STASH | BRANCH | MESSAGE",
		"<yellow>stash@{0}</> | <cyan>main</> | my custom stash",
		"<yellow>stash@{1}</> | <cyan>feature</> | older note",
	))
}

func TestStashSave(t *testing.T) {
	in := lines("Saved working directory and index state WIP on main: 0c123be Fix login bug")
	check(t, format(in), want(
		"<green>Saved working directory and index state WIP on main: 0c123be Fix login bug</>",
	))
}

func TestStashPopKeepsDroppedLineLast(t *testing.T) {
	in := lines(
		"On branch main",
		"Your branch is up to date with 'origin/main'.",
		"",
		"Changes not staged for commit:",
		"  (use \"git add <file>...\" to update what will be committed)",
		"  (use \"git restore <file>...\" to discard changes in working directory)",
		"\tmodified:   main.go",
		"",
		"no changes added to commit (use \"git add\" and/or \"git commit -a\")",
		"Dropped refs/stash@{0} (a75f87030a270f8af92d073376b0f6fd45ced75b)",
	)
	check(t, format(in), want(
		"<bold>BRANCH</>  <cyan>main</>  <green>up to date with origin/main</>",
		"",
		"<bold red>UNSTAGED (1)</>",
		"  <red>modified</>  main.go",
		"<green>Dropped refs/stash@{0} (a75f87030a270f8af92d073376b0f6fd45ced75b)</>",
	))
}

func TestStashShowWithoutEntries(t *testing.T) {
	in := lines("No stash entries found.")
	check(t, format(in), want("<yellow>No stash entries found.</>"))
}

// ---------------------------------------------------------------------------
// git revert / cherry-pick / clean
// ---------------------------------------------------------------------------

func TestRevertSummary(t *testing.T) {
	in := lines(
		"[main 9e91e8e] Revert \"Fix greeting\"",
		" Date: Sat Oct 3 17:15:34 2026 +0530",
		" 1 file changed, 1 insertion(+), 1 deletion(-)",
	)
	check(t, format(in), want(
		"<yellow>[main 9e91e8e]</> Revert \"Fix greeting\"",
		"<dim> Date: Sat Oct 3 17:15:34 2026 +0530</>",
		"<bold> 1 file changed</>, <green>1 insertion(+)</>, <red>1 deletion(-)</>",
	))
}

func TestCherryPickSummary(t *testing.T) {
	in := lines(
		"[main b55e0e0] Add parser support",
		" 2 files changed, 10 insertions(+)",
	)
	check(t, format(in), want(
		"<yellow>[main b55e0e0]</> Add parser support",
		"<bold> 2 files changed</>, <green>10 insertions(+)</>",
	))
}

func TestCleanRemovalsAreRed(t *testing.T) {
	in := lines(
		"Removing junk.txt",
		"Removing jd/",
	)
	check(t, format(in), want(
		"<red>Removing junk.txt</>",
		"<red>Removing jd/</>",
	))
}

func TestCleanDryRunIsNotRed(t *testing.T) {
	in := lines("Would remove k.txt")
	check(t, format(in), want("<yellow>Would remove k.txt</>"))
}

// ---------------------------------------------------------------------------
// git remote / git blame
// ---------------------------------------------------------------------------

func TestRemoteVerboseBecomesTable(t *testing.T) {
	in := lines(
		"origin\tgit@github.com:alice/parse.git (fetch)",
		"origin\tgit@github.com:alice/parse.git (push)",
		"upstream\thttps://github.com/bob/x.git (fetch)",
		"upstream\thttps://github.com/bob/x.git (push)",
	)
	check(t, format(in), want(
		"REMOTE | URL | DIR",
		"<cyan>origin</> | git@github.com:alice/parse.git | <dim>fetch</>",
		"<cyan>origin</> | git@github.com:alice/parse.git | <dim>push</>",
		"<cyan>upstream</> | https://github.com/bob/x.git | <dim>fetch</>",
		"<cyan>upstream</> | https://github.com/bob/x.git | <dim>push</>",
	))
}

func TestBlameColorsMetadataOnly(t *testing.T) {
	in := lines(
		"40b2353a (Bartholomew Fitzgerald 2026-10-03 17:22:04 +0530 1) package main",
		"ecdcf6ea (Alice                  2026-10-03 17:22:04 +0530 4) func b() {}",
		"00000000 (Not Committed Yet      2026-10-03 17:22:08 +0530 5) func c() {}",
	)
	check(t, format(in), want(
		"<yellow>40b2353a</> (<cyan>Bartholomew</><dim> Fitzgerald 2026-10-03 17:22:04 +0530 </><green>1</>) package main",
		"<yellow>ecdcf6ea</> (<cyan>Alice</><dim>                  2026-10-03 17:22:04 +0530 </><green>4</>) func b() {}",
		"<dim>00000000</> (<cyan>Not Committed Yet</><dim>      2026-10-03 17:22:08 +0530 </><green>5</>) func c() {}",
	))
}

func TestBlameBoundaryCommitKeepsCaret(t *testing.T) {
	in := lines("^5b5408c (Alice 2026-10-03 17:15:21 +0530 2) ")
	check(t, format(in), want(
		"<yellow>^5b5408c</> (<cyan>Alice</><dim> 2026-10-03 17:15:21 +0530 </><green>2</>) ",
	))
}

func TestBlameSuppressAuthor(t *testing.T) {
	in := lines(
		"^49672bc 1) line1",
		"ed7c3cc0 2) line2",
	)
	check(t, format(in), want(
		"<yellow>^49672bc</> <green>1</>) line1",
		"<yellow>ed7c3cc0</> <green>2</>) line2",
	))
}

// ---------------------------------------------------------------------------
// git status -s / git log --graph
// ---------------------------------------------------------------------------

func TestStatusShortColorsCodes(t *testing.T) {
	in := lines(
		"A  new.txt",
		" M a.txt",
		"?? scratch.txt",
		"UU conflict.txt",
	)
	check(t, format(in), want(
		"<green>A </> new.txt",
		" <yellow>M</> a.txt",
		"<red>??</> scratch.txt",
		"<bold red>UU</> conflict.txt",
	))
}

func TestStatusShortBranchLine(t *testing.T) {
	in := lines("## main...origin/main [ahead 1]")
	check(t, format(in), want("<cyan>main</>...<dim>origin/main</> [<yellow>ahead 1</>]"))
}

func TestResetReportsStatusCodes(t *testing.T) {
	in := lines(
		"Unstaged changes after reset:",
		"M\ta.txt",
		"D\tgone.txt",
	)
	check(t, format(in), want(
		"<bold>Unstaged changes after reset:</>",
		"<yellow>M</>\ta.txt",
		"<red>D</>\tgone.txt",
	))
}

func TestPushRefLinesBeatStatusCodes(t *testing.T) {
	// a push line starts with two code characters too, so it must not be
	// mistaken for a status code
	in := lines(
		" ! [rejected]        old -> old (non-fast-forward)",
		"   abc1234..def5678  main -> main",
	)
	check(t, format(in), want(
		"<red> ! [rejected]        old -> old (non-fast-forward)</>",
		"<yellow>   abc1234..def5678  main -> main</>",
	))
}

func TestLogGraphDimsDrawing(t *testing.T) {
	in := lines(
		"* ed7c3cc Change line two",
		"| ",
		"| Author: Alice <alice@example.com>",
		"* 49672bc Initial commit",
	)
	check(t, format(in), want(
		"<dim>* </><yellow>ed7c3cc</> Change line two",
		"<dim>| </>",
		"<dim>| </><cyan>Author: Alice <alice@example.com></>",
		"<dim>* </><yellow>49672bc</> Initial commit",
	))
}

func TestLogGraphKeepsDiffstatIntact(t *testing.T) {
	// a leading space belongs to a diffstat row, not to a graph
	in := lines(" src/a.go | 2 +-")
	check(t, format(in), want(" src/a.go | 2 <green>+</><red>-</>"))
}

// ---------------------------------------------------------------------------
// git shortlog / reflog / worktree / submodule / config / describe
// ---------------------------------------------------------------------------

func TestShortlogSummaryBecomesTable(t *testing.T) {
	in := lines("     3\tAlice\n     1\tBob Fitzgerald")
	check(t, format(in), want(
		"COMMITS | AUTHOR",
		"<yellow>3</> | <cyan>Alice</>",
		"<yellow>1</> | <cyan>Bob Fitzgerald</>",
	))
}

func TestShortlogWithSubjects(t *testing.T) {
	in := lines(
		"Alice (2):",
		"      First commit",
		"      Second commit",
	)
	check(t, format(in), want(
		"<cyan>Alice</> <dim>(2)</>",
		"  <dim>First commit</>",
		"  <dim>Second commit</>",
	))
}

func TestReflogColorsHashAndRef(t *testing.T) {
	in := lines(
		"ed7c3cc (HEAD -> main) HEAD@{0}: reset: moving to HEAD",
		"02f4c2f HEAD@{5}: commit: Add line four",
	)
	check(t, format(in), want(
		"<yellow>ed7c3cc</> <dim>(HEAD -> main)</> <cyan>HEAD@{0}</>: reset: moving to HEAD",
		"<yellow>02f4c2f</> <cyan>HEAD@{5}</>: commit: Add line four",
	))
}

// A push ref update also starts with " * ", which is what a graph line looks
// like. It must keep its own colors.
func TestGraphPrefixDoesNotSwallowPushLines(t *testing.T) {
	in := lines(
		" * [new branch]      feature -> feature",
		" * [deleted]         (none)     old -> old",
	)
	check(t, format(in), want(
		"<green> * [new branch]      feature -> feature</>",
		"<red> * [deleted]         (none)     old -> old</>",
	))
}
