// Paging exists because parse runs tools through a pipe.
//
// journalctl and git each switch their own pager off when their output is not a
// terminal, and here it never is: tool.Capture collects the output into a buffer
// so parse can format it. Left alone, `parse journalctl` prints the entire
// backlog in one go, thousands of lines past the bottom of the screen, with the
// only way back being a scroll wheel on a terminal that no longer knows what it
// is showing. `git log` has the same problem for the same reason.
//
// So parse takes the pager back: the formatted output goes to $PAGER instead of
// straight to the screen, which is what puts the arrow keys back.
//
// The rules it follows, all of which exist to keep parse from breaking a
// pipeline or trapping output that was never going to be read by a person:
//
//   - Only when stdout is a terminal. Redirected or piped output is written
//     straight through, so `parse journalctl > log.txt` still works.
//   - Never in front of something interactive or endless. `git add -p` and
//     `systemctl watch` get the terminal untouched, because a pager between
//     them and the keyboard traps Ctrl-C in the pager itself.
//   - Only when a pager is actually installed, and never for `dumb` terminals.
//
// Every command parse formats is a candidate, not a chosen few. It used to be
// an allowlist of `journalctl` and `git`, on the grounds that a pager which
// opens and closes again on every invocation is worse than none -- but `less -F`
// answers that on its own: output that fits on one screen is printed straight
// through without the pager ever taking the screen, so there is nothing to
// flicker and nothing to press `q` past. `parse du` over a large tree and
// `parse git diff` on a long branch are floods for exactly the same reason
// journalctl is, and the rule that fits them all is not "is this tool noisy"
// but "did it fill more than a screen".
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/atif-1402/parse/cmd"
)

// pager holds the running pager process and the pipe parse now writes into.
type pager struct {
	cmd   *exec.Cmd
	in    *os.File
	saved *os.File
	done  bool
}

// shouldPage decides whether this invocation is one to put in a pager. The
// terminal check is a parameter rather than a call to isTerminal so the whole
// decision can be tested without a terminal.
func shouldPage(name string, args []string, noPager, stdoutTTY bool) bool {
	if !stdoutTTY || noPager {
		return false
	}
	if cmd.NeedsTerminal(name, args) {
		return false
	}
	// The tools' own opt-out reaches parse as an argument of its own, because
	// after the tool's name every flag belongs to the tool.
	if hasNoPager(args) {
		return false
	}
	// less needs a real terminal type to draw with. Without one it prints a
	// warning and limps, which is worse than not paging at all.
	if term := os.Getenv("TERM"); term == "" || term == "dumb" {
		return false
	}
	_, _, err := pagerCommand()
	return err == nil
}

func hasNoPager(args []string) bool {
	for _, a := range args {
		if a == "--no-pager" || strings.HasPrefix(a, "--no-pager=") {
			return true
		}
	}
	return false
}

// pagerCommand returns the pager to run and the flags to run it with.
//
// PARSE_PAGER wins over PAGER, which wins over less. Whichever is chosen, less
// gets -R -F -X added when they are not already there: without -R it throws the
// colors away, without -F it sits there holding a single screenful, and without
// -X it wipes the terminal on the way out. Flags already given are left alone,
// and because less takes the command line over $LESS, adding them cannot be
// undone by an old $LESS from somewhere.
func pagerCommand() (string, []string, error) {
	spec := os.Getenv("PARSE_PAGER")
	if spec == "" {
		spec = os.Getenv("PAGER")
	}
	fields := strings.Fields(spec)
	name := "less"
	var flags []string
	if len(fields) > 0 {
		name = fields[0]
		flags = fields[1:]
	}
	if filepath.Base(name) == "less" {
		for _, want := range []string{"-R", "-F", "-X"} {
			if !hasAnyFlag(flags, want) {
				flags = append(flags, want)
			}
		}
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", nil, err
	}
	return path, flags, nil
}

// hasAnyFlag reports whether flag is already in the list, either on its own or
// inside a bundle such as -FRX. flag is a single-letter option, written the way
// it would be typed: "-R".
func hasAnyFlag(flags []string, flag string) bool {
	if len(flag) != 2 || flag[0] != '-' || flag[1] == '-' {
		return false
	}
	for _, f := range flags {
		if f == flag {
			return true
		}
		// A bundle is a single dash and several letters. A long option is not a
		// bundle, so --no-pager must not count as containing "p".
		if len(f) > 1 && f[0] == '-' && f[1] != '-' && strings.ContainsRune(f[1:], rune(flag[1])) {
			return true
		}
	}
	return false
}

// startPager runs the pager and points os.Stdout at it, so every tool that
// writes to os.Stdout -- directly or through tool.Capture -- lands in the pager
// instead of on the screen.
//
// Nothing is changed unless the pager actually starts. A missing or broken pager
// must not cost the user their output.
func startPager() (*pager, error) {
	name, flags, err := pagerCommand()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(name, flags...)
	saved := os.Stdout
	// The pager draws on the real terminal, not on the pipe being fed.
	cmd.Stdout = saved
	cmd.Stderr = os.Stderr

	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdin = r
	if err := cmd.Start(); err != nil {
		r.Close()
		w.Close()
		return nil, err
	}
	// The child has its own descriptor for the read end now.
	r.Close()

	os.Stdout = w
	return &pager{cmd: cmd, in: w, saved: saved}, nil
}

// finish puts os.Stdout back and waits for the pager, which ends when the write
// end of the pipe closes. The pager's own exit code is deliberately dropped: it
// says whether the user quit the pager, which is not the tool's exit code and
// must not become one.
func (p *pager) finish() {
	if p == nil || p.done {
		return
	}
	p.done = true
	os.Stdout = p.saved
	p.in.Close()
	p.cmd.Wait()
}
