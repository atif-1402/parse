// Package cmd is parse's front door: one entry point per supported utility,
// plus the dispatcher for piped input.
//
// Each utility keeps its own folder (cmd/git, cmd/systemctl, cmd/ps, ...).
// This file exists so main.go has a single import and so the order piped input
// is sniffed in lives in one readable place.
package cmd

import (
	"bufio"
	"bytes"
	"io"
	"strings"

	"github.com/atif-1402/parse/cmd/docker"
	"github.com/atif-1402/parse/cmd/du"
	"github.com/atif-1402/parse/cmd/find"
	"github.com/atif-1402/parse/cmd/findmnt"
	"github.com/atif-1402/parse/cmd/free"
	"github.com/atif-1402/parse/cmd/git"
	"github.com/atif-1402/parse/cmd/ip"
	"github.com/atif-1402/parse/cmd/journalctl"
	"github.com/atif-1402/parse/cmd/kubectl"
	"github.com/atif-1402/parse/cmd/lsblk"
	"github.com/atif-1402/parse/cmd/lsof"
	"github.com/atif-1402/parse/cmd/ps"
	"github.com/atif-1402/parse/cmd/ss"
	"github.com/atif-1402/parse/cmd/systemctl"
	"github.com/atif-1402/parse/internal/tool"
)

// Tool describes one command parse knows about, for `parse -l`.
type Tool struct {
	Name string
	// What parse does for it. Only Used is empty for a tool parse runs but
	// never reformats, which is still worth listing: `parse <tool>` works and
	// keeps the tool's exit code.
	Used string
}

// Git implements `parse git <args>`.
func Git(args []string) int { return git.Run(args) }

// Systemctl implements `parse systemctl <args>`.
func Systemctl(args []string) int { return systemctl.Run(args) }

// Kubectl implements `parse kubectl <args>`.
func Kubectl(args []string) int { return kubectl.Run(args) }

// Journalctl implements `parse journalctl <args>`.
func Journalctl(args []string) int { return journalctl.Run(args) }

// Ps implements `parse ps <args>`.
func Ps(args []string) int { return ps.Run(args) }

// Docker implements `parse docker <args>`.
func Docker(args []string) int { return docker.Run(args) }

// Find implements `parse find <args>`.
func Find(args []string) int { return find.Run(args) }

// Findmnt implements `parse findmnt <args>`.
func Findmnt(args []string) int { return findmnt.Run(args) }

// Du implements `parse du <args>`.
func Du(args []string) int { return du.Run(args) }

// Free implements `parse free <args>`.
func Free(args []string) int { return free.Run(args) }

// Lsof implements `parse lsof <args>`.
func Lsof(args []string) int { return lsof.Run(args) }

// Ip implements `parse ip <args>`.
func Ip(args []string) int { return ip.Run(args) }

// Ss implements `parse ss <args>`.
func Ss(args []string) int { return ss.Run(args) }

// Lsblk implements `parse lsblk <args>`.
func Lsblk(args []string) int { return lsblk.Run(args) }

// SetHooks installs the color and table functions that live in the CLI.
// main calls this once at startup.
func SetHooks(paint func(color, s string) string, printTable func(w io.Writer, headers []string, rows [][]tool.Cell)) {
	tool.Paint = paint
	tool.PrintTable = printTable
}

// Pipe formats piped output from any supported tool, guessing which one
// produced it.
//
// Text that matches no formatter is written through byte for byte, including
// any color the original tool produced. That is deliberate: if a user ran
// `grep --color=always` and piped it into parse, the color was asked for and
// parse has no reason to remove it.
//
// The order matters. Each detector is deliberately strict about the shape it
// accepts, because a false positive rewrites output from a different tool. The
// ones with a distinctive header come first; git's detector is the loosest and
// is tried last.
//
// Detection and formatting both work on the text with ANSI removed, because
// every detector matches plain prefixes such as "diff --git " and an escape
// sequence in front of the first word would make it miss. That is what lets
// parse handle output a tool colored itself: the tool's color is dropped and
// parse's own formatting takes over, so a diff is never half one palette and
// half another.
//
// Text no detector claims is written byte for byte, with its color intact. If
// the user asked for `grep --color=always`, parse has no business removing it.
func Pipe(w io.Writer, text string) {
	PipeClaim(w, text, nil)
}

// PipeClaim is Pipe with a hook that runs once a detector has claimed the text
// and before any of it is written.
//
// The hook exists for the pager. The direct path can ask shouldPage about the
// command line it was given, but the piped path has no command line: the only
// way to know whether this text is a flood is to let the detectors say which
// tool produced it. Paging has to start at that moment rather than before,
// because a pager opened for every pipe would then sit in front of
// `ls | parse` and `ps aux | parse` too.
func PipeClaim(w io.Writer, text string, claim func(tool string)) {
	plain := tool.StripANSI(text)
	// CRLF first, then strip. A \\r left at the end of a line used to survive
	// into the output, so a hunk header and the lines under it disagreed about
	// what ends a line and the whole block rendered as one long line. Git
	// produces CRLF when core.autocrlf is on and when a diff is read from a
	// file saved on another system, so it is worth handling.
	plain = strings.ReplaceAll(plain, "\r\n", "\n")
	tool, sub, ok := Detect(plain)
	if !ok {
		io.WriteString(w, text)
		return
	}
	if claim != nil {
		claim(tool)
	}
	for _, d := range detectors {
		if d.tool == tool {
			d.format(w, sub, plain)
			return
		}
	}
	io.WriteString(w, text)
}

// PipeStream formats piped input that may be too large to hold in memory.
//
// It reads a short prefix first, because the detectors need to be told apart
// before anything can be formatted, and then either streams or buffers on the
// answer. Only the journal is streamed: it is the one piped source with no
// upper bound on its length, and the one whose whole output has to be readable
// before a single line can be printed if it is not. Everything else keeps the
// buffered path, where the detector sees all of the text rather than a prefix
// of it and so cannot be fooled by a short opening.
func PipeStream(r io.Reader, w io.Writer, claim func(tool string)) {
	br := bufio.NewReaderSize(r, 64*1024)

	// One read rather than a full buffer: this returns whatever has arrived
	// instead of waiting for a fixed number of bytes, so a producer that is
	// still going is not held up waiting to reach the quota.
	head := make([]byte, 32*1024)
	n, _ := br.Read(head)
	if n > 0 {
		if tool, _, ok := Detect(string(head[:n])); ok && tool == "journalctl" {
			if claim != nil {
				claim(tool)
			}
			// The prefix is pushed back in front of the rest so the line that
			// straddles the two is not cut in half.
			journalctl.Stream(io.MultiReader(bytes.NewReader(head[:n]), br), w)
			return
		}
	}

	rest, _ := io.ReadAll(br)
	PipeClaim(w, string(head[:n])+string(rest), claim)
}

// Detect reports which tool's detector claims text, and which subcommand of it
// was recognized. ok is false when no detector wanted the text, in which case
// parse leaves it alone.
func Detect(text string) (tool, sub string, ok bool) {
	for _, d := range detectors {
		if sub := d.detect(text); sub != "" {
			return d.tool, sub, true
		}
	}
	return "", "", false
}

// NeedsTerminal reports whether a command line must be handed the terminal
// untouched, so the pager stays out of its way.
//
// Two kinds of command qualify, and both are the same problem from parse's
// side: something either reads the keyboard as it goes (`git add -p`, a `git
// commit` with no message, `kubectl exec`) or never finishes at all
// (`journalctl -f`, `kubectl logs -f`, `systemctl watch`). A pager in front of
// either traps Ctrl-C in the pager, so the command cannot be stopped at all.
//
// The question is asked here, before anything runs, rather than left to each
// tool. A tool decides for itself whether it can format the output, but that
// happens after the pager has already taken over stdout, so by the time a tool
// says "this one needs the terminal" it is too late to be useful.
func NeedsTerminal(name string, args []string) bool {
	switch name {
	case "git":
		return git.NeedsTerminal(args)
	case "journalctl":
		return journalctl.NeedsTerminal(args)
	case "kubectl":
		return kubectl.NeedsTerminal(args)
	case "free":
		return free.NeedsTerminal(args)
	case "lsof":
		return lsof.NeedsTerminal(args)
	case "findmnt":
		return findmnt.NeedsTerminal(args)
	case "systemctl":
		return systemctl.NeedsTerminal(args)
	case "docker":
		return docker.NeedsTerminal(args)
	}
	return false
}

// detector is one tool's chance to claim the piped text. detect returns the
// subcommand it recognized, or "" to let the next detector try. tool is the
// command's name: the pager needs it, and neither detect nor format has any
// other reason to know it.
type detector struct {
	tool   string
	detect func(string) string
	format func(io.Writer, string, string)
}

// detectors is the sniffing order, tried top to bottom. Journal lines and
// systemctl reports have distinctive shapes, while git's detector is the
// loosest, so git is last.
var detectors = []detector{
	{
		tool: "journalctl",
		detect: func(text string) string {
			if journalctl.LooksLike(text) {
				return "journal"
			}
			return ""
		},
		format: func(w io.Writer, sub, text string) { journalctl.Format(w, text) },
	},
	// Before lsblk: both tables open with a capitalised NAME column, and the
	// kubectl detector is the one that insists on a column lsblk never prints.
	{tool: "systemctl", detect: systemctl.Detect, format: systemctl.Format},
	{tool: "kubectl", detect: kubectl.Detect, format: kubectl.Format},
	{tool: "lsblk", detect: lsblk.Detect, format: func(w io.Writer, sub, text string) { lsblk.Format(w, text) }},
	// findmnt's heading is a row of its own column names, which nothing else
	// parse knows prints in capitals — and `df` is the one table a user pipes
	// next to it, spelled in lower case.
	{tool: "findmnt", detect: findmnt.Detect, format: func(w io.Writer, sub, text string) { findmnt.Format(w, text) }},
	// docker's ps heading opens with a "CONTAINER ID" column, two words in one
	// cell, which no other table parse knows begins with; its inspect JSON is
	// recognized by keys only docker prints.
	{tool: "docker", detect: docker.Detect, format: docker.Format},
	{tool: "ss", detect: ss.Detect, format: func(w io.Writer, sub, text string) { ss.Format(w, text) }},
	{tool: "ps", detect: ps.Detect, format: func(w io.Writer, sub, text string) { ps.Format(w, text) }},
	{tool: "find", detect: find.Detect, format: func(w io.Writer, sub, text string) { find.Format(w, text) }},
	{tool: "du", detect: du.Detect, format: func(w io.Writer, sub, text string) { du.Format(w, text) }},
	{tool: "free", detect: free.Detect, format: func(w io.Writer, sub, text string) { free.Format(w, text) }},
	{tool: "lsof", detect: lsof.Detect, format: func(w io.Writer, sub, text string) { lsof.Format(w, text) }},
	{tool: "ip", detect: ip.Detect, format: ip.Format},
	{tool: "git", detect: git.Detect, format: git.Format},
}
