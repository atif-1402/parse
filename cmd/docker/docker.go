// Package docker formats docker output. Two subcommands are understood so far:
// `docker ps` (and `ps -a`), whose table gets a bold heading, a STATUS tinted
// by state and a tinted published port; and `docker inspect`, whose JSON gets
// colored keys, values and a few state-aware fields. Every other subcommand,
// including the machine forms of either and the streaming of `docker logs -f`,
// is handed through byte for byte.
package docker

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/atif-1402/parse/internal/tool"
)

const (
	bold   = tool.Bold
	dim    = tool.Dim
	red    = tool.Red
	green  = tool.Green
	yellow = tool.Yellow
	cyan   = tool.Cyan
)

// Run implements `parse docker <args>`.
func Run(args []string) int {
	sub := verb(args)
	if !reformat(sub, args) {
		return tool.Passthrough("docker", args)
	}

	if sub == "ps" && !isPipe() {
		return runPSDirect(args)
	}

	if sub == "inspect" && !isPipe() {
		return runInspectDirect(args)
	}

	text, code, started := tool.Capture("docker", args)
	if !started {
		return code
	}
	Format(os.Stdout, sub, text)
	return code
}

func isPipe() bool {
	stat, _ := os.Stdin.Stat()
	return (stat.Mode() & os.ModeCharDevice) == 0
}

func runPSDirect(args []string) int {
	dockerArgs := []string{"ps", "--format", "json"}
	for _, a := range args {
		if a != "ps" {
			dockerArgs = append(dockerArgs, a)
		}
	}
	text, code, started := tool.Capture("docker", dockerArgs)
	if !started {
		return code
	}

	var containers []Container
	dec := json.NewDecoder(strings.NewReader(text))
	for dec.More() {
		var raw map[string]interface{}
		if err := dec.Decode(&raw); err != nil {
			break
		}
		containers = append(containers, NormalizeContainer(raw))
	}

	containers = SortContainers(containers)
	output := RenderPS(containers)
	fmt.Fprint(os.Stdout, output)
	return code
}

func runInspectDirect(args []string) int {
	dockerArgs := []string{"inspect"}
	fromTool := false
	for _, a := range args {
		switch {
		case a == "inspect":
			// skip the subcommand itself
		case a == "--show-secrets":
			fromTool = true
		default:
			dockerArgs = append(dockerArgs, a)
		}
	}
	text, code, started := tool.Capture("docker", dockerArgs)
	if !started {
		return code
	}

	var raw []map[string]interface{}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		fmt.Fprint(os.Stdout, text)
		return code
	}

	views := NormalizeInspect(raw)
	applyShowSecrets(views, showSecrets || fromTool)
	output := RenderInspect(views)
	if output == "" {
		output = text
	}
	fmt.Fprint(os.Stdout, output)
	return code
}

// showSecrets is the CLI's --show-secrets, set once by cmd.SetShowSecrets
// before any formatting. It reaches the piped path through Format; the direct
// path also accepts the flag after `inspect`, where it belongs to docker.
var showSecrets bool

// SetShowSecrets records whether real env values must be shown. Called once
// from main via cmd.SetShowSecrets.
func SetShowSecrets(v bool) { showSecrets = v }

// applyShowSecrets flips every view to render real values instead of masks.
func applyShowSecrets(views []InspectView, show bool) {
	if !show {
		return
	}
	for i := range views {
		views[i].Env.ShowSecrets = true
	}
}

var managerCommands = map[string]bool{
	"container": true, "image": true, "network": true, "volume": true,
	"plugin": true, "node": true, "service": true, "secret": true,
	"config": true, "context": true, "system": true,
}

func verb(args []string) string {
	first := ""
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if first == "" {
			first = a
			if !managerCommands[a] {
				return a
			}
			continue
		}
		return a
	}
	return ""
}

func reformat(sub string, args []string) bool {
	switch sub {
	case "ps":
		return !psScriptForm(args)
	case "inspect":
		return !inspectScriptForm(args)
	}
	return false
}

func psScriptForm(args []string) bool {
	for _, a := range args {
		switch {
		case a == "-q", a == "--quiet", a == "--format", strings.HasPrefix(a, "--format="):
			return true
		}
	}
	return false
}

func inspectScriptForm(args []string) bool {
	for _, a := range args {
		switch {
		case a == "-f", a == "--format", strings.HasPrefix(a, "--format="):
			return true
		}
	}
	return false
}

func NeedsTerminal(args []string) bool {
	switch verb(args) {
	case "events", "attach", "exec":
		return true
	case "logs":
		return hasFlag(args, "-f") || hasFlag(args, "--follow")
	case "stats":
		return !hasFlag(args, "--no-stream")
	case "run":
		return interactive(args)
	}
	return false
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func interactive(args []string) bool {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			continue
		}
		if strings.HasPrefix(a, "--") {
			if a == "--interactive" || a == "--tty" {
				return true
			}
			continue
		}
		if strings.ContainsAny(a, "it") {
			return true
		}
	}
	return false
}

func Detect(text string) string {
	if _, _, ok := psHeader(strings.Split(text, "\n")); ok {
		return "ps"
	}
	if looksLikeInspect(text) {
		return "inspect"
	}
	return ""
}

// formatHook lets tests force a panic inside Format to exercise the recover.
var formatHook func(sub, text string)

func Format(w io.Writer, sub, text string) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "parse: warning: docker %s formatter panicked: %v\n", sub, r)
			io.WriteString(w, text)
		}
	}()
	if formatHook != nil {
		formatHook(sub, text)
	}
	switch sub {
	case "ps":
		formatPS(w, text)
	case "inspect":
		formatInspect(w, text)
	default:
		io.WriteString(w, text)
	}
}

// psHeader locates the docker ps heading. It must be the first non-empty
// line: a heading found anywhere mid-text (a quoted "CONTAINER ID ... NAMES"
// line inside a git diff, say) would steal that text from its real formatter.
func psHeader(lines []string) (int, []int, bool) {
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.Contains(line, "CONTAINER ID") && strings.Contains(line, "NAMES") {
			starts := dfColumnStarts(line)
			return i, starts, starts != nil
		}
		return -1, nil, false
	}
	return -1, nil, false
}

// looksLikeInspect claims only JSON that is an array of objects shaped like
// `docker inspect` output: containers (Id, State, Config) or networks
// (Driver, IPAM). Anything else — ss -o json, ip -j, kubectl — passes
// through untouched.
func looksLikeInspect(text string) bool {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "[") {
		return false
	}
	var arr []map[string]interface{}
	if err := json.Unmarshal([]byte(text), &arr); err != nil || len(arr) == 0 {
		return false
	}
	first := arr[0]
	return hasAllKeys(first, "Id", "State", "Config")
}

func hasAllKeys(m map[string]interface{}, keys ...string) bool {
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return false
		}
	}
	return true
}

func formatPS(w io.Writer, text string) {
	lines := strings.Split(text, "\n")
	headIdx, starts, ok := psHeader(lines)
	if !ok {
		io.WriteString(w, text)
		return
	}
	fields := headerFields(lines[headIdx], starts)
	var containers []Container
	for i := headIdx + 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		c := parsePSRow(line, starts, fields)
		if c.Name != "" {
			containers = append(containers, c)
		}
	}
	// A heading with no rows under it renders as an empty table, which would
	// swallow the input. Text that parsed to nothing is text this formatter
	// does not understand, so it goes back untouched.
	if len(containers) == 0 && strings.TrimSpace(text) != "" {
		io.WriteString(w, text)
		return
	}
	containers = SortContainers(containers)
	output := RenderPS(containers)
	io.WriteString(w, output)
}

func formatInspect(w io.Writer, text string) {
	var raw []map[string]interface{}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		io.WriteString(w, text)
		return
	}
	views := NormalizeInspect(raw)
	applyShowSecrets(views, showSecrets)
	output := RenderInspect(views)
	if output == "" {
		io.WriteString(w, text)
		return
	}
	io.WriteString(w, output)
}

// dfColumnStarts computes column start positions from a header line.
// Uses 2+ consecutive spaces as column separators to preserve multi-word headers.
func dfColumnStarts(header string) []int {
	var starts []int
	runes := []rune(header)
	inCol := false
	for i, r := range runes {
		if r != ' ' && !inCol {
			starts = append(starts, i)
			inCol = true
		} else if r == ' ' {
			// Check if next char is also space (2+ spaces = column separator)
			if i+1 < len(runes) && runes[i+1] == ' ' {
				inCol = false
			}
		}
	}
	return starts
}

func headerFields(header string, starts []int) []string {
	var fields []string
	runes := []rune(header)
	for i, start := range starts {
		end := len(runes)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		field := strings.TrimSpace(string(runes[start:end]))
		fields = append(fields, field)
	}
	return fields
}

func cellBounds(runes []rune, starts []int, col int) (int, int) {
	if col >= len(starts) {
		return 0, 0
	}
	lo := starts[col]
	hi := len(runes)
	if col+1 < len(starts) {
		hi = starts[col+1]
	}
	return lo, hi
}

// columnStarts is an alias for dfColumnStarts for test compatibility.
func columnStarts(header string) []int {
	return dfColumnStarts(header)
}

// labels is an alias for headerFields for test compatibility.
func labels(header string, starts []int) []string {
	return headerFields(header, starts)
}

// indexOf returns the index of val in slice, or -1 if not found.
func indexOf(slice []string, val string) int {
	for i, s := range slice {
		if s == val {
			return i
		}
	}
	return -1
}
