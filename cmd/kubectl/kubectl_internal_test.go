// Tests for the kubectl helpers: which reports are claimed, which are handed
// straight back, and how the table and the key/value block are laid out.
package kubectl

import (
	"io"
	"strings"
	"testing"

	"github.com/atif-1402/parse/internal/tool"
)

func init() {
	tool.Paint = func(color, s string) string {
		if color == "" || s == "" {
			return s
		}
		return "<" + color + ">" + s + "</>"
	}
	// The real printer lines the columns up; a join is enough to see which
	// values landed in which cell.
	tool.PrintTable = func(w io.Writer, headers []string, rows [][]tool.Cell) {
		io.WriteString(w, strings.Join(headers, " | ")+"\n")
		for _, r := range rows {
			cells := make([]string, len(r))
			for i, c := range r {
				cells[i] = tool.PaintColor(c.Color, c.Text)
			}
			io.WriteString(w, strings.Join(cells, " | ")+"\n")
		}
	}
}

func lines(l ...string) string { return strings.Join(l, "\n") + "\n" }

const podsSample = `NAME                                READY   STATUS              RESTARTS   AGE
nginx-deployment-67d4bdd6f5-w6kd7   1/1     Running             0          5d
broken-app-7d9c5f8b4-l2k8v            0/1     CrashLoopBackOff    47         19m
new-pod-8c4d6b9f5-q7w3e               0/1     Pending             0          45s
`

const wideSample = `NAME                                READY   STATUS    RESTARTS   AGE   IP           NODE            NOMINATED NODE   READINESS GATES
nginx-deployment-67d4bdd6f5-w6kd7   1/1     Running   0          5d    10.88.0.3    kube-worker-1   <none>           <none>
`

const describeSample = `Name:         nginx-67d4bdd6f5-w6kd7
Namespace:    default
Status:       Running
Ready:        True
Annotations:  <none>
State:        Waiting
  Reason:     ImagePullBackOff
Conditions:
  Type              Status
  Ready             False
Events:
  ----    ------     ----  ----     -------
  Normal  Scheduled  34s   default-scheduler  Assigned to kube-worker-1
  Warning  Failed      17m   kubelet  Error: ImagePullBackOff
`

func TestDetectClaimsGetAndDescribe(t *testing.T) {
	if got := Detect(podsSample); got != "get" {
		t.Errorf("Detect(pods) = %q, want get", got)
	}
	if got := Detect(describeSample); got != "describe" {
		t.Errorf("Detect(describe) = %q, want describe", got)
	}
	if got := Detect(wideSample); got != "get" {
		t.Errorf("Detect(wide) = %q, want get", got)
	}
}

// The other capitalised tables have to survive the sniffing, or parse would
// start rewriting output it knows nothing about.
func TestDetectLeavesOtherTablesAlone(t *testing.T) {
	cases := map[string]string{
		"lsblk":     lines("NAME     MAJ:MIN RM   SIZE RO TYPE  MOUNTPOINTS", "sda        8:0    0 111.8G  0 disk"),
		"docker ps": lines("CONTAINER ID   IMAGE   COMMAND   CREATED   STATUS    PORTS   NAMES", "abc123   nginx   \"x\"   5d   Up 5d   80/tcp   web"),
		"systemctl": lines("UNIT        LOAD   ACTIVE SUB     DESCRIPTION", "nginx.service loaded active running Nginx"),
		"ps":        lines("USER PID %CPU %MEM COMMAND", "root 1 0.0 0.1 init"),
	}
	for name, text := range cases {
		if got := Detect(text); got != "" {
			t.Errorf("Detect claimed %s output as %q", name, got)
		}
	}
}

func TestGetAllIsLeftAlone(t *testing.T) {
	// One table per kind, each with its own header: no single column set to
	// lay out, so it goes through untouched.
	all := lines(
		"NAME       READY   STATUS    RESTARTS   AGE",
		"pod/foo    1/1     Running   0          5d",
		"",
		"NAME       TYPE        CLUSTER-IP   AGE",
		"service/x  ClusterIP   10.96.0.1    30d",
	)
	if got := Detect(all); got != "" {
		t.Errorf("Detect(get all) = %q, want no claim", got)
	}
}

func TestRaggedRowsAreNotShredded(t *testing.T) {
	// A row that does not line up with the header would be cut mid-word if the
	// header's offsets were trusted, so the whole thing is handed back as is.
	ragged := lines("NAME   READY   STATUS", "foo    1/1     Running", "bar  0/1  CrashLoopBackOff")
	var b strings.Builder
	Format(&b, "get", ragged)
	if got := b.String(); got != ragged {
		t.Errorf("ragged table was rewritten:\ngot  %q\nwant %q", got, ragged)
	}
}

func TestGetTwoWordColumnsStayWhole(t *testing.T) {
	names, _ := parseHeader("NAME   READY   NOMINATED NODE   READINESS GATES")
	want := []string{"NAME", "READY", "NOMINATED NODE", "READINESS GATES"}
	if len(names) != len(want) {
		t.Fatalf("parseHeader = %q, want %q", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("column %d = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestWideTableIsLaidOut(t *testing.T) {
	var b strings.Builder
	if !formatGet(&b, wideSample) {
		t.Fatal("formatGet refused a well-formed wide table")
	}
	if !strings.Contains(b.String(), "<none> | <none>") {
		t.Errorf("wide row cells lost:\n%s", b.String())
	}
}

func TestGetTintsStatusAndReady(t *testing.T) {
	var b strings.Builder
	if !formatGet(&b, podsSample) {
		t.Fatal("formatGet refused a well-formed table")
	}
	out := b.String()
	for _, want := range []string{
		"<green>Running</>",        // the healthy pod
		"<red>CrashLoopBackOff</>", // the one you ran the command for
		"<yellow>Pending</>",
		"<green>1/1</>",
		"<red>0/1</>",
		"<yellow>47</>", // a restart count worth noticing
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in:\n%s", want, out)
		}
	}
}

func TestDescribeDimsKeysAndTintsStates(t *testing.T) {
	var b strings.Builder
	if !formatDescribe(&b, describeSample) {
		t.Fatal("formatDescribe refused a well-formed describe block")
	}
	out := b.String()
	for _, want := range []string{
		"<bold>Name:</>",           // the section headings a reader scans for
		"<bold>Namespace:</>",      // also at the left margin
		"<dim>Reason:</>",          // the keys nested inside one
		"<green>Running</>",        // Status
		"<green>True</>",           // Ready
		"<red>ImagePullBackOff</>", // the Reason you came for
		"<red>False</>",            // a failed condition, second column
		"<dim><none></>",           // nothing to see here
		"<yellow>Warning</>",       // the leading word of an event row
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in:\n%s", want, out)
		}
	}
}

// In the Conditions table the condition name comes first and its True/False
// second, so tinting the first word would colour "Ready" green on a pod that is
// not ready.
func TestDescribeConditionsTintsTheStatusNotTheName(t *testing.T) {
	var b strings.Builder
	if !formatDescribe(&b, describeSample) {
		t.Fatal("formatDescribe refused a well-formed describe block")
	}
	for _, line := range strings.Split(b.String(), "\n") {
		if strings.Contains(line, "False") && !strings.Contains(line, "<red>False</>") {
			t.Errorf("a False condition was left untinted: %q", line)
		}
	}
}

// A key with no value is a heading, and kubectl leaves it bare rather than
// padding it out to where a value would have started.
func TestDescribeHeadingsKeepNoTrailingSpace(t *testing.T) {
	for _, line := range strings.Split(mustFormat(t, describeSample), "\n") {
		if strings.HasSuffix(line, " ") {
			t.Errorf("line ends in a space: %q", line)
		}
	}
}

func mustFormat(t *testing.T, text string) string {
	t.Helper()
	var b strings.Builder
	Format(&b, "describe", text)
	return b.String()
}

// A wrapped event message that happens to contain a colon is not a key, and
// treating it as one would dim the message and lose the rest of the line.
func TestDescribeLeavesWrappedMessagesAlone(t *testing.T) {
	line := "  fit failure on node (kubernetes-node-6ta5): Node didn't have enough resource"
	if got := describeLine(line, ""); got != line {
		t.Errorf("describeLine(%q) = %q, want it unchanged", line, got)
	}
}

func TestDescribeKeepsContainerHashesQuiet(t *testing.T) {
	digest := "docker.io/library/nginx@sha256:2834dc507516af02784808c5f48b7cbe38b8ed5d0f4837f16e78d00deb7e7767"
	if got := describeValue(digest); !strings.HasPrefix(got, "<dim>") {
		t.Errorf("describeValue(digest) = %q, want it dimmed", got)
	}
	id := "containerd://5403af59a2b46ee5a23fb0ae4b1e077f7ca5c5fb7af16e1ab21c00e0e616462a"
	if got := describeValue(id); !strings.HasPrefix(got, "<dim>") {
		t.Errorf("describeValue(id) = %q, want it dimmed", got)
	}
}

func TestKubectlSubcommandSkipsFlagValues(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"get", "pods"}, "get"},
		{[]string{"describe", "pod/x"}, "describe"},
		{[]string{"-n", "prod", "get", "pods"}, "get"}, // the value is not the verb
		{[]string{"--context", "dev", "describe", "po"}, "describe"},
		{[]string{"apply", "-f", "x.yaml"}, "apply"},
		{[]string{}, ""},
	}
	for _, c := range cases {
		if got := kubectlSubcommand(c.args); got != c.want {
			t.Errorf("kubectlSubcommand(%q) = %q, want %q", c.args, got, c.want)
		}
	}
}

func TestFormattableLeavesOutputForProgramsAndTails(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"get", "pods"}, true},
		{[]string{"get", "pods", "-o", "wide"}, true},
		{[]string{"describe", "pod/x"}, true},
		{[]string{"get", "pods", "-o", "json"}, false},
		{[]string{"get", "pods", "-o", "yaml"}, false},
		{[]string{"get", "pods", "-o", "name"}, false},
		{[]string{"get", "pods", "-o", "jsonpath={.items[*].metadata.name}"}, false},
		{[]string{"get", "pods", "-o", "custom-columns=NAME:.metadata.name"}, false},
		{[]string{"get", "pods", "-ojson"}, false},
		{[]string{"get", "pods", "--template={{.x}}"}, false},
		{[]string{"get", "pods", "--no-headers"}, false},
		{[]string{"get", "pods", "-w"}, false},      // never ends
		{[]string{"get", "pods", "--watch"}, false}, // never ends
		{[]string{"get", "pods", "--watch-only"}, false},
	}
	for _, c := range cases {
		if got := formattable(c.args); got != c.want {
			t.Errorf("formattable(%q) = %v, want %v", c.args, got, c.want)
		}
	}
}

func TestFormatWritesUnrecognizedTextThrough(t *testing.T) {
	// What kubectl says when there is nothing to list, and when it cannot reach
	// the server. Neither is a table, so both go out byte for byte.
	for _, text := range []string{
		"No resources found in default namespace.\n",
		"The connection to the server localhost:8080 was refused\n",
	} {
		var b strings.Builder
		Format(&b, "get", text)
		if b.String() != text {
			t.Errorf("Format rewrote %q as %q", text, b.String())
		}
	}
}

// Everything here either reads the keyboard or never finishes, so in both cases
// a pager in front of it would trap Ctrl-C in the pager.
func TestNeedsTerminalForLiveAndInteractiveCommands(t *testing.T) {
	for _, args := range [][]string{
		{"logs", "-f"},
		{"logs", "--follow"},
		{"logs", "-f", "pod/api-0"},
		{"-n", "prod", "logs", "-f"},
		{"get", "pods", "-w"},
		{"get", "pods", "--watch"},
		{"get", "pods", "--watch-only"},
		{"exec", "-it", "api-0", "sh"},
		{"attach", "-it", "api-0"},
	} {
		if !NeedsTerminal(args) {
			t.Errorf("NeedsTerminal(%q) = false, want true", args)
		}
	}
	for _, args := range [][]string{
		nil,
		{"get", "pods"},
		{"describe", "pod", "api-0"},
		{"logs", "--tail=50"},
		{"logs", "pod/api-0"},
		// -f is --filename here, not follow, so nothing is streaming.
		{"-f", "manifest.yaml", "apply"},
	} {
		if NeedsTerminal(args) {
			t.Errorf("NeedsTerminal(%q) = true, want false", args)
		}
	}
}
