// Tests for docker: which tables are claimed, that only the heading and the
// STATUS cell are touched, and that everything else docker can print is left
// byte for byte.
package docker

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/atif-1402/parse/internal/tool"
)

// The tint is written as markers rather than escapes so a failure can be read.
func init() {
	tool.Paint = func(color, s string) string {
		if color == "" || s == "" {
			return s
		}
		return "<" + color + ">" + s + "</>"
	}
}

func lines(l ...string) string { return strings.Join(l, "\n") + "\n" }

// stripTags undoes the Paint override above.
func stripTags(s string) string {
	for _, c := range []string{bold, dim, red, green, yellow, cyan} {
		s = strings.ReplaceAll(s, "<"+c+">", "")
	}
	return strings.ReplaceAll(s, "</>", "")
}

// The two tables a real `docker ps` and `docker ps -a` printed here, verbatim,
// ellipsis and all. Column width follows the widest cell, so the two tables
// differ in where STATUS starts.
var psTable = lines(
	`CONTAINER ID   IMAGE     COMMAND                  CREATED              STATUS              PORTS     NAMES`,
	`804cf2759ae1   alpine    "sleep 1000"             38 seconds ago       Up 37 seconds                 demo-sleep`,
	`80dff6f6d71a   nginx     "/docker-entrypoint.…"   About a minute ago   Up About a minute   80/tcp    demo-web`,
)

var psATable = lines(
	`CONTAINER ID   IMAGE         COMMAND                  CREATED              STATUS                      PORTS     NAMES`,
	`804cf2759ae1   alpine        "sleep 1000"             22 seconds ago       Up 21 seconds                         demo-sleep`,
	`19e5abb2567f   hello-world   "/hello"                 50 seconds ago       Exited (0) 50 seconds ago             demo-one`,
	`80dff6f6d71a   nginx         "/docker-entrypoint.…"   About a minute ago   Up About a minute           80/tcp    demo-web`,
)

func TestDetectClaimsThePsTables(t *testing.T) {
	for name, text := range map[string]string{"ps": psTable, "ps -a": psATable} {
		if got := Detect(text); got != "ps" {
			t.Errorf("Detect(%s) = %q, want ps", name, got)
		}
	}
}

func TestDetectClaimsInspect(t *testing.T) {
	inspectJSON := "[\n    {\n        \"Id\": \"804cf2759ae1\",\n        \"State\": {\"Status\": \"running\"},\n        \"Config\": {\"Image\": \"alpine\"}\n    }\n]\n"
	if got := Detect(inspectJSON); got != "inspect" {
		t.Errorf("Detect(inspect json) = %q, want inspect", got)
	}
}

func TestDetectRefusesForeignText(t *testing.T) {
	for name, text := range map[string]string{
		"ls -l": "-rw-r--r-- 1 atif atif 4096 file\n",
		"free":  lines("               total        used        free      shared  buff/cache   available", "Mem:         8004464     5661088      454460      464540     2989260     2343376"),
		"prose": "CONTAINER ID is a column docker ps prints first, and STATUS too.\n",
		"empty": "",
	} {
		if got := Detect(text); got != "" {
			t.Errorf("Detect(%s) = %q, want it claimed by nobody", name, got)
		}
	}
}

// ss -o json, ip -j and kubectl JSON are arrays of objects too — they must
// never be swallowed by the docker inspect detector.
func TestDetectRefusesForeignJSON(t *testing.T) {
	for name, text := range map[string]string{
		"ss -o json":    `[{"netid":"tcp","state":"ESTAB","recv-q":0,"send-q":0,"local":"10.0.0.5:22","peer":"10.0.0.9:51234"}]`,
		"ip -j addr":    `[{"ifindex":1,"ifname":"lo","flags":["LOOPBACK","UP"],"mtu":65536,"addr_info":[{"family":"inet","local":"127.0.0.1"}]}]`,
		"kubectl items": `[{"apiVersion":"v1","kind":"PodList","items":[{"metadata":{"name":"web"},"status":{"phase":"Running"}}]}]`,
		"bare object":   `{"Id":"abc","State":{"Status":"running"}}`,
		"empty array":   `[]`,
		"array of ints": `[1,2,3]`,
		"array of strs": `["a","b"]`,
	} {
		if got := Detect(text); got != "" {
			t.Errorf("Detect(%s) = %q, want it claimed by nobody", name, got)
		}
	}
}

// Container- and network-shaped inspect JSON is still claimed.
func TestDetectClaimsContainerAndNetworkInspect(t *testing.T) {
	container := `[{"Id":"abc","State":{"Status":"running"},"Config":{"Image":"nginx"}}]`
	network := `[{"Name":"bridge","Id":"net1","Driver":"bridge","IPAM":{"Config":[{"Subnet":"172.17.0.0/16"}]}}]`
	for name, text := range map[string]string{"container": container, "network": network} {
		if got := Detect(text); got != "inspect" {
			t.Errorf("Detect(%s) = %q, want \"inspect\"", name, got)
		}
	}
}

// Format produces the new semantic table with columns: NAME, STATE, IMAGE, PORTS, CREATED, ID
func TestFormatProducesNewTable(t *testing.T) {
	for name, text := range map[string]string{"ps": psTable, "ps -a": psATable} {
		var b strings.Builder
		formatPS(&b, text)
		got := b.String()

		// Check new columns exist
		for _, want := range []string{"NAME", "STATE", "IMAGE", "PORTS", "CREATED", "ID"} {
			if !strings.Contains(got, want) {
				t.Errorf("Format(%s) missing column %q in\n%q", name, want, got)
			}
		}

		// Check heading is bold
		if !strings.Contains(got, "<bold>NAME") {
			t.Errorf("Format(%s) heading not bold", name)
		}

		// Check state colors
		if !strings.Contains(got, "<green>running") {
			t.Errorf("Format(%s) missing green running state", name)
		}

		// Check summary line
		if !strings.Contains(got, "container") {
			t.Errorf("Format(%s) missing summary line", name)
		}
	}
}

func TestFormatDecoratesNewColumns(t *testing.T) {
	var b strings.Builder
	formatPS(&b, psATable)
	got := b.String()

	// New format decorations
	for _, want := range []string{
		"<bold>NAME", // heading bold
		"<bold>STATE",
		"<bold>IMAGE",
		"<bold>PORTS",
		"<bold>CREATED",
		"<bold>ID",
		"<green>running",    // running state green
		"<dim>exited",       // exited state dim (exit code 0)
		"<dim>-",            // missing ports dimmed
		"<dim>80dff6f6d71a", // ID dimmed
		"<bold>demo-web",    // name bold
		"<bold>demo-sleep",
		"<bold>demo-one",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Format(ps -a) missing %q in\n%q", want, got)
		}
	}

	// The nginx port is only exposed, never published, so it stays plain (dimmed as "-")
	if strings.Contains(got, "<cyan>80/tcp") {
		t.Errorf("an unpublished port was tinted as published:\n%q", got)
	}
}

func TestFormatFindsStatusBehindAWideCommand(t *testing.T) {
	var b strings.Builder
	formatPS(&b, psTable)
	if !strings.Contains(b.String(), "<green>running") {
		t.Errorf("Format(ps) did not tint the nginx row\n%q", b.String())
	}
}

func TestFormatLeavesForeignTextAlone(t *testing.T) {
	text := "[\n    {\"Id\": \"abc\"}\n]\n"
	var b strings.Builder
	formatPS(&b, text)
	if b.String() != text {
		t.Errorf("Format rewrote non-ps text\n got: %q\nwant: %q", b.String(), text)
	}
}

// A single space belongs to a value ("CONTAINER ID"), only a run of two or more
// separates columns — the rule that keeps the heading from being torn in two.
func TestColumnStartsKeepsMultiWordColumns(t *testing.T) {
	starts := columnStarts(`CONTAINER ID   IMAGE     COMMAND     CREATED     STATUS     PORTS     NAMES`)
	if starts == nil {
		t.Fatal("a real ps heading was not recognized")
	}
	names := labels(`CONTAINER ID   IMAGE     COMMAND     CREATED     STATUS     PORTS     NAMES`, starts)
	if names[0] != "CONTAINER ID" {
		t.Errorf("first column = %q, want CONTAINER ID", names[0])
	}
	if got := indexOf(names, "STATUS"); got < 0 {
		t.Errorf("STATUS not found among %q", names)
	}
}

func TestScriptForms(t *testing.T) {
	for _, args := range [][]string{
		{"ps", "-q"},
		{"ps", "--quiet"},
		{"ps", "--format", "{{.ID}}"},
		{"ps", "--format={{.ID}}"},
	} {
		if !psScriptForm(args) {
			t.Errorf("psScriptForm(%v) = false, want true", args)
		}
		if reformat("ps", args) {
			t.Errorf("reformat(%v) = true, a script form must pass through", args)
		}
	}
	if !reformat("ps", []string{"ps", "-a"}) {
		t.Error("reformat(ps -a) = false, want the table captured")
	}
	if reformat("logs", []string{"logs", "-f"}) {
		t.Error("reformat(logs -f) = true, only ps is formatted")
	}
}

func TestNeedsTerminalKeepsTheStreamingCommands(t *testing.T) {
	for name, args := range map[string][]string{
		"events":            {"events"},
		"attach":            {"attach", "demo"},
		"exec":              {"exec", "-it", "demo", "sh"},
		"logs -f":           {"logs", "-f", "demo"},
		"logs --follow":     {"logs", "--follow", "demo"},
		"stats":             {"stats"},
		"run -it":           {"run", "-it", "alpine", "sh"},
		"run --interactive": {"run", "--interactive", "alpine"},
	} {
		if !NeedsTerminal(args) {
			t.Errorf("NeedsTerminal(%s) = false, a pager would trap it", name)
		}
	}
	for name, args := range map[string][]string{
		"ps":              {"ps"},
		"ps -a":           {"ps", "-a"},
		"logs":            {"logs", "demo"},
		"stats no-stream": {"stats", "--no-stream"},
		"run -d":          {"run", "-d", "alpine"},
	} {
		if NeedsTerminal(args) {
			t.Errorf("NeedsTerminal(%s) = true, it ends on its own", name)
		}
	}
}

// secretEnvInspect is docker inspect JSON for a container whose env carries
// custom secret-looking vars. Before the fix, normalizeEnv wrote to
// env.Secrets without make(), panicking with "assignment to entry in nil map".
var secretEnvInspect = lines(
	`[`,
	`    {`,
	`        "Id": "aaaabbbbccccdddd0000111122223333444455556666777788889999",`,
	`        "Name": "/fx-full",`,
	`        "RestartCount": 3,`,
	`        "State": {`,
	`            "Status": "exited",`,
	`            "Running": false,`,
	`            "Paused": false,`,
	`            "Restarting": false,`,
	`            "OOMKilled": false,`,
	`            "Dead": false,`,
	`            "Pid": 0,`,
	`            "ExitCode": 1,`,
	`            "Error": "",`,
	`            "StartedAt": "2026-10-10T12:00:00.000000000Z",`,
	`            "FinishedAt": "2026-10-10T12:01:00.000000000Z"`,
	`        },`,
	`        "Image": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",`,
	`        "Config": {`,
	`            "Hostname": "fx-full",`,
	`            "Image": "nginx:latest",`,
	`            "Env": [`,
	`                "PATH=/usr/local/sbin:/usr/local/bin",`,
	`                "DB_PASSWORD=s3cr3t",`,
	`                "API_TOKEN=tok_abcdef123456",`,
	`                "DATABASE_URL=postgres://user:pass@host/db",`,
	`                "APP_MODE=prod"`,
	`            ],`,
	`            "Cmd": ["nginx", "-g", "daemon off;"]`,
	`        },`,
	`        "HostConfig": {`,
	`            "RestartPolicy": { "Name": "on-failure", "MaximumRetryCount": 5 },`,
	`            "Privileged": false,`,
	`            "NetworkMode": "bridge"`,
	`        },`,
	`        "NetworkSettings": {`,
	`            "Ports": {},`,
	`            "Networks": {`,
	`                "bridge": {`,
	`                    "IPAddress": "172.17.0.2",`,
	`                    "Gateway": "172.17.0.1"`,
	`                }`,
	`            }`,
	`        }`,
	`    }`,
	`]`,
)

// Regression: inspect JSON with custom secret env vars must format without
// panicking.
func TestInspectWithSecretEnvDoesNotPanic(t *testing.T) {
	var out strings.Builder
	Format(&out, "inspect", secretEnvInspect) // must not panic

	got := out.String()
	if !strings.Contains(got, "fx-full") {
		t.Fatalf("output missing container name:\n%s", got)
	}
	if !strings.Contains(got, "4 vars") {
		t.Errorf("expected 3 custom vars counted, got:\n%s", got)
	}
	if strings.Contains(got, "s3cr3t") || strings.Contains(got, "tok_abcdef123456") {
		t.Errorf("secret value leaked into output:\n%s", got)
	}
}

// Format must recover from a panic: original input untouched on w, warning on
// stderr.
func TestFormatRecoversFromPanic(t *testing.T) {
	formatHook = func(sub, text string) { panic("forced") }
	defer func() { formatHook = nil }()

	var out strings.Builder
	Format(&out, "inspect", "RAW-INPUT-Untouched")

	if got := out.String(); got != "RAW-INPUT-Untouched" {
		t.Errorf("recover did not print original input, got %q", got)
	}
}

// A ps heading with no rows under it must not render an empty table: zero
// rows from non-empty input means the text was not really docker ps, so it
// goes back untouched.
func TestPSTableWithNoRowsFallsBackToInput(t *testing.T) {
	input := "CONTAINER ID   IMAGE     COMMAND     CREATED     STATUS     PORTS     NAMES\n"
	var out strings.Builder
	Format(&out, "ps", input)
	if got := out.String(); got != input {
		t.Errorf("zero-row ps input was swallowed\n got: %q\nwant: %q", got, input)
	}
}

// docker system df is recognized as docker's own table (so no other detector
// can claim it) and then handed back untouched — the formatter is a stub and
// the README promises "passed through unchanged".
// Every df-shaped table — the real one and this memory-style variant — is
// claimed as df, so no other detector can rewrite it. What the formatter then
// does with it lives in df_internal_test.go.
func TestSystemDFVariantsAreClaimedAsDF(t *testing.T) {
	text := lines(
		"TYPE            TOTAL     USED     FREE      SHARED  BUFFERS   CACHE     ACTIVE    SIZE",
		"Images          5         2        3         0       0         0         1         150MB",
		"Containers      3         1        2         0       0         0         1         10MB",
	)
	if got := Detect(text); got != "df" {
		t.Errorf("Detect = %q, want df: an unclaimed table could be rewritten by someone else", got)
	}
}

// psPortsTable builds a real two-line docker ps table whose column starts the
// header defines, the way docker itself pads a table to its widest cell.
func psPortsTable(ports string) string {
	header := []string{"CONTAINER ID", "IMAGE", "COMMAND", "CREATED", "STATUS", "PORTS", "NAMES"}
	row := []string{"abc123def456", "nginx", `"nginx"`, "1s ago", "Up 1s", ports, "demo"}
	widths := make([]int, len(header))
	for i := range header {
		widths[i] = len(header[i])
		if n := len(row[i]); n > widths[i] {
			widths[i] = n
		}
	}
	join := func(cells []string) string {
		var b strings.Builder
		for i, c := range cells {
			b.WriteString(c)
			if i < len(cells)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-len(c)+3))
			}
		}
		return b.String()
	}
	return join(header) + "\n" + join(row) + "\n"
}

// psPortsRendered runs a PORTS column value through the full ps formatter.
func psPortsRendered(t *testing.T, ports string) string {
	t.Helper()
	var out strings.Builder
	Format(&out, "ps", psPortsTable(ports))
	return out.String()
}

// The ps PORTS column through docker's own spellings: one wildcard binding
// printed twice (0.0.0.0 and ::) collapses to a single line, "[::]" is never
// mangled into "[:0", ranges stay ranges, and a declared port stays
// unpublished.
func TestPSPortColumnRendering(t *testing.T) {
	t.Run("ipv4/ipv6 twins collapse to one", func(t *testing.T) {
		got := psPortsRendered(t, "0.0.0.0:8081->80/tcp, :::8081->80/tcp")
		if n := strings.Count(got, "8081->80/tcp"); n != 1 {
			t.Errorf("the twin bindings must render once, got %d:\n%s", n, got)
		}
		for _, bad := range []string{":::", "0.0.0.0", "[:0"} {
			if strings.Contains(got, bad) {
				t.Errorf("%q leaked into the rendered ports:\n%s", bad, got)
			}
		}
	})

	t.Run("ipv6-only binding", func(t *testing.T) {
		got := psPortsRendered(t, "[::]:8081->80/tcp")
		if n := strings.Count(got, "8081->80/tcp"); n != 1 {
			t.Errorf("the ipv6 binding must render once, got %d:\n%s", n, got)
		}
		for _, bad := range []string{"[:0", "[::]"} {
			if strings.Contains(got, bad) {
				t.Errorf("%q leaked into the rendered ports:\n%s", bad, got)
			}
		}
	})

	t.Run("port range stays a range", func(t *testing.T) {
		got := psPortsRendered(t, "8000-8002->8000-8002/tcp")
		if !strings.Contains(got, "8000-8002->8000-8002/tcp") {
			t.Errorf("the range was not preserved:\n%s", got)
		}
		if strings.Contains(got, "8000->8000/tcp") {
			t.Errorf("the range was collapsed to one port:\n%s", got)
		}
	})

	t.Run("declared but unpublished", func(t *testing.T) {
		got := psPortsRendered(t, "80/tcp")
		if !strings.Contains(got, "80/tcp") {
			t.Errorf("the declared port is missing:\n%s", got)
		}
		if strings.Contains(got, "->") {
			t.Errorf("an unpublished port grew an arrow:\n%s", got)
		}
	})
}

// Secret values are masked by default; the password inside a URL is masked
// too; --show-secrets reveals everything.
func TestEnvSecretMasking(t *testing.T) {
	var raw []map[string]interface{}
	if err := json.Unmarshal([]byte(secretEnvInspect), &raw); err != nil {
		t.Fatal(err)
	}

	masked := stripTags(RenderInspect(NormalizeInspect(raw)))
	for _, want := range []string{"DB_PASSWORD=********", "API_TOKEN=********",
		"postgres://user:********@host/db", "APP_MODE=prod"} {
		if !strings.Contains(masked, want) {
			t.Errorf("masked output missing %q:\n%s", want, masked)
		}
	}
	for _, leak := range []string{"s3cr3t", "tok_abcdef123456", "user:pass@"} {
		if strings.Contains(masked, leak) {
			t.Errorf("masked output leaks %q:\n%s", leak, masked)
		}
	}

	views := NormalizeInspect(raw)
	for i := range views {
		views[i].Env.ShowSecrets = true
	}
	shown := stripTags(RenderInspect(views))
	for _, want := range []string{"DB_PASSWORD=s3cr3t", "API_TOKEN=tok_abcdef123456",
		"postgres://user:pass@host/db"} {
		if !strings.Contains(shown, want) {
			t.Errorf("--show-secrets output missing %q:\n%s", want, shown)
		}
	}
	if strings.Contains(shown, "(secrets masked") {
		t.Errorf("--show-secrets output still shows masked note:\n%s", shown)
	}
}

// What the image already ships is not what the user set: a KEY=VALUE the
// image defines byte for byte is dropped, an override of the same key is
// kept, and image noise is quiet even before any lookup happens.
func TestInspectHidesVarsTheImageAlreadySets(t *testing.T) {
	old := imageEnvOf
	imageEnvOf = func(string) (map[string]bool, bool) {
		return map[string]bool{
			"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin": true,
			"VERSION=1": true,
		}, true
	}
	defer func() { imageEnvOf = old }()

	var raw []map[string]interface{}
	fixture := lines(
		`[`,
		`    {`,
		`        "Id": "aaaabbbbccccdddd0000111122223333444455556666777788889999",`,
		`        "State": {"Status": "running", "Running": true},`,
		`        "Config": {`,
		`            "Image": "example:latest",`,
		`            "Env": [`,
		`                "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",`,
		`                "VERSION=2",`,
		`                "APP_MODE=prod"`,
		`            ]`,
		`        }`,
		`    }`,
		`]`,
	)
	if err := json.Unmarshal([]byte(fixture), &raw); err != nil {
		t.Fatal(err)
	}

	got := stripTags(RenderInspect(NormalizeInspect(raw)))
	for _, want := range []string{"env       2 vars", "VERSION=2", "APP_MODE=prod"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	for _, hide := range []string{"PATH=", "VERSION=1"} {
		if strings.Contains(got, hide) {
			t.Errorf("the image's own %q leaked into the output:\n%s", hide, got)
		}
	}
}

// installFakeDocker puts a fake `docker` first on PATH that records the args
// it was called with, prints stdout and exits with exitCode.
func installFakeDocker(t *testing.T, stdout string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	outFile := filepath.Join(dir, "stdout")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > \"$FAKE_ARGS\"\n" +
		"cat \"$FAKE_OUT\"\n" +
		"exit " + strconv.Itoa(exitCode)
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outFile, []byte(stdout), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_ARGS", argsFile)
	t.Setenv("FAKE_OUT", outFile)
	return argsFile
}

// captureStdout runs fn with os.Stdout swapped for a pipe and returns what
// was written.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// Direct mode must re-exec docker with --format json and render the table.
func TestRunPSDirectRewritesToJSONAndRenders(t *testing.T) {
	row := `{"ID":"80dff6f6d71a","Names":"demo-web","Image":"nginx:latest",` +
		`"Command":"nginx -g daemon off;","CreatedAt":"2026-10-10 12:00:00 +0000 UTC",` +
		`"RunningFor":"5 minutes ago","State":"running","Status":"Up 5 minutes",` +
		`"Ports":"0.0.0.0:80->80/tcp"}`
	argsFile := installFakeDocker(t, row+"\n", 0)

	code := 0
	out := captureStdout(t, func() { code = runPSDirect([]string{"ps", "-a"}) })

	if code != 0 {
		t.Errorf("runPSDirect exit = %d, want 0", code)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	got := string(args)
	for _, want := range []string{"ps", "--format", "json", "-a"} {
		if !strings.Contains(got, want+"\n") {
			t.Errorf("fake docker was not called with %q, args were:\n%s", want, got)
		}
	}
	if !strings.Contains(out, "<bold>NAME") {
		t.Errorf("table heading missing:\n%q", out)
	}
	if !strings.Contains(out, "demo-web") || !strings.Contains(out, "<green>running") {
		t.Errorf("row not rendered:\n%q", out)
	}
}

// Direct-mode inspect renders and honours --show-secrets; the flag itself is
// never forwarded to docker.
func TestRunInspectDirectRendersAndShowSecrets(t *testing.T) {
	argsFile := installFakeDocker(t, secretEnvInspect, 0)

	out := captureStdout(t, func() {
		runInspectDirect([]string{"inspect", "fx-full"})
	})
	if !strings.Contains(out, "fx-full") {
		t.Errorf("inspect output missing container name:\n%s", out)
	}
	if strings.Contains(out, "s3cr3t") {
		t.Errorf("default output leaked a secret:\n%s", out)
	}
	args, _ := os.ReadFile(argsFile)
	if strings.Contains(string(args), "--show-secrets") {
		t.Errorf("--show-secrets was forwarded to docker: %s", args)
	}

	out = captureStdout(t, func() {
		runInspectDirect([]string{"inspect", "fx-full", "--show-secrets"})
	})
	if !strings.Contains(out, "DB_PASSWORD=s3cr3t") {
		t.Errorf("--show-secrets output missing real secret:\n%s", out)
	}
}

// Direct mode must return docker's real exit code, not always 0.
func TestDirectModeReturnsDockerExitCode(t *testing.T) {
	installFakeDocker(t, "", 7)

	if code := runPSDirect([]string{"ps"}); code != 7 {
		t.Errorf("runPSDirect exit = %d, want docker's 7", code)
	}
	if code := runInspectDirect([]string{"inspect", "nope"}); code != 7 {
		t.Errorf("runInspectDirect exit = %d, want docker's 7", code)
	}
}
