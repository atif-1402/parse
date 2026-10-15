// Tests for the docker image inspect formatter: what it must claim, what it
// must hide, what it must mask, and what it must never steal from the
// container or network formatters.
package docker

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// imageFixture is docker image inspect JSON for one image, with a base
// image's vars (PATH, GPG_KEY), one user-added var, one secret by name, one
// secret inside a URL, a healthcheck and two labels. No Parent: the default
// table is the hiding baseline. Created three weeks and a day ago so the
// humanized age stays "3w" across runs.
func imageFixture() string {
	created := time.Now().Add(-22 * 24 * time.Hour).Format(time.RFC3339Nano)
	return `[
  {
    "Id": "sha256:aaaabbbbccccddddeeeeffff00001111222233334444555566667777888899990000",
    "RepoTags": ["fx-img:1", "fx-img:latest"],
    "Created": "` + created + `",
    "Size": 7340032,
    "Architecture": "amd64",
    "Os": "linux",
    "RootFS": {"Type": "layers", "Layers": ["sha256:l1", "sha256:l2", "sha256:l3"]},
    "Config": {
      "Entrypoint": ["/docker-entrypoint.sh"],
      "Cmd": ["nginx", "-g", "daemon off;"],
      "User": "",
      "WorkingDir": "/usr/share/nginx/html",
      "ExposedPorts": {"80/tcp": {}, "443/tcp": {}},
      "Volumes": {"/var/log/nginx": {}},
      "Env": [
        "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
        "GPG_KEY=A4C4641B91F0ACF48B5048C63A4FDD3E4E5EFF60",
        "APP_MODE=prod",
        "DB_PASSWORD=hunter2",
        "DATABASE_URL=postgres://app:letmein@db:5432/app",
        "MY_API_KEY=sk-live-123"
      ],
      "Healthcheck": {
        "Test": ["CMD-SHELL", "curl -f http://localhost/ || exit 1"],
        "Interval": 30000000000
      },
      "Labels": {"org.opencontainers.image.source": "https://example.com", "maintainer": "ops"}
    }
  }
]`
}

// The whole layout in one claim: header, size, layers, command lines, the
// root warning, the env reduced to what the user baked in, secrets masked,
// the healthcheck and the labels.
func TestImageInspectRendersTheWholeLayout(t *testing.T) {
	var out strings.Builder
	Format(&out, "inspect", imageFixture())
	got := out.String()
	plain := stripTags(got)

	for _, want := range []string{
		"fx-img:1, fx-img:latest",
		"aaaabbbbcccc",
		"7.34MB · created 3w ago · linux/amd64",
		"layers    3",
		"entrypoint /docker-entrypoint.sh",
		"cmd       nginx -g daemon off;",
		"user      root",
		"workdir   /usr/share/nginx/html",
		"ports     443/tcp, 80/tcp",
		"volumes   /var/log/nginx",
		"env       4 vars",
		"APP_MODE=prod",
		"DB_PASSWORD=********",
		"DATABASE_URL=postgres://app:********@db:5432/app",
		"MY_API_KEY=********",
		"(secrets baked into the image, use --show-secrets)",
		"health    curl -f http://localhost/ || exit 1 (every 30s)",
		"labels    2 labels",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("output is missing %q\n---\n%s", want, plain)
		}
	}

	// Base image's own vars must not appear at all.
	for _, hide := range []string{"PATH=", "GPG_KEY="} {
		if strings.Contains(plain, hide) {
			t.Errorf("base image env %q leaked into the output\n---\n%s", hide, plain)
		}
	}
	// Root is docker's default on every public image: dim, not red. Red
	// is for the real problem — the secrets warning below.
	if !strings.Contains(got, paintDim("root")) {
		t.Errorf("the root user is not dimmed:\n%s", got)
	}
	if strings.Contains(got, paintRed("root")) {
		t.Errorf("the root user must not be red:\n%s", got)
	}
	// The secrets warning is the real problem: red.
	if !strings.Contains(got, paintRed("(secrets baked into the image, use --show-secrets)")) {
		t.Errorf("the secrets warning is not red:\n%s", got)
	}
	// Masks must be dimmed.
	if !strings.Contains(got, paintDim("hunter2")) && !strings.Contains(got, paintDim("********")) {
		t.Errorf("masked secrets are not dimmed:\n%s", got)
	}
}

// --show-secrets answers with the real values and drops the warning.
func TestImageInspectShowSecretsRevealsValues(t *testing.T) {
	SetShowSecrets(true)
	defer SetShowSecrets(false)

	var out strings.Builder
	Format(&out, "inspect", imageFixture())
	plain := stripTags(out.String())

	for _, want := range []string{
		"DB_PASSWORD=hunter2",
		"DATABASE_URL=postgres://app:letmein@db:5432/app",
		"MY_API_KEY=sk-live-123",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("show-secrets is missing %q\n---\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "use --show-secrets") {
		t.Errorf("the warning must go away under --show-secrets:\n%s", plain)
	}
}

// With a recorded parent, the parent's exact pairs are the hiding baseline:
// vars the base image shipped are gone, the user's own stay.
func TestImageInspectComparesAgainstTheParentImage(t *testing.T) {
	old := imageEnvOf
	imageEnvOf = func(string) (map[string]bool, bool) {
		return map[string]bool{
			"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin": true,
			"GPG_KEY=A4C4641B91F0ACF48B5048C63A4FDD3E4E5EFF60":                  true,
			"APP_MODE=prod": true, // same key=value as base: hidden too
		}, true
	}
	defer func() { imageEnvOf = old }()

	text := strings.Replace(imageFixture(),
		`"Id": "sha256:aaa`,
		`"Parent": "sha256:base", "Id": "sha256:aaa`, 1)

	var out strings.Builder
	Format(&out, "inspect", text)
	plain := stripTags(out.String())

	for _, hide := range []string{"PATH=", "GPG_KEY=", "APP_MODE="} {
		if strings.Contains(plain, hide) {
			t.Errorf("parent's env %q should have been hidden:\n%s", hide, plain)
		}
	}
	if !strings.Contains(plain, "DB_PASSWORD=********") {
		t.Errorf("the user's own secret survived the parent comparison but was not found:\n%s", plain)
	}
}

// Two images in one JSON: one block each, blank line between, the second
// image's own tags and short id in its own header.
func TestImageInspectMultiImageBlocks(t *testing.T) {
	multi := `[
  {"Id": "sha256:1111111111111111111111111111111111111111111111111111111111111111",
   "RepoTags": ["alpine:3.19"], "Size": 3331248, "Architecture": "amd64", "Os": "linux",
   "RootFS": {"Type": "layers", "Layers": ["a"]},
   "Config": {"Cmd": ["sh"], "User": "1000:1000", "Env": ["PATH=/bin"]}},
  {"Id": "sha256:2222222222222222222222222222222222222222222222222222222222222222",
   "RepoTags": ["nginx:latest"], "Size": 67108864, "Architecture": "arm64", "Os": "linux",
   "RootFS": {"Type": "layers", "Layers": ["a", "b"]},
   "Config": {"Cmd": ["nginx"], "User": ""}}
]`
	var out strings.Builder
	Format(&out, "inspect", multi)
	plain := stripTags(out.String())

	if !strings.Contains(plain, "alpine:3.19") || !strings.Contains(plain, "nginx:latest") {
		t.Errorf("both images must have headers:\n%s", plain)
	}
	if !strings.Contains(plain, "\n\n") {
		t.Errorf("the two images must be separated by a blank line:\n%q", plain)
	}
	// A numbered user is not root.
	if !strings.Contains(plain, "user      1000:1000") {
		t.Errorf("the numbered user must show:\n%s", plain)
	}
}

// Garbage, empty arrays and arrays of plain numbers are never claimed.
func TestImageInspectPassesGarbageThrough(t *testing.T) {
	for _, in := range []string{
		"not json\n",
		"[1, 2, 3]\n",
		"[]\n",
		`{"Id": "abc"}` + "\n", // bare object, not an array
	} {
		var out strings.Builder
		Format(&out, "inspect", in)
		if got := out.String(); got != in {
			t.Errorf("garbage was rewritten\n--- got ---\n%q\n--- want ---\n%q", got, in)
		}
	}
}

// An image array whose elements are missing almost everything must fall back
// to the raw JSON rather than render one lonely header line.
func TestImageInspectThinRenderFallsBackToRaw(t *testing.T) {
	raw := "[{\"Id\": \"sha256:x\", \"RootFS\": {}}]\n"
	var out strings.Builder
	Format(&out, "inspect", raw)
	if got := out.String(); got != raw {
		t.Errorf("a thin render must fall back to the raw JSON\n--- got ---\n%q\n--- want ---\n%q", got, raw)
	}
}

// A container inspect and a network inspect must never reach the image
// renderer: containers carry State, networks carry Driver+IPAM and no
// RootFS, and each already has its own formatter.
func TestImageInspectDoesNotClaimContainerOrNetworkJSON(t *testing.T) {
	var raw []map[string]interface{}

	container := `[{"Id":"abc","State":{"Status":"running"},"Config":{"Image":"nginx"}}]`
	if err := json.Unmarshal([]byte(container), &raw); err != nil {
		t.Fatal(err)
	}
	if imageObject(raw[0]) {
		t.Error("container JSON was claimed as an image")
	}

	var netRaw []map[string]interface{}
	network := `[{"Name":"bridge","Driver":"bridge","IPAM":{"Driver":"default"}}]`
	if err := json.Unmarshal([]byte(network), &netRaw); err != nil {
		t.Fatal(err)
	}
	if imageObject(netRaw[0]) {
		t.Error("network JSON was claimed as an image")
	}
}

// One array, one container and one image — `docker inspect fx-full
// alpine:3.19` — decided per object: the container keeps its own sections,
// the image keeps size, layers and user. Blank line between the two blocks.
func TestInspectMixedContainerAndImageInOneArray(t *testing.T) {
	mixed := `[
  {"Id": "c1882de90a9ef3b4", "Name": "/fx-full",
   "State": {"Status": "running", "ExitCode": 0},
   "Config": {"Image": "nginx:latest", "Env": ["PATH=/usr/bin"]}},
  {"Id": "sha256:6baf43584bcbb1b6a5e3f9d1a0c8e9f10000000000000000000000000000000",
   "RepoTags": ["alpine:3.19"], "Size": 3331248,
   "Architecture": "amd64", "Os": "linux",
   "RootFS": {"Type": "layers", "Layers": ["sha256:a"]},
   "Config": {"Cmd": ["/bin/sh"], "User": "1000:1000", "WorkingDir": "/"}}
]`
	var out strings.Builder
	Format(&out, "inspect", mixed)
	plain := stripTags(out.String())

	for _, want := range []string{
		// the container half
		"fx-full",
		"(nginx:latest)",
		"state",
		// the image half — exactly what the container path used to drop
		"alpine:3.19",
		"layers    1",
		"3.331MB",
		"user      1000:1000",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("mixed output is missing %q\n---\n%s", want, plain)
		}
	}
	// workdir / is a default and stays hidden.
	if strings.Contains(plain, "workdir") {
		t.Errorf("workdir / must be hidden:\n%s", plain)
	}
	if !strings.Contains(plain, "\n\n") {
		t.Errorf("the two blocks must be separated by a blank line:\n%q", plain)
	}
}

// An unknown object inside a docker inspect array is printed raw rather
// than dropped or half-rendered — never less than raw docker would.
func TestInspectUnknownObjectInArrayIsPrintedRaw(t *testing.T) {
	mixed := `[
  {"Id": "c1882de90a9ef3b4", "Name": "/fx-full",
   "State": {"Status": "running"},
   "Config": {"Image": "nginx"}},
  {"Foo": "bar"}
]`
	var out strings.Builder
	Format(&out, "inspect", mixed)
	plain := stripTags(out.String())

	if !strings.Contains(plain, "fx-full") {
		t.Errorf("the container half was not rendered:\n%s", plain)
	}
	if !strings.Contains(plain, "{\n    \"Foo\": \"bar\"\n}") {
		t.Errorf("the unknown object was not printed raw:\n%s", plain)
	}
}

// Detection: an image array reaches the inspect detector; ss/kubectl JSON
// still does not.
func TestImageInspectDetection(t *testing.T) {
	img := `[
  {"Id": "sha256:abc", "RootFS": {"Layers": ["a"]}, "Architecture": "amd64", "Os": "linux"}
]`
	if got := Detect(img); got != "inspect" {
		t.Errorf("Detect(image inspect JSON) = %q, want inspect", got)
	}
	foreign := `[{"Id": "abc", "ClusterIP": "10.0.0.1"}]`
	if got := Detect(foreign); got != "" {
		t.Errorf("Detect(foreign JSON) = %q, want passthrough", got)
	}
}

// The direct path must ask docker for `image inspect`, not `inspect image`.
func TestRunInspectDirectKeepsTheImageVerb(t *testing.T) {
	argsFile := installFakeDocker(t, imageFixture(), 0)

	out := captureStdout(t, func() {
		code := runInspectDirect([]string{"image", "inspect", "fx-img:1"})
		if code != 0 {
			t.Errorf("exit = %d, want 0", code)
		}
	})
	if !strings.Contains(out, "fx-img:1") {
		t.Errorf("image layout not rendered:\n%s", out)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	// docker inspect is already JSON; the direct path must not add a format.
	want := "image\ninspect\nfx-img:1\n"
	if string(args) != want {
		t.Errorf("docker was called with %q, want %q", args, want)
	}
}
