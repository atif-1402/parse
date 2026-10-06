package journalctl

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/atif-1402/parse/internal/tool"
)

func TestFormattableJournalctl(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"plain", []string{"--user", "-n", "20"}, true},
		// A bare `parse journalctl` prints the same shape of line as the
		// flagged form, only more of them, so it must be formatted. It used to
		// be turned away for having no arguments, which left the most common
		// invocation of all completely unformatted.
		{"no arguments at all", nil, true},
		{"short-iso is for humans", []string{"-o", "short-iso"}, true},
		{"json is not", []string{"-o", "json"}, false},
		{"json-pretty is not", []string{"-o", "json-pretty"}, false},
		{"export is not", []string{"-o", "export"}, false},
		{"cat has no prefix to format", []string{"-o", "cat"}, false},
		{"following never ends", []string{"-f"}, false},
		{"long follow flag", []string{"--follow"}, false},
		{"uppercase follow flag", []string{"-F"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formattableJournalctl(tt.args); got != tt.want {
				t.Errorf("formattableJournalctl(%q) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestOutputMode(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"-o", "json"}, "json"},
		{[]string{"-ojson"}, "json"},
		{[]string{"-o=json"}, "json"},
		{[]string{"--output", "json"}, "json"},
		{[]string{"--output=json"}, "json"},
		{[]string{"--user", "-n", "5", "-o", "short-iso"}, "short-iso"},
		{[]string{"status"}, ""},
		{[]string{}, ""},
	}
	for _, tt := range tests {
		if got := tool.OutputMode(tt.args); got != tt.want {
			t.Errorf("tool.OutputMode(%q) = %q, want %q", tt.args, got, tt.want)
		}
	}
}

func TestHasLongFlag(t *testing.T) {
	args := []string{"list-units", "--type=service", "--value"}
	if !tool.HasLongFlag(args, "--value") {
		t.Error("--value not found")
	}
	if tool.HasLongFlag(args, "--plain") {
		t.Error("--plain reported as present")
	}
}

func TestColorJournalLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{
			"the default format",
			"Oct 03 23:54:40 anom foo.service[123]: started",
			"<dim>Oct 03 23:54:40 anom foo.service[123]:</> started",
		},
		{
			"an iso timestamp",
			"2026-10-03T23:54:42+05:30 anom foo.service[464003]: started",
			"<dim>2026-10-03T23:54:42+05:30 anom foo.service[464003]:</> started",
		},
		{
			"no pid",
			"Oct 03 23:54:40 anom foo.service: started",
			"<dim>Oct 03 23:54:40 anom foo.service:</> started",
		},
		{
			"indented, as systemctl status prints them",
			"     Jul 03 12:00:01 host foo[9]: started",
			"     <dim>Jul 03 12:00:01 host foo[9]:</> started",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ColorLine(tt.line)
			if !ok {
				t.Fatalf("ColorLine(%q) did not match", tt.line)
			}
			if got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestColorJournalLineIgnoresLinesWithoutAPrefix(t *testing.T) {
	// journalctl -o cat prints bare messages, and they must pass through.
	for _, line := range []string{
		"Error: something went wrong",
		"just a message with spaces",
		"",
		"2026-10-03T23:54:42+05:30 no-unit-or-pid here",
	} {
		if got, ok := ColorLine(line); ok {
			t.Errorf("ColorLine(%q) matched and produced %q", line, got)
		}
	}
}

func TestColorMessage(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want string
	}{
		{"error at the start", "Error: it broke", "<red>Error:</> it broke"},
		{"warning at the start", "Warning: odd", "<yellow>Warning:</> odd"},
		{"plain word is colored too", "failed to open", "<red>failed</> to open"},
		{"a continuation marker is dimmed", "> Error: bad key", "<dim>> </><red>Error:</> bad key"},
		{"case insensitive", "WARNING: loud", "<yellow>WARNING:</> loud"},
		{
			"a keyword mid-sentence is left alone",
			"Errors from xkbcomp are not fatal",
			"Errors from xkbcomp are not fatal",
		},
		{
			"a keyword as part of a word is left alone",
			"terrorized by the widget",
			"terrorized by the widget",
		},
		{"an ordinary message", "Using 0, ignoring 0", "Using 0, ignoring 0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := colorMessage(tt.msg); got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestFormatJournal(t *testing.T) {
	in := lines(
		"-- Logs begin at Sat 2026-10-03 16:31:36 IST. --",
		"Oct 03 23:54:40 anom foo.service[123]: > Warning:  odd",
		"-- Boot d282b4a19d654a978b5eed5d1ad8d86f --",
		"Oct 03 23:54:41 anom foo.service[123]: started",
	)
	want := lines(
		"<dim>-- Logs begin at Sat 2026-10-03 16:31:36 IST. --</>",
		"<dim>Oct 03 23:54:40 anom foo.service[123]:</> <dim>> </><yellow>Warning:</>  odd",
		"<dim>-- Boot d282b4a19d654a978b5eed5d1ad8d86f --</>",
		"<dim>Oct 03 23:54:41 anom foo.service[123]:</> started",
	)
	var sb strings.Builder
	Format(&sb, in)
	if got := sb.String(); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEachLine(t *testing.T) {
	var got []string
	tool.EachLine("a\nb\nc\n", func(l string) { got = append(got, l) })
	if strings.Join(got, ",") != "a,b,c" {
		t.Errorf("eachLine = %v, want [a b c]", got)
	}
	got = nil
	tool.EachLine("", func(l string) { got = append(got, l) })
	if len(got) != 0 {
		t.Errorf("eachLine on empty text called fn %d times", len(got))
	}
}

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

// lines joins lines with newlines and adds the trailing newline the tools print.
func lines(l ...string) string {
	return strings.Join(l, "\n") + "\n"
}

// The pager asks the same question the formatter asks, so a flag cannot be
// known to one and unknown to the other. --follow-new used to be.
func TestFollowsCoversEveryFollowFlag(t *testing.T) {
	for _, args := range [][]string{
		{"-f"},
		{"-F"},
		{"--follow"},
		{"--follow-new"},
		{"-u", "nginx", "-f"},
		{"-n", "10", "--follow-new"},
	} {
		if !Follows(args) {
			t.Errorf("Follows(%q) = false, want true", args)
		}
		if formattableJournalctl(args) {
			t.Errorf("formattableJournalctl(%q) = true, want false", args)
		}
		if !NeedsTerminal(args) {
			t.Errorf("NeedsTerminal(%q) = false, want true", args)
		}
	}
	for _, args := range [][]string{nil, {"-n", "200"}, {"-u", "nginx"}, {"--since", "1 hour ago"}} {
		if Follows(args) {
			t.Errorf("Follows(%q) = true, want false", args)
		}
	}
}
