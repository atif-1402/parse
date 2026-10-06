// parse makes messy command output readable.
//
//	parse git status        run the command and format it
//	git status | parse      format piped output
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/atif-1402/parse/cmd"
	"github.com/atif-1402/parse/cmd/git"
	"github.com/atif-1402/parse/internal/tool"
)

const usage = `parse - make command output readable

Usage:
  parse <tool> <args>      run a command and show formatted output
  <tool> <args> | parse    format output that is piped in

Tools:
  git systemctl journalctl ps find du findmnt free lsof ip ss lsblk kubectl
  (run 'parse -l' to see what parse does for each)

Options:
  --color=MODE             auto (default), always or never
  -w, --side-by-side       show a diff as old | new, in two columns
  --no-pager               print it all instead of paging
  -l, --list               list the supported commands
  -h, --help               show this help
  -v, --version            show the version

Paging:
  Output goes through $PAGER (less by default) when it is a terminal and fills
  more than a screen, since parse runs the command through a pipe and its own
  pager switches itself off. Short output is printed straight through.
  Piped output is measured the same way, even for a command parse does not
  format: a flood of lines gets the pager, one screenful prints straight.
  Commands that read the keyboard or never end are left alone (git add -p,
  journalctl -f, systemctl watch) so Ctrl-C still reaches them.
  Set PARSE_PAGER or PAGER to choose one, or --no-pager to opt out.
`

// version is stamped at build time with -ldflags "-X main.version=..."
// and falls back to "dev" for a plain `go build`.
var version = "dev"

// tool describes one command parse can run or format, and is what `parse -l`
// prints. Grouped the way the README groups them, not alphabetically, because
// the order a newcomer should try them in is not the alphabet.
type toolInfo struct {
	name string
	what string
}

var toolList = []toolInfo{
	{"git", "status, log, diff, branch, blame, grep, stash, remote, shortlog, reflog, and the rest"},
	{"systemctl", "status, list-units, list-unit-files, list-dependencies, is-active, is-failed"},
	{"journalctl", "the whole log line: prefix dimmed, severities colored"},
	{"ps", "aux and friends: busy values tinted, long COMMAND dimmed"},
	{"find", "-ls turned into named columns instead of eleven positional fields"},
	{"du", "sizes always carry a unit; large entries tinted"},
	{"findmnt", "the mount tree dimmed; disk filesystems cyan, options quietened, a filling filesystem tinted"},
	{"free", "bare numbers get a unit; a filling machine is tinted"},
	{"lsof", "the socket state at the end of a row colored; the heading bolded"},
	{"ip", "addr and link collapsed per interface; route colored"},
	{"ss", "colliding headers split; the process name given its own column"},
	{"lsblk", "device tree dimmed; wrapped mountpoints indented under their device"},
	{"kubectl", "experimental: get tables aligned with STATUS tinted; describe keys dimmed, states colored"},
}

// tools are the subcommands parse knows how to run. Anything else is reported
// rather than guessed at. The error message names only what was typed and does
// not print this list, but the tests use it to prove the dispatch table and the
// list shown by `parse -l` cannot drift apart.
var tools = names()

func names() []string {
	out := make([]string, 0, len(toolList))
	for _, t := range toolList {
		out = append(out, t.name)
	}
	return out
}

// supported reports whether name is a command parse knows how to run.
func supported(name string) bool {
	return slices.Contains(tools, name)
}

// toolListText is what `parse -l` prints. It says what parse actually changes,
// so a reader can tell whether running a tool through parse is worth it.
func toolListText() string {
	var b strings.Builder
	b.WriteString("parse supports these commands:\n\n")
	width := 0
	for _, t := range toolList {
		if len(t.name) > width {
			width = len(t.name)
		}
	}
	for _, t := range toolList {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, t.name, t.what)
	}
	b.WriteString("\nAnything else is passed through untouched. Run 'parse -h' for usage.\n")
	return b.String()
}

// noArgs is what bare `parse` prints. It must not be the full usage: the user
// has not asked a question yet, and the next thing they need is the two shapes
// parse accepts, not every flag.
const noArgs = `parse: nothing to do.

Give parse a command to run:
  parse git status
Or pipe output into it:
  git status | parse

Run 'parse -h' or 'parse --help' for the full usage.
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	opts, rest, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse: "+err.Error())
		return 2
	}
	if opts.showList {
		fmt.Print(toolListText())
		return 0
	}
	if opts.showVersion {
		fmt.Println("parse", version)
		return 0
	}
	colorEnabled = opts.color()
	cmd.SetHooks(paint, printTable)
	// The two-column diff is the one thing parse prints that has to know how
	// much room it has, so it is measured once here.
	git.SetWidth(tool.TerminalWidth())
	git.SetSideBySide(opts.sideBySide)

	if len(rest) == 0 {
		if !isTerminal(os.Stdin) {
			return runPipe(opts.noPager)
		}
		fmt.Fprint(os.Stderr, noArgs)
		return 2
	}

	// The pager has to be started before the tool runs, and after the color mode
	// has been decided, because color is a question about the real terminal.
	//
	// Only a tool parse actually knows how to run gets one here, because it is
	// the only thing on this path whose arguments can say whether the command
	// reads the keyboard or never ends. The piped path has no command line and
	// decides for itself, once there is enough output to decide on.
	if supported(rest[0]) && shouldPage(rest[0], rest[1:], opts.noPager, isTerminal(os.Stdout)) {
		if p, err := startPager(); err == nil {
			defer p.finish()
		}
	}

	switch rest[0] {
	case "-h", "--help", "help":
		fmt.Print(usage)
		return 0
	case "git":
		return cmd.Git(rest[1:])
	case "systemctl":
		return cmd.Systemctl(rest[1:])
	case "journalctl":
		return cmd.Journalctl(rest[1:])
	case "ps":
		return cmd.Ps(rest[1:])
	case "find":
		return cmd.Find(rest[1:])
	case "du":
		return cmd.Du(rest[1:])
	case "findmnt":
		return cmd.Findmnt(rest[1:])
	case "free":
		return cmd.Free(rest[1:])
	case "lsof":
		return cmd.Lsof(rest[1:])
	case "ip":
		return cmd.Ip(rest[1:])
	case "ss":
		return cmd.Ss(rest[1:])
	case "lsblk":
		return cmd.Lsblk(rest[1:])
	case "kubectl":
		return cmd.Kubectl(rest[1:])
	case "-l", "--list":
		fmt.Print(toolListText())
		return 0
	case "-v", "--version":
		// Only reachable when no tool was named, which parseArgs already
		// turned into showVersion; kept for the case where rest starts with it.
		fmt.Println("parse", version)
		return 0
	}
	// parse runs a command it knows how to format, and says so plainly when it
	// does not know one: a typo is a typo to fix, not something to browse, and
	// the list is one flag away (`parse -l`). The piped path is unaffected —
	// `<cmd> | parse` shows the output of any command, formatted when parse
	// knows it and untouched when it does not.
	fmt.Fprintf(os.Stderr, "parse: %s is not supported yet\n", rest[0])
	return 2
}

// options is what parse itself should do, as opposed to the tool it runs.
type options struct {
	colorMode   string
	showVersion bool
	showList    bool
	sideBySide  bool
	noPager     bool
}

// color decides whether to emit ANSI codes. "auto" keeps the friendly
// default: color only when writing to a terminal, and never when NO_COLOR
// is set.
func (o options) color() bool {
	switch o.colorMode {
	case "always":
		return true
	case "never":
		return false
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	return isTerminal(os.Stdout)
}

// parseArgs pulls parse's own flags out of args. --color is accepted before or
// after the subcommand, because both read naturally:
//
//	parse --color=always git log | less
//	git status | parse --color=never
func parseArgs(args []string) (options, []string, error) {
	o := options{colorMode: "auto"}
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--color" && len(rest) == 0:
			// Only before a tool is named, like the other parse flags below.
			// After that it belongs to the tool: `parse git log --color always`
			// must hand both words to git rather than eating them as parse's own.
			if i+1 == len(args) {
				return o, nil, fmt.Errorf("--color needs a value: auto, always or never")
			}
			i++
			if err := o.setColor(args[i]); err != nil {
				return o, nil, err
			}
		case strings.HasPrefix(a, "--color=") && len(rest) == 0:
			if err := o.setColor(strings.TrimPrefix(a, "--color=")); err != nil {
				return o, nil, err
			}
		case (a == "-w" || a == "--side-by-side") && len(rest) == 0:
			// parse's own diff view. Only claimed before a tool is named, so
			// `parse git diff -w` reaches git's own flag handling instead.
			o.sideBySide = true
		case (a == "-l" || a == "--list") && len(rest) == 0:
			o.showList = true
		case a == "--no-pager" && len(rest) == 0:
			// Paging is parse's own doing, so it has to be able to say no. Only
			// before a tool is named; after that `--no-pager` is the tool's, and
			// shouldPage reads it from the tool's own arguments anyway.
			o.noPager = true
		case (a == "-v" || a == "--version") && len(rest) == 0:
			// Only claim the version flag before a tool is named. After that
			// it belongs to the tool: `parse git --version` must ask git.
			o.showVersion = true
		default:
			rest = append(rest, a)
		}
	}
	return o, rest, nil
}

func (o *options) setColor(v string) error {
	switch v {
	case "auto", "always", "never":
		o.colorMode = v
		return nil
	}
	return fmt.Errorf("bad --color value %q: use auto, always or never", v)
}

// runPipe handles `<tool> ... | parse`.
//
// Empty input is a normal answer, not a mistake. `git diff` on a clean tree
// prints nothing, `git status --porcelain` prints nothing when nothing is
// staged, `git log --author=whoever` prints nothing when nobody matches, and
// `ip route show table nosuchtable` prints nothing because there is no such
// table. Treating those as errors made parse announce a problem that did not
// exist, and it exited 1, so a script checking $? broke over a clean tree.
//
// So: read whatever arrived, format it, exit 0. An empty stream formats to
// nothing and says nothing, which is what the command already said. If someone
// really did pipe the wrong stream, bash has usually already told them about it
// on stderr, and parse adding a guess about *why* would only be a second
// opinion on something the user is already looking at.
// stdoutWriter writes to whatever os.Stdout happens to be at the moment of the
// write, rather than to the file it was when it was made. The pager works by
// replacing os.Stdout, so a writer captured before the pager starts would keep
// writing to the terminal and quietly bypass it.
type stdoutWriter struct{}

func (stdoutWriter) Write(p []byte) (int, error) { return os.Stdout.Write(p) }

// runPipe formats text that arrived on stdin. noPager is passed along so that
// `parse --no-pager` means the same thing here as it does for a named tool.
func runPipe(noPager bool) int {
	tty := isTerminal(os.Stdout)
	out := bufio.NewWriterSize(stdoutWriter{}, 64*1024)
	var p *pager
	// The flush has to happen before the pager is finished, because after that
	// os.Stdout is the terminal again and the last of the output would go
	// straight past the pager the user was just reading.
	defer func() {
		out.Flush()
		if p != nil {
			p.finish()
		}
	}()
	start := func() {
		if p != nil {
			return
		}
		if started, err := startPager(); err == nil {
			p = started
		}
	}

	// Output parse does not recognize is still output someone is reading, so
	// it is held back just long enough to find out whether it is a flood: one
	// screenful or less is printed straight through, and anything longer gets
	// the same pager everything else does. A command parse has never heard of
	// reads as well piped as a supported one does.
	gate := &screenGate{to: out, start: start}
	if tty && !noPager {
		gate.limit, gate.width = screenSize()
	} else {
		gate.release() // nowhere to page to, so nothing to hold back
	}

	cmd.PipeStream(os.Stdin, gate, func(name string) {
		// The args are nil because there is no command line to inspect here:
		// there is no `--no-pager` after a tool name and nothing that follows
		// the log, so the follow rule has nothing to catch.
		if shouldPage(name, nil, noPager, tty) {
			start()
		}
		gate.release() // a formatter writes from here, into whatever is open
	})
	gate.release() // the stream ended short of a screenful
	return 0
}

// screenGate decides whether piped output is long enough to need a pager, by
// holding it back until either the answer is yes or the stream runs out.
//
// It is the piped equivalent of what the direct path decides from the command
// line before anything runs. Holding is the only thing it does: once the pager
// is open, or the output fits, or there is no terminal to page on, every byte
// after that goes straight through and the gate is done.
type screenGate struct {
	to    *bufio.Writer
	start func()

	limit int // rows the terminal has
	width int // columns it has
	used  int // rows the output held so far would take
	col   int // columns of the line being counted

	buf  bytes.Buffer
	open bool
}

func (g *screenGate) Write(p []byte) (int, error) {
	if g.open {
		return g.to.Write(p)
	}
	n, _ := g.buf.Write(p)
	if g.used <= g.limit {
		g.count(p)
	}
	if g.used > g.limit {
		g.start()
		g.release()
	}
	return n, nil
}

// count adds up the rows p would take on screen. A line that runs past the
// terminal's width wraps and so is worth more than one row, which is why a
// screenful is counted rather than measured in bytes: a hundred short lines
// fill the screen whether or not they add up to many bytes.
func (g *screenGate) count(p []byte) {
	for _, b := range p {
		if b == '\n' {
			g.used += 1 + (g.col-1)/g.width
			g.col = 0
			continue
		}
		g.col++
	}
}

// release hands everything held so far to the destination and stops holding.
// It is called when a formatter is about to write, when the output turns out
// to be a flood, and when the stream ends.
func (g *screenGate) release() {
	if g.open {
		return
	}
	g.open = true
	if g.buf.Len() > 0 {
		g.to.Write(g.buf.Bytes())
		g.buf.Reset()
	}
}

// screenSize is the terminal's rows and columns, or the usual 80x24 when it
// will not say. The width is used only to count wrapped lines, so being wrong
// about it costs at most a pager that prints the output anyway.
func screenSize() (rows, cols int) {
	rows, cols = tool.TerminalHeight(), tool.TerminalWidth()
	if rows <= 0 {
		rows = 24
	}
	if cols <= 0 {
		cols = 80
	}
	return rows, cols
}

// isTerminal reports whether f is an interactive terminal. The kernel is asked
// directly, because the file mode cannot tell a terminal from /dev/null and
// both are character devices: see tool.IsTerminal.
func isTerminal(f *os.File) bool {
	return tool.IsTerminal(f)
}

// printTable prints an aligned table. Padding is computed from the plain
// text, so ANSI codes never throw off the alignment.
func printTable(w io.Writer, headers []string, rows [][]tool.Cell) {
	header := make([]tool.Cell, len(headers))
	for i, h := range headers {
		header[i] = tool.Cell{Text: h, Color: "bold"}
	}
	all := append([][]tool.Cell{header}, rows...)

	widths := make([]int, len(headers))
	for _, row := range all {
		for i, c := range row {
			if i < len(widths) {
				if n := utf8.RuneCountInString(c.Text); n > widths[i] {
					widths[i] = n
				}
			}
		}
	}

	for _, row := range all {
		var sb strings.Builder
		for i := range widths {
			var c tool.Cell
			if i < len(row) {
				c = row[i]
			}
			sb.WriteString(paint(c.Color, c.Text))
			if i < len(widths)-1 { // no trailing padding on the last column
				sb.WriteString(strings.Repeat(" ", widths[i]-utf8.RuneCountInString(c.Text)+2))
			}
		}
		fmt.Fprintln(w, sb.String())
	}
}
