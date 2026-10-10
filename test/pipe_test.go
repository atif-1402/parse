// Tests for the piped-input contract: what parse must not touch, and what it
// must stay quiet about.
//
// The rule the whole file leans on is that output parse does not understand is
// passed through byte for byte. These tests are mostly about the ways that
// promise was being broken: machine-readable git formats being rewritten into
// nonsense, and an empty pipe being reported as an error.
package test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/atif-1402/parse/cmd"
)

// throughCmd runs text through the full dispatcher, which is what `<tool> | parse`
// uses. Unlike format() in git_test.go this does not go straight to git.Pipe, so
// it also proves that no other tool claims the text first.
func throughCmd(text string) string {
	var sb strings.Builder
	cmd.Pipe(&sb, text)
	return sb.String()
}

// passthrough asserts that text comes back completely unchanged. Anything less
// is a bug: a reader who piped machine-readable output expects the same bytes.
func passthrough(t *testing.T, name, text string) {
	t.Helper()
	if got := throughCmd(text); got != text {
		t.Errorf("%s was rewritten\n--- got ---\n%q\n--- want ---\n%q", name, got, text)
	}
}

// The machine-readable git formats. Each of these is a script's input, and
// rewriting one is worse than not formatting it at all.
//
// --numstat was the worst of them: the du detector claimed it, because its rows
// are "3\t2\tcmd/main.go" and du reads "3<TAB>anything" as a size and a path.
// The added/removed counts came back as sizes: "3K  2\tcmd/main.go".
func TestMachineFormatsSurviveIntact(t *testing.T) {
	passthrough(t, "numstat", lines(
		"3\t2\tcmd/main.go",
		"0\t1\tREADME.md",
		"12\t0\tnew.go",
	))
	passthrough(t, "numstat binary", lines(
		"-\t-\tlogo.png",
		"2\t3\told name.txt",
	))
	passthrough(t, "raw", lines(
		":100644 100644 abc1234 def5678 M\tcmd/main.go",
		":000000 100644 0000000 9917ab0 A\t.gitignore",
	))
	passthrough(t, "name-only", lines(
		"cmd/main.go",
		"README.md",
	))
	passthrough(t, "name-status", lines(
		"M\tcmd/main.go",
		"A\t.gitignore",
		"R100\told.go\tnew.go",
	))
	passthrough(t, "shortstat", lines(
		" 1 file changed, 2 insertions(+), 1 deletion(-)",
	))
	passthrough(t, "diff --stat", lines(
		" cmd/main.go | 4 ++--",
		" 1 file changed, 2 insertions(+), 2 deletions(-)",
	))
}

// --raw is safe above even though it looks like a diff, because its rows start
// with a colon and never with "diff --git".

// --word-diff opens with the same `diff --git` header a patch does but has no
// hunks, so it used to be accepted as a diff and printed under a fake "+0 -0"
// header with the interesting part left unformatted.
func TestWordDiffIsNotMistakenForAPatch(t *testing.T) {
	passthrough(t, "word-diff", lines(
		"diff --git a/main.go b/main.go",
		"index 111aaaa..222bbbb 100644",
		"--- a/main.go",
		"+++ b/main.go",
		"@@ -1,3 +1,3 @@",
		"[-old line-]{+new line+}",
	))
}

// Prose that happens to begin with the words `diff --git` is not a diff. This is
// what the README of a project explaining its own diff format would contain.
func TestProseBeginningWithDiffGitIsNotADiff(t *testing.T) {
	passthrough(t, "prose", lines(
		"diff --git is described in the docs, and the a/ b/ prefixes matter.",
		"See the notes file for details.",
	))
}

// Empty input is a normal answer, not a failure. `git diff` on a clean tree
// prints nothing, `git status --porcelain` prints nothing when nothing is
// staged, `git stash list` prints nothing when there are no stashes. parse used
// to print "no input on stdin" and exit 1, which told the user their command had
// failed when it had succeeded and there was simply nothing to report.
func TestEmptyInputIsSilent(t *testing.T) {
	if got := throughCmd(""); got != "" {
		t.Errorf("empty input produced %q, want nothing at all", got)
	}
}

// Whitespace-only input is passed through untouched, byte for byte, rather than
// swallowed or reported. A command that prints a bare newline printed a bare
// newline, and rewriting that would be the same class of bug as rewriting
// --numstat: parse changed output it did not understand. What matters is that
// none of it produces a complaint on stderr.
func TestWhitespaceOnlyInputIsPassedThroughNotComplainedAbout(t *testing.T) {
	for _, text := range []string{"\n", "  ", "\n\n", " \t\n"} {
		if got := throughCmd(text); got != text {
			t.Errorf("input %q became %q, want it unchanged", text, got)
		}
	}
}

// CRLF input used to keep its \r, so a hunk header and the lines under it
// disagreed about where a line ended and the block rendered as one long line.
func TestCRLFDiffIsNormalized(t *testing.T) {
	in := "diff --git a/main.go b/main.go\r\n" +
		"--- a/main.go\r\n" +
		"+++ b/main.go\r\n" +
		"@@ -1 +1 @@\r\n" +
		"-old\r\n" +
		"+new\r\n"
	got := throughCmd(in)
	if strings.Contains(got, "\r") {
		t.Errorf("a carriage return survived into the output: %q", got)
	}
	if !strings.Contains(got, "-old") || !strings.Contains(got, "+new") {
		t.Errorf("the diff body was lost: %q", got)
	}
}

// Foreign color is the user's own: they asked for `grep --color=always`, so
// parse has no business removing it. This is the other half of the contract
// above, and it is what makes the "byte for byte" promise complete.
func TestUnrecognizedColorIsLeftAlone(t *testing.T) {
	passthrough(t, "colored grep", "\x1b[31mred\x1b[0m\n\x1b[1mbold\x1b[0m\n")
}

// Ordinary files that happen to be piped in must not be claimed by a
// formatter. A go.mod is not a du listing just because it has numbers in it.
func TestOrdinaryTextIsNotClaimed(t *testing.T) {
	passthrough(t, "go.mod", lines(
		"module github.com/user/coldlock",
		"",
		"go 1.22.0",
		"",
		"require (",
		"\tgithub.com/spf13/cobra v1.8.0",
		")",
	))
	passthrough(t, "markdown prose", lines(
		"# coldlock",
		"",
		"Hard-lock distraction blocker for Linux. Block sites two ways.",
	))
}

// df formats its own table, so parse has nothing to add and must not claim
// either of its shapes. The pipe is where that choice is settled: there is no
// command line to read a flag from, so it rests on the text alone.
func TestDfOutputIsLeftAlone(t *testing.T) {
	passthrough(t, "df -h", lines(
		"Filesystem        Size  Used Avail Use% Mounted on",
		"dev               3.8G    0B  3.8G   0% /dev",
		"/dev/mapper/root 109.8G 68.8G 38.4G 65% /",
	))
	passthrough(t, "df -P", lines(
		"Filesystem     1024-blocks     Used Available Capacity Mounted on",
		"dev              3886860        0   3886860       0% /dev",
		"/dev/mapper/root 115104768 72152624  40286608      65% /",
	))
	passthrough(t, "df -i", lines(
		"Filesystem        Inodes IUsed   IFree IUse% Mounted on",
		"dev                971715   2129  949586   1% /dev",
		"/dev/mapper/root         0      0       0    - /",
	))
}

// free is claimed on the strength of its header alone: the text cannot say
// which form printed it, so the unit is assumed to be free's own default, the
// way du's is.
func TestFreeIsClaimedInThePipe(t *testing.T) {
	got := throughCmd(freeSample)
	if !strings.Contains(got, "7.6G") {
		t.Errorf("free was not claimed:\n%s", got)
	}
}

// lsof is claimed on the strength of its heading: COMMAND first, NAME last,
// the owner of the process between them. Everything else about the text — the
// arrow in an address, the bracket at the end — is left to the formatter.
func TestLsofIsClaimedInThePipe(t *testing.T) {
	got := throughCmd(lsofSample)
	if !strings.Contains(got, "<bold>COMMAND</>") {
		t.Errorf("lsof was not claimed:\n%q", got)
	}
	if !strings.Contains(got, "<green>(ESTABLISHED)</>") {
		t.Errorf("the socket state was not marked:\n%q", got)
	}
}

// `-F` prints one field per line with no heading to claim it by, and `-t`
// prints pids and nothing else. Neither is a table, and rewriting either is
// worse than leaving it alone.
func TestLsofScriptFormsAreLeftAlone(t *testing.T) {
	passthrough(t, "lsof -F", lines(
		"p",
		"769",
		"f",
		"cwd",
		"n/proc/769/cwd",
	))
	passthrough(t, "lsof -t", lines("769", "784"))
}

// `free -L` prints one line of name-and-value pairs with no header to claim it
// by, and no unit parse can safely add.
func TestFreeSingleLineFormIsLeftAlone(t *testing.T) {
	passthrough(t, "free -L", lines(
		"SwapUse      816644 CachUse     2993844  MemUse     5631420 MemFree      472552",
	))
}

// findmnt is claimed on the strength of its heading: its own column names in
// capitals, which df — the table a user is most likely to pipe beside it —
// spells in lower case.
func TestFindmntIsClaimedInThePipe(t *testing.T) {
	got := throughCmd(findmntSample)
	if !strings.Contains(got, "<bold>TARGET</>") {
		t.Errorf("findmnt was not claimed:\n%q", got)
	}
	if !strings.Contains(got, "<dim>├─</>") {
		t.Errorf("the mount tree was not marked:\n%q", got)
	}
}

// Raw output separates its fields with one space and no padding, so there are
// no columns to read by; JSON and output with no heading have nothing to claim
// them by at all. None of them may be rewritten.
func TestFindmntMachineFormsAreLeftAlone(t *testing.T) {
	passthrough(t, "findmnt -r", lines(
		"TARGET SOURCE FSTYPE OPTIONS",
		"/proc proc proc rw,nosuid,nodev,noexec,relatime",
		"/sys sysfs sysfs rw,nosuid,nodev,noexec,relatime",
	))
	passthrough(t, "findmnt -n", lines(
		"/       /dev/mapper/root[/@] btrfs rw,relatime,compress=zstd:3",
		"├─/proc proc proc rw,nosuid,nodev,noexec,relatime",
	))
	passthrough(t, "findmnt -J", lines(
		`{`,
		`    "filesystems": [`,
		`        { "target": "/", "fstype": "btrfs" }`,
		`    ]`,
		`}`,
	))
}

// lsblk's own column selection prints a heading made entirely of names
// findmnt also uses, which is a table neither tool may claim: lsblk refuses
// it for lacking NAME, and findmnt must refuse it too rather than paint
// another tool's output with its palette. The text passes through as it came.
func TestLsblkColumnSelectionIsLeftAlone(t *testing.T) {
	passthrough(t, "lsblk -o FSTYPE,SIZE", lines(
		"FSTYPE        SIZE",
		"            111.8G",
		"vfat            2G",
		"crypto_LUKS 109.8G",
		"btrfs       109.8G",
	))
	passthrough(t, "lsblk -o LABEL,UUID", lines(
		"LABEL                              UUID",
		"                                  1234-5678",
		"root                        abcd-ef01-2345",
	))
	// The df-style heading holds SOURCE, USE% and TARGET — names lsblk does
	// not have — so findmnt's own -D form is still claimed.
	got := throughCmd(findmntDfSample)
	if !strings.Contains(got, "<bold>SOURCE</>") {
		t.Errorf("findmnt -D was not claimed:\n%q", got)
	}
}

// docker ps is claimed by a heading that opens with the two-word column
// "CONTAINER ID", so the parser uses runs of two spaces for the boundaries and
// leaves the single space inside a value alone.
func TestDockerPsIsClaimedInThePipeNew(t *testing.T) {
	got := throughCmd(lines(
		`CONTAINER ID   IMAGE     COMMAND                  CREATED              STATUS              PORTS     NAMES`,
		`804cf2759ae1   alpine    "sleep 1000"             38 seconds ago       Up 37 seconds                 demo-sleep`,
		`80dff6f6d71a   nginx     "/docker-entrypoint.…"   About a minute ago   Up About a minute   80/tcp    demo-web`,
	))
	// New format: NAME, STATE, IMAGE, PORTS, CREATED, ID
	if !strings.Contains(got, `<bold>NAME`) {
		t.Errorf("the docker heading was not claimed:\n%q", got)
	}
	if !strings.Contains(got, `<green>running 37s`) {
		t.Errorf("the docker STATUS was not tinted:\n%q", got)
	}
}

// The ps heading only counts when it is the first non-empty line. A diff that
// quotes a "CONTAINER ID ... NAMES" line mid-text belongs to git: claiming it
// as docker output used to swallow the whole diff (zero rows rendered as an
// empty table) or panic while parsing the quoted header.
func TestGitDiffQuotingPsHeadingStaysWithGit(t *testing.T) {
	got := throughCmd(lines(
		`diff --git a/example_test.go b/example_test.go`,
		`index 1111111..2222222 100644`,
		`--- a/example_test.go`,
		`+++ b/example_test.go`,
		`@@ -1,2 +1,3 @@`,
		` package test`,
		"+\t`CONTAINER ID   IMAGE     COMMAND   CREATED   STATUS   PORTS     NAMES`,",
	))
	if !strings.Contains(got, `example_test.go`) || !strings.Contains(got, `+1 -0`) {
		t.Errorf("the diff was not formatted by git:\n%q", got)
	}
	if strings.Contains(got, `diff --git`) {
		t.Errorf("the raw diff header survived, so nothing claimed the text:\n%q", got)
	}
	if strings.TrimSpace(got) == "" {
		t.Error("the diff was swallowed to empty output")
	}
}

// A script asking docker for a machine-readable form must get the same bytes
// back, even though the subcommand is ps.
func TestDockerScriptFormsAreLeftAloneNew(t *testing.T) {
	passthrough(t, "ps -q", lines(
		"804cf2759ae1",
		"80dff6f6d71a",
	))
	passthrough(t, "ps --format", lines(
		"804cf2759ae1\tdemo-sleep",
		"80dff6f6d71a\tdemo-web",
	))
}

// docker inspect's pretty JSON is claimed by keys only docker prints, so the
// pipe path colors it without swallowing another tool's JSON.
func TestDockerInspectIsClaimedInThePipeNew(t *testing.T) {
	got := throughCmd(lines(
		`[`,
		`    {`,
		`        "Id": "80dff6f6d71aa1b23ecded391328a1a8ecd92e229fd5fff52df489a2e4cd5fe5",`,
		`        "Name": "/demo-web",`,
		`        "State": {`,
		`            "Status": "running",`,
		`            "ExitCode": 0`,
		`        },`,
		`        "Config": {`,
		`            "Image": "nginx:latest",`,
		`            "Env": ["PATH=/usr/bin"]`,
		`        },`,
		`        "HostConfig": {}`,
		`    }`,
		`]`,
	))
	if !strings.Contains(got, `state`) {
		t.Errorf("docker inspect was not claimed:\n%q", got)
	}
	if !strings.Contains(got, `<green>running`) {
		t.Errorf("the inspect status was not tinted:\n%q", got)
	}
}

// A script asking docker for a template gets template output, not JSON. Even
// when it looks like JSON it is one compact line and must pass through.
func TestDockerInspectFormatFormsAreLeftAloneNew(t *testing.T) {
	passthrough(t, "inspect -f", "running\n")
	passthrough(t, "inspect --format json", `{"Status":"running","Running":true,"ExitCode":0}`+"\n")
}

// dfTable pads each cell the way docker does — rune counts, two spaces of
// breathing room — so the parser's column starts line up.
func dfTable(header []string, rows [][]string) string {
	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = len([]rune(h)) + 2
	}
	for _, row := range rows {
		for i, c := range row {
			if n := len([]rune(c)) + 2; n > widths[i] {
				widths[i] = n
			}
		}
	}
	line := func(cells []string) string {
		var b strings.Builder
		for i, c := range cells {
			b.WriteString(c)
			if i < len(cells)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-len([]rune(c))))
			}
		}
		return b.String()
	}
	var b strings.Builder
	b.WriteString(line(header))
	b.WriteByte('\n')
	for _, row := range rows {
		b.WriteString(line(row))
		b.WriteByte('\n')
	}
	return b.String()
}

// `docker system df --format '{{.Type}}'` prints one word per line, which no
// detector may claim: a script asked for machine-readable output and must
// get it back byte for byte.
func TestDockerSystemDFFormatTemplateIsLeftAlone(t *testing.T) {
	passthrough(t, "df --format template", lines(
		"Images",
		"Containers",
		"Local Volumes",
		"Build Cache",
	))
}

// `docker system df -v` gets formatted for a pipe, and the README documents
// the shape: the summary leads with build cache included, tables sort by
// size with state and name as tiebreaks, the containers table answers
// "which containers take space", the empty ones collapse to one dim line,
// and the build cache is a line of its own. This fixture carries the rows
// this box's docker does not produce: a dangling image, an unlinked volume
// next to a linked one, an all-zero SHARED SIZE column, and a build cache
// with no rows.
func TestDockerSystemDFFormatsThePipe(t *testing.T) {
	images := dfTable(
		[]string{"REPOSITORY", "TAG", "IMAGE ID", "CREATED", "SIZE", "SHARED SIZE", "UNIQUE SIZE", "CONTAINERS"},
		[][]string{
			{"<none>", "<none>", "deadbeef1234", "2 days ago", "76.9MB", "0B", "62.98MB", "0"},
			{"nginx", "latest", "f9ea18bfa4fa", "4 days ago", "243MB", "0B", "242.7MB", "5"},
			{"busybox", "1.36", "73aaf090f3d8", "3 years ago", "6.68MB", "0B", "6.679MB", "0"},
		},
	)
	containers := dfTable(
		[]string{"CONTAINER ID", "IMAGE", "COMMAND", "LOCAL VOLUMES", "SIZE", "CREATED", "STATUS", "NAMES"},
		[][]string{
			{"aaaabbbbcccc", "nginx", `"/docker-en…"`, "0", "8.19kB", "3 hours ago", "Up 3 hours (unhealthy)", "fx-run"},
			{"dddeeefff000", "alpine", `"sleep 10"`, "0", "2.5MB", "5 hours ago", "Exited (1) 4 hours ago", "fx-big"},
			{"111122223333", "alpine", `"sleep 1"`, "0", "0B", "5 hours ago", "Created", "fx-zero"},
		},
	)
	volumes := dfTable(
		[]string{"VOLUME NAME", "LINKS", "SIZE"},
		[][]string{
			{"fx-vol-used", "1", "52.43MB"},
			{"fx-vol-unused", "0", "0B"},
		},
	)
	cache := dfTable(
		[]string{"CACHE ID", "CACHE TYPE", "SIZE", "CREATED", "LAST USED", "USAGE", "SHARED"},
		nil,
	)
	stdin := "Images space usage:\n\n" + images +
		"\nContainers space usage:\n\n" + containers +
		"\nLocal Volumes space usage:\n\n" + volumes +
		"\nBuild cache usage: 0B\n\n" + cache

	out := runParse(t, stdin)

	first := strings.SplitN(out, "\n", 2)[0]
	want := "~images 326.6MB · volumes 52.43MB · ~containers 2.508MB · ~build cache 0B · reclaimable 83.58MB"
	if first != want {
		t.Errorf("first line = %q, want %q", first, want)
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("a pipe got escape codes:\n%q", out)
	}
	if strings.Contains(out, "SHARED SIZE") {
		t.Errorf("the all-zero SHARED SIZE column survived:\n%s", out)
	}
	if strings.Contains(out, "CACHE ID") {
		t.Errorf("the empty build cache printed its table:\n%s", out)
	}
	if !strings.Contains(out, "build cache: empty") {
		t.Errorf("the empty build cache has no line of its own:\n%s", out)
	}
	if !strings.Contains(out, "dangling unused") {
		t.Errorf("the <none> image was not tagged:\n%s", out)
	}
	if !strings.Contains(out, "exited 1 4h ago") {
		t.Errorf("the stopped container's status was not split like ps:\n%s", out)
	}
	if !strings.Contains(out, "1 container at 0B (1 created)") {
		t.Errorf("the empty container did not collapse into the dim line:\n%s", out)
	}
	if strings.Contains(out, "CONTAINER ID") || strings.Contains(out, "COMMAND") {
		t.Errorf("the containers table is repeating ps:\n%s", out)
	}
	nginx := strings.Index(out, "f9ea18bfa4fa")
	dangling := strings.Index(out, "deadbeef1234")
	busybox := strings.Index(out, "73aaf090f3d8")
	if nginx < 0 || dangling < 0 || busybox < 0 || !(nginx < dangling && dangling < busybox) {
		t.Errorf("the images table is not sorted by size:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "fx-vol-used") && strings.Contains(line, "unused") {
			t.Errorf("the linked volume was tagged unused: %q", line)
		}
		if strings.HasPrefix(strings.TrimSpace(line), "nginx") && strings.Contains(line, "unused") {
			t.Errorf("the used image was tagged unused: %q", line)
		}
	}
}

// A JSON array with none of docker's keys belongs to someone else.
func TestOtherToolsJSONIsLeftAloneDockerNew(t *testing.T) {
	passthrough(t, "kubectl -o json", lines(
		`[`,
		`    {`,
		`        "apiVersion": "v1",`,
		`        "kind": "Pod",`,
		`        "metadata": {"name": "web"},`,
		`    }`,
		`]`,
	))
}

// docker network inspect's pretty JSON is claimed by keys only docker prints,
// so the pipe path colors it without swallowing another tool's JSON.
func TestDockerNetworkInspectIsClaimedInThePipeNew(t *testing.T) {
	got := throughCmd(lines(
		`[`,
		`    {`,
		`        "Name": "bridge",`,
		`        "Id": "abc123net",`,
		`        "Driver": "bridge",`,
		`        "IPAM": {`,
		`            "Driver": "default",`,
		`            "Config": [`,
		`                {"Subnet": "172.17.0.0/16", "Gateway": "172.17.0.1"}`,
		`            ]`,
		`        }`,
		`    }`,
		`]`,
	))
	if !strings.Contains(got, `bridge`) {
		t.Errorf("network inspect JSON was not claimed:\n%q", got)
	}
}

// Black box, the real binary: network inspect JSON comes out as a header
// plus the attached containers table, with the id and unset flags hidden.
func TestDockerNetworkInspectFormatsThePipe(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain")
	}
	fix := lines(
		`[`,
		`    {`,
		`        "Name": "appnet",`,
		`        "Id": "abc123netid",`,
		`        "Scope": "local",`,
		`        "Driver": "bridge",`,
		`        "EnableIPv6": false,`,
		`        "Internal": false,`,
		`        "Attachable": false,`,
		`        "IPAM": {"Driver": "default", "Config": [{"Subnet": "172.20.0.0/16", "Gateway": "172.20.0.1"}]},`,
		`        "Containers": {`,
		`            "c1": {"Name": "zeta", "MacAddress": "aa:bb:cc:dd:ee:01", "IPv4Address": "172.20.0.10/16", "IPv6Address": ""},`,
		`            "c2": {"Name": "alpha", "MacAddress": "aa:bb:cc:dd:ee:02", "IPv4Address": "172.20.0.11/16", "IPv6Address": ""}`,
		`        }`,
		`    }`,
		`]`,
	)
	out := runParse(t, fix)
	if !strings.Contains(out, "appnet  bridge, local") {
		t.Errorf("header missing:\n%s", out)
	}
	if !strings.Contains(out, "subnet   172.20.0.0/16") || !strings.Contains(out, "gateway  172.20.0.1") {
		t.Errorf("subnet/gateway missing:\n%s", out)
	}
	if strings.Index(out, "alpha") > strings.Index(out, "zeta") || strings.Index(out, "alpha") < 0 {
		t.Errorf("table not sorted by name:\n%s", out)
	}
	for _, hidden := range []string{"abc123netid", "internal", "attachable", "EndpointID"} {
		if strings.Contains(out, hidden) {
			t.Errorf("%q leaked:\n%s", hidden, out)
		}
	}
	empty := runParse(t, "[]")
	if empty != "[]" {
		t.Errorf("empty array mangled: %q", empty)
	}
}

// runParse builds the real binary and runs it with the given stdin and
// arguments, returning stdout. It is black-box on purpose: --show-secrets is
// parsed by main before the piped path starts, which is exactly the wiring an
// in-process cmd.Pipe call would skip.
func runParse(t *testing.T, stdin string, args ...string) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain")
	}
	bin := t.TempDir() + "/parse"
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = ".."
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("building parse: %v", err)
	}
	c := exec.Command(bin, args...)
	c.Stdin = strings.NewReader(stdin)
	var out, errb strings.Builder
	c.Stdout = &out
	c.Stderr = &errb
	if err := c.Run(); err != nil {
		t.Fatalf("running parse %v: %v\nstderr: %s", args, err, errb.String())
	}
	return out.String()
}

// inspectSecretsFixture is docker inspect JSON for a container whose env
// carries the three shapes the renderer masks: a PASSWORD key, a TOKEN key,
// and a URL with a password in it.
var inspectSecretsFixture = lines(
	`[`,
	`    {`,
	`        "Id": "aaaabbbbccccdddd0000111122223333444455556666777788889999",`,
	`        "Name": "/fx-secrets",`,
	`        "State": {`,
	`            "Status": "exited",`,
	`            "Running": false,`,
	`            "ExitCode": 0,`,
	`            "StartedAt": "2026-10-10T12:00:00.000000000Z",`,
	`            "FinishedAt": "2026-10-10T12:01:00.000000000Z"`,
	`        },`,
	`        "Image": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",`,
	`        "Config": {`,
	`            "Image": "alpine:3.19",`,
	`            "Env": [`,
	`                "PATH=/usr/local/sbin:/usr/local/bin",`,
	`                "DB_PASSWORD=s3cr3t",`,
	`                "API_TOKEN=tok_abcdef123456",`,
	`                "DATABASE_URL=postgres://user:pass@host/db",`,
	`                "APP_MODE=prod"`,
	`            ]`,
	`        },`,
	`        "HostConfig": { "NetworkMode": "bridge" },`,
	`        "NetworkSettings": { "Ports": {}, "Networks": {} }`,
	`    }`,
	`]`,
)

// --show-secrets must reach the piped path: main claims the flag before a tool
// is named, hands it to the docker renderer, and without it all three secret
// shapes stay masked while the note points back at the flag.
func TestPipeShowSecretsFlag(t *testing.T) {
	masked := runParse(t, inspectSecretsFixture)
	shown := runParse(t, inspectSecretsFixture, "--show-secrets")

	for _, want := range []string{
		"DB_PASSWORD=********",
		"API_TOKEN=********",
		"postgres://user:********@host/db",
		"(secrets masked, use --show-secrets)",
	} {
		if !strings.Contains(masked, want) {
			t.Errorf("masked pipe output missing %q:\n%s", want, masked)
		}
	}
	for _, real := range []string{"s3cr3t", "tok_abcdef123456", "user:pass@"} {
		if strings.Contains(masked, real) {
			t.Errorf("masked pipe output leaked %q:\n%s", real, masked)
		}
	}

	for _, want := range []string{
		"DB_PASSWORD=s3cr3t",
		"API_TOKEN=tok_abcdef123456",
		"postgres://user:pass@host/db",
	} {
		if !strings.Contains(shown, want) {
			t.Errorf("--show-secrets pipe output missing %q:\n%s", want, shown)
		}
	}
	if strings.Contains(shown, "********") {
		t.Errorf("--show-secrets pipe output still masks values:\n%s", shown)
	}
	if strings.Contains(shown, "(secrets masked") {
		t.Errorf("--show-secrets pipe output still carries the masked note:\n%s", shown)
	}
}
