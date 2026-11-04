// Package curl formats curl's output. Three shapes are worth formatting
// and all are claimed only from their opening line: a response's status
// line with headers under it (`curl -I`, `-i`), where the status gets a
// color by code and the header keys go cyan; curl's own verbose stream
// (`curl -v … 2>&1`), where the wall of `*` lines is dimmed, the response
// status still colored, and failures red; and a bare `curl: (n)` failure
// with nothing around it, painted red on its own. Bodies, JSON, `-w`
// write-outs and every other form pass through byte for byte.
//
// curl's progress meter shares the stream: stderr is unbuffered, so in
// `curl -I … 2>&1` the meter arrives before curl's buffered stdout, and in
// `curl -v … 2>&1` a meter fragment can be glued to the next verbose line
// without a line break (`0  0 … 0* using HTTP/2`). The meter is dropped
// from formatted output — it is chrome for a terminal, not content —
// while everything curl actually says is kept byte for byte.
package curl

import (
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/atif-1402/parse/internal/tool"
)

const (
	bold   = tool.Bold
	red    = tool.Red
	green  = tool.Green
	yellow = tool.Yellow
	cyan   = tool.Cyan
	dim    = tool.Dim
)

var (
	// HTTP/2 200, HTTP/1.1 404 Not Found, HTTP/3 200 …
	reStatus = regexp.MustCompile(`^HTTP/\d+(\.\d+)? \d{3}`)
	// A header line: a run of non-space printable chars, a colon, then
	// the value (which may be empty).
	reHeader = regexp.MustCompile(`^[!-~]+:( .*)?$`)
	// The request line curl's verbose stream prints: `> GET / HTTP/1.1`.
	reRequest = regexp.MustCompile(`^> [A-Z][A-Z0-9-]* \S+ HTTP/\d`)
	// One progress-meter update: a run of digits and spaces, rewritten in
	// place with carriage returns, e.g. `  0      0   0      0 …`.
	reMeterDigits = regexp.MustCompile(`^[0-9 ]+$`)
	// A meter fragment glued in front of a real line with no break, as
	// when curl writes `0 … 0* using HTTP/2` in one go. At least one
	// digit or space must sit in front, so a plain `* ` line is not one.
	reMeterGlue = regexp.MustCompile(`^[0-9 ]+(\* |> |HTTP/|curl: \()`)
	// curl's own failure line, with nothing around it: the whole error
	// `curl -f`, a bad host or a refused connect leaves on stderr.
	reCurlError = regexp.MustCompile(`^curl: \(\d+\)`)
	// curl's own marker around a binary chunk in the verbose stream:
	// `{ [1566 bytes data]`. Exact enough that no JSON body matches it.
	reDataMark = regexp.MustCompile(`^[{}] \[\d+ bytes data\]\r?$`)
)

// isMeter reports whether one carriage-return fragment of the stream is
// the progress meter rather than something curl is saying. The meter's
// two header lines name their columns, so they are matched by content.
func isMeter(seg string) bool {
	t := strings.TrimSpace(seg)
	if t == "" {
		return false
	}
	if strings.HasPrefix(t, "% Total") || strings.Contains(t, "Dload") {
		return true
	}
	return reMeterDigits.MatchString(t)
}

// isMeterLine reports whether a whole newline line is meter: every one of
// its carriage-return fragments is meter, or a fragment glued to what
// follows it (`0  0* using HTTP/2` — the `*` line there would be dimmed
// on its own anyway).
func isMeterLine(line string) bool {
	heard := false
	for _, seg := range strings.Split(line, "\r") {
		if seg == "" {
			continue
		}
		heard = true
		if !isMeter(seg) && !reMeterGlue.MatchString(seg) {
			return false
		}
	}
	return heard
}

// unGlue strips a meter fragment written without a line break in front of
// a real line, so `0  0 … 0HTTP/2 200` can still be read as a status
// line. Lines that do not start with a fragment come back unchanged.
func unGlue(line string) string {
	if m := reMeterGlue.FindStringSubmatch(line); m != nil {
		return line[len(m[0])-len(m[1]):]
	}
	return line
}

// saying returns the first line curl actually says something on: meter
// lines and empty lines are skipped, and a meter fragment glued to the
// line is taken off the front.
func saying(text string) string {
	for _, line := range strings.Split(text, "\n") {
		for _, seg := range strings.Split(line, "\r") {
			if seg == "" || isMeter(seg) {
				continue
			}
			return unGlue(seg)
		}
	}
	return ""
}

// dropMeter takes the progress meter out of a line. A line that is all
// meter comes back empty — the caller drops it, newline and all — and a
// fragment glued to a real line loses only the fragment:
// `0 … 0* using HTTP/2` → `* using HTTP/2`. Lines without meter come
// back untouched, byte for byte; metered reports whether any was found.
func dropMeter(line string) (cleaned string, metered bool) {
	if !isMeterLine(line) {
		return line, false
	}
	var kept []string
	for _, seg := range strings.Split(line, "\r") {
		if seg == "" || isMeter(seg) {
			continue
		}
		kept = append(kept, unGlue(seg))
	}
	if len(kept) == 0 {
		return "", true
	}
	return strings.Join(kept, "\r"), true
}

// takeMeter removes the meter from a line while the meter can still be
// where it belongs. Before the response line no body exists yet, so all
// meter goes; after it, only lines still carrying carriage returns are
// meter — curl rewrites its ticks in place — and a body line of digits
// (LF-terminated, no ticks) must survive.
func takeMeter(line string, seenStatus bool) (string, bool) {
	if seenStatus && !strings.Contains(line, "\r") {
		return line, false
	}
	return dropMeter(line)
}

// Detect reports which curl shape the text is: "headers", "verbose",
// "error", or "" when it is not curl's to claim. The first line curl
// says anything on decides — the progress meter in front of it is not a
// line curl says anything on — and the rest of the text has to back it
// up: a status line with no headers under it, or a `*` bullet list with
// no request and no curl error in it, is somebody else's text.
func Detect(text string) string {
	first := saying(text)
	if first == "" {
		return ""
	}
	if reStatus.MatchString(first) {
		for _, line := range strings.Split(text, "\n") {
			if reHeader.MatchString(line) {
				return "headers"
			}
		}
		return ""
	}
	if strings.HasPrefix(first, "* ") {
		for _, line := range strings.Split(text, "\n") {
			for _, seg := range strings.Split(line, "\r") {
				// A connect that dies before the request still says
				// `curl: (7)`, which no bullet list carries.
				if reRequest.MatchString(unGlue(seg)) || strings.HasPrefix(unGlue(seg), "curl: (") {
					return "verbose"
				}
			}
		}
	}
	// A failure curl reported on its own — `curl -f`, a bad host — with
	// no verbose stream around it.
	if reCurlError.MatchString(first) {
		return "error"
	}
	return ""
}

// statusCode pulls the code out of a status line: "HTTP/1.1 404 Not
// Found" → "404". Empty when the line is not one.
func statusCode(line string) string {
	fields := strings.Fields(line)
	if len(fields) >= 2 && strings.HasPrefix(fields[0], "HTTP/") && len(fields[1]) == 3 {
		return fields[1]
	}
	return ""
}

// statusColor maps a code to parse's traffic lights: 2xx green, 3xx
// yellow, 4xx and 5xx red.
func statusColor(code string) string {
	switch code[0] {
	case '2':
		return green
	case '3':
		return yellow
	default:
		return red
	}
}

// statusLine paints a whole status line in its code's color, bold.
func statusLine(line string) string {
	code := statusCode(line)
	if code == "" {
		return line
	}
	return tool.PaintColor(bold+" "+statusColor(code), line)
}

// verboseError reports whether a verbose line is a failure rather than
// progress. The list is phrases curl only writes when something went
// wrong; normal lines like "SSL connection using TLS1.3" and success
// lines like "SSL certificate verified via OpenSSL." stay out of it —
// "failed" is what carries certificate failures.
func verboseError(line string) bool {
	l := strings.ToLower(line)
	for _, p := range []string{
		"refused", "failed", "error", "timed out",
		"could not resolve", "couldn't resolve", "empty reply",
		"reset by peer", "no route",
	} {
		if strings.Contains(l, p) {
			return true
		}
	}
	return false
}

// Format writes curl's text with its status, headers, failures and noise
// made scannable. sub comes from Detect; anything unrecognised — the body
// of a plain GET, a JSON blob, a -w write-out — is written back byte for
// byte.
func Format(w io.Writer, sub, text string) {
	if text == "" || (sub != "headers" && sub != "verbose" && sub != "error") {
		io.WriteString(w, text)
		return
	}
	switch sub {
	case "headers":
		formatHeaders(w, text)
	case "verbose":
		formatVerbose(w, text)
	default:
		formatError(w, text)
	}
}

// Pipe is the detector's format hook.
func Pipe(w io.Writer, sub, text string) { Format(w, sub, text) }

// splitLines cuts the text into the lines to walk, dropping only the one
// empty element a trailing newline leaves behind — a blank line at the
// end of the stream is a line of the stream and comes back out.
func splitLines(text string) []string {
	lines := strings.Split(text, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

// formatHeaders colors the status line and the header keys, and leaves
// everything after the blank line — the body `-i` attaches — untouched.
// A second status line restarts the header run, so a redirect chain
// colors every hop. The progress meter that rides in front of the
// response when stderr and stdout share a pipe is dropped: it only ever
// comes before the status line, where no body can be mistaken for it.
func formatHeaders(w io.Writer, text string) {
	lines := splitLines(text)
	inHeaders := true
	seenStatus := false
	for _, line := range lines {
		if cleaned, metered := takeMeter(line, seenStatus); metered {
			if cleaned == "" {
				continue
			}
			line = cleaned
		}
		switch {
		case strings.TrimRight(line, "\r") == "":
			inHeaders = false
		case reStatus.MatchString(line):
			inHeaders = true
			seenStatus = true
			line = statusLine(line)
		case inHeaders && reHeader.MatchString(line):
			if i := strings.Index(line, ":"); i > 0 {
				line = tool.PaintColor(cyan, line[:i]) + line[i:]
			}
		}
		io.WriteString(w, line)
		io.WriteString(w, "\n")
	}
}

// formatVerbose dims the `*`, `>` and `<` lines curl speaks in and its
// `{ … bytes data]` chunk markers, colors a status line wherever it
// appears, reds anything that reads as a failure (including curl's own
// `curl: (6) …`), and lets the body that sits between the headers and
// the closing lines pass through as it came. The progress meter is
// dropped where it can only be meter, as in formatHeaders.
func formatVerbose(w io.Writer, text string) {
	lines := splitLines(text)
	seenStatus := false
	for _, line := range lines {
		if cleaned, metered := takeMeter(line, seenStatus); metered {
			if cleaned == "" {
				continue
			}
			line = cleaned
		}
		switch {
		case strings.HasPrefix(line, "curl: ("):
			line = tool.PaintColor(bold+" "+red, line)
		case reDataMark.MatchString(line):
			line = tool.PaintColor(dim, line)
		case strings.HasPrefix(line, "* "), line == "*",
			strings.HasPrefix(line, "> "), line == ">",
			strings.HasPrefix(line, "< "), line == "<":
			inner := strings.TrimPrefix(line, "* ")
			inner = strings.TrimPrefix(inner, "> ")
			inner = strings.TrimPrefix(inner, "< ")
			switch {
			case reStatus.MatchString(inner):
				if code := statusCode(inner); code != "" {
					seenStatus = true
					line = tool.PaintColor(bold+" "+statusColor(code), line)
				}
			case verboseError(line):
				line = tool.PaintColor(red, line)
			default:
				line = tool.PaintColor(dim, line)
			}
		}
		io.WriteString(w, line)
		io.WriteString(w, "\n")
	}
}

// formatError handles a failure curl reported on its own line: the
// meter that ran before it is dropped and the `curl: (n) …` line goes
// bold red. Only meter before the error is touched — anything after it
// is stdout, curl's own output, and stays byte for byte.
func formatError(w io.Writer, text string) {
	lines := splitLines(text)
	seenError := false
	for _, line := range lines {
		if !seenError {
			if cleaned, metered := takeMeter(line, false); metered {
				if cleaned == "" {
					continue
				}
				line = cleaned
			}
			if reCurlError.MatchString(line) {
				seenError = true
				line = tool.PaintColor(bold+" "+red, line)
			}
		}
		io.WriteString(w, line)
		io.WriteString(w, "\n")
	}
}

// Run implements `parse curl <args>`. Only stdout is formatted, and only
// when it is a response; stderr stays on stderr, so `parse curl -v` shows
// curl's verbose stream raw on the terminal while `curl -v … 2>&1 | parse`
// formats the combined text on the pipe. Docker's exit code is curl's.
func Run(args []string) int {
	text, code, started := tool.Capture("curl", args)
	if !started {
		return code
	}
	Format(os.Stdout, Detect(text), text)
	return code
}
