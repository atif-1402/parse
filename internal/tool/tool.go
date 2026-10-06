// Package tool holds the plumbing every supported utility shares: how to run
// a command, how to hand back its exit code, and how to ask the CLI for color.
//
// It lives outside cmd/ so that cmd/git, cmd/systemctl and cmd/journalctl can
// all import it without importing each other.
package tool

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"unsafe"
)

// Cell is one table value with an optional color name.
type Cell struct {
	Text, Color string
}

// Hooks set by main before any command runs. Colors and the table printer live
// in the CLI (main.go, colors.go), which cannot be imported from here.
var (
	Paint      func(color, s string) string
	PrintTable func(w io.Writer, headers []string, rows [][]Cell)
)

// Color names understood by Paint. Combine with a space, e.g. "bold green".
const (
	Bold   = "bold"
	Dim    = "dim"
	Red    = "red"
	Green  = "green"
	Yellow = "yellow"
	Cyan   = "cyan"
)

// PaintColor wraps s in the named color. It returns s unchanged when color is
// off, the name is unknown, or s is empty.
func PaintColor(color, s string) string {
	if Paint == nil || color == "" || s == "" {
		return s
	}
	return Paint(color, s)
}

var reANSI = regexp.MustCompile("\x1b\\[[0-9;?]*[A-Za-z]")

// StripANSI removes any color codes a tool already printed, so parse does not
// stack its own colors on top of them.
func StripANSI(s string) string {
	return reANSI.ReplaceAllString(s, "")
}

// EachLine calls fn for every line in text, without the trailing newline.
func EachLine(text string, fn func(string)) {
	if text == "" {
		return
	}
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		fn(line)
	}
}

// OutputMode returns the value of -o/--output, or "" when it is unset. It
// understands "-o json", "-ojson", "--output json" and "--output=json".
func OutputMode(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if v, ok := strings.CutPrefix(a, "--output="); ok {
			return v
		}
		if v, ok := strings.CutPrefix(a, "-o="); ok {
			return v
		}
		if a == "-o" || a == "--output" {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
		if len(a) > 2 && strings.HasPrefix(a, "-o") {
			return a[2:]
		}
	}
	return ""
}

// HasLongFlag reports whether args contains flag, either as "--flag" or as the
// "--flag=value" form.
func HasLongFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag || strings.HasPrefix(a, flag+"=") {
			return true
		}
	}
	return false
}

// Passthrough runs a command with the terminal attached and no formatting.
func Passthrough(name string, args []string) int {
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return ExitCode(err)
	}
	return 0
}

// Capture runs a command and returns its stdout, with stderr passed through to
// the terminal as it arrives.
//
// The two streams are kept apart on purpose. Merging them, which is what
// CombinedOutput does, puts a tool's diagnostics into the text parse is about to
// format, so `parse git log` outside a repository printed git's "fatal: not a
// git repository" on stdout where vanilla git writes it to stderr. A program
// reading parse's output then gets an error as if it were data. Errors still
// reach the reader, on the stream they belong to, and in the order they
// happened. started is false when the command could not be run at all.
func Capture(name string, args []string) (text string, code int, started bool) {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	data, err := cmd.Output()
	if err != nil {
		code = ExitCode(err)
		if _, ran := err.(*exec.ExitError); !ran {
			return "", code, false
		}
	}
	return string(data), code, true
}

// ExitCode returns the child's exit status, or 127 if it could not start.
// A nil error means the child succeeded, so it reports 0 rather than treating
// the absence of a failure as something to complain about on stderr.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	fmt.Fprintf(os.Stderr, "parse: %v\n", err)
	return 127
}

// FirstNonEmpty returns the first line that has something on it.
func FirstNonEmpty(lines []string) string {
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			return l
		}
	}
	return ""
}

// machineOutput lists the -o/--output modes that are meant to be read by a
// program. Both systemd tools share these spellings.
var machineOutput = map[string]bool{
	"json": true, "json-pretty": true, "json+pretty": true, "json+sse": true,
	"json-secure": true, "export": true, "export-binary": true,
	"cat": true, "verbose": true, "all": true, "short-xattr": true,
}

// IsMachineOutput reports whether an output mode is machine-readable, and so
// must be passed through untouched.
func IsMachineOutput(mode string) bool {
	return machineOutput[mode]
}

// IsTerminal reports whether f is a terminal a user is sitting at.
//
// This asks the kernel directly (TCGETS) rather than looking at the file mode,
// because the mode cannot tell a terminal apart from /dev/null: both are
// character devices. The difference matters, because stdin being /dev/null means
// "empty input, carry on silently" while stdin being a terminal means "nobody
// piped anything, show usage". Getting that backwards turns every empty pipe
// into a wall of text and every bare `parse` into silence.
//
// A pipe, a file, or /dev/null all fail the ioctl and return false, which is
// exactly the "not interactive" answer callers want.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	var termios [64]byte
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL,
		f.Fd(),
		uintptr(syscall.TCGETS),
		uintptr(unsafe.Pointer(&termios[0])))
	return err == 0
}

// TerminalWidth returns the width of the terminal on stdout in columns, or 0
// when stdout is not a terminal or the size cannot be determined. Callers pick
// their own fallback, since a pager and a pipe have no width to report.
func TerminalWidth() int {
	_, cols := terminalSize()
	return cols
}

// TerminalHeight returns the height of the terminal on stdout in rows, or 0
// when stdout is not a terminal or the size cannot be determined.
func TerminalHeight() int {
	rows, _ := terminalSize()
	return rows
}

func terminalSize() (rows, cols int) {
	var ws struct{ rows, cols, xpix, ypix uint16 }
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL,
		os.Stdout.Fd(),
		uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(&ws)))
	if err != 0 {
		return 0, 0
	}
	return int(ws.rows), int(ws.cols)
}
