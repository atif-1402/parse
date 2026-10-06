package systemctl

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/atif-1402/parse/internal/tool"
)

// The Paint and PrintTable hooks are replaced with tag-emitting versions by
// git_internal_test.go's init, so every expectation here spells colors out.

func TestSystemctlSubcommand(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"status", "foo.service"}, "status"},
		{[]string{"--user", "status"}, "status"},
		{[]string{"--user", "is-active", "foo.service"}, "is-active"},
		{[]string{"--type=service", "list-units"}, "list-units"},
		{[]string{"list-units", "--type", "service"}, "list-units"},
		{[]string{"-o", "json", "list-units"}, "list-units"},
		{[]string{"--state", "failed"}, ""},
		{[]string{}, ""},
	}
	for _, tt := range tests {
		if got := systemctlSubcommand(tt.args); got != tt.want {
			t.Errorf("systemctlSubcommand(%q) = %q, want %q", tt.args, got, tt.want)
		}
	}
}

func TestFormattableSystemctl(t *testing.T) {
	tests := []struct {
		args []string
		want bool
	}{
		{[]string{"--user", "status", "foo"}, true},
		{[]string{"list-units"}, true},
		{[]string{"list-units", "--output=json"}, false},
		{[]string{"list-units", "-o", "json"}, false},
		{[]string{"list-units", "--value"}, false},
		{[]string{"list-unit-files", "-ojson"}, false},
	}
	for _, tt := range tests {
		if got := formattableSystemctl(tt.args); got != tt.want {
			t.Errorf("formattableSystemctl(%q) = %v, want %v", tt.args, got, tt.want)
		}
	}
}

func TestUnitStateColor(t *testing.T) {
	tests := map[string]string{
		"active": green, "enabled": green, "linked": green,
		"activating": yellow, "reloading": yellow, "deactivating": yellow,
		"failed": red, "masked": red, "error": red, "bad": red,
		"inactive": dim, "disabled": dim, "static": dim, "transient": dim,
		"loaded": dim, "unknown": dim, "": dim,
	}
	for state, want := range tests {
		if got := unitStateColor(state); got != want {
			t.Errorf("unitStateColor(%q) = %q, want %q", state, got, want)
		}
	}
}

func TestUnescapeUnitName(t *testing.T) {
	// systemd escapes the hyphen in unit names derived from paths.
	got := unescapeUnitName(`sys-devices-pci0000:00-usb1-1\x2d1-1.device`)
	want := "sys-devices-pci0000:00-usb1-1-1-1.device"
	if got != want {
		t.Errorf("unescapeUnitName = %q, want %q", got, want)
	}
	// Only \x2d is decoded: any other escape is left alone.
	if got := unescapeUnitName(`weird\x20name`); got != `weird\x20name` {
		t.Errorf("unescapeUnitName decoded an escape it should not: %q", got)
	}
}

func TestColorStatusLineHeader(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		state string
		want  string
	}{
		{
			"failed unit gets a red bullet",
			"● foo.service - Foo daemon", "failed",
			"<red>●</> <bold>foo.service</> - Foo daemon",
		},
		{
			"active unit gets a green bullet",
			"● foo.service - Foo daemon", "active",
			"<green>●</> <bold>foo.service</> - Foo daemon",
		},
		{
			"header with no description",
			"● foo.service", "active",
			"<green>●</> <bold>foo.service</>",
		},
		{
			"--plain drops the bullet",
			"foo.service - Foo daemon", "failed",
			"<bold>foo.service</> - Foo daemon",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := colorStatusHeader(tt.line, tt.state)
			if !ok {
				t.Fatalf("colorStatusLine(%q) did not match", tt.line)
			}
			if got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestColorStatusLineLabels(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{
			"Active state is colored, the rest of the line is not",
			"     Active: active (running) since Sat 2026-10-03",
			"     <dim>Active:</> <green>active</> (running) since Sat 2026-10-03",
		},
		{
			"failed state is red",
			"     Active: failed (Result: exit-code)",
			"     <dim>Active:</> <red>failed</> (Result: exit-code)",
		},
		{
			"Loaded state is colored",
			"     Loaded: loaded (/usr/lib/systemd/user/foo.service; enabled)",
			"     <dim>Loaded:</> <green>loaded</> (/usr/lib/systemd/user/foo.service; enabled)",
		},
		{
			"a masked unit is red",
			"     Loaded: masked (/etc/systemd/user/foo.service; bad)",
			"     <dim>Loaded:</> <red>masked</> (/etc/systemd/user/foo.service; bad)",
		},
		{
			"reference detail is dimmed",
			" Invocation: 0e15c0cf12784d2ca50f77bed1b7f603",
			" <dim>Invocation:</> <dim>0e15c0cf12784d2ca50f77bed1b7f603</>",
		},
		{
			"the pid stays readable",
			"   Main PID: 1066 (foo)",
			"   <dim>Main PID:</> 1066 (foo)",
		},
		{
			"a cgroup child dims its gutter and pid",
			"             ├─1066 /usr/lib/foo",
			"<dim>             ├─1066 </>/usr/lib/foo",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := colorStatusLine(tt.line)
			if !ok {
				t.Fatalf("colorStatusLine(%q) did not match", tt.line)
			}
			if got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestColorStatusLineLeavesOtherLinesAlone(t *testing.T) {
	for _, line := range []string{"", "  ", "26 unit files listed."} {
		if got, ok := colorStatusLine(line); ok {
			t.Errorf("colorStatusLine(%q) matched and produced %q", line, got)
		}
	}
}

func TestActiveState(t *testing.T) {
	block := strings.Split(strings.TrimRight(lines(
		"● foo.service - Foo",
		"     Active: failed (Result: exit-code)",
	), "\n"), "\n")
	if got := activeState(block, 0); got != "failed" {
		t.Errorf("activeState = %q, want %q", got, "failed")
	}

	two := strings.Split(strings.TrimRight(lines(
		"● a.service - A",
		"     Active: active (running)",
		"● b.service - B",
		"     Active: inactive (dead)",
	), "\n"), "\n")
	if got := activeState(two, 0); got != "active" {
		t.Errorf("first block: got %q, want active", got)
	}
	if got := activeState(two, 2); got != "inactive" {
		t.Errorf("second block: got %q, want inactive", got)
	}
}

func TestFormatUnitStatus(t *testing.T) {
	in := lines(
		"● foo.service - Foo daemon",
		"     Loaded: loaded (/usr/lib/systemd/user/foo.service; static)",
		"     Active: active (running) since Sat 2026-10-03 16:31:43 IST",
		" Invocation: 0e15c0cf12784d2ca50f77bed1b7f603",
		"             ├─1066 /usr/lib/foo",
		"     Jul 03 12:00:01 host foo[9]: started",
		"3 units listed.",
	)
	want := lines(
		"<green>●</> <bold>foo.service</> - Foo daemon",
		"     <dim>Loaded:</> <green>loaded</> (/usr/lib/systemd/user/foo.service; static)",
		"     <dim>Active:</> <green>active</> (running) since Sat 2026-10-03 16:31:43 IST",
		" <dim>Invocation:</> <dim>0e15c0cf12784d2ca50f77bed1b7f603</>",
		"<dim>             ├─1066 </>/usr/lib/foo",
		"     <dim>Jul 03 12:00:01 host foo[9]:</> started",
		"<dim>3 units listed.</>",
	)
	var sb strings.Builder
	formatUnitStatus(&sb, in)
	if got := sb.String(); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatUnitStatusUnknownUnitIsRed(t *testing.T) {
	var sb strings.Builder
	formatUnitStatus(&sb, lines("Unit foo.service could not be found."))
	want := "<red>Unit foo.service could not be found.</>\n"
	if got := sb.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatListUnits(t *testing.T) {
	in := lines(
		"  UNIT                    LOAD   ACTIVE SUB       DESCRIPTION",
		"  foo.service             loaded active running     Foo daemon",
		"  sys-devices-pci0-1\\x2d2.device loaded active plugged   A device",
		"  2 loaded units listed.",
	)
	// The footer keeps its original indentation: parse adds color, it does not
	// rewrite text.
	want := lines(
		"UNIT | LOAD | ACTIVE | SUB | DESCRIPTION",
		"<bold>foo.service</> | <dim>loaded</> | <green>active</> | <dim>running</> | Foo daemon",
		"<bold>sys-devices-pci0-1-2.device</> | <dim>loaded</> | <green>active</> | <dim>plugged</> | A device",
		"<dim>  2 loaded units listed.</>",
	)
	var sb strings.Builder
	formatListUnits(&sb, in)
	if got := sb.String(); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatUnitFiles(t *testing.T) {
	in := lines(
		"  UNIT FILE                STATE     PRESET",
		"  foo.service              enabled   enabled",
		"  bar.service              disabled  disabled",
		"  app-thing-123.scope      transient -",
	)
	want := lines(
		"UNIT FILE | STATE | PRESET",
		"<bold>foo.service</> | <green>enabled</> | <dim>enabled</>",
		"<bold>bar.service</> | <dim>disabled</> | <dim>disabled</>",
		"<bold>app-thing-123.scope</> | <dim>transient</> | <dim>-</>",
	)
	var sb strings.Builder
	formatUnitFiles(&sb, in)
	if got := sb.String(); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatUnitState(t *testing.T) {
	var sb strings.Builder
	formatUnitState(&sb, lines("active", "inactive", "failed"))
	want := lines("<green>active</>", "<dim>inactive</>", "<red>failed</>")
	if got := sb.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
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

// watch redraws unit state as it changes and monitor tails journal messages;
// neither ends on its own, so the pager has to stay out of the way.
func TestNeedsTerminalForTheEndlessViews(t *testing.T) {
	for _, args := range [][]string{
		{"watch"},
		{"watch", "nginx.service"},
		{"monitor"},
		{"--no-pager", "watch"},
		{"-o", "json", "watch"},
	} {
		if !NeedsTerminal(args) {
			t.Errorf("NeedsTerminal(%q) = false, want true", args)
		}
	}
	for _, args := range [][]string{
		nil,
		{"status", "nginx"},
		{"list-units"},
		{"is-active", "nginx"},
		{"-o", "json", "list-units"},
	} {
		if NeedsTerminal(args) {
			t.Errorf("NeedsTerminal(%q) = true, want false", args)
		}
	}
}
