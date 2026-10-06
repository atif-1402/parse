// Package journalctl formats journalctl output: the prefix of every line is
// dimmed so the message is the only bright text left.
package journalctl

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/atif-1402/parse/internal/tool"
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

// Journalctl implements `parse journalctl <args>`: run journalctl, format its
// output, and return journalctl's exit code.
func Run(args []string) int {
	// Being called with no arguments is not a reason to skip the formatting. A
	// bare `parse journalctl` prints the same shape of line as
	// `parse journalctl -n 400`, only more of them, so it is formatted exactly
	// like the flagged form. It used to take the passthrough path, which left
	// the most common invocation of all completely unformatted.
	if !formattableJournalctl(args) {
		return tool.Passthrough("journalctl", args)
	}
	return runStreaming(args)
}

// runStreaming runs journalctl and formats each line as it arrives.
//
// This deliberately avoids tool.Capture, which collects the child's output
// into one buffer before anything is printed. That is harmless for the tools
// whose output is roughly a screenful and hopeless for journalctl: this
// machine's journal holds over five million lines and takes ninety seconds to
// read, so buffering it means holding all of it in memory while showing the
// user nothing at all. Streaming keeps the memory flat and puts the first line
// on screen while journalctl is still reading.
//
// It also leaves stderr on the terminal instead of merging it into the stream,
// which is both the honest place for a warning and one less thing to corrupt
// the formatted lines.
func runStreaming(args []string) int {
	cmd := exec.Command("journalctl", args...)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr

	pr, err := cmd.StdoutPipe()
	if err != nil {
		return tool.ExitCode(err)
	}
	if err := cmd.Start(); err != nil {
		pr.Close()
		return tool.ExitCode(err)
	}

	// bufio.Reader rather than bufio.Scanner: a single journal line can be far
	// longer than Scanner's token limit, and one long line should not cut the
	// output short.
	r := bufio.NewReaderSize(pr, 64*1024)
	out := bufio.NewWriterSize(os.Stdout, 64*1024)
	for {
		line, err := r.ReadString('\n')
		if len(line) > 0 {
			formatted := FormatLine(strings.TrimRight(line, "\n"))
			out.WriteString(formatted)
			out.WriteByte('\n')
			// Flush per line so the output appears immediately rather than in
			// 64KB instalments; the pager is doing the work of keeping the
			// screen readable, not hiding the delay.
			if ferr := out.Flush(); ferr != nil {
				// Nobody is reading any more: the user has quit the pager. Go
				// only turns a broken pipe into a fatal SIGPIPE on fd 1 and 2,
				// and os.Stdout here is the pager's pipe rather than either, so
				// this arrives as an ordinary error that has to be acted on.
				// Without it, pressing q would close the pager and leave parse
				// to work silently through the rest of a five million line
				// journal before the shell prompt came back.
				cmd.Process.Kill()
				out.Flush()
				pr.Close()
				cmd.Wait()
				return 0
			}
		}
		if err != nil {
			break
		}
	}
	out.Flush()
	pr.Close()

	return tool.ExitCode(cmd.Wait())
}

// Follows reports whether these args ask journalctl to keep following the log,
// which means the output never ends on its own.
//
// It is one predicate for two questions that must not disagree: whether to page
// the output (a pager in front of a live stream traps Ctrl-C in the pager) and
// whether to format it (journalctl already pages a live stream itself, so
// parse's best move is to get out of the way). When these were two separate
// lists, --follow-new was known to one and not the other.
func Follows(args []string) bool {
	for _, a := range args {
		switch a {
		case "-f", "-F", "--follow", "--follow-new":
			return true
		}
	}
	return false
}

// NeedsTerminal reports whether this invocation needs the terminal untouched,
// which is what keeps the pager out of the way. See cmd.NeedsTerminal.
func NeedsTerminal(args []string) bool { return Follows(args) }

// formattableJournalctl is false when the output is machine-readable, or when
// journalctl follows the log and would never finish.
func formattableJournalctl(args []string) bool {
	if Follows(args) {
		return false
	}
	return !tool.IsMachineOutput(tool.OutputMode(args))
}

// ---------------------------------------------------------------------------
// Formatting
// ---------------------------------------------------------------------------

// reJournalLine matches "<time> <host> <unit>[<pid>]: <message>". The time is
// either the default "Oct 03 23:54:40" or an ISO form, and nothing else is
// accepted: a looser "any word" here would make systemctl's own
// "Active: failed (Result: ...)" rows look like journal lines.
// Host and unit are not told apart: both are dimmed, so there is nothing to
// gain from guessing which is which. The leading group absorbs the indent
// "systemctl status" puts on the log lines it appends to a failed unit.
var reJournalLine = regexp.MustCompile(
	`^(\s*)((?:[A-Z][a-z]{2}\s+\d{1,2}\s+\d{2}:\d{2}:\d{2})|\d{4}-\d{2}-\d{2}T[\d:+\-.]*)\s+(\S+)\s+([^\s\[:]+)(?:\[(\d+)\])?:\s?(.*)$`)

// reJournalRule matches the "-- Boot <id> --" and "-- Logs begin at ... --"
// separators journalctl prints between boots.
var reJournalRule = regexp.MustCompile(`^\s*--.*--\s*$`)

// reMessageKeyword matches a severity word at the start of a message, allowing
// for the "> " continuation markers multi-line tool output uses. Anchoring to
// the start is deliberate: log text is arbitrary, so a keyword in the middle of
// a sentence is left alone.
var reMessageKeyword = regexp.MustCompile(
	`(?i)^(>|\||\s)*\s*(error|failed|failure|fatal|panic|critical|crit|segfault|assertion|warning|warn)\b(:)?`)

// Format writes the whole of text, formatted, to w. It is the buffered
// counterpart of Stream and the one parse uses when the input already fits in
// memory.
func Format(w io.Writer, text string) {
	tool.EachLine(text, func(line string) {
		fmt.Fprintln(w, FormatLine(line))
	})
}

// Stream formats journal text from r as it arrives.
//
// This is the piped counterpart of runStreaming, and it exists for the same
// reason: `journalctl | parse` used to read its whole input to the end before
// writing anything, which for a journal of millions of lines means holding all
// of it in memory and showing the user nothing for the better part of a
// minute. Writing stops at the first write error, because the usual cause is
// the user having quit the pager.
func Stream(r io.Reader, w io.Writer) {
	br := bufio.NewReaderSize(r, 64*1024)
	out := bufio.NewWriterSize(w, 64*1024)
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			out.WriteString(FormatLine(strings.TrimRight(line, "\n")))
			out.WriteByte('\n')
			// Flushed per line so the first screenful is on screen while the
			// rest is still being read.
			if ferr := out.Flush(); ferr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// FormatLine returns one formatted line: the prefix dimmed when the line is a
// journal entry, a rule dimmed when it is one of journalctl's separators, and
// the line untouched when parse has nothing to say about it.
func FormatLine(line string) string {
	if s, ok := ColorLine(line); ok {
		return s
	}
	if reJournalRule.MatchString(line) {
		return tool.PaintColor(dim, line)
	}
	return line
}

// ColorLine dims the prefix so the message is the only bright text on
// the line.
func ColorLine(line string) (string, bool) {
	m := reJournalLine.FindStringSubmatch(line)
	if m == nil {
		return line, false
	}
	head := m[2] + " " + m[3] + " " + m[4]
	if m[5] != "" {
		head += "[" + m[5] + "]"
	}
	return m[1] + tool.PaintColor(dim, head+":") + " " + colorMessage(m[6]), true
}

// colorMessage paints a severity word at the start of a message, and nothing
// else.
func colorMessage(msg string) string {
	m := reMessageKeyword.FindStringSubmatch(msg)
	if m == nil {
		return msg
	}
	word := m[2] + m[3]
	color := red
	if strings.EqualFold(m[2], "warning") || strings.EqualFold(m[2], "warn") {
		color = yellow
	}
	lead := msg[:len(m[0])-len(word)]
	return tool.PaintColor(dim, lead) + tool.PaintColor(color, word) + msg[len(m[0]):]
}

// ---------------------------------------------------------------------------
// Piped input
// ---------------------------------------------------------------------------

// JournalctlPipe formats piped journalctl output (`journalctl ... | parse`).
func Pipe(w io.Writer, text string) {
	Format(w, tool.StripANSI(text))
}

// looksLikeJournal reports whether any of the first few lines carries a
// journal prefix. The prefix is distinctive enough that this cannot be
// confused with the other tools parse handles.
func LooksLike(text string) bool {
	seen := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if _, ok := ColorLine(line); ok {
			return true
		}
		if seen++; seen == 3 {
			break
		}
	}
	return false
}
