// Tests for the find -ls helpers: the row regex, the human size conversion,
// and the mode coloring.
package find

import (
	"fmt"
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
	tool.PrintTable = func(w io.Writer, headers []string, rows [][]tool.Cell) {
		fmt.Fprintln(w, strings.Join(headers, " | "))
	}
}

const lsRow = "     261      0 drwxr-xr-x   1 atif     atif         1486 Sep 29 23:13 /home/dev/.config"

func TestLsRERejectsNonFindOutput(t *testing.T) {
	if Detect(lsRow+"\n") != "ls" {
		t.Error("a real find -ls row should be detected")
	}
	// ls -l has a similar shape but no leading block count and a different
	// column set, so it must not be claimed.
	if got := Detect("total 4\n-rw-r--r-- 1 atif atif 4096 Sep 29 23:13 dir\n"); got != "" {
		t.Errorf("claimed foreign output: %q", got)
	}
}

func TestLsREKeepsAPathWithSpaces(t *testing.T) {
	row := "     1      0 -rw-r--r--   1 atif     atif     12 Sep 29 23:13 /tmp/a b/c d.txt"
	m := lsRE.FindStringSubmatch(row)
	if m == nil {
		t.Fatalf("did not match:\n%s", row)
	}
	if m[9] != "/tmp/a b/c d.txt" {
		t.Errorf("path = %q, want the whole path with its spaces", m[9])
	}
}

func TestHumanSize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"512", "512"},
		{"2048", "2K"},
		{"1572864", "1.5M"},
		{"1073741824", "1G"},
	}
	for _, c := range cases {
		if got := humanSize(c.in); got != c.want {
			t.Errorf("humanSize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsExecutableReadsTheExecuteBits(t *testing.T) {
	if !isExecutable("-rwxr-xr-x") {
		t.Error("rwxr-xr-x is executable")
	}
	if isExecutable("-rw-r--r--") {
		t.Error("rw-r--r-- is not executable")
	}
	if isExecutable("drwxr-xr-x") == false {
		t.Error("a directory has execute bits and should read as executable")
	}
}
