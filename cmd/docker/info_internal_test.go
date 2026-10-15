// Tests for the docker info formatter: what it claims, what it shows, what
// it counts as hidden, and what it must never rewrite — docker version, a
// daemon that is down, --format output, garbage.
package docker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// infoFixture is this box's real `docker info`, trimmed to the fields the
// formatter reads, with swarm active, debug on, live restore on, a backing
// filesystem and a WARNING line — the non-default everything at once.
//
// Field lines the parser counts (colon + non-empty value, warnings and the
// Client:/Server: headers excluded): 36. Surfaced: 29. Hidden: 7 —
// the client's Version, the client's false Debug Mode, both plugin Paths,
// Runtimes, OSType and the false Experimental.
func infoFixture() string {
	return `Client:
 Version:    29.7.2
 Context:    default
 Debug Mode: false
 Plugins:
  buildx: Docker Buildx (Docker Inc.)
    Version:  0.37.0
    Path:     /usr/lib/docker/cli-plugins/docker-buildx
  compose: Docker Compose (Docker Inc.)
    Version:  5.5.1
    Path:     /usr/lib/docker/cli-plugins/docker-compose

Server:
 Containers: 20
  Running: 1
  Paused: 0
  Stopped: 19
 Images: 9
 Server Version: 29.7.2
 Storage Driver: overlayfs
  Backing Filesystem: extfs
 Logging Driver: json-file
 Cgroup Driver: systemd
 Cgroup Version: 2
 Swarm: active
 Runtimes: io.containerd.runc.v2 runc
 Default Runtime: runc
 containerd version: 1294c24a7da8e5a793ed378161673abe94118892.m
 runc version: 1.5.1
 Kernel Version: 7.2.5-4-omarchy
 Operating System: Omarchy
 OSType: linux
 Architecture: x86_64
 CPUs: 4
 Total Memory: 7.634GiB
 Name: anom
 Docker Root Dir: /var/lib/docker
 Debug Mode: true
 Experimental: false
 Insecure Registries:
  127.0.0.0/8
  ::1/128
 Live Restore Enabled: true
WARNING: Content and image metadata are not encrypted by default.
`
}

// The whole layout on the fixture: every row people look for, the
// non-default flags, the warning verbatim in red, the hidden count dim.
func TestInfoRendersTheLayout(t *testing.T) {
	var out strings.Builder
	Format(&out, "info", infoFixture())
	got := out.String()
	plain := stripTags(got)

	for _, want := range []string{
		"docker 29.7.2 · context default · anom",
		"containers 20 total · 1 running · 0 paused · 19 stopped · 9 images",
		"storage    overlayfs (extfs) · root /var/lib/docker",
		"cgroup     2 · systemd",
		"runtime    runc 1.5.1 · containerd 1294c24a7da8",
		"system     Omarchy · 7.2.5-4-omarchy · x86_64 · 4 CPUs · 7.634GiB",
		"logging    json-file",
		"plugins    buildx 0.37.0 · compose 5.5.1",
		"swarm      active",
		"live restore on",
		"debug      on",
		"7 more fields hidden, run docker info for everything",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("output is missing %q\n---\n%s", want, plain)
		}
	}

	// The warning must survive verbatim and be red.
	warning := "WARNING: Content and image metadata are not encrypted by default."
	if !strings.Contains(got, paintRed(warning)) {
		t.Errorf("the WARNING line is not red or not verbatim:\n%s", got)
	}
	// The hidden count is dim.
	if !strings.Contains(got, paintDim("7 more fields hidden, run docker info for everything")) {
		t.Errorf("the hidden count is not dim:\n%s", got)
	}
	// Defaults stay hidden: experimental is false, registries are stock.
	if strings.Contains(plain, "experimental") || strings.Contains(plain, "insecure") {
		t.Errorf("a default flag leaked into the output:\n%s", plain)
	}
}

// With everything at its default — swarm inactive, debug off, live restore
// off — none of the conditional rows appear.
func TestInfoHidesTheConditionalRowsAtDefaults(t *testing.T) {
	text := strings.NewReplacer(
		"Swarm: active", "Swarm: inactive",
		"Debug Mode: true", "Debug Mode: false",
		"Live Restore Enabled: true", "Live Restore Enabled: false",
	).Replace(infoFixture())

	var out strings.Builder
	Format(&out, "info", text)
	plain := stripTags(out.String())

	for _, hide := range []string{"swarm", "live restore", "debug", "experimental", "insecure"} {
		if strings.Contains(plain, hide+" ") || strings.Contains(plain, hide+":") {
			t.Errorf("conditional row %q appeared at defaults:\n%s", hide, plain)
		}
	}
	// The summary itself still renders.
	if !strings.Contains(plain, "docker 29.7.2 · context default · anom") {
		t.Errorf("the header disappeared:\n%s", plain)
	}
}

// Detection: docker info is claimed; docker version, a daemon that is
// down, --format output and garbage are not.
func TestInfoDetection(t *testing.T) {
	version := lines(
		"Client:",
		" Version:           29.7.2",
		" API version:       1.55",
		" Context:           default",
		"",
		"Server:",
		" Engine:",
		"  Version:          29.7.2",
		"  OS/Arch:          linux/amd64",
	)
	down := lines(
		"Client:",
		" Version:    29.7.2",
		" Context:    default",
		"",
		"Server:",
		"failed to connect to the docker API at unix:///tmp/nope.sock; check if the path is correct and if the daemon is running: dial unix /tmp/nope.sock: connect: no such file or directory",
	)
	for name, text := range map[string]string{
		"info":      infoFixture(),
		"version":   version,
		"daemon":    down,
		"format":    "29.7.2\n",
		"json":      `{"ServerVersion":"29.7.2"}` + "\n",
		"garbage":   "random output\n",
		"empty":     "",
		"warn only": "WARNING: something\n",
	} {
		want := ""
		if name == "info" {
			want = "info"
		}
		if got := Detect(text); got != want {
			t.Errorf("Detect(%s) = %q, want %q", name, got, want)
		}
	}
}

// Anything that is not a full info output must come back byte for byte
// from the info formatter itself, even when it is handed to it directly.
func TestInfoFormatterPassesNonInfoThrough(t *testing.T) {
	for _, in := range []string{
		"Client:\n Version: 29.7.2\n\nServer:\n Engine:\n",
		"29.7.2\n",
		"Server:\nfailed to connect to the docker API at unix:///tmp/nope.sock\n",
		"not docker at all\n",
		"",
	} {
		var out strings.Builder
		Format(&out, "info", in)
		if got := out.String(); got != in {
			t.Errorf("non-info text was rewritten\n--- got ---\n%q\n--- want ---\n%q", got, in)
		}
	}
}

// The security row is earned: rootless or userns gets it, plain seccomp
// with its builtin profile and cgroupns does not.
func TestInfoSecurityRowOnlyForRootlessOrUserns(t *testing.T) {
	withSecurity := func(opts ...string) string {
		var block strings.Builder
		block.WriteString(" Security Options:\n")
		for _, o := range opts {
			block.WriteString("  " + o + "\n")
		}
		return strings.Replace(infoFixture(), " Kernel Version:",
			block.String()+" Kernel Version:", 1)
	}

	for name, tc := range map[string]struct {
		opts  []string
		wants []string
	}{
		"rootless": {[]string{"seccomp", "cgroupns", "rootless"}, []string{"security   rootless"}},
		"userns":   {[]string{"name=seccomp,profile=builtin", "userns"}, []string{"security   userns"}},
		"plain":    {[]string{"seccomp", "Profile: builtin", "cgroupns"}, nil},
		"none":     {},
	} {
		var out strings.Builder
		Format(&out, "info", withSecurity(tc.opts...))
		plain := stripTags(out.String())

		if !strings.Contains(plain, "docker 29.7.2 · context default · anom") {
			t.Errorf("%s: the summary disappeared:\n%s", name, plain)
		}
		got := strings.Contains(plain, "security ")
		want := len(tc.wants) > 0
		if got != want {
			t.Errorf("%s: security row present=%v, want %v:\n%s", name, got, want, plain)
		}
		for _, w := range tc.wants {
			if !strings.Contains(plain, w) {
				t.Errorf("%s: output is missing %q:\n%s", name, w, plain)
			}
		}
	}
}

// infoJSON is a fixed `docker info --format '{{json .}}'` answer with four
// keys the summary never surfaces — ID, the false Debug, the inactive
// Swarm, the disabled Live Restore — so the hidden count is 4, plus a
// rootless security option that does earn its row.
const infoJSON = `{"ServerVersion":"29.7.2","Name":"anom","Containers":5,` +
	`"ContainersRunning":1,"ContainersPaused":0,"ContainersStopped":4,"Images":2,` +
	`"Driver":"overlay2","DockerRootDir":"/var/lib/docker","CgroupDriver":"systemd",` +
	`"CgroupVersion":"2","DefaultRuntime":"runc","KernelVersion":"6.0.0",` +
	`"OperatingSystem":"TestOS","Architecture":"x86_64","NCPU":8,"MemTotal":8589934592,` +
	`"LoggingDriver":"json-file",` +
	`"ClientInfo":{"Context":"default","Debug":false,"Plugins":[` +
	`{"Name":"compose","Version":"5.5.1"},{"Name":"buildx","Version":"0.37.0"}]},` +
	`"ID":"fce41074","Debug":false,"Swarm":{"LocalNodeState":"inactive"},` +
	`"LiveRestoreEnabled":false,` +
	`"SecurityOptions":["name=seccomp,profile=builtin","rootless"]}`

// Direct mode asks for the JSON form and renders it: same rows, plugin
// versions sorted, the hidden count taken from the real key count.
func TestRunInfoDirectUsesJSONFormat(t *testing.T) {
	argsFile := installFakeDocker(t, infoJSON+"\n", 0)

	out := captureStdout(t, func() {
		code := runInfoDirect([]string{"info"})
		if code != 0 {
			t.Errorf("exit = %d, want 0", code)
		}
	})

	for _, want := range []string{
		"docker 29.7.2 · context default · anom",
		"containers 5 total · 1 running · 0 paused · 4 stopped · 2 images",
		"storage    overlay2 · root /var/lib/docker",
		"system     TestOS · 6.0.0 · x86_64 · 8 CPUs · 8GiB",
		"plugins    buildx 0.37.0 · compose 5.5.1",
		"security   rootless",
		"4 more fields hidden, run docker info for everything",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("direct output is missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, "swarm") || strings.Contains(out, "debug ") {
		t.Errorf("defaults leaked in direct mode:\n%s", out)
	}

	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(args); !strings.Contains(got, "--format") || !strings.Contains(got, "{{json .}}") {
		t.Errorf("direct mode did not ask for the json form, args:\n%s", got)
	}
}

// When this docker refuses the json form, direct mode falls back to
// parsing docker's own text.
func TestRunInfoDirectFallsBackToText(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" >> \"$FAKE_ARGS\"\n" +
		"case \"$*\" in *--format*) exit 1;; esac\n" +
		"cat \"$FAKE_OUT\"\n" +
		"exit 0"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "out"), []byte(infoFixture()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_ARGS", filepath.Join(dir, "args"))
	t.Setenv("FAKE_OUT", filepath.Join(dir, "out"))

	out := captureStdout(t, func() {
		if code := runInfoDirect([]string{"info"}); code != 0 {
			t.Errorf("exit = %d, want 0", code)
		}
	})
	if !strings.Contains(out, "docker 29.7.2 · context default · anom") {
		t.Errorf("text fallback did not render:\n%s", out)
	}
	args, err := os.ReadFile(filepath.Join(dir, "args"))
	if err != nil {
		t.Fatal(err)
	}
	if calls := strings.Count(string(args), "info\n"); calls < 2 {
		t.Errorf("expected the json attempt then the plain call, args:\n%s", args)
	}
}

// Direct mode returns docker's real exit code, not always 0.
func TestRunInfoDirectReturnsDockerExitCode(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte("#!/bin/sh\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	captureStdout(t, func() {
		if code := runInfoDirect([]string{"info"}); code != 7 {
			t.Errorf("exit = %d, want docker's 7", code)
		}
	})
}
