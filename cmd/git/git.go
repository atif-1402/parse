// Package git formats git output: running git, detecting piped git
// output, and turning it into something readable.
package git

import (
	"github.com/atif-1402/parse/internal/tool"

	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Colors and the table cell come from the shared plumbing. They are aliased
// here so the formatting code below reads like ordinary parse code.
const (
	bold   = tool.Bold
	dim    = tool.Dim
	red    = tool.Red
	green  = tool.Green
	yellow = tool.Yellow
	cyan   = tool.Cyan
)

// Cell is one table value with an optional color name.
type Cell = tool.Cell

// sbsWidth is the terminal width the two-column view fits into. It is set once
// at startup from the terminal itself, because a diff is the one thing parse
// prints that cannot be read without knowing how much room it has.
var (
	sbsWidth     int
	sbsRequested bool
	// sbsAlways is set by `parse --side-by-side`, for the piped form where
	// parse has no git argument list to look at.
	sbsAlways bool
)

// SetWidth records the width to lay the two-column diff out in.
func SetWidth(n int) { sbsWidth = n }

// SetSideBySide turns the two-column diff on for text that arrives on stdin,
// where there is no argument list to inspect.
func SetSideBySide(on bool) { sbsAlways = on }

// wantSideBySide reports whether the two-column view was asked for, and strips
// parse's own flag out of the argument list so it is never handed to git, which
// has no such option and would refuse to run.
//
// Only for the commands that produce a diff to lay out. `-w` means something
// else entirely to `git log` and `git add -p` (ignore whitespace), and stripping
// it there silently changed what those commands did, with nothing on screen to
// say so.
//
// Only when it is really a flag, not an argument. git takes values that look
// exactly like this: `git commit -m -w` sets the commit message to "-w", and
// eating that word would have committed the wrong thing. So an occurrence that
// follows a value-taking flag is left alone.
func wantSideBySide(args []string) (bool, []string) {
	if !diffSubcommand(gitSubcommand(args)) {
		return false, args
	}
	keep := make([]string, 0, len(args))
	on := false
	expectsValue := false
	afterTerminator := false
	for _, a := range args {
		// A flag that takes a separate value swallows the next word whatever it
		// looks like.
		if expectsValue {
			expectsValue = false
			keep = append(keep, a)
			continue
		}
		if afterTerminator {
			// Everything past "--" is a path or a revision, never a flag.
			keep = append(keep, a)
			continue
		}
		if a == "--" {
			afterTerminator = true
			keep = append(keep, a)
			continue
		}
		if gitFlagTakesValue(a) {
			expectsValue = true
			keep = append(keep, a)
			continue
		}
		switch a {
		case "-w", "--side-by-side":
			on = true
		default:
			keep = append(keep, a)
		}
	}
	return on, keep
}

// diffSubcommand reports whether a git subcommand's output can be a diff worth
// laying out in two columns.
//
// `git log` is not here even though `log -p` prints a diff: -w means "ignore
// whitespace" to `git log`, and taking that flag away from a plain `git log -w`
// would silently change what the command does. A user who wants the two-column
// view of a log's patch asks for it as `parse git log -p -w`, which is
// unambiguous, rather than having parse guess which meaning was meant.
func diffSubcommand(sub string) bool {
	switch sub {
	case "diff", "show", "format-patch", "range-diff":
		return true
	}
	return false
}

// gitFlagTakesValue reports whether a git flag consumes the argument after it.
// Short flags are only listed when they are known to take one, because being
// wrong in that direction would swallow a real flag.
func gitFlagTakesValue(a string) bool {
	switch a {
	case "-C", "-c", "--git-dir", "--work-tree", "--namespace", "--config-env",
		"-m", "--message", "-F", "--file", "--author", "--date", "--pretty",
		"--format", "-o", "--output", "--unified", "-U", "--diff-filter",
		"--src-prefix", "--dst-prefix", "--find-renames", "-S", "-G", "--color",
		"--decorate", "--log-size", "--since", "--until", "--before", "--after",
		"--grep", "--author=", "--committer", "--max-count", "-n":
		return true
	}
	return false
}

// GitPipe formats piped git output (`git ... | parse`), guessing which
// command produced it.
func Pipe(w io.Writer, text string) {
	text = tool.StripANSI(text)
	Format(w, Detect(text), text)
}

// Git commands parse runs and formats itself. Anything else either prints
// output that already reads well on its own (rev-parse, ls-files, cat-file,
// worktree list, config --list, describe, am, ...) or prompts (bisect, ...).
// Those pass straight through to git untouched, so scripts that read them
// keep working.
var gitCommands = map[string]bool{
	"clone": true, "init": true, // start a working area
	"add": true, "mv": true, "restore": true, "rm": true, "clean": true, // the current change
	"blame": true, "annotate": true, "diff": true, "grep": true, // inspect
	"log": true, "show": true, "status": true, // history and state
	"shortlog": true, "reflog": true, // history and state
	"branch": true, "checkout": true, "switch": true, "tag": true, // move between refs
	"commit": true, "merge": true, "rebase": true, "reset": true, // grow and tweak history
	"cherry-pick": true, "revert": true, // grow and tweak history
	"remote": true, "stash": true, "submodule": true, // collaborate
	"fetch": true, "pull": true, "push": true, // network
}

// ---------------------------------------------------------------------------
// Running git
// ---------------------------------------------------------------------------

// Git implements `parse git <args>`: run git, format its output, and
// return git's exit code.
func Run(args []string) int {
	sub := gitSubcommand(args)
	if !gitCommands[sub] || needsTerminal(sub, subcommandArgs(args)) {
		return tool.Passthrough("git", args)
	}

	// git decides whether to decorate its log output by looking at stdout: a
	// terminal gets branch names, tags and HEAD markers, a pipe gets nothing.
	// parse captures through a pipe, so `parse git log` was quietly showing
	// less than `git log` in a terminal. Asking for the decorations explicitly
	// gets them back, unless the user has already said what they want.
	sbsRequested, args = wantSideBySide(args)
	args = withDecorate(args)

	cmd := exec.Command("git", args...)
	cmd.Stdin = os.Stdin
	// stderr stays on stderr, the way git itself puts it. Merging the two made
	// git's own diagnostics part of the text parse formats, so a fatal error
	// came out on stdout and a reader of parse's output could not tell it from
	// data.
	cmd.Stderr = os.Stderr
	data, err := cmd.Output()
	code := 0
	if err != nil {
		code = tool.ExitCode(err)
		if _, ran := err.(*exec.ExitError); !ran {
			return code // git could not be started
		}
	}

	out := bufio.NewWriter(os.Stdout)
	Format(out, sub, tool.StripANSI(string(data)))
	out.Flush()
	return code
}

// withDecorate adds --decorate=short to a log when the user has not chosen a
// decoration mode of their own. It only touches `git log`, and only the display
// forms: piping log output into another tool, or asking for a patch, must keep
// exactly what git would have printed.
func withDecorate(args []string) []string {
	sub := gitSubcommand(args)
	if sub != "log" {
		return args
	}
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--decorate"), strings.HasPrefix(a, "--no-decorate"),
			strings.HasPrefix(a, "--pretty"), strings.HasPrefix(a, "--format"),
			a == "-p", a == "--patch", a == "--stat", a == "--graph",
			strings.HasPrefix(a, "--no-patch"), strings.HasPrefix(a, "--raw"):
			return args
		}
	}
	// --decorate=short is the form git's own "auto" mode uses for a terminal:
	// branch names and tags, but no full URLs or stash subjects.
	out := make([]string, 0, len(args)+1)
	out = append(out, args...)
	return append(out, "--decorate=short")
}

// gitSubcommand finds the subcommand in `git [global flags] <command> ...`.
func gitSubcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-C" || a == "-c" || a == "--git-dir" || a == "--work-tree" ||
			a == "--namespace" || a == "--config-env":
			i++ // skip the flag's value
		case strings.HasPrefix(a, "-"):
			// some other global flag
		default:
			return a
		}
	}
	return ""
}

// needsTerminal reports whether git may open an editor or prompt, in which
// case the output must not be captured.
// NeedsTerminal reports whether these git arguments run something interactive,
// such as `git add -p` or `git commit` opening an editor. Such a command reads
// the keyboard and must be handed the terminal untouched, so the pager has to
// know about it as well as git.Run.
func NeedsTerminal(args []string) bool {
	return needsTerminal(gitSubcommand(args), subcommandArgs(args))
}

// subcommandArgs returns the arguments that belong to the subcommand, dropping
// git's global options and the values they take.
//
// This matters because a global option and a subcommand flag can share a letter:
// in `git -C dir commit` the `-C` is the directory to run in, but a naive scan
// reads it as `commit --reuse-message`, decides a message was supplied, and
// concludes the commit is not interactive when it very much is.
func subcommandArgs(args []string) []string {
	for i, a := range args {
		switch {
		case a == "-C" || a == "-c" || a == "--git-dir" || a == "--work-tree" ||
			a == "--namespace" || a == "--config-env":
			continue // the next argument is this one's value
		case strings.HasPrefix(a, "-"):
			continue // a global flag of its own
		default:
			return args[i+1:]
		}
	}
	return nil
}

// needsTerminal reports whether git may open an editor or prompt, in which case
// the output must not be captured. args are the subcommand's own arguments, so
// the global options in front of them cannot be mistaken for its flags.
func needsTerminal(sub string, args []string) bool {
	switch sub {
	case "add", "restore", "reset":
		return hasFlag(args, "--patch", 'p') || hasFlag(args, "--interactive", 'i')
	case "rebase":
		return hasFlag(args, "--interactive", 'i') || hasFlag(args, "--continue", 0) || hasFlag(args, "--edit-todo", 0)
	case "commit":
		hasMessage := hasFlag(args, "--message", 'm') || hasFlag(args, "--file", 'F') ||
			hasFlag(args, "--reuse-message", 'C') || hasFlag(args, "--no-edit", 0)
		return !hasMessage
	case "tag":
		if hasFlag(args, "--annotate", 'a') || hasFlag(args, "--sign", 's') {
			return !(hasFlag(args, "--message", 'm') || hasFlag(args, "--file", 'F'))
		}
	case "clean":
		return hasFlag(args, "--interactive", 'i')
	case "stash":
		// `git stash` with no subcommand is `stash push`.
		return hasFlag(args, "--patch", 'p')
	case "revert", "cherry-pick":
		// These only open the commit message editor when stdin, stdout and
		// stderr are all terminals. We capture stdout, so the default needs no
		// editor; only ask for one when the user explicitly did.
		return hasFlag(args, "--edit", 'e') || hasFlag(args, "--edit-todo", 0) ||
			hasFlag(args, "--continue", 0) || hasFlag(args, "--skip", 0) ||
			hasFlag(args, "--abort", 0) || hasFlag(args, "--quit", 0)
	}
	return false
}

// hasFlag matches a long flag exactly, or a short flag anywhere in a cluster
// such as -am. Pass 0 for short if the flag has no short form.
func hasFlag(args []string, long string, short byte) bool {
	for _, a := range args {
		if a == long {
			return true
		}
		if short != 0 && len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.IndexByte(a[1:], short) >= 0 {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Detecting piped output
// ---------------------------------------------------------------------------

var reCommitHdr = regexp.MustCompile(`^commit [0-9a-f]{7,40}`)

// detectGit guesses which git command produced text. It returns "" when it
// cannot tell, and the output is then colored generically.
func Detect(text string) string {
	lines := strings.Split(text, "\n")
	first := tool.FirstNonEmpty(lines)
	_, _, _, oneline := onelineCommit(first)

	switch {
	case hasAnyPrefix(first, "On branch ", "HEAD detached ", "Not currently on any branch",
		"rebase in progress", "interactive rebase in progress"):
		return "status"
	case strings.HasPrefix(first, "diff --git "), reDiffHeader.MatchString(first):
		// The header alone is not enough. --word-diff and --color-words print
		// the same header and then no hunks, and a README that explains diff
		// format starts a line with these words too. Claiming those put a
		// meaningless "+0 -0" summary in front of output parse had not actually
		// parsed, so the whole stream was rewritten. Only claim text that
		// really records an edit, and let everything else through untouched.
		if _, ok := parseDiff(text); ok {
			return "diff"
		}
	case reStashLine.MatchString(first):
		return "stash"
	case reRemoteV.MatchString(first):
		return "remote"
	case reReflog.MatchString(first):
		return "reflog"
	case reShortlogHead.MatchString(first), reShortlogSum.MatchString(first):
		return "shortlog"
	// `git status -s` and `-sb`. The "## " header of -sb is unmistakable;
	// the bare XY codes are checked below, after every line has been seen.
	case strings.HasPrefix(first, "## "):
		return "status"
	case looksLikeStatusCodes(lines):
		return "status"
	// blame lines look like `git log --oneline` lines (hash, space, text), so
	// they have to be recognized before the log check below
	case reBlame.MatchString(first), reBlameSupp.MatchString(first):
		return "blame"
	// `git show` on a commit opens with the same "commit <sha>" header as
	// `git log`, so the log check below would claim it, but the patch that
	// follows is what the reader actually came for. A diff section anywhere in
	// the text is what tells the two apart.
	case reCommitHdr.MatchString(first) && strings.Contains(text, "diff --git "):
		return "show"
	case reCommitHdr.MatchString(first), oneline:
		return "log"
	case looksLikeBranchList(lines):
		return "branch"
	}
	return ""
}

// looksLikeStatusCodes: every line is a status code in front of a path, the
// shape `git status -s` and `git status --porcelain` print.
func looksLikeStatusCodes(lines []string) bool {
	codes := false
	for _, l := range lines {
		if strings.TrimSpace(l) == "" || strings.HasPrefix(l, "## ") {
			continue
		}
		// " ! [rejected] old -> old" is a push ref update, not a status: it
		// starts with two code characters too, but is padded into columns and
		// always names the ref it moved.
		if strings.Contains(l, " -> ") || !reStatusCode.MatchString(l) {
			return false
		}
		codes = true
	}
	return codes
}

// looksLikeBranchList: every line starts with "* " or two spaces, and one
// of them is the current branch.
func looksLikeBranchList(lines []string) bool {
	current := false
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if strings.HasPrefix(l, "* ") {
			current = true
		} else if !strings.HasPrefix(l, "  ") {
			return false
		}
	}
	return current
}

// ---------------------------------------------------------------------------
// Formatting
// ---------------------------------------------------------------------------

// formatGit formats the output of `git <sub>`. Specific formatters return
// false when the text isn't what they expect (an error message, say), and
// the generic colorizer takes over.
// formatGit routes text to the formatter for sub.
func Format(w io.Writer, sub, text string) {
	if isGitError(text) {
		formatText(w, text) // git refused: color the message instead of formatting it
		return
	}
	switch sub {
	case "status":
		// -s / -sb / --porcelain output has no prose to lay out, only
		// status codes, so it needs its own pass.
		if formatStatusShort(w, text) {
			return
		}
		if formatStatus(w, text) {
			return
		}
	case "log":
		if formatLog(w, text) {
			return
		}
	case "branch":
		if formatBranch(w, text) {
			return
		}
	case "tag":
		if formatTag(w, text) {
			return
		}
	case "diff":
		// The two-column view is a different rendering of the same text, so it
		// goes through the same path. It is asked for with parse's own -w, which
		// wantSideBySide has already removed from the argument list; git has no
		// such flag, so there is no conflict to preserve here.
		if sbsRequested || sbsAlways {
			if sideBySide(w, text, sbsWidth) {
				return
			}
		}
		if formatDiff(w, text) {
			return
		}
	case "show":
		// A commit header followed by a patch. The header is worth reading, so
		// it is kept and colored; the patch after it goes through the diff
		// formatter, which is the whole reason show needed its own case.
		//
		// -w has to be checked here as well as in the diff case, or
		// `git show | parse -w` would silently come out one-column while
		// `parse git show -w` came out two-column. Same input, same flags,
		// different output depending on which form was typed.
		if (sbsRequested || sbsAlways) && formatShowSideBySide(w, text, sbsWidth) {
			return
		}
		formatShow(w, text)
		return
	case "grep":
		formatGrep(w, text)
		return
	case "blame", "annotate":
		formatBlame(w, text)
		return
	case "shortlog":
		if formatShortlog(w, text) {
			return
		}
	case "reflog":
		formatReflog(w, text)
		return
	case "stash":
		// `stash list` is a list; `stash pop`/`apply` print a status block
		// that formatStatus already knows how to lay out.
		if formatStashList(w, text) {
			return
		}
		if formatStatus(w, text) {
			return
		}
	case "remote":
		if formatRemote(w, text) {
			return
		}
		if formatRemoteShow(w, text) {
			return
		}
	case "submodule":
		formatSubmodule(w, text)
		return
	}
	formatText(w, text)
}

// isGitError reports whether git refused to run, which it always says on the
// first line. A formatter that claimed such output would print the message
// with no color at all, because nothing in it looks like commits or paths.
func isGitError(text string) bool {
	first := tool.FirstNonEmpty(strings.Split(text, "\n"))
	return strings.HasPrefix(first, "fatal:") || strings.HasPrefix(first, "error:")
}

// --- git log ---------------------------------------------------------------

type commit struct {
	hash, author, date, message string
	// deco is the decoration git puts after the hash: "HEAD -> master",
	// "tag: v1.0", "side", or several separated by ", ". It is what tells you
	// where you are, so it is kept rather than dropped.
	deco string
	// merge is the "Merge: a b" parent line, kept as-is so a merge commit is
	// recognizable as one instead of looking like an ordinary commit.
	merge string
}

// formatLog prints commits as a table. It returns false when the log has
// anything it can't represent (-p, --stat, --graph, ...).
func formatLog(w io.Writer, text string) bool {
	commits, unknown := parseLog(text)
	if len(commits) == 0 || unknown > 0 {
		return false
	}

	oneline := true
	for _, c := range commits {
		if c.author != "" || c.date != "" {
			oneline = false
		}
	}

	// The decoration column only appears when git printed one, so a log
	// without --decorate keeps the same four columns it always had.
	decorated := false
	for _, c := range commits {
		if c.deco != "" {
			decorated = true
			break
		}
	}

	headers := []string{"COMMIT"}
	if oneline {
		if decorated {
			headers = append(headers, "REF")
		}
		headers = append(headers, "MESSAGE")
	} else {
		headers = append(headers, "AUTHOR", "DATE")
		if decorated {
			headers = append(headers, "REF")
		}
		headers = append(headers, "MESSAGE")
	}

	var rows [][]Cell
	for _, c := range commits {
		row := []Cell{{Text: c.hash, Color: yellow}}
		if !oneline {
			row = append(row, Cell{Text: c.author, Color: cyan}, Cell{Text: c.date})
		}
		if decorated {
			row = append(row, Cell{Text: c.deco, Color: decoColor(c.deco)})
		}
		row = append(row, Cell{Text: messageWithMerge(c)})
		rows = append(rows, row)
	}

	tool.PrintTable(w, headers, rows)
	return true
}

// decoColor picks a color for a decoration. HEAD is where you are, so it is
// the one that has to stand out; tags are dimmer than branches because they are
// annotations rather than positions.
func decoColor(deco string) string {
	switch {
	case strings.HasPrefix(deco, "HEAD"):
		return bold + " " + green
	case strings.HasPrefix(deco, "tag:"):
		return dim
	default:
		return cyan
	}
}

// messageWithMerge marks a merge commit and shows its parents. Without this a
// merge looks exactly like a normal commit, which is misleading: it suggests
// the branch was developed linearly when it was not.
func messageWithMerge(c commit) string {
	if c.merge == "" {
		return c.message
	}
	parents := strings.Fields(c.merge)
	short := make([]string, 0, len(parents))
	for _, p := range parents {
		short = append(short, shortHash(p))
	}
	return fmt.Sprintf("%s (merge of %s)", c.message, strings.Join(short, ", "))
}

// parseLog reads default and --oneline log output. unknown counts lines it
// did not understand.
func parseLog(text string) (commits []commit, unknown int) {
	for _, line := range strings.Split(text, "\n") {
		n := len(commits)
		switch {
		case strings.TrimSpace(line) == "":
			// blank separator
		case strings.HasPrefix(line, "commit "):
			h, deco := commitHash(line)
			if h != "" {
				commits = append(commits, commit{hash: h, deco: deco})
			} else {
				unknown++
			}
		case strings.HasPrefix(line, "Author:") && n > 0:
			commits[n-1].author = authorName(line)
		case strings.HasPrefix(line, "Date:") && n > 0:
			commits[n-1].date = shortDate(strings.TrimSpace(line[len("Date:"):]))
		case strings.HasPrefix(line, "Merge:") && n > 0:
			commits[n-1].merge = strings.TrimSpace(line[len("Merge:"):])
		case strings.HasPrefix(line, "    ") && n > 0:
			if msg := strings.TrimSpace(line); msg != "" && commits[n-1].message == "" {
				commits[n-1].message = msg // first line of the message only
			}
		default:
			if h, deco, msg, ok := onelineCommit(line); ok {
				commits = append(commits, commit{hash: h, deco: deco, message: msg})
			} else {
				unknown++
			}
		}
	}
	return commits, unknown
}

// commitHash handles "commit abc123... (HEAD -> main)". It returns the short
// hash and the decoration, if any. The decoration is what says where HEAD is,
// which branch a commit is on, and which tags point at it, so dropping it
// loses the part of a log you actually read it for.
func commitHash(line string) (hash, deco string) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", ""
	}
	// A decoration can hold spaces of its own, as in
	// "(HEAD -> master, tag: v2)", so the parentheses are matched as a pair
	// rather than assumed to sit inside one whitespace-separated field.
	if i := strings.Index(line, "("); i > 0 {
		if j := strings.LastIndex(line, ")"); j > i {
			deco = strings.TrimSpace(line[i+1 : j])
		}
	}
	return shortHash(fields[1]), deco
}

// onelineCommit parses "<hash> <message>" from git log --oneline, plus the
// optional decoration git puts in between: "<hash> (HEAD -> main) <message>".
func onelineCommit(line string) (hash, deco, msg string, ok bool) {
	if line == "" || line[0] == ' ' {
		return "", "", "", false
	}
	i := strings.IndexByte(line, ' ')
	if i < 7 || i > 40 || !isHex(line[:i]) {
		return "", "", "", false
	}
	rest := strings.TrimSpace(line[i+1:])
	if strings.HasPrefix(rest, "(") {
		// The decoration may be the whole line, as in "2ac556b (tag: v1.0)",
		// so the closing parenthesis is what ends it rather than a following
		// space. Inside it a ")" can only belong to something like
		// "(HEAD -> master)", which git never nests further.
		if j := strings.Index(rest, ")"); j > 0 {
			return shortHash(line[:i]), rest[1:j], strings.TrimSpace(rest[j+1:]), true
		}
	}
	return shortHash(line[:i]), "", rest, true
}

func authorName(line string) string {
	s := strings.TrimSpace(line[len("Author:"):])
	if i := strings.Index(s, " <"); i >= 0 {
		s = s[:i]
	}
	return s
}

// shortDate turns git's date formats into YYYY-MM-DD, or returns s as-is.
func shortDate(s string) string {
	layouts := []string{
		"Mon Jan 2 15:04:05 2006 -0700", // default
		"2006-01-02 15:04:05 -0700",     // --date=iso
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return s
}

func shortHash(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

func isHex(s string) bool {
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return s != ""
}

// graphPrefix returns the graph drawing at the start of a `git log --graph`
// line, such as "|  * " in "|  * 02f4c2f Fix bug". It returns "" when the line
// has no graph, which is how a plain diff line or an indented commit subject
// is told apart from one.
func graphPrefix(line string) string {
	const strokes = "*|\\/"
	i, spaces := 0, 0
	for i < len(line) {
		switch {
		case strings.IndexByte(strokes, line[i]) >= 0:
			spaces = 0
			i++
			if i < len(line) && line[i] == ' ' {
				i++
			}
		case line[i] == ' ':
			// spaces only belong to the graph if a stroke follows them
			spaces++
			i++
		default:
			i -= spaces
			return line[:i]
		}
	}
	return line
}

// --- git status ------------------------------------------------------------

type statusItem struct{ kind, path string }

type statusSection struct {
	title, color string
	kinds        bool // entries look like "modified:   path"
	entries      []statusItem
}

var statusKinds = map[string]bool{
	"modified": true, "new file": true, "deleted": true, "renamed": true,
	"copied": true, "typechange": true, "both modified": true, "both added": true,
	"both deleted": true, "added by us": true, "added by them": true,
	"deleted by us": true, "deleted by them": true,
}

var (
	reDiverged = regexp.MustCompile(`^Your branch and '(.+)' have diverged,`)
	reCounts   = regexp.MustCompile(`^and have (\d+) and (\d+) different commits? each`)
)

// syncOf turns git's "Your branch ..." line into a short phrase such as
// "up to date with origin/main". It returns "" for anything else. git status
// and git checkout both print these lines, so both go through here.
func syncOf(line string) string {
	switch {
	case strings.HasPrefix(line, "Your branch and "):
		if m := reDiverged.FindStringSubmatch(line); m != nil {
			return "diverged from " + m[1]
		}
	case strings.HasPrefix(line, "Your branch "):
		s := strings.TrimPrefix(line, "Your branch is ")
		return strings.TrimSuffix(strings.ReplaceAll(s, "'", ""), ".")
	}
	return ""
}

// syncAheadBehind is the second half of git's diverged message, which git
// prints on a line of its own.
func syncAheadBehind(line string) string {
	if m := reCounts.FindStringSubmatch(line); m != nil {
		return fmt.Sprintf(" (%s ahead, %s behind)", m[1], m[2])
	}
	return ""
}

// syncColor is green when local matches the remote, yellow when it does not,
// and red when the upstream branch is gone.
func syncColor(phrase string) string {
	switch {
	case strings.HasPrefix(phrase, "up to date"):
		return green
	case strings.HasPrefix(phrase, "gone"):
		return red
	}
	return yellow
}

// reStatusCode matches the status code git puts in front of a path. Two
// shapes exist: `git status -s` prints "XY path", while `git reset` prints
// "M\tpath" with a single code character and a tab.
var reStatusCode = regexp.MustCompile(`^([ MARCUDT?!])([ MARCUDT?!]?)([ \t])(.+)$`)

// statusCodeColor picks the color that best explains a status code. A code
// with two letters means "both sides changed", so the more alarming of the
// two wins. Not every conflict spells a U: "AA" is both added and "DD" is
// both deleted, and both still need resolving.
func statusCodeColor(xy string) string {
	switch {
	case strings.ContainsAny(xy, "U"), xy == "AA", xy == "DD":
		return "bold red" // unmerged
	case xy == "??":
		return red // untracked
	case xy == "!!":
		return dim // ignored
	case strings.ContainsAny(xy, "D"):
		return red
	case strings.ContainsAny(xy, "ARC"):
		return green
	case strings.ContainsAny(xy, "MT"):
		return yellow
	}
	return ""
}

// colorStatusCode colors just the code in a status line and leaves the path
// alone, because the path is the part you read twice. git pads the first
// column with a space for changes that are not staged, and that padding stays
// uncolored so the letters line up under each other.
func colorStatusCode(line string) string {
	m := reStatusCode.FindStringSubmatch(line)
	if m == nil {
		return line
	}
	code := m[1] + m[2]
	letters := strings.TrimLeft(code, " ")
	pad := strings.Repeat(" ", len(code)-len(letters))
	return pad + tool.PaintColor(statusCodeColor(code), letters) + m[3] + m[4]
}

// reStatusBranchLine matches the "## main...origin/main [ahead 1]" header that
// `git status -sb` prints instead of "On branch main".
var reStatusBranchLine = regexp.MustCompile(`^## (\S+?)(?:\.\.\.(\S+))?(?: \[(.+)\])?$`)

// formatStatusShort colors `git status -s`, `-sb` and `--porcelain` output.
// These lines carry no prose, so the readable part is the code and the branch
// header; it returns false for anything else.
func formatStatusShort(w io.Writer, text string) bool {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) == 0 {
		return false
	}

	// decide before printing anything: a false return must not have written a
	// half colored block for the next formatter to follow
	codes, header := false, false
	for _, l := range lines {
		switch {
		case l == "":
		case strings.HasPrefix(l, "## "):
			header = true
		case reStatusCode.MatchString(l):
			codes = true
		default:
			return false
		}
	}
	// a clean `git status -sb` prints the branch header and nothing else
	if !codes && !header {
		return false
	}

	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "## "):
			fmt.Fprintln(w, colorStatusBranch(l))
		case reStatusCode.MatchString(l):
			fmt.Fprintln(w, colorStatusCode(l))
		}
	}
	return true
}

// colorStatusBranch colors the "## branch...upstream [ahead 1]" header line.
func colorStatusBranch(line string) string {
	m := reStatusBranchLine.FindStringSubmatch(line)
	if m == nil {
		return line
	}
	branch, upstream, state := m[1], m[2], m[3]
	if upstream == "" && state == "" {
		return tool.PaintColor(bold, line)
	}
	return tool.PaintColor(cyan, branch) + "..." + tool.PaintColor(dim, upstream) + " [" + tool.PaintColor(syncColor(state), state) + "]"
}

// formatStatus reformats the long `git status` output (English). It returns
// false if the text doesn't look like a status.
func formatStatus(w io.Writer, text string) bool {
	var (
		branch, sync string
		notes        []string
		tail         []string
		sections     []*statusSection
		cur          *statusSection
		clean        bool
	)
	add := func(title, color string, kinds bool) *statusSection {
		s := &statusSection{title: title, color: color, kinds: kinds}
		sections = append(sections, s)
		return s
	}

	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, "\r")
		trim := strings.TrimSpace(line)
		switch {
		case trim == "":
		case strings.HasPrefix(line, "On branch "):
			branch = strings.TrimPrefix(line, "On branch ")
		case strings.HasPrefix(line, "HEAD detached "), strings.HasPrefix(line, "Not currently on any branch"):
			branch = strings.TrimSuffix(trim, ".")
		case strings.HasPrefix(line, "Your branch and "):
			if s := syncOf(line); s != "" {
				sync = s
			}
		case strings.HasPrefix(line, "Your branch "):
			if s := syncOf(line); s != "" {
				sync = s
			}
		case strings.HasPrefix(line, "and have "):
			sync += syncAheadBehind(line)
		case trim == "Changes to be committed:":
			cur = add("STAGED", green, true)
		case trim == "Changes not staged for commit:":
			cur = add("UNSTAGED", red, true)
		case trim == "Unmerged paths:":
			cur = add("CONFLICTS", red, true)
		case trim == "Untracked files:":
			cur = add("UNTRACKED", red, false)
		case trim == "Ignored files:":
			cur = add("IGNORED", dim, false)
		case strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "        "):
			if cur != nil {
				kind, path := statusEntry(trim, cur.kinds)
				cur.entries = append(cur.entries, statusItem{kind, path})
			}
		case strings.HasPrefix(trim, "(") && strings.HasSuffix(trim, ")"):
			// hint such as (use "git add <file>..." to update what will be committed)
		case strings.HasPrefix(trim, "nothing to commit"):
			clean = true
		case strings.HasPrefix(trim, "Dropped refs/stash"):
			tail = append(tail, trim) // git stash pop: belongs after the lists
		case hasAnyPrefix(trim, "no changes added to commit", "nothing added to commit"):
		default:
			notes = append(notes, trim)
		}
	}

	if branch == "" && len(sections) == 0 && !clean {
		return false
	}

	if branch != "" {
		head := tool.PaintColor(bold, "BRANCH") + "  " + tool.PaintColor(cyan, branch)
		if sync != "" {
			head += "  " + tool.PaintColor(syncColor(sync), sync)
		}
		fmt.Fprintln(w, head)
	}
	for _, n := range notes {
		fmt.Fprintln(w, tool.PaintColor(yellow, n))
	}
	if clean {
		fmt.Fprintln(w, tool.PaintColor(green, "clean - nothing to commit"))
	}

	kindWidth := 0
	for _, s := range sections {
		for _, e := range s.entries {
			if len(e.kind) > kindWidth {
				kindWidth = len(e.kind)
			}
		}
	}
	for _, s := range sections {
		if len(s.entries) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n%s\n", tool.PaintColor(bold+" "+s.color, fmt.Sprintf("%s (%d)", s.title, len(s.entries))))
		for _, e := range s.entries {
			if e.kind != "" {
				pad := strings.Repeat(" ", kindWidth-len(e.kind))
				fmt.Fprintf(w, "  %s%s  %s\n", tool.PaintColor(s.color, e.kind), pad, e.path)
			} else {
				fmt.Fprintf(w, "  %s\n", tool.PaintColor(s.color, e.path))
			}
		}
	}
	for _, t := range tail {
		fmt.Fprintln(w, tool.PaintColor(green, t))
	}
	return true
}

// statusEntry splits "modified:   path" into kind and path.
func statusEntry(s string, withKind bool) (kind, path string) {
	if withKind {
		if i := strings.Index(s, ":"); i > 0 && statusKinds[s[:i]] {
			return s[:i], strings.TrimSpace(s[i+1:])
		}
	}
	return "", s
}

// --- git branch / tag / grep ----------------------------------------------

// reBranchLine matches "  main  ed7c3cc subject", where the first two
// characters are git's marker: "* " for the checked out branch, "+ " for one
// checked out in another worktree, "  " for the rest.
var reBranchLine = regexp.MustCompile(`^([* +] )(\S+|\(.*?\))(\s+)([0-9a-f]{7,40})(\s.*)?$`)

// formatBranch colors `git branch [-v|-vv|-a]` output.
func formatBranch(w io.Writer, text string) bool {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if !looksLikeBranchList(lines) {
		return false
	}
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			fmt.Fprintln(w)
			continue
		}
		// `git branch` output already reads clearly: only the markers that
		// carry meaning get a color.
		nameColor := ""
		switch {
		case strings.HasPrefix(l, "* "):
			nameColor = green // the branch you are on
		case strings.HasPrefix(l, "+ "):
			nameColor = yellow // checked out in another worktree
		case strings.HasPrefix(l[2:], "remotes/"):
			nameColor = red
		}
		if m := reBranchLine.FindStringSubmatch(l); m != nil {
			fmt.Fprintln(w, m[1]+tool.PaintColor(nameColor, m[2])+m[3]+tool.PaintColor(yellow, m[4])+m[5])
		} else {
			fmt.Fprintln(w, l[:2]+tool.PaintColor(nameColor, l[2:]))
		}
	}
	return true
}

// formatTag colors the tag names from `git tag [-l] [-n]`.
func formatTag(w io.Writer, text string) bool {
	if hasAnyPrefix(text, "fatal:", "error:") || strings.Contains(text, "Deleted tag") {
		return false
	}
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if l == "" {
			continue
		}
		if l[0] == ' ' || l[0] == '\t' { // continuation of a -n annotation
			fmt.Fprintln(w, l)
			continue
		}
		name, rest := l, ""
		if i := strings.IndexAny(l, " \t"); i >= 0 {
			name, rest = l[:i], l[i:]
		}
		fmt.Fprintln(w, tool.PaintColor(yellow, name)+rest)
	}
	return true
}

var (
	reGrepNum  = regexp.MustCompile(`^([^:]+):(\d+):(.*)$`)
	reGrepFile = regexp.MustCompile(`^([^:]+):(.*)$`)
)

// formatGrep colors `git grep` output: file, line number, matching text.
func formatGrep(w io.Writer, text string) {
	if text == "" {
		return
	}
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if hasAnyPrefix(l, "fatal:", "error:", "warning:") {
			fmt.Fprintln(w, colorLine(l))
		} else if l == "--" {
			fmt.Fprintln(w, tool.PaintColor(dim, l))
		} else if m := reGrepNum.FindStringSubmatch(l); m != nil {
			fmt.Fprintln(w, tool.PaintColor(cyan, m[1])+":"+tool.PaintColor(green, m[2])+":"+m[3])
		} else if m := reGrepFile.FindStringSubmatch(l); m != nil {
			fmt.Fprintln(w, tool.PaintColor(cyan, m[1])+":"+m[2])
		} else {
			fmt.Fprintln(w, l)
		}
	}
}

// --- git stash / remote / blame --------------------------------------------

var reStashLine = regexp.MustCompile(`^(stash@\{(\d+)\}): (.+)$`)

// formatStashList prints `git stash list` as a table. It returns false when the
// text is not a stash list, so the other stash output (push, pop, show) keeps
// its own formatting.
func formatStashList(w io.Writer, text string) bool {
	var (
		rows   [][]Cell
		hashes = true
	)
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		m := reStashLine.FindStringSubmatch(l)
		if m == nil {
			return false
		}
		branch, msg := splitStashMsg(m[3])
		hash := ""
		if h, _, rest, ok := onelineCommit(msg); ok {
			hash, msg = h, rest
		} else {
			hashes = false // `git stash push -m "..."` has no commit hash
		}
		rows = append(rows, []Cell{{Text: m[1], Color: yellow}, {Text: hash, Color: yellow}, {Text: branch, Color: cyan}, {Text: msg, Color: ""}})
	}
	if len(rows) == 0 {
		return false
	}
	if hashes {
		tool.PrintTable(w, []string{"STASH", "COMMIT", "BRANCH", "MESSAGE"}, rows)
		return true
	}
	plain := make([][]Cell, 0, len(rows))
	for _, r := range rows {
		plain = append(plain, []Cell{r[0], r[2], r[3]})
	}
	tool.PrintTable(w, []string{"STASH", "BRANCH", "MESSAGE"}, plain)
	return true
}

// splitStashMsg splits the tail of a stash entry into branch and message:
// "WIP on main: abc1234 Fix bug" gives "main" and "abc1234 Fix bug".
func splitStashMsg(s string) (branch, msg string) {
	for _, p := range []string{"WIP on ", "On "} {
		if strings.HasPrefix(s, p) {
			s = strings.TrimPrefix(s, p)
			if i := strings.Index(s, ": "); i >= 0 {
				return s[:i], strings.TrimSpace(s[i+2:])
			}
			break
		}
	}
	return "", s
}

var (
	reRemoteV = regexp.MustCompile(`^(\S+)\t+(\S+) \((fetch|push)\)$`)
	// A bare `git remote` prints one remote name per line. Names are short and
	// never contain spaces, which is what keeps error messages and `remote show`
	// blocks from being mistaken for a list of names.
	reRemoteName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
)

// formatRemote prints `git remote` as a list of names and `git remote -v` as a
// table. `remote add`, `remove` and `rename` print nothing when they succeed, so
// unrecognized output falls back to the generic colorizer.
func formatRemote(w io.Writer, text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")

	verbose := false
	for _, l := range lines {
		if reRemoteV.MatchString(l) {
			verbose = true
			break
		}
	}

	if !verbose {
		for _, l := range lines {
			if strings.TrimSpace(l) == "" {
				continue
			}
			if !reRemoteName.MatchString(l) {
				return false // not a list of remote names
			}
			fmt.Fprintln(w, tool.PaintColor(yellow, l))
		}
		return true
	}

	// One row per line: git prints a fetch and a push line per remote, and the
	// two URLs can differ.
	var rows [][]Cell
	for _, l := range lines {
		m := reRemoteV.FindStringSubmatch(l)
		if m == nil {
			return false
		}
		dir := "fetch"
		if m[3] == "push" {
			dir = "push"
		}
		rows = append(rows, []Cell{{Text: m[1], Color: cyan}, {Text: m[2], Color: ""}, {Text: dir, Color: dim}})
	}
	tool.PrintTable(w, []string{"REMOTE", "URL", "DIR"}, rows)
	return true
}

var (
	reBlame     = regexp.MustCompile(`^(\^?[0-9a-f]{7,40}) \((.+?)(\s+)(\d+)\) (.*)$`)
	reBlameSupp = regexp.MustCompile(`^(\^?[0-9a-f]{7,40}) (\d+)\) (.*)$`)
)

// formatBlame colors the metadata git blame puts in front of each code line and
// leaves the code itself alone, so the code stays exactly as git printed it.
func formatBlame(w io.Writer, text string) {
	if text == "" {
		return
	}
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if m := reBlameSupp.FindStringSubmatch(l); m != nil {
			// `git blame -s` drops the author and date, leaving "<hash> <n>)"
			fmt.Fprintln(w, tool.PaintColor(blameHashColor(m[1]), m[1])+" "+
				tool.PaintColor(green, m[2])+") "+m[3])
			continue
		}
		m := reBlame.FindStringSubmatch(l)
		if m == nil {
			fmt.Fprintln(w, l)
			continue
		}
		author, date := splitBlameWho(m[2])
		// date keeps git's padding, so the dates of every line stay aligned
		fmt.Fprintln(w, tool.PaintColor(blameHashColor(m[1]), m[1])+" ("+tool.PaintColor(cyan, author)+
			tool.PaintColor(dim, date+m[3])+tool.PaintColor(green, m[4])+") "+m[5])
	}
}

// blameHashColor dims the all zero hash git blame uses for a line that is not
// committed yet, and marks the caret that flags a boundary commit.
func blameHashColor(hash string) string {
	if strings.Trim(hash, "^0") == "" {
		return dim
	}
	return yellow
}

// splitBlameWho splits "Alice             2026-10-03 17:15:21 +0530" into author
// and date. git pads the author to a fixed width, so only the author's own
// length is meaningful; the padding is kept by the caller.
func splitBlameWho(inner string) (author, date string) {
	const uncommitted = "Not Committed Yet"
	if rest, ok := strings.CutPrefix(inner, uncommitted); ok {
		return uncommitted, rest
	}
	if i := strings.Index(inner, " "); i > 0 {
		return inner[:i], inner[i:]
	}
	return inner, ""
}

// --- git shortlog / reflog / worktree / submodule / config / describe -------

var (
	// reShortlogSum is "12\tAuthor Name" from `git shortlog -s`. The author may
	// contain spaces but not a tab, and that restriction is the point: git's
	// --numstat rows are "3\t2\tcmd/main.go", which is three tab-separated
	// fields, and an earlier version of this pattern accepted them. Every
	// --numstat row was then reported as a commit count and the real file name
	// was printed as the author's name.
	reShortlogSum  = regexp.MustCompile(`^\s*(\d+)\t([^\t]+)$`)
	reShortlogHead = regexp.MustCompile(`^(\S.*?) \((\d+)\):$`)
)

// formatShortlog turns `git shortlog` output into a per author summary. With
// -s or -n git prints only the counts, which make a clean table; without them
// it prints each author's subjects, which are kept under a colored heading.
func formatShortlog(w io.Writer, text string) bool {
	var (
		rows     [][]Cell
		authors  []string
		subjects = map[string][]string{}
	)
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if m := reShortlogSum.FindStringSubmatch(l); m != nil {
			rows = append(rows, []Cell{{Text: m[1], Color: yellow}, {Text: m[2], Color: cyan}})
			continue
		}
		if m := reShortlogHead.FindStringSubmatch(l); m != nil {
			authors = append(authors, m[1])
			subjects[m[1]] = nil
			continue
		}
		if len(authors) == 0 {
			return false // not a shortlog
		}
		last := authors[len(authors)-1]
		subjects[last] = append(subjects[last], strings.TrimSpace(l))
	}

	switch {
	case len(rows) > 0:
		tool.PrintTable(w, []string{"COMMITS", "AUTHOR"}, rows)
	case len(authors) > 0:
		for _, a := range authors {
			fmt.Fprintln(w, tool.PaintColor(cyan, a)+" "+tool.PaintColor(dim, fmt.Sprintf("(%d)", len(subjects[a]))))
			for _, s := range subjects[a] {
				fmt.Fprintln(w, "  "+tool.PaintColor(dim, s))
			}
		}
	default:
		return false
	}
	return true
}

// reReflog matches a reflog line: "<hash> (<decoration>) <ref>@{1>: <action>".
var reReflog = regexp.MustCompile(`^(\^?[0-9a-f]{7,40})(?: \((.*)\))? (\S+@\{\d+\}): (.*)$`)

// formatReflog colors `git reflog` output: the hash of the commit, the ref it
// moved, and then git's own description of what happened, which reads as is.
func formatReflog(w io.Writer, text string) {
	if text == "" {
		return
	}
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		m := reReflog.FindStringSubmatch(l)
		if m == nil {
			fmt.Fprintln(w, colorLine(l))
			continue
		}
		line := tool.PaintColor(yellow, m[1])
		if m[2] != "" {
			line += " " + tool.PaintColor(dim, "("+m[2]+")")
		}
		fmt.Fprintln(w, line+" "+tool.PaintColor(cyan, m[3])+": "+m[4])
	}
}

// reSubmoduleStatus matches "<flag> <hash> <path> (<describe>)", the line
// `git submodule status` prints. The flag is a space when the recorded commit
// is checked out, "+" when it is not, "-" when the submodule is missing and
// "U" when it has a conflict.
var reSubmoduleStatus = regexp.MustCompile(`^([ +\-U])([0-9a-f]{7,40}) (\S+)(?: \((.*)\))?$`)

// formatSubmodule colors `git submodule status` and the plain path list that
// `git submodule` prints with no arguments.
func formatSubmodule(w io.Writer, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		m := reSubmoduleStatus.FindStringSubmatch(l)
		if m == nil {
			fmt.Fprintln(w, tool.PaintColor(cyan, l))
			continue
		}
		flag := ""
		if m[1] != " " {
			flag = green // the submodule is ahead of what the parent recorded
		}
		line := tool.PaintColor(flag, m[1]) + tool.PaintColor(yellow, m[2]) + " " + tool.PaintColor(cyan, m[3])
		if m[4] != "" {
			line += " " + tool.PaintColor(dim, "("+m[4]+")")
		}
		fmt.Fprintln(w, line)
	}
}

// reRemoteShowLabel matches an indented "  Fetch URL: /path" pair, keeping
// the indent out of the label so only the words get colored.
var reRemoteShowLabel = regexp.MustCompile(`^(\s+)(\S.*?:)(.*)$`)

// formatRemoteShow lays out the block `git remote show <name>` prints: the
// remote name, bold section headings, cyan "label:" pairs and dim detail lines.
func formatRemoteShow(w io.Writer, text string) bool {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if !strings.HasPrefix(tool.FirstNonEmpty(lines), "* remote ") {
		return false
	}
	for _, l := range lines {
		switch {
		case l == "":
			fmt.Fprintln(w)
		case strings.HasPrefix(l, "* remote "):
			fmt.Fprintln(w, tool.PaintColor(bold, "REMOTE")+"  "+tool.PaintColor(cyan, strings.TrimSpace(l[9:])))
		case strings.HasSuffix(strings.TrimSpace(l), ":"):
			fmt.Fprintln(w, tool.PaintColor(bold, strings.TrimSpace(l)))
		case reRemoteShowLabel.MatchString(l):
			m := reRemoteShowLabel.FindStringSubmatch(l)
			fmt.Fprintln(w, m[1]+tool.PaintColor(cyan, m[2])+m[3])
		default:
			fmt.Fprintln(w, tool.PaintColor(dim, l))
		}
	}
	return true
}

var (
	reIndex    = regexp.MustCompile(`^index [0-9a-f]+\.\.[0-9a-f]+`)
	reDiffstat = regexp.MustCompile(`^( \S.*\|\s+\d+)(\s*)(\+*)(-*)$`)
	reSummary  = regexp.MustCompile(`^( \d+ files? changed)(.*)$`)
	reInsert   = regexp.MustCompile(`\d+ insertions?\(\+\)`)
	reDelete   = regexp.MustCompile(`\d+ deletions?\(-\)`)
	reBracket  = regexp.MustCompile(`^(\[[^\]]* [0-9a-f]{7,40}\])(.*)$`)
	reTrack    = regexp.MustCompile(`^branch '.*' set up to track`)
)

const detachedAdvice = "You are in 'detached HEAD' state"

// formatText colors diff, show, commit, push, pull, checkout, stash, ... output.
func formatText(w io.Writer, text string) {
	if text == "" {
		return
	}
	advice := false // inside git's detached HEAD explanation
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		painted := colorLine(line)
		if advice && painted != line {
			advice = false // a line git wanted colored: the block is over
		}
		if advice || strings.HasPrefix(line, detachedAdvice) {
			advice = true
			fmt.Fprintln(w, tool.PaintColor(dim, line)) // dim the prose, keep the one line that matters readable
			continue
		}
		fmt.Fprintln(w, painted)
	}
}

func colorLine(line string) string {
	// `git log --graph` draws the branch history on the left. Dim it so the
	// commits read clearly, and color what is left as any other line. A push
	// or fetch ref update also starts with " * ", so it is told apart by the
	// "[new branch]" marker it carries and the "old -> new" it ends with.
	if g := graphPrefix(line); g != "" {
		rest := strings.TrimPrefix(line, g)
		if !strings.Contains(rest, " -> ") && !strings.HasPrefix(rest, "[") {
			if h, deco, msg, ok := onelineCommit(rest); ok {
				out := tool.PaintColor(dim, g) + tool.PaintColor(yellow, h)
				if deco != "" {
					out += " " + tool.PaintColor(cyan, "("+deco+")")
				}
				return out + " " + msg
			}
			return tool.PaintColor(dim, g) + colorLine(rest)
		}
	}

	switch {
	case line == "":
		return line
	// diffs
	case hasAnyPrefix(line, "diff --git", "+++ ", "--- "):
		return tool.PaintColor(bold, line)
	case strings.HasPrefix(line, "@@"):
		return tool.PaintColor(cyan, line)
	case line[0] == '+':
		return tool.PaintColor(green, line)
	case line[0] == '-':
		return tool.PaintColor(red, line)
	case reIndex.MatchString(line),
		hasAnyPrefix(line, "new file mode", "deleted file mode", "old mode", "new mode",
			"similarity index", "rename from", "rename to", "copy from", "copy to", "Binary files"):
		return tool.PaintColor(dim, line)
	// commit headers (git show, git log -p, git revert)
	case reCommitHdr.MatchString(line):
		return tool.PaintColor(yellow, line)
	case strings.HasPrefix(line, "Author:"):
		return tool.PaintColor(cyan, line)
	case hasAnyPrefix(line, "Date:", "Merge:", " Date:"):
		return tool.PaintColor(dim, line)
	// problems and notes
	case hasAnyPrefix(line, "error:", "fatal:", "CONFLICT", "Automatic merge failed", "Could not apply"):
		return tool.PaintColor(red, line)
	case strings.HasPrefix(line, "warning:"):
		return tool.PaintColor(yellow, line)
	case hasAnyPrefix(line, "hint:", "remote:", "From ", "To ", "Cloning into", "Auto-merging",
		"Updating ", "Receiving objects", "Resolving deltas", "Previous HEAD position was"):
		return tool.PaintColor(dim, line)
	case reTrack.MatchString(line):
		return tool.PaintColor(dim, line)
	// git checkout and git clean
	case strings.HasPrefix(line, "Your branch "):
		if s := syncOf(line); s != "" {
			return tool.PaintColor(syncColor(s), s)
		}
	case strings.HasPrefix(line, "Removing "): // git clean deleted something
		return tool.PaintColor(red, line)
	// section headings git reset prints above the codes
	case hasAnyPrefix(line, "Unstaged changes after", "Changes to be committed:"):
		return tool.PaintColor(bold, line)
	// notes worth noticing
	case hasAnyPrefix(line, "Note: switching to", "No stash entries found.", "Would remove"):
		return tool.PaintColor(yellow, line)
	// success messages
	case hasAnyPrefix(line, "Already up to date", "Everything up-to-date", "Fast-forward", "Switched to",
		"Already on", "Initialized empty", "Reinitialized existing", "Successfully rebased",
		"Merge made by", "Deleted branch", "Deleted tag", "Updated ",
		"Saved working directory and index state", "Dropped refs/stash", "HEAD is now at"):
		return tool.PaintColor(green, line)
	}

	if m := reDiffstat.FindStringSubmatch(line); m != nil {
		return m[1] + m[2] + tool.PaintColor(green, m[3]) + tool.PaintColor(red, m[4])
	}
	if m := reSummary.FindStringSubmatch(line); m != nil {
		rest := reInsert.ReplaceAllStringFunc(m[2], func(s string) string { return tool.PaintColor(green, s) })
		rest = reDelete.ReplaceAllStringFunc(rest, func(s string) string { return tool.PaintColor(red, s) })
		return tool.PaintColor(bold, m[1]) + rest
	}
	if m := reBracket.FindStringSubmatch(line); m != nil { // [main abc1234] message
		return tool.PaintColor(yellow, m[1]) + m[2]
	}

	switch {
	case strings.HasPrefix(line, " create mode"):
		return tool.PaintColor(green, line)
	case strings.HasPrefix(line, " delete mode"):
		return tool.PaintColor(red, line)
	case hasAnyPrefix(line, " rename ", " copy ", " mode change"):
		return tool.PaintColor(yellow, line)
	case !strings.HasPrefix(line, "    ") && strings.Contains(line, " -> "): // push/fetch ref updates
		switch {
		case strings.Contains(line, "[new branch]") || strings.Contains(line, "[new tag]"):
			return tool.PaintColor(green, line)
		case strings.Contains(line, "[deleted]") || strings.Contains(line, "[rejected]"):
			return tool.PaintColor(red, line)
		default:
			return tool.PaintColor(yellow, line)
		}
	// a status code in front of a path, the way `git reset` reports what is
	// still unstaged. This goes last because a push line such as
	// " ! [rejected] old -> old" also starts with two code characters.
	case reStatusCode.MatchString(line):
		return colorStatusCode(line)
	}
	return line
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// --- git diff ---------------------------------------------------------------
//
// A raw diff is a wall of context: four boilerplate lines per file, then the
// hunk, and the file name is easy to lose track of once the hunks run long.
// formatDiff replaces the boilerplate with one line per file carrying the path,
// what happened to it, and how many lines moved, and it keeps the hunks below
// that line so the code still reads normally.

var (
	// reDiffHeader is the "diff --git a/x b/x" line, which only exists in the
	// default and --no-prefix-off forms.
	reDiffHeader = regexp.MustCompile(`^diff --git (.+)$`)
	// reHunk is "@@ -1,4 +1,6 @@ optional section heading".
	reHunk = regexp.MustCompile(`^@@ [^@]*@@(.*)$`)
	// reDiffStat is git's own summary, as printed by --stat and by show.
	reDiffStat = regexp.MustCompile(`^ (\S.*\s\|\s+[\d+-]+)$|^( \d+ files? changed.*)$`)
)

// diffFile is one file's block of a diff.
type diffFile struct {
	// path is the file the change applies to.
	path string
	// from is the old path when git reported a rename.
	from string
	// kind is "added", "deleted", "renamed", "mode" or "" for an edit.
	kind string
	// mode is set for a pure mode change, which has no hunks at all.
	mode string
	// binary is set when git said the contents are binary.
	binary bool
	// trailingStat is set when git's own diffstat summary followed this file's
	// last hunk, which is what `show --patch --stat` prints. It marks the point
	// where the patch ends, so the summary is not swallowed as hunk content.
	trailingStat bool
	// lines are the hunks, kept verbatim.
	lines   []string
	added   int
	removed int
	// index is the file's position in the parseDiff result, used to key its
	// two-column rows.
	index int
}

// parseDiff splits diff text into one entry per file. It returns ok=false for
// anything that is not a diff, so a caller can fall back to generic coloring
// rather than print a table for something else.
func parseDiff(text string) (files []diffFile, ok bool) {
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if m := reDiffHeader.FindStringSubmatch(l); m != nil {
			files = append(files, diffFile{})
			files[len(files)-1].path = diffPath(m[1])
			continue
		}
		if len(files) == 0 {
			// Text before the first header means this is not a diff.
			if strings.TrimSpace(l) == "" {
				continue
			}
			return nil, false
		}
		f := &files[len(files)-1]

		switch {
		case strings.HasPrefix(l, "new file mode"):
			f.kind = "added"
			continue
		case strings.HasPrefix(l, "deleted file mode"):
			f.kind = "deleted"
			continue
		case strings.HasPrefix(l, "similarity index "):
			continue // already implied by the "renamed" kind
		case strings.HasPrefix(l, "rename from "):
			f.kind = "renamed"
			f.from = strings.TrimPrefix(l, "rename from ")
			f.path = ""
			continue
		case strings.HasPrefix(l, "rename to "):
			f.kind = "renamed"
			f.path = strings.TrimPrefix(l, "rename to ")
			continue
		case strings.HasPrefix(l, "old mode "):
			f.mode = strings.TrimPrefix(l, "old mode ")
			continue
		case strings.HasPrefix(l, "new mode "):
			f.mode = strings.TrimPrefix(l, "new mode ")
			continue
		case strings.HasPrefix(l, "index "), strings.HasPrefix(l, "--- "),
			strings.HasPrefix(l, "+++ "):
			// Blob hashes and the /dev/null pair. The path is already known
			// from the header, and the hashes mean nothing to a reader.
			continue
		case strings.HasPrefix(l, "Binary files ") || strings.HasPrefix(l, "GIT binary patch"):
			f.binary = true
			continue
		}

		// git's own summary, which --stat and `show --patch --stat` place after
		// the last hunk. Without this it was appended to the final file's body
		// and printed as though it were part of the patch, so "app.py | 4 +-"
		// turned up between the closing hunk and whatever came next.
		if len(f.lines) > 0 && reDiffStat.MatchString(l) {
			f.trailingStat = true
			return
		}

		if reHunk.MatchString(l) {
			f.lines = append(f.lines, l)
			continue
		}
		switch {
		case strings.HasPrefix(l, "+"):
			f.added++
		case strings.HasPrefix(l, "-"):
			f.removed++
		}
		f.lines = append(f.lines, l)
	}
	if len(files) == 0 {
		return nil, false
	}
	// A `diff --git` header is not enough to call something a patch. Several git
	// formats start with the same header and carry no hunks at all, and rewriting
	// them produced nonsense: --word-diff's "[-old-]{+new+}" came back under a
	// bogus "+0 -0" header, and a README paragraph beginning "diff --git is
	// described..." was reported as a change to a file named "is".
	//
	// So a diff is only a diff if something in it actually records an edit: a
	// line added or removed, a binary marker, a new/deleted/renamed file, or a
	// mode change. Anything else falls through the pipeline untouched, which is
	// the right outcome for a machine-readable format.
	//
	// Counting real +/- lines matters as much as seeing a hunk header, because
	// --word-diff prints the "@@" header and then its differences inside
	// brackets on a context line. That is still not a patch.
	for _, f := range files {
		if f.added > 0 || f.removed > 0 || f.binary || f.mode != "" ||
			f.kind == "added" || f.kind == "deleted" || f.kind == "renamed" {
			return files, true
		}
	}
	return nil, false
}

// diffPath pulls the new path out of "a/x b/x". The two paths are the same in
// almost every case, so taking the second half is right; the exception is a
// rename, which git reports on its own "rename from"/"rename to" lines.
func diffPath(arg string) string {
	fields := strings.Fields(arg)
	if len(fields) == 2 {
		return stripDiffPrefix(fields[1])
	}
	if len(fields) == 1 {
		return stripDiffPrefix(fields[0])
	}
	return stripDiffPrefix(arg)
}

func stripDiffPrefix(p string) string {
	p = strings.Trim(p, `"`)
	for _, prefix := range []string{"a/", "b/", "c/", "i/", "w/", "o/"} {
		if strings.HasPrefix(p, prefix) {
			return p[len(prefix):]
		}
	}
	return p
}

// formatShow prints a commit header and then its patch. `git show` was in
// gitCommands from the start but had no case in Format, so the patch kept every
// line of boilerplate git prints: "commit <full sha>", "Author:", "Date:", the
// blank line, "index 0000000..9917ab0", the "---" and "+++" pair.
//
// The header is kept, because "which commit is this" is a fair question to ask
// of `git show`, and the patch is handed to the same formatter as `git diff`, so
// both spellings of the same change come out the same shape.
func formatShow(w io.Writer, text string) {
	header, patch := splitShow(text)
	if patch == "" {
		// No patch at all: `git show --stat` on a merge, or a tag object. The
		// generic colorizer is better than nothing here.
		formatText(w, text)
		return
	}
	writeShowHeader(w, header)
	fmt.Fprintln(w)
	if !formatDiff(w, patch) {
		formatText(w, patch)
	}
}

// formatShowSideBySide is formatShow with the two-column diff view. It returns
// false when the patch cannot be laid out that way, so the caller can fall back
// to the ordinary view rather than printing something half-done.
func formatShowSideBySide(w io.Writer, text string, width int) bool {
	header, patch := splitShow(text)
	if patch == "" {
		return false
	}
	if _, ok := parseDiff(patch); !ok {
		return false
	}
	writeShowHeader(w, header)
	fmt.Fprintln(w)
	return sideBySide(w, patch, width)
}

// splitShow divides `git show` output at the first `diff --git` line: the commit
// header above it, the patch below. The patch is empty when there is none.
func splitShow(text string) (header, patch string) {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, l := range lines {
		if reDiffHeader.MatchString(l) {
			return strings.Join(lines[:i], "\n"), strings.Join(lines[i:], "\n")
		}
	}
	return text, ""
}

// writeShowHeader prints the commit header. It is parsed the same way a log
// entry is, so a commit shown and a commit logged read identically.
func writeShowHeader(w io.Writer, header string) {
	commits, _ := parseLog(header)
	for _, c := range commits {
		fmt.Fprintln(w, strings.TrimRight(fmt.Sprintf("%s  %s  %s",
			tool.PaintColor(yellow, c.hash),
			tool.PaintColor(cyan, c.author),
			tool.PaintColor(dim, c.date)), "  "))
		if c.message != "" {
			fmt.Fprintln(w, c.message)
		}
	}
	if len(commits) == 0 {
		for _, l := range strings.Split(header, "\n") {
			if strings.TrimSpace(l) != "" {
				fmt.Fprintln(w, tool.PaintColor(dim, l))
			}
		}
	}
}

// formatDiff prints the hunks, one header line per file. It returns false when
// the text is not a diff.
func formatDiff(w io.Writer, text string) bool {
	files, ok := parseDiff(text)
	if !ok {
		return false
	}

	// One line per file, then its hunks. A summary table was tried here and
	// removed: it repeated every file name a second time before the hunks that
	// the reader actually came for, so the output was longer than the change.
	for i, f := range files {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, diffFileHeader(f))
		if f.binary {
			fmt.Fprintln(w, tool.PaintColor(dim, "  (binary contents not shown)"))
			continue
		}
		for _, l := range f.lines {
			fmt.Fprintln(w, diffLine(l))
		}
	}
	return true
}

func kindText(f diffFile) string {
	switch f.kind {
	case "renamed":
		return "renamed"
	case "added":
		return "new file"
	case "deleted":
		return "deleted"
	}
	if f.mode != "" {
		return "mode " + shortMode(f.mode)
	}
	return "modified"
}

// kindColor keeps a deleted or renamed file from looking like a normal edit,
// which is the thing worth noticing first in a diff.
func kindColor(f diffFile) string {
	switch f.kind {
	case "added":
		return green
	case "deleted":
		return red
	case "renamed":
		return yellow
	}
	if f.mode != "" {
		return dim
	}
	return ""
}

func shortMode(m string) string {
	if i := strings.LastIndexByte(m, ' '); i >= 0 {
		return m[i+1:]
	}
	return m
}

// magnitude colors the line count by how much moved: a big change deserves a
// second look, a one-line change does not.
func magnitude(f diffFile) string {
	switch {
	case f.added+f.removed >= 100:
		return bold + " " + yellow
	case f.added+f.removed >= 20:
		return yellow
	}
	return dim
}

// diffName is what to call the file: a rename shows both ends, since knowing
// only the new name loses the fact that this used to be something else.
func diffName(f diffFile) string {
	switch {
	case f.path == "":
		return f.from
	case f.from != "" && f.from != f.path:
		return f.from + " → " + f.path
	}
	return f.path
}

// diffFileHeader is the single line that stands in for git's four boilerplate
// lines. The path is what a reader is actually looking for.
func diffFileHeader(f diffFile) string {
	out := tool.PaintColor(bold+" "+cyan, diffName(f))
	if k := kindText(f); k != "modified" {
		out += "  " + tool.PaintColor(kindColor(f), k)
	}
	if f.binary {
		return out
	}
	return out + "  " + tool.PaintColor(magnitude(f), fmt.Sprintf("+%d -%d", f.added, f.removed))
}

// diffLine colors one line of a hunk. Context is left alone so the code stays
// the thing you read; the hunk header is dimmed because it is a position, not
// content.
func diffLine(l string) string {
	if m := reHunk.FindStringSubmatch(l); m != nil {
		head := strings.TrimRight(l, " ")
		tail := l[len(head):]
		// The text after the closing @@ is git's guess at the enclosing
		// function, which is the only part of the line worth reading.
		if s := strings.TrimSpace(m[1]); s != "" {
			return tool.PaintColor(dim, head[:len(head)-len(s)]) + tool.PaintColor(cyan, s) + tail
		}
		return tool.PaintColor(dim, l)
	}
	switch {
	case strings.HasPrefix(l, "+"):
		return tool.PaintColor(green, l)
	case strings.HasPrefix(l, "-"):
		return tool.PaintColor(red, l)
	case strings.HasPrefix(l, "\\"):
		return tool.PaintColor(dim, l) // "\ No newline at end of file"
	}
	return l
}

// --- side-by-side diff ------------------------------------------------------
//
// A unified diff answers "what changed" but makes you read it twice: once to
// match a -line with its +line, once to understand the result. Putting the old
// and new version of the file in two columns makes the change legible in one
// pass, which is the whole point of a diff.
//
// It is opt-in because it needs width, and a narrow terminal or a pipe has none.

var (
	// reHunkRange pulls the two ranges out of "@@ -1,4 +1,6 @@".
	reHunkRange = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)
	// reNoNewline is git's marker for a missing trailing newline.
	reNoNewline = regexp.MustCompile(`^\\ No newline at end of file`)
)

// sbsRow is one line of the two-column view. Either side may be empty, which is
// what happens to an added line's left half.
type sbsRow struct {
	left, right     string
	leftNo, rightNo int // 0 means the line does not exist on that side
}

// sideBySide prints a diff as old | new. It returns false when the text is not
// a diff, so the caller can fall back.
func sideBySide(w io.Writer, text string, width int) bool {
	files, ok := parseDiff(text)
	if !ok {
		return false
	}

	rows := collectSbsRows(files)
	if rows == nil {
		return false
	}
	if width <= 0 {
		width = 80
	}

	for i, f := range files {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, diffFileHeader(f))
		if f.binary {
			fmt.Fprintln(w, tool.PaintColor(dim, "  (binary contents not shown)"))
			continue
		}
		for _, r := range rows[f.index] {
			fmt.Fprintln(w, sbsLine(r, width))
		}
	}
	return true
}

// collectSbsRows turns each file's hunk lines into paired rows. It returns nil
// if any line is one the pairing does not understand, so an unfamiliar diff
// format falls back to unified rather than being mangled.
func collectSbsRows(files []diffFile) map[int][]sbsRow {
	out := make(map[int][]sbsRow, len(files))
	for fi := range files {
		f := &files[fi]
		f.index = fi

		var rows []sbsRow
		var removed, added []string // pending runs, paired off below
		// leftNo/rightNo are the next line number on each side; leftStart and
		// rightStart are where the pending run began.
		leftNo, rightNo := 0, 0
		leftStart, rightStart := 0, 0
		inHunk := false

		flush := func() {
			// A removed run and an added run belong on the same rows, which is
			// the entire benefit of the two-column view: the old line sits
			// beside the new one it became.
			//
			// The numbers come from where each run started, not from where the
			// counters ended: a run of two removals advances leftNo past both
			// of them, and by then the first one's number is gone. leftStart is
			// the number of the run's first line, so it needs no offset.
			n := len(removed)
			if len(added) > n {
				n = len(added)
			}
			for i := 0; i < n; i++ {
				r := sbsRow{}
				if i < len(removed) {
					r.left, r.leftNo = removed[i], leftStart+i
				}
				if i < len(added) {
					r.right, r.rightNo = added[i], rightStart+i
				}
				rows = append(rows, r)
			}
			removed, added = nil, nil
			leftStart, rightStart = leftNo, rightNo
		}

		for _, l := range f.lines {
			switch {
			case reHunk.MatchString(l):
				flush()
				inHunk = true
				leftNo, rightNo = hunkStart(l)
				leftStart, rightStart = leftNo, rightNo
				rows = append(rows, sbsRow{left: l, right: "", leftNo: -1})
			case !inHunk:
				// Not part of a hunk: keep it as a full-width note.
				rows = append(rows, sbsRow{left: l, leftNo: -1})
			case reNoNewline.MatchString(l):
				rows = append(rows, sbsRow{left: l, leftNo: -1})
			case strings.HasPrefix(l, "-"):
				if len(removed) == 0 {
					leftStart = leftNo
				}
				removed = append(removed, l[1:])
				leftNo++
			case strings.HasPrefix(l, "+"):
				if len(added) == 0 {
					rightStart = rightNo
				}
				added = append(added, l[1:])
				rightNo++
			case strings.HasPrefix(l, " "), l == "":
				flush()
				content := strings.TrimPrefix(l, " ")
				rows = append(rows, sbsRow{left: content, right: content, leftNo: leftNo, rightNo: rightNo})
				leftNo++
				rightNo++
			default:
				flush()
				rows = append(rows, sbsRow{left: l, leftNo: -1})
			}
		}
		flush()
		out[fi] = rows
	}
	return out
}

// hunkStart returns the first old and new line numbers a hunk covers.
func hunkStart(l string) (left, right int) {
	m := reHunkRange.FindStringSubmatch(l)
	if m == nil {
		return 0, 0
	}
	return atoi(m[1]), atoi(m[3])
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// sbsLine renders one row of the two-column view, sized to fit width.
func sbsLine(r sbsRow, width int) string {
	if r.leftNo == -1 && strings.HasPrefix(r.left, "@@") {
		return sbsHunkHeader(r.left, width)
	}
	if r.leftNo == -1 {
		return tool.PaintColor(dim, truncate(r.left, width))
	}

	// The gutters show line numbers, which is what lets you jump to a line in
	// your editor without counting the diff.
	widthDigits := maxOf(digits(r.leftNo), digits(r.rightNo))
	blank := strings.Repeat(" ", widthDigits) + " "
	gutter := blankNo(r.leftNo, blank)
	sep := " │ "
	// Each side spends its number column plus one space before the content.
	perSide := widthDigits + 1

	avail := width - 2*perSide - utf8Len(sep)
	if avail < 20 {
		// Too narrow for two columns to be honest; one is still readable.
		return tool.PaintColor(dim, gutter) + " " + truncate(oneLine(r), width-utf8Len(gutter)-1)
	}
	half := avail / 2
	if avail%2 == 1 {
		half++
	}

	// The left column is padded so the separator lands in the same column on
	// every row. The right one is last on the line, so padding it would only
	// leave trailing whitespace behind.
	left := padSide(truncate(r.left, half), half)
	right := truncate(r.right, avail-half)

	return blankNo(r.leftNo, blank) + tool.PaintColor(red, left) +
		tool.PaintColor(dim, sep) + blankNo(r.rightNo, blank) + tool.PaintColor(green, right)
}

func sbsHunkHeader(l string, width int) string {
	if sm := reHunk.FindStringSubmatch(l); sm != nil {
		if s := strings.TrimSpace(sm[1]); s != "" {
			return tool.PaintColor(dim, truncate(strings.TrimRight(l, " "), width-utf8Len(s)-1)) +
				tool.PaintColor(cyan, s)
		}
	}
	return tool.PaintColor(dim, truncate(l, width))
}

func oneLine(r sbsRow) string {
	switch {
	case r.left != "" && r.right != "" && r.left != r.right:
		return "-" + r.left + "  +" + r.right
	case r.left != "":
		return "-" + r.left
	case r.right != "":
		return "+" + r.right
	}
	return r.left
}

// blankNo renders a line number right-aligned, or blanks when that side has no
// line, so the two columns stay in step down the whole hunk.
func blankNo(n int, blank string) string {
	if n <= 0 {
		return blank
	}
	return fmt.Sprintf("%*d ", utf8Len(blank)-1, n)
}

// padSide pads to a visible width, ignoring color codes. Every string here may
// already carry escapes, so len() would count them.
func padSide(s string, width int) string {
	n := utf8Len(s)
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}

func digits(n int) int {
	d := 1
	for n >= 10 {
		n /= 10
		d++
	}
	return d
}

func maxOf(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// utf8Len counts visible characters. Color codes and the box glyphs the CLI
// uses are all single-width, but a CJK or emoji character in the code being
// diffed is double-width and would push the columns out of alignment.
func utf8Len(s string) int {
	n := 0
	for _, r := range tool.StripANSI(s) {
		if isWide(r) {
			n += 2
			continue
		}
		n++
	}
	return n
}

func isWide(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F, // Hangul Jamo
		r >= 0x2E80 && r <= 0xA4CF && r != 0x303F, // CJK radicals through Yi
		r >= 0xAC00 && r <= 0xD7A3,                // Hangul syllables
		r >= 0xF900 && r <= 0xFAFF,                // CJK compatibility ideographs
		r >= 0xFE30 && r <= 0xFE6F,                // CJK compatibility forms
		r >= 0xFF00 && r <= 0xFF60,                // fullwidth forms
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x1F300 && r <= 0x1F64F, // emoji
		r >= 0x1F900 && r <= 0x1F9FF,
		r >= 0x20000 && r <= 0x3FFFD: // CJK extensions
		return true
	}
	return false
}

// truncate cuts a line to a visible width, marking that it was cut. Losing the
// tail of a long line silently would be worse than the extra marker.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if utf8Len(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	out := make([]rune, 0, width)
	n := 0
	for _, r := range s {
		w := 1
		if isWide(r) {
			w = 2
		}
		if n+w > width-1 {
			break
		}
		out = append(out, r)
		n += w
	}
	return string(out) + "…"
}
