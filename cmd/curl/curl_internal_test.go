// Tests for the curl formatter: which shapes it claims, how the status
// line, headers and verbose noise come out, and what must stay byte for
// byte.
package curl

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/atif-1402/parse/internal/tool"
)

// The tint is written as markers rather than escapes so a failure can be
// read.
func init() {
	tool.Paint = func(color, s string) string {
		if color == "" || s == "" {
			return s
		}
		return "<" + color + ">" + s + "</>"
	}
}

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

// installFakeCurl puts a fake `curl` on PATH that records its arguments,
// prints stdout from the fixture and exits with the given code.
func installFakeCurl(t *testing.T, stdout string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	outFile := filepath.Join(dir, "stdout")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > \"$FAKE_ARGS\"\n" +
		"cat \"$FAKE_OUT\"\n" +
		"exit " + strconv.Itoa(exitCode)
	if err := os.WriteFile(filepath.Join(dir, "curl"), []byte(script), 0o755); err != nil {
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

func format(sub, text string) string {
	var sb strings.Builder
	Format(&sb, sub, text)
	return sb.String()
}

// The -I shape: status line in its code's color, header keys cyan, and
// the body -i attaches after the blank line left exactly as it came.
func TestCurlHeadersClaimedAndColored(t *testing.T) {
	fix := "HTTP/2 200\n" +
		"content-type: text/html; charset=UTF-8\n" +
		"content-length: 1256\n" +
		"\n" +
		"<!doctype html>\n" +
		"<html><body>hi</body></html>\n"

	if got := Detect(fix); got != "headers" {
		t.Fatalf("Detect = %q, want headers", got)
	}
	out := format(Detect(fix), fix)
	for _, want := range []string{
		"<bold green>HTTP/2 200</>",
		"<cyan>content-type</>: text/html; charset=UTF-8",
		"<cyan>content-length</>: 1256",
		"<!doctype html>",
		"<html><body>hi</body></html>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
}

// Every code gets its own traffic light: 2xx green, 3xx yellow, 4xx and
// 5xx red.
func TestCurlStatusColors(t *testing.T) {
	for _, tc := range []struct{ line, color string }{
		{"HTTP/2 204 No Content", "<bold green>"},
		{"HTTP/1.1 301 Moved Permanently", "<bold yellow>"},
		{"HTTP/1.1 404 Not Found", "<bold red>"},
		{"HTTP/2 503 Service Unavailable", "<bold red>"},
	} {
		out := format("headers", tc.line+"\nserver: x\n")
		if !strings.Contains(out, tc.color+tc.line+"</>") {
			t.Errorf("%q not painted %s:\n%s", tc.line, tc.color, out)
		}
	}
}

// A redirect chain restarts the header run, so every hop's status line
// is colored and the keys under each stay cyan.
func TestCurlRedirectChainColorsEveryHop(t *testing.T) {
	fix := "HTTP/1.1 301 Moved Permanently\n" +
		"location: https://example.com/\n" +
		"\n" +
		"HTTP/2 200\n" +
		"content-type: text/html\n" +
		"\n" +
		"<html>final</html>\n"
	out := format("headers", fix)
	for _, want := range []string{
		"<bold yellow>HTTP/1.1 301 Moved Permanently</>",
		"<cyan>location</>: https://example.com/",
		"<bold green>HTTP/2 200</>",
		"<html>final</html>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("chain output is missing %q:\n%s", want, out)
		}
	}
}

// The verbose stream: noise dimmed, the response status still colored,
// the body between them untouched, and the closing line dim again.
func TestCurlVerboseDimsNoiseAndColorsStatus(t *testing.T) {
	fix := "* Trying 93.184.216.34:443...\n" +
		"* Connected to example.com (93.184.216.34) port 443 (#0)\n" +
		"* ALPN: server accepted h2\n" +
		"> GET / HTTP/2\n" +
		"> Host: example.com\n" +
		">\n" +
		"< HTTP/2 200\n" +
		"< content-type: text/html\n" +
		"\n" +
		"<html>body</html>\n" +
		"* Connection #0 to host example.com left intact\n"

	if got := Detect(fix); got != "verbose" {
		t.Fatalf("Detect = %q, want verbose", got)
	}
	out := format(Detect(fix), fix)
	for _, want := range []string{
		"<dim>* Trying 93.184.216.34:443...</>",
		"<dim>> GET / HTTP/2</>",
		"<dim>></>",
		"<bold green>< HTTP/2 200</>",
		"<dim>< content-type: text/html</>",
		"<html>body</html>",
		"<dim>* Connection #0 to host example.com left intact</>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("verbose output is missing %q:\n%s", want, out)
		}
	}
}

// Failures read red — the connection-level phrases in the * lines and
// curl's own `curl: (n)` trailer.
func TestCurlVerboseErrorsAreRed(t *testing.T) {
	fix := "* Trying 203.0.113.1:443...\n" +
		"* Connection refused\n" +
		"* Closing connection #0\n" +
		"curl: (7) Failed to connect to 203.0.113.1 port 443 after 0 ms: Connection refused\n"
	out := format("verbose", fix)
	for _, want := range []string{
		"<dim>* Trying 203.0.113.1:443...</>",
		"<red>* Connection refused</>",
		"<bold red>curl: (7) Failed to connect to 203.0.113.1 port 443 after 0 ms: Connection refused</>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("error output is missing %q:\n%s", want, out)
		}
	}
}

// A connect that dies before any request line still carries curl's
// `curl: (7)`, which is what claims the text.
func TestCurlDetectsConnectFailureWithoutRequest(t *testing.T) {
	fix := "* Trying 203.0.113.1:443...\n" +
		"* Connection refused\n" +
		"curl: (7) Failed to connect to 203.0.113.1 port 443: Connection refused\n"
	if got := Detect(fix); got != "verbose" {
		t.Errorf("Detect = %q, want verbose", got)
	}
}

// Everything a script reads or a server sends as data comes back byte
// for byte: an HTML body, JSON, a -w write-out, a lone status line with
// no headers, a curl error with no -v around it, a bullet list, garbage.
func TestCurlLeavesEverythingElseAlone(t *testing.T) {
	for name, text := range map[string]string{
		"html body":       "<!doctype html>\n<html><body>hi</body></html>\n",
		"json":            `{"hello": "world"}` + "\n",
		"write-out":       "200\n",
		"plain text":      "ok\n",
		"status alone":    "HTTP/2 200\n",
		"curl error only": "curl: (6) Could not resolve host: nonexistent.invalid\n",
		"bullets":         "* first point\n* second point\n",
		"garbage":         "random text\nwith a colon: in it\n",
		"empty":           "",
	} {
		if got := Detect(text); got != "" {
			t.Errorf("%s: Detect = %q, want passthrough", name, got)
		}
		var sb strings.Builder
		Format(&sb, Detect(text), text)
		if sb.String() != text {
			t.Errorf("%s: output must be byte for byte:\n%q", name, sb.String())
		}
	}
}

// An unknown sub, like an empty one, writes the text back untouched.
func TestFormatUnknownSubIsVerbatim(t *testing.T) {
	text := "HTTP/2 200\nserver: x\n"
	for _, sub := range []string{"", "bogus"} {
		var sb strings.Builder
		Format(&sb, sub, text)
		if sb.String() != text {
			t.Errorf("Format(%q) altered the text:\n%q", sub, sb.String())
		}
	}
}

// The direct path runs curl with the user's own arguments, formats the
// response when stdout has one, and reports the args docker was given.
func TestRunDirectFormatsHeadersAndKeepsArgs(t *testing.T) {
	argsFile := installFakeCurl(t, "HTTP/2 200\ncontent-type: text/html\n\n<body/>\n", 0)

	out := captureStdout(t, func() {
		if code := Run([]string{"-I", "https://example.com"}); code != 0 {
			t.Errorf("exit = %d, want 0", code)
		}
	})
	if !strings.Contains(out, "<bold green>HTTP/2 200</>") {
		t.Errorf("headers not formatted:\n%s", out)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(string(args))
	want := []string{"-I", "https://example.com"}
	if len(got) != len(want) {
		t.Fatalf("curl args = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("curl args[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// A body with no response headers around it passes through, and curl's
// exit code is kept.
func TestRunDirectBodyPassthroughAndExitCode(t *testing.T) {
	body := "<html><body>hi</body></html>\n"
	installFakeCurl(t, body, 22)

	out := captureStdout(t, func() {
		if code := Run([]string{"https://example.com"}); code != 22 {
			t.Errorf("exit = %d, want 22", code)
		}
	})
	if out != body {
		t.Errorf("body must be byte for byte:\n%q", out)
	}
}

// stripPaint removes the <color> markers again so output can be compared
// byte for byte against the input. Only real marker names are removed, so
// body content like `<html>` or `< HTTP/2 200` survives.
func stripPaint(s string) string {
	re := regexp.MustCompile(`</>|<(?:bold|dim|red|green|yellow|cyan)(?: (?:bold|dim|red|green|yellow|cyan))*>`)
	return re.ReplaceAllString(s, "")
}

// The progress meter rides in front of the response when stderr and
// stdout share a pipe (`curl -I … 2>&1`). It is skipped while claiming
// and dropped afterwards — chrome for a terminal, not content — while
// every line curl actually sent survives byte for byte.
func TestCurlMeterBeforeStatusIsDropped(t *testing.T) {
	fix := "  % Total    % Received % Xferd  Average Speed  Time    Time    Time   Current\n" +
		"                                 Dload  Upload  Total   Spent   Left   Speed\n" +
		"\r  0      0   0      0   0      0      0      0                              0\n" +
		"HTTP/2 200 \n" +
		"content-type: text/html\n" +
		"\n"

	if got := Detect(fix); got != "headers" {
		t.Fatalf("Detect = %q, want headers (meter must not block the claim)", got)
	}
	out := format(Detect(fix), fix)
	for _, gone := range []string{"% Total", "Dload", "      0 "} {
		if strings.Contains(out, gone) {
			t.Errorf("meter %q must be dropped, not printed:\n%q", gone, out)
		}
	}
	for _, want := range []string{
		"<bold green>HTTP/2 200 </>",
		"<cyan>content-type</>: text/html",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("metered output missing %q:\n%q", want, out)
		}
	}
	want := "HTTP/2 200 \ncontent-type: text/html\n\n"
	if stripPaint(out) != want {
		t.Errorf("everything curl sent must be byte for byte:\ngot  %q\nwant %q", stripPaint(out), want)
	}
}

// A meter fragment glued to the next verbose line with no line break
// loses only the fragment — the `*` line under it comes out dimmed like
// every other verbose line.
func TestCurlMeterGluedToVerboseLineIsDropped(t *testing.T) {
	fix := "  % Total    % Received % Xferd  Average Speed  Time    Time    Time   Current\n" +
		"\r  0      0   0      0* using HTTP/2\n" +
		"* Connected to example.com port 443 (#0)\n" +
		"} [5 bytes data]\n" +
		"> GET / HTTP/2\n" +
		"< HTTP/2 200\n" +
		"\n"

	if got := Detect(fix); got != "verbose" {
		t.Fatalf("Detect = %q, want verbose", got)
	}
	out := format(Detect(fix), fix)
	if strings.Contains(out, "% Total") || strings.Contains(out, "0      0") {
		t.Errorf("meter must be dropped:\n%q", out)
	}
	for _, want := range []string{
		"<dim>* using HTTP/2</>",
		"<dim>* Connected to example.com port 443 (#0)</>",
		"<dim>} [5 bytes data]</>",
		"<bold green>< HTTP/2 200</>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("glued meter output missing %q:\n%q", want, out)
		}
	}
	want := "* using HTTP/2\n" +
		"* Connected to example.com port 443 (#0)\n" +
		"} [5 bytes data]\n" +
		"> GET / HTTP/2\n" +
		"< HTTP/2 200\n" +
		"\n"
	if stripPaint(out) != want {
		t.Errorf("everything curl said must be byte for byte:\ngot  %q\nwant %q", stripPaint(out), want)
	}
}

// A meter with nothing under it — the stream of a download whose headers
// never reach stdout — is not curl's response to claim.
func TestCurlMeterAloneIsLeftAlone(t *testing.T) {
	fix := "  % Total    % Received % Xferd  Average Speed  Time    Time    Time   Current\n" +
		"                                 Dload  Upload  Total   Spent   Left   Speed\n" +
		"\r  1024   1024  1024    0    0   1024      0      0\n"
	if got := Detect(fix); got != "" {
		t.Errorf("Detect = %q, want passthrough", got)
	}
	var sb strings.Builder
	Format(&sb, Detect(fix), fix)
	if sb.String() != fix {
		t.Errorf("meter must be byte for byte:\n%q", sb.String())
	}
}

// The success line "SSL certificate verified via OpenSSL." is good news
// and must not be read as the certificate error.
func TestCurlVerifiedCertificateStaysDim(t *testing.T) {
	fix := "* SSL certificate verified via OpenSSL.\n* OpenSSL verify result: 0\n"
	out := format("verbose", fix)
	if strings.Contains(out, "<red>") {
		t.Errorf("verified certificate painted red:\n%q", out)
	}
	if !strings.Contains(out, "<dim>* SSL certificate verified via OpenSSL.</>") {
		t.Errorf("verified certificate not dimmed:\n%q", out)
	}
}

// After the response, meter ticks still ride in as carriage-returned
// rewrites and are dropped — while a body line that is all digits but
// carries no tick is data and comes through untouched.
func TestCurlMeterAfterStatusDropsButDigitBodyStays(t *testing.T) {
	fix := "* Connected to example.com port 443 (#0)\n" +
		"> GET / HTTP/2\n" +
		"< HTTP/2 200 \n" +
		"< content-type: text/plain\n" +
		"\n" +
		"\r100    577   0    577   0      0   4764      0                              0\r100    577   0    577   0      0   4762      0                              0\n" +
		"100\n" +
		"42\n" +
		"* Connection #0 to host example.com:443 left intact\n"

	out := format("verbose", fix)
	if strings.Contains(out, "4764") || strings.Contains(out, "4762") {
		t.Errorf("meter ticks must be dropped:\n%q", out)
	}
	for _, want := range []string{"<bold green>< HTTP/2 200 </>", "100\n42\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%q", want, out)
		}
	}
	want := "* Connected to example.com port 443 (#0)\n" +
		"> GET / HTTP/2\n" +
		"< HTTP/2 200 \n" +
		"< content-type: text/plain\n" +
		"\n" +
		"100\n" +
		"42\n" +
		"* Connection #0 to host example.com:443 left intact\n"
	if stripPaint(out) != want {
		t.Errorf("body must be byte for byte:\ngot  %q\nwant %q", stripPaint(out), want)
	}
}
