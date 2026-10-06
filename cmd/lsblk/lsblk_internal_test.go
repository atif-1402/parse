// Tests for the lsblk helpers: the header detector, the continuation-line
// test and the word coloring.
package lsblk

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

func lines(l ...string) string { return strings.Join(l, "\n") + "\n" }

const blkSample = `NAME     MAJ:MIN RM   SIZE RO TYPE  MOUNTPOINTS
sda        8:0    0 111.8G  0 disk
└─sda2     8:2    0 109.8G  0 part
  └─root 253:0    0 109.8G  0 crypt /var/log
                                    /home
zram0    252:0    0   7.6G  0 disk  [SWAP]
`

func TestDetectNeedsTheLsblkHeader(t *testing.T) {
	if got := Detect(blkSample); got != "lsblk" {
		t.Errorf("Detect = %q, want lsblk", got)
	}
	if got := Detect("USER PID COMMAND\nroot 1 init\n"); got != "" {
		t.Errorf("claimed foreign output: %q", got)
	}
}

func TestIsContinuationSpotsAWrappedMountpoint(t *testing.T) {
	if !isContinuation("                                    /home") {
		t.Error("a bare absolute path is a wrapped mountpoint")
	}
	if isContinuation("sda        8:0    0 111.8G  0 disk") {
		t.Error("a device row is not a continuation")
	}
	if isContinuation("└─root 253:0    0 109.8G  0 crypt /var/log") {
		t.Error("a tree row is not a continuation")
	}
}

func TestReplaceWordOnlyMatchesWholeWords(t *testing.T) {
	got := replaceWord("disk sda-disk disk", "disk", yellow)
	if strings.Count(got, "<yellow>") != 2 {
		t.Errorf("expected exactly two whole-word matches:\n%s", got)
	}
	if strings.Contains(got, "yellow>sda-disk") {
		t.Errorf("colored part of a longer word:\n%s", got)
	}
}

func TestReplaceWordKeepsSpacing(t *testing.T) {
	in := "0 109.8G 0 part  /boot"
	got := replaceWord(in, "part", dim)
	if !strings.Contains(got, "</>  /boot") {
		t.Errorf("spacing after the colored word changed:\n%q", got)
	}
}

func TestFormatIndentsWrappedMountpoints(t *testing.T) {
	var sb strings.Builder
	Format(&sb, blkSample)
	got := sb.String()
	var homeLine string
	for _, l := range strings.Split(got, "\n") {
		plain := strings.NewReplacer("<bold>", "", "<dim>", "", "<cyan>", "", "</>", "").Replace(l)
		if strings.TrimSpace(plain) == "/home" {
			homeLine = plain
		}
	}
	if homeLine == "" {
		t.Fatalf("lost the mountpoint:\n%s", got)
	}
	if !strings.HasPrefix(homeLine, " ") {
		t.Errorf("a wrapped mountpoint should be indented under its device:\n%s", got)
	}
}
