// Tests for the docker volume inspect formatter: the layout, what the
// empty fields hide, what the detection claims, and the direct path's
// `volume inspect` verb.
package docker

import (
	"os"
	"strings"
	"testing"
)

// volumeFixture is real `docker volume inspect` JSON for one local volume
// with null Labels and Options — the shape the box actually prints.
func volumeFixture() string {
	return `[
  {
    "CreatedAt": "2026-10-09T19:36:19+05:30",
    "Driver": "local",
    "Labels": null,
    "Mountpoint": "/var/lib/docker/volumes/parse-demo/_data",
    "Name": "parse-demo",
    "Options": null,
    "Scope": "local"
  }
]`
}

// The whole layout in one claim: header with driver and scope,
// mountpoint and created rows, and no row for the null maps.
func TestVolumeInspectRendersTheLayout(t *testing.T) {
	var out strings.Builder
	Format(&out, "inspect", volumeFixture())
	plain := stripTags(out.String())

	for _, want := range []string{
		"volume parse-demo  local · scope local",
		"mountpoint /var/lib/docker/volumes/parse-demo/_data",
		"created    2026-10-09T19:36:19+05:30",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("output is missing %q:\n%s", want, plain)
		}
	}
	for _, hide := range []string{"labels", "options"} {
		if strings.Contains(plain, hide) {
			t.Errorf("null %q must stay hidden:\n%s", hide, plain)
		}
	}
	if strings.Contains(plain, `"Name"`) {
		t.Errorf("the raw JSON leaked:\n%s", plain)
	}
}

// Labels and Options, when present, get one row each as sorted key=value
// pairs.
func TestVolumeInspectRendersLabelsAndOptions(t *testing.T) {
	fix := `[
  {
    "CreatedAt": "2026-10-09T19:36:19+05:30",
    "Driver": "local",
    "Labels": {"team": "core", "env": "prod"},
    "Mountpoint": "/var/lib/docker/volumes/labeled/_data",
    "Name": "labeled",
    "Options": {"type": "local", "o": "bind"},
    "Scope": "local"
  }
]`
	var out strings.Builder
	Format(&out, "inspect", fix)
	plain := stripTags(out.String())

	for _, want := range []string{
		"labels     env=prod · team=core",
		"options    o=bind · type=local",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("output is missing %q:\n%s", want, plain)
		}
	}
}

// Two volumes become two blocks separated by a blank line, and a volume
// beside an unknown object still renders the volume while the unknown
// one is printed raw — never less than docker said.
func TestVolumeInspectMultiAndMixedArrays(t *testing.T) {
	two := `[
  {"CreatedAt": "2026-10-09T19:36:19+05:30", "Driver": "local",
   "Labels": null, "Mountpoint": "/var/lib/docker/volumes/a/_data",
   "Name": "a", "Options": null, "Scope": "local"},
  {"CreatedAt": "2026-10-10T08:00:00+05:30", "Driver": "local",
   "Labels": null, "Mountpoint": "/var/lib/docker/volumes/b/_data",
   "Name": "b", "Options": null, "Scope": "local"}
]`
	var out strings.Builder
	Format(&out, "inspect", two)
	plain := stripTags(out.String())
	if !strings.Contains(plain, "volume a") || !strings.Contains(plain, "volume b") {
		t.Errorf("both volumes must render:\n%s", plain)
	}
	if !strings.Contains(plain, "\n\n") {
		t.Errorf("the two blocks must be separated by a blank line:\n%q", plain)
	}

	mixed := `[
  {"Id": "c1882de90a9ef3b4", "Name": "/fx-full",
   "State": {"Status": "running", "ExitCode": 0},
   "Config": {"Image": "nginx:latest"}},
  {"CreatedAt": "2026-10-09T19:36:19+05:30", "Driver": "local",
   "Labels": null, "Mountpoint": "/var/lib/docker/volumes/parse-demo/_data",
   "Name": "parse-demo", "Options": null, "Scope": "local"},
  {"Foo": "bar"}
]`
	out.Reset()
	Format(&out, "inspect", mixed)
	plain = stripTags(out.String())
	for _, want := range []string{"fx-full", "volume parse-demo", `"Foo": "bar"`} {
		if !strings.Contains(plain, want) {
			t.Errorf("mixed output is missing %q:\n%s", want, plain)
		}
	}
}

// Detection: a volume array is claimed; an empty array, a --format
// object and garbage are not.
func TestVolumeInspectDetection(t *testing.T) {
	if got := Detect(volumeFixture()); got != "inspect" {
		t.Errorf("Detect(volume inspect JSON) = %q, want inspect", got)
	}
	if got := Detect("[]\n"); got != "" {
		t.Errorf("Detect(empty array) = %q, want passthrough", got)
	}
	format := `{"CreatedAt": "2026-10-09T19:36:19+05:30", "Driver": "local", ` +
		`"Labels": null, "Mountpoint": "/m", "Name": "v", "Options": null, "Scope": "local"}`
	if got := Detect(format); got != "" {
		t.Errorf("Detect(--format object) = %q, want passthrough", got)
	}
	if got := Detect("no docker here\n"); got != "" {
		t.Errorf("Detect(garbage) = %q, want passthrough", got)
	}
}

// A volume object whose fields are all empty renders one line, which is
// less than raw docker said — the original bytes come back untouched.
func TestVolumeInspectThinFallsBackToRaw(t *testing.T) {
	fix := "[{\"CreatedAt\": \"\", \"Driver\": \"\", \"Labels\": null, " +
		"\"Mountpoint\": \"\", \"Name\": \"thin\", \"Options\": null, \"Scope\": \"\"}]\n"
	var out strings.Builder
	Format(&out, "inspect", fix)
	if out.String() != fix {
		t.Errorf("thin volume must pass through byte for byte:\n%q", out.String())
	}
}

// The direct path must ask docker for `volume inspect`, not build
// `docker inspect volume <name>` — and it keeps docker's exit code.
func TestRunInspectDirectKeepsTheVolumeVerb(t *testing.T) {
	argsFile := installFakeDocker(t, volumeFixture(), 0)

	out := captureStdout(t, func() {
		code := runInspectDirect([]string{"volume", "inspect", "parse-demo"})
		if code != 0 {
			t.Errorf("exit = %d, want 0", code)
		}
	})
	if !strings.Contains(stripTags(out), "volume parse-demo") {
		t.Errorf("volume layout not rendered:\n%s", out)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(string(args))
	want := []string{"volume", "inspect", "parse-demo"}
	if len(got) != len(want) {
		t.Fatalf("docker args = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("docker args[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// A failing daemon keeps its exit code on the direct path.
func TestRunInspectDirectReturnsVolumeExitCode(t *testing.T) {
	installFakeDocker(t, "", 7)
	captureStdout(t, func() {
		if code := runInspectDirect([]string{"volume", "inspect", "nope"}); code != 7 {
			t.Errorf("exit = %d, want 7", code)
		}
	})
}
