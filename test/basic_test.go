// Package test checks the basic utilities end to end through the exported
// Pipe entry points, using the same text the real commands print.
//
// Most tests use each tool's own format helper rather than going through
// cmd.Pipe, because cmd.Pipe guesses which tool produced the text and falls back
// to raw passthrough when nothing matches. That way each formatter is exercised
// directly. The tests at the bottom go through cmd.Pipe, because the dispatcher
// itself is where the two ways of using parse can drift apart.
//
// Colors are replaced by readable tags so expectations are easy to read:
// Paint("green", "x") becomes "<green>x</>".
package test

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/atif-1402/parse/cmd"
	"github.com/atif-1402/parse/cmd/du"
	"github.com/atif-1402/parse/cmd/find"
	"github.com/atif-1402/parse/cmd/findmnt"
	"github.com/atif-1402/parse/cmd/free"
	"github.com/atif-1402/parse/cmd/ip"
	"github.com/atif-1402/parse/cmd/lsblk"
	"github.com/atif-1402/parse/cmd/lsof"
	"github.com/atif-1402/parse/cmd/ps"
	"github.com/atif-1402/parse/cmd/ss"
	"github.com/atif-1402/parse/internal/tool"
)

func init() {
	paint := func(color, s string) string {
		if color == "" || s == "" {
			return s
		}
		return "<" + color + ">" + s + "</>"
	}
	printTable := func(w io.Writer, headers []string, rows [][]tool.Cell) {
		fmt.Fprintln(w, strings.Join(headers, " | "))
		for _, row := range rows {
			parts := make([]string, len(row))
			for i, c := range row {
				parts[i] = paint(c.Color, c.Text)
			}
			fmt.Fprintln(w, strings.Join(parts, " | "))
		}
	}
	tool.Paint = paint
	tool.PrintTable = printTable
}

// pipe runs text through a formatter and returns it without the trailing
// newline.
func via(fn func(io.Writer, string), input string) string {
	var sb strings.Builder
	fn(&sb, input)
	return strings.TrimRight(sb.String(), "\n")
}

func viaSub(fn func(io.Writer, string, string), sub, input string) string {
	var sb strings.Builder
	fn(&sb, sub, input)
	return strings.TrimRight(sb.String(), "\n")
}

// stripTags removes the <color> and </> markers the test hooks add, leaving
// the plain text so it can be compared against the original input.
func stripTags(s string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, "<")
		j := strings.Index(s, ">")
		if i < 0 || j < 0 || j < i {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		s = s[j+1:]
	}
}

// ---------------------------------------------------------------------------
// ps
// ---------------------------------------------------------------------------

func TestPsDimsBusyColumnsAndKeepsCommand(t *testing.T) {
	in := lines(
		"USER         PID %CPU %MEM    VSZ   RSS TTY      STAT START   TIME COMMAND",
		"root           1  0.0  0.1  20872 13384 ?        Ss   Oct03   0:28 /sbin/init splash",
		"atif       18853 33.3 12.3 75936584 990172 pts/0 Rl+  Oct03 172:33 opencode",
	)
	got := via(ps.Pipe, in)
	if !strings.Contains(got, "opencode") {
		t.Errorf("lost the command:\n%s", got)
	}
	if !strings.Contains(got, "<yellow>") {
		t.Errorf("a busy CPU should stand out:\n%s", got)
	}
}

func TestPsKeepsLongCommandOnTheLine(t *testing.T) {
	long := strings.Repeat("--some-browser-flag ", 12)
	in := lines(
		"USER PID %CPU %MEM VSZ RSS TTY STAT START TIME COMMAND",
		"atif 3501 3.0 5.5 56088068 442392 ? Rl Oct03 16:23 /usr/lib/chromium/chromium "+long,
	)
	got := via(ps.Pipe, in)
	// The flags stay on the line; only the far end is dimmed.
	if !strings.Contains(got, "--some-browser-flag") {
		t.Errorf("truncated the command instead of dimming it:\n%s", got)
	}
	if !strings.Contains(got, "</>") {
		t.Errorf("expected a dimmed tail:\n%s", got)
	}
}

// ---------------------------------------------------------------------------
// find -ls
// ---------------------------------------------------------------------------

func TestFindLsTurnsPositionalFieldsIntoColumns(t *testing.T) {
	in := lines(
		"     261      0 drwxr-xr-x   1 atif     atif         1486 Sep 29 23:13 /home/dev/.config",
		"     262      0 -rw-r--r--   1 atif     atif           28 Sep 22 12:24 /home/dev/.config/a",
	)
	got := via(find.Pipe, in)
	if !strings.Contains(got, "INODE") || !strings.Contains(got, "PATH") {
		t.Errorf("expected named columns:\n%s", got)
	}
	// The path must survive: it is the reason the command was run.
	if !strings.Contains(got, "/home/dev/.config") {
		t.Errorf("lost the path:\n%s", got)
	}
}

func TestFindLsColorsDirectoriesAndExecutables(t *testing.T) {
	in := lines(
		"     1      0 drwxr-xr-x   1 atif     atif        4096 Sep 29 23:13 /tmp/dir",
		"     2      0 -rwxr-xr-x   1 atif     atif        1024 Sep 29 23:13 /tmp/run",
	)
	got := via(find.Pipe, in)
	if !strings.Contains(got, "<cyan>") {
		t.Errorf("directories should stand out:\n%s", got)
	}
	if !strings.Contains(got, "<green>") {
		t.Errorf("executables should stand out:\n%s", got)
	}
}

// ---------------------------------------------------------------------------
// du
// ---------------------------------------------------------------------------

func TestDuAddsUnitsToBareBlockCounts(t *testing.T) {
	in := lines("22972\tproject/", "104\tnotes/")
	got := via(du.Pipe, in)
	// Bare du prints kilobytes with no unit; parse says so.
	if !strings.Contains(got, "22972K") {
		t.Errorf("expected a unit on the size:\n%s", got)
	}
	if !strings.Contains(stripTags(got), "project/") {
		t.Errorf("lost the path:\n%s", got)
	}
}

func TestDuHighlightsLargeEntries(t *testing.T) {
	in := lines("492M\t.cargo/", "104K\tnotes/")
	got := via(du.Pipe, in)
	if !strings.Contains(stripTags(got), "492M") {
		t.Errorf("lost the size:\n%s", got)
	}
	// 492M crosses the 100M mark, so it is tinted; 104K is not.
	if !strings.Contains(got, "<yellow>") || !strings.Contains(got, "492M") {
		t.Errorf("a large entry should stand out:\n%s", got)
	}
	if strings.Contains(got, "yellow>  104K<") {
		t.Errorf("a small entry should stay plain:\n%s", got)
	}
}

// ---------------------------------------------------------------------------
// free
// ---------------------------------------------------------------------------

// A real `free`, in its default form: the numbers are counts of kibibytes and
// say nothing about it.
const freeSample = `               total        used        free      shared  buff/cache   available
Mem:         8004464     5661088      454460      464540     2989260     2343376
Swap:       16009028      819024    15190004
`

const freeHumanSample = `               total        used        free      shared  buff/cache   available
Mem:           7.6Gi       5.4Gi       443Mi       453Mi       2.9Gi       2.2Gi
Swap:           15Gi       799Mi        14Gi
`

// columnEnds is where each of a line's fields stops, in bytes. free aligns
// every column on one right edge, and that edge is what a reader's eye lands
// on when they go down the table.
func columnEnds(line string) []int {
	var ends []int
	at := 0
	for _, f := range strings.Fields(line) {
		at = strings.Index(line[at:], f) + at + len(f)
		ends = append(ends, at)
	}
	return ends
}

func TestFreeAddsUnitsToItsBareNumbers(t *testing.T) {
	plain := stripTags(via(free.Pipe, freeSample))
	for _, want := range []string{"7.6G", "443M", "15G", "Mem:", "Swap:"} {
		if !strings.Contains(plain, want) {
			t.Errorf("lost %q:\n%s", want, plain)
		}
	}
	// Every row's first value has to end where the header's first column ends,
	// or the table is a column adrift for anyone reading down it.
	linesOut := strings.Split(plain, "\n")
	edge := columnEnds(linesOut[0])[0]
	for _, line := range linesOut[1:] {
		if line == "" {
			continue
		}
		if got := columnEnds(line)[1]; got != edge {
			t.Errorf("first value ends at %d, header's first column at %d:\n%s", got, edge, plain)
		}
	}
}

// free -h has already done the work. parse must not do it twice, and must not
// move a single column while it looks at it.
func TestFreeLeavesFreeHumanOutputAlone(t *testing.T) {
	got := via(free.Pipe, freeHumanSample)
	if got != strings.TrimRight(freeHumanSample, "\n") {
		t.Errorf("free -h was rewritten:\ngot  %q\nwant %q", got, freeHumanSample)
	}
}

func TestFreeTintsMachineWhoseMemoryIsGoing(t *testing.T) {
	in := lines(
		"               total        used        free      shared  buff/cache   available",
		"Mem:         1000000      960000       40000       10000       500000       40000",
	)
	got := via(free.Pipe, in)
	if !strings.Contains(got, "<red>") {
		t.Errorf("a machine at 96%% should say so:\n%s", got)
	}
	// The tint wraps the number and nothing else, so the column still ends
	// where it did.
	if edge := columnEnds(freeSample)[0]; columnEnds(stripTags(got))[0] != edge {
		t.Errorf("the tint moved a column:\n%s", got)
	}
}

// ---------------------------------------------------------------------------
// lsof
// ---------------------------------------------------------------------------

// A real `lsof -i`: a heading, two rows whose names run into each other over
// an arrow, and the socket state lsof writes in brackets at the end.
const lsofSample = `COMMAND     PID USER  FD   TYPE  DEVICE SIZE/OFF NODE NAME
chromium  62754 atif 204u  IPv4  350953      0t0  UDP mdns.mcast.net:mdns
chromium  62799 atif  19u  IPv6 1838988      0t0  UDP anom:36726->[2600:1901:0:47fc::]:https (ESTABLISHED)
`

// The default form, whose heading carries two columns no row has anything for:
// TID and TASKCMD are empty on every line, so the cells cannot be read by
// position.
const lsofPlainSample = `COMMAND      PID    TID TASKCMD   USER   FD      TYPE             DEVICE  SIZE/OFF    NODE NAME
systemd      769                  atif  cwd   unknown                                      /proc/769/cwd (readlink: Permission denied)
`

// stripColors takes parse's markers back out of lsof's output. lsof's own
// names contain "->", so only the markers parse adds are removed, rather than
// everything between a "<" and a ">".
func stripColors(s string) string {
	for _, tag := range []string{"<bold>", "<dim>", "<red>", "<green>", "<yellow>", "<cyan>", "</>"} {
		s = strings.ReplaceAll(s, tag, "")
	}
	return s
}

func TestLsofColorsRowsWithoutMovingThem(t *testing.T) {
	got := via(lsof.Pipe, lsofSample)
	if plain := stripColors(got); plain != strings.TrimRight(lsofSample, "\n") {
		t.Errorf("lsof moved a byte:\ngot  %q\nwant %q", plain, lsofSample)
	}
	for _, want := range []string{
		"<bold>COMMAND</>", "<cyan>IPv6</>", "<green>(ESTABLISHED)</>",
		"0t0  UDP anom:36726->",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

// The default form has empty cells in it, so no cell of a row can be trusted
// to sit under the column it looks like. The note lsof wrote at the end of the
// name still reads as a note.
func TestLsofColorsTheNoteOfARowItCannotRead(t *testing.T) {
	got := via(lsof.Pipe, lsofPlainSample)
	if plain := stripColors(got); plain != strings.TrimRight(lsofPlainSample, "\n") {
		t.Errorf("lsof moved a byte:\ngot  %q\nwant %q", plain, lsofPlainSample)
	}
	if !strings.Contains(got, "<dim>(readlink: Permission denied)</>") {
		t.Errorf("the note was not marked:\n%s", got)
	}
	if strings.Contains(got, "<dim>cwd</>") || strings.Contains(got, "<dim>unknown</>") {
		t.Errorf("a shifted cell was colored:\n%s", got)
	}
}

// ---------------------------------------------------------------------------
// findmnt
// ---------------------------------------------------------------------------

// The default form: findmnt's own heading, then the mount tree with the branch
// it draws in front of each target.
const findmntSample = `TARGET                                        SOURCE                   FSTYPE          OPTIONS
/                                             /dev/mapper/root[/@]     btrfs           rw,relatime,compress=zstd:3,ssd,space_cache=v2,subvolid=256,subvol=/@
├─/proc                                       proc                     proc            rw,nosuid,nodev,noexec,relatime
│ └─/proc/sys/fs/binfmt_misc                  systemd-1                autofs          rw,relatime,fd=43,pgrp=1,timeout=0,minproto=5,maxproto=5,direct,pipe_ino=763
│   └─/proc/sys/fs/binfmt_misc                binfmt_misc              binfmt_misc     rw,nosuid,nodev,noexec,relatime
├─/sys                                        sys                      sysfs           rw,nosuid,nodev,noexec,relatime
`

// The df imitation, whose SIZE, USED and AVAIL columns are right-aligned under
// headings that sit at their right edge with them.
const findmntDfSample = `SOURCE                   FSTYPE     SIZE   USED AVAIL USE% TARGET
dev                      devtmpfs   3.7G      0  3.7G   0% /dev
run                      tmpfs      3.8G   1.3M  3.8G   0% /run
/dev/mapper/root[/@]     btrfs    109.8G  69.1G 38.1G  63% /
tmpfs                    tmpfs      3.8G   580K  3.8G   0% /dev/shm
`

func TestFindmntColorsTheTreeWithoutMovingIt(t *testing.T) {
	got := via(findmnt.Pipe, findmntSample)
	if plain := stripTags(got); plain != strings.TrimRight(findmntSample, "\n") {
		t.Errorf("findmnt moved a byte:\ngot  %q\nwant %q", plain, findmntSample)
	}
	for _, want := range []string{
		"<bold>TARGET</>", "<bold>OPTIONS</>",
		"<dim>├─</>/proc", "<dim>│   └─</>/proc/sys",
		"<cyan>btrfs</>", "<dim>proc</>", "<dim>sysfs</>",
		"<dim>rw,nosuid,nodev,noexec,relatime</>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

// The df form is where the tricky columns are: SIZE and USE% end at their
// right edge while their headings sit with them, so reading them by the start
// of a heading would put every value under the wrong column.
func TestFindmntColorsTheRightAlignedColumns(t *testing.T) {
	got := via(findmnt.Pipe, findmntDfSample)
	if plain := stripTags(got); plain != strings.TrimRight(findmntDfSample, "\n") {
		t.Errorf("findmnt moved a byte:\ngot  %q\nwant %q", plain, findmntDfSample)
	}
	for _, want := range []string{"<bold>SIZE</>", "<bold>USE%</>", "<cyan>btrfs</>", "<dim>devtmpfs</>"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

// ---------------------------------------------------------------------------
// ip
// ---------------------------------------------------------------------------

const ipAddrSample = `1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN group default qlen 1000
    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00
    inet 127.0.0.1/8 scope host lo
       valid_lft forever preferred_lft forever
2: enp2s0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc fq_codel state UP group default qlen 1000
    link/ether 0a:e0:af:c2:15:d8 brd 0a:e0:af:c2:15:d8
    altname enx0ae0afc215d8
    inet 192.168.1.23/24 brd 192.168.1.255 scope global dynamic noprefixroute enp2s0
       valid_lft 64900sec preferred_lft 64900sec
    inet6 fe80::6578:60ad:8bdc:c23a/64 scope link noprefixroute
       valid_lft forever preferred_lft forever
`

func TestIpAddrCollapsesBlocksAndKeepsAddresses(t *testing.T) {
	got := viaSub(ip.Format, "addr", ipAddrSample)
	for _, want := range []string{"enp2s0", "192.168.1.23/24", "0a:e0:af:c2:15:d8", "altname"} {
		if !strings.Contains(got, want) {
			t.Errorf("lost %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "<bold green>") {
		t.Errorf("an UP interface should be green:\n%s", got)
	}
}

func TestIpAddrDropsForeverButKeepsExpiringLease(t *testing.T) {
	got := viaSub(ip.Format, "addr", ipAddrSample)
	if strings.Contains(got, "forever") {
		t.Errorf("valid_lft forever is noise:\n%s", got)
	}
	// A lease that actually ends is information, so it must survive.
	if !strings.Contains(got, "64900sec") {
		t.Errorf("dropped a finite lease:\n%s", got)
	}
}

func TestIpRouteOnlyColors(t *testing.T) {
	in := "default via 192.168.1.1 dev enp2s0 proto dhcp src 192.168.1.23 metric 100 \n"
	got := viaSub(ip.Format, "route", in)
	stripped := stripTags(got)
	if strings.TrimSpace(stripped) != strings.TrimSpace(in) {
		t.Errorf("route text should be unchanged, got %q", stripped)
	}
}

// ---------------------------------------------------------------------------
// ss
// ---------------------------------------------------------------------------

func TestSsFixesTheHeaderCollision(t *testing.T) {
	in := lines("Netid State  Recv-Q Send-Q   Local Address:Port  Peer Address:PortProcess")
	got := via(ss.Pipe, in)
	if strings.Contains(got, "PortProcess") {
		t.Errorf("header columns still collide:\n%s", got)
	}
}

func TestSsSeparatesTheProcessName(t *testing.T) {
	in := lines(
		"Netid State  Recv-Q Send-Q   Local Address:Port  Peer Address:Port Process",
		"udp   UNCONN 0      0      224.0.0.251:5353  224.0.0.251:5353 users:((\"chromium\",pid=3568,fd=63))",
	)
	got := via(ss.Pipe, in)
	if !strings.Contains(got, "chromium") {
		t.Errorf("lost the process name:\n%s", got)
	}
	if !strings.Contains(got, "UNCONN") {
		t.Errorf("lost the state:\n%s", got)
	}
}

// ---------------------------------------------------------------------------
// lsblk
// ---------------------------------------------------------------------------

const lsblkSample = `NAME     MAJ:MIN RM   SIZE RO TYPE  MOUNTPOINTS
sda        8:0    0 111.8G  0 disk
├─sda1     8:1    0     2G  0 part  /boot
└─sda2     8:2    0 109.8G  0 part
  └─root 253:0    0 109.8G  0 crypt /var/log
                                    /home
                                    /
zram0    252:0    0   7.6G  0 disk  [SWAP]
`

func TestLsblkIndentsWrappedMountpoints(t *testing.T) {
	got := via(lsblk.Pipe, lsblkSample)
	// /home belongs to root; it must not float at the far column edge.
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "/home") && !strings.HasPrefix(strings.TrimLeft(line, " "), "crypt") {
			if !strings.HasPrefix(line, " ") {
				t.Errorf("wrapped mountpoint is not indented under its device:\n%s", got)
			}
		}
	}
}

func TestLsblkKeepsTheTreeShape(t *testing.T) {
	got := via(lsblk.Pipe, lsblkSample)
	for _, c := range []string{"├", "└", "─"} {
		if !strings.Contains(got, c) {
			t.Errorf("lost tree character %q:\n%s", c, got)
		}
	}
	if !strings.Contains(got, "zram0") {
		t.Errorf("lost a device:\n%s", got)
	}
}

// ---------------------------------------------------------------------------
// The two ways of using parse agree with each other
// ---------------------------------------------------------------------------

// A tool that colorized its own output before it reached parse must still be
// recognized: the escape sequence in front of "diff --git " used to hide the
// line from every detector, so parse silently passed the text through.
func TestPipeDetectsThroughSelfAddedColor(t *testing.T) {
	in := "\x1b[1mdiff --git i/f.txt w/f.txt\x1b[0m\n" +
		"\x1b[36m@@ -1,3 +1,4 @@\x1b[0m\n" +
		"\x1b[31m-two\x1b[0m\n" +
		"\x1b[32m+TWO\x1b[0m\n"
	got := via(cmd.Pipe, in)
	if strings.Contains(got, "\x1b[1m") {
		t.Errorf("the tool's own color should have been replaced by parse's, got %q", got)
	}
	if !strings.Contains(stripTags(got), "-two") {
		t.Errorf("diff content was lost:\n%s", got)
	}
}

// When parse has no formatter, a color the user asked for is left alone. This
// is the grep case: `grep --color=always ... | parse` must not be uncolored.
func TestPipeKeepsForeignColorWhenUnrecognized(t *testing.T) {
	in := "func: \x1b[31mreturn\x1b[0m one\nfunc: nothing to see\n"
	// Compared raw, without via: the trailing newline is part of what has to
	// survive, and via trims it off.
	var sb strings.Builder
	cmd.Pipe(&sb, in)
	got := sb.String()
	if got != in {
		t.Errorf("unrecognized colored output must pass through byte for byte:\ngot  %q\nwant %q", got, in)
	}
}

// The piped and the direct form of the same command have to agree, since a
// user should not get different output depending on which one they typed.
func TestPipedAndDirectFormsAgree(t *testing.T) {
	piped := via(cmd.Pipe, ipAddrSample)
	direct := viaSub(ip.Format, "addr", ipAddrSample)
	if piped != direct {
		t.Errorf("ip addr differs between the two forms:\npiped  %q\ndirect %q", piped, direct)
	}

	piped = via(cmd.Pipe, lsblkSample)
	direct = via(lsblk.Pipe, lsblkSample)
	if piped != direct {
		t.Errorf("lsblk differs between the two forms:\npiped  %q\ndirect %q", piped, direct)
	}
}
