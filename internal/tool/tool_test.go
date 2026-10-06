package tool

import (
	"os/exec"
	"testing"
)

func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		// A child that succeeded has no error. Reporting anything but 0 here
		// made a successful command look like a failure to the shell.
		{"success", nil, 0},
		{"real failure", exec.Command("sh", "-c", "exit 3").Run(), 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExitCode(tt.err); got != tt.want {
				t.Errorf("ExitCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

// The two streams must stay apart. Merging them put a tool's diagnostics into
// the text parse formats, so `parse git log` outside a repository wrote git's
// "fatal: not a git repository" to stdout, where a program reading parse's
// output could not tell it from data.
func TestCaptureKeepsStderrOffStdout(t *testing.T) {
	text, code, started := Capture("sh", []string{"-c", "echo out; echo err >&2"})
	if !started {
		t.Fatal("started = false for a command that exists")
	}
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if text != "out\n" {
		t.Errorf("stdout = %q, want %q (stderr must not be in it)", text, "out\n")
	}
}

// The exit code is the tool's own even though the failure text no longer comes
// back with it, which is the whole point of splitting the streams.
func TestCaptureKeepsTheExitCode(t *testing.T) {
	text, code, started := Capture("sh", []string{"-c", "echo boom >&2; exit 3"})
	if !started {
		t.Fatal("started = false for a command that exists")
	}
	if code != 3 {
		t.Errorf("code = %d, want 3", code)
	}
	if text != "" {
		t.Errorf("stdout = %q, want empty when the command only wrote to stderr", text)
	}
}
