// Package test's systemd coverage: parse's systemctl and journalctl
// formatting, end to end through the exported entry points, using the same
// text the real tools print.
package test

import (
	"strings"
	"testing"

	"github.com/atif-1402/parse/cmd"
)

// pipe runs text through parse's piped-input path, so these tests cover
// detection and formatting together.
func pipe(input string) string {
	var sb strings.Builder
	cmd.Pipe(&sb, input)
	return strings.TrimRight(sb.String(), "\n")
}

func TestPipeFormatsSystemctlStatus(t *testing.T) {
	in := lines(
		"● sshd.service - OpenSSH daemon",
		"     Loaded: loaded (/usr/lib/systemd/system/sshd.service; enabled)",
		"     Active: active (running) since Tue 2026-10-01 09:12:44 IST; 3 days ago",
		" Invocation: 0e15c0cf12784d2ca50f77bed1b7f603",
		"   Main PID: 812 (sshd)",
		"      Tasks: 1 (limit: 3845)",
		"     Memory: 3.1M",
	)
	check(t, pipe(in), want(
		"<green>●</> <bold>sshd.service</> - OpenSSH daemon",
		"     <dim>Loaded:</> <green>loaded</> (/usr/lib/systemd/system/sshd.service; enabled)",
		"     <dim>Active:</> <green>active</> (running) since Tue 2026-10-01 09:12:44 IST; 3 days ago",
		" <dim>Invocation:</> <dim>0e15c0cf12784d2ca50f77bed1b7f603</>",
		"   <dim>Main PID:</> 812 (sshd)",
		"      <dim>Tasks:</> <dim>1 (limit: 3845)</>",
		"     <dim>Memory:</> <dim>3.1M</>",
	))
}

func TestPipeColorsAFailedUnitRed(t *testing.T) {
	in := lines(
		"● nginx.service - A high performance web server",
		"     Loaded: loaded (/usr/lib/systemd/system/nginx.service; enabled)",
		"     Active: failed (Result: exit-code) since Tue 2026-10-01 09:12:44 IST",
	)
	got := pipe(in)
	if !strings.Contains(got, "<red>●</>") {
		t.Errorf("failed unit bullet is not red:\n%s", got)
	}
	if !strings.Contains(got, "<red>failed</>") {
		t.Errorf("failed state is not red:\n%s", got)
	}
}

func TestPipeRedactsAnUnknownUnit(t *testing.T) {
	in := lines("Unit nope.service could not be found.")
	got := pipe(in)
	if !strings.Contains(got, "<red>") {
		t.Errorf("an unknown unit should be red, got:\n%s", got)
	}
}

func TestPipeFormatsListUnitsAndDecodesHexEscapes(t *testing.T) {
	in := lines(
		"  UNIT                    LOAD   ACTIVE SUB       DESCRIPTION",
		"  sys-devices-pci0-1\\x2d2.device loaded active plugged   A device",
		"  nginx.service           loaded failed failed      A web server",
		"  2 loaded units listed.",
	)
	got := pipe(in)
	// systemd escapes the hyphen in unit names derived from paths.
	if !strings.Contains(got, "<bold>sys-devices-pci0-1-2.device</>") {
		t.Errorf("\\x2d was not decoded:\n%s", got)
	}
	if strings.Contains(got, `\x2d`) {
		t.Errorf("an escape survived:\n%s", got)
	}
	if !strings.Contains(got, "<red>failed</>") {
		t.Errorf("failed state is not red:\n%s", got)
	}
	if !strings.Contains(got, "<dim>  2 loaded units listed.</>") {
		t.Errorf("the footer was not kept:\n%s", got)
	}
}

func TestPipeFormatsListUnitFiles(t *testing.T) {
	in := lines(
		"  UNIT FILE         STATE    PRESET",
		"  nginx.service     enabled  enabled",
		"  sshd.service      disabled disabled",
	)
	check(t, pipe(in), want(
		"UNIT FILE | STATE | PRESET",
		"<bold>nginx.service</> | <green>enabled</> | <dim>enabled</>",
		"<bold>sshd.service</> | <dim>disabled</> | <dim>disabled</>",
	))
}

func TestPipeFormatsIsActive(t *testing.T) {
	if got := pipe("active\n"); got != "<green>active</>" {
		t.Errorf("got %q", got)
	}
	if got := pipe("failed\n"); got != "<red>failed</>" {
		t.Errorf("got %q", got)
	}
	if got := pipe("inactive\n"); got != "<dim>inactive</>" {
		t.Errorf("got %q", got)
	}
}

func TestPipeFormatsJournalOutput(t *testing.T) {
	in := lines(
		"-- Logs begin at Sat 2026-10-03 16:31:36 IST. --",
		"Oct 03 23:54:40 anom foo.service[123]: started",
		"Oct 03 23:54:41 anom foo.service[123]: Warning: odd key",
	)
	check(t, pipe(in), want(
		"<dim>-- Logs begin at Sat 2026-10-03 16:31:36 IST. --</>",
		"<dim>Oct 03 23:54:40 anom foo.service[123]:</> started",
		"<dim>Oct 03 23:54:41 anom foo.service[123]:</> <yellow>Warning:</> odd key",
	))
}

func TestPipeLeavesUnrecognizedTextAlone(t *testing.T) {
	// Nothing parse knows about must come out byte for byte unchanged.
	for _, in := range []string{
		"hello world\n",
		"total 48\ndrwxr-xr-x 2 user group 4096 Oct  3 16:31 .\n",
		"192.168.1.1  00:1a:2b:3c:4d:5e\n",
	} {
		if got := pipe(in); got != strings.TrimRight(in, "\n") {
			t.Errorf("unrecognized text was changed:\n got  %q\n want %q", got, in)
		}
	}
}

func TestPipeStillHandlesGit(t *testing.T) {
	// The git detectors must keep working now that other tools are in the mix.
	in := lines("## main...origin/main", "M  cmd/git.go")
	got := pipe(in)
	if !strings.Contains(got, "<") {
		t.Errorf("git status output was not formatted:\n%s", got)
	}
}
