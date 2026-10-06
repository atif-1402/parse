// Tests for the findmnt helpers: the heading findmnt always prints, the
// column finder the colors hang off, and the arguments that mean the output is
// not a table at all. The samples below are findmnt's own words, copied as it
// printed them — right-aligned numbers, tree drawings and all.
package findmnt

import (
	"bytes"
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
}

// strip takes the test's color markers back out, so a formatted line can be
// held against the line it came from.
func strip(s string) string {
	for _, tag := range []string{"<bold>", "<dim>", "<red>", "<green>", "<yellow>", "<cyan>", "</>"} {
		s = strings.ReplaceAll(s, tag, "")
	}
	return s
}

func format(text string) string {
	var buf bytes.Buffer
	Format(&buf, text)
	return buf.String()
}

const treeSample = `TARGET                                        SOURCE                   FSTYPE          OPTIONS
/                                             /dev/mapper/root[/@]     btrfs           rw,relatime,compress=zstd:3,ssd,space_cache=v2,subvolid=256,subvol=/@
├─/proc                                       proc                     proc            rw,nosuid,nodev,noexec,relatime
│ └─/proc/sys/fs/binfmt_misc                  systemd-1                autofs          rw,relatime,fd=43,pgrp=1,timeout=0,minproto=5,maxproto=5,direct,pipe_ino=763
└─/sys                                        sys                      sysfs           rw,nosuid,nodev,noexec,relatime
`

const dfSample = `SOURCE                   FSTYPE     SIZE   USED  AVAIL USE% TARGET
dev                      devtmpfs   3.7G      0   3.7G   0% /dev
run                      tmpfs      3.8G   1.3M   3.8G   0% /run
/dev/mapper/root[/@]     btrfs    109.8G  69.1G  38.1G  93% /
tmpfs                    tmpfs      3.8G 187.8M   3.6G   5% /dev/shm`

const useSample = `TARGET                                          SIZE USE%
/                                             109.8G  85%
├─/proc                                            0    -
│ └─/proc/sys/fs/binfmt_misc                       0    -
│   └─/proc/sys/fs/binfmt_misc                     0    -`

const oneRowSample = `TARGET SOURCE FSTYPE OPTIONS
/tmp   tmpfs  tmpfs  rw,nosuid,nodev,nr_inodes=1048576,inode64,huge=advise,usrquota
`

const readOnlySample = `TARGET                                        SOURCE                   FSTYPE          OPTIONS
│ ├─/run/credentials/systemd-journald.service none                     tmpfs           ro,nosuid,nodev,noexec,relatime,nosymfollow,size=1024k,nr_inodes=1024,mode=700,inode64,huge=advise,noswap`

const fsOptionsSample = `TARGET                                        FS-OPTIONS
/                                             rw,compress=zstd:3,ssd,space_cache=v2,subvolid=256,subvol=/@
├─/proc                                       rw
`

const oneOptionsSample = `OPTIONS
rw,relatime,compress=zstd:3,ssd,space_cache=v2,subvolid=256,subvol=/@
rw,nosuid,nodev,noexec,relatime
`

const rawSample = `TARGET SOURCE FSTYPE OPTIONS
/proc proc proc rw,nosuid,nodev,noexec,relatime
/sys sys sysfs rw,nosuid,nodev,noexec,relatime
`

const asciiSample = `TARGET                                        SOURCE                   FSTYPE          OPTIONS
/                                             /dev/mapper/root[/@]     btrfs           rw,relatime,compress=zstd:3,ssd,space_cache=v2,subvolid=256,subvol=/@
|-/proc                                       proc                     proc            rw,nosuid,nodev,noexec,relatime`

const noHeadingSample = `/                                             /dev/mapper/root[/@]     btrfs           rw,relatime,compress=zstd:3
├─/proc                                       proc                     proc            rw,nosuid,nodev,noexec,relatime
`

func TestDetectClaimsEveryTableHeading(t *testing.T) {
	for _, heading := range []string{
		"TARGET                                        SOURCE                   FSTYPE          OPTIONS",
		"SOURCE                   FSTYPE     SIZE   USED  AVAIL USE% TARGET",
		"TARGET SOURCE FSTYPE OPTIONS",
		"TARGET                                          SIZE USE%",
		"INO.TOTAL INO.USED INO.AVAIL INO.USE%",
		// Any heading holding a column lsblk does not have is one lsblk
		// could never print, whatever order findmnt's -o is given in.
		"FSTYPE          TARGET",
		"SIZE          TARGET",
	} {
		if got := Detect(heading + "\n"); got != "findmnt" {
			t.Errorf("Detect(heading) = %q, want findmnt for %q", got, heading)
		}
	}
}

func TestDetectLeavesOtherTablesAlone(t *testing.T) {
	for name, text := range map[string]string{
		"df":         "Filesystem        1K-blocks      Used Available Use% Mounted on\n",
		"lsblk":      "NAME MAJ:MIN RM SIZE RO TYPE MOUNTPOINTS\n",
		"systemctl":  "UNIT LOAD ACTIVE SUB DESCRIPTION\n",
		"no heading": noHeadingSample,
		"raw data":   "/proc proc proc rw,nosuid,nodev,noexec,relatime\n",
		"empty":      "\n\n",
		// A heading spelled only from the names findmnt and lsblk share is
		// exactly what `lsblk -o FSTYPE,SIZE` prints, so neither tool may
		// claim it and the text stays as it came in.
		"shared FSTYPE,SIZE":  "FSTYPE        SIZE\n",
		"shared LABEL,UUID":   "LABEL                              UUID\n",
		"shared MAJ:MIN,ID":   "MAJ:MIN   ID\n",
		"shared FSTYPE,SIZE+": "FSTYPE SIZE LABEL UUID PARTLABEL\n",
	} {
		if got := Detect(text); got != "" {
			t.Errorf("Detect(%s) = %q, want \"\"", name, got)
		}
	}
}

func TestFormatLeavesATableBothToolsCouldPrintAlone(t *testing.T) {
	// Even if Format were handed such a text, the heading it reads by is the
	// same one Detect refuses, so there is no path by which these bytes are
	// rewritten.
	const shared = `FSTYPE        SIZE
btrfs       109.8G
tmpfs         3.8G
`
	if got := format(shared); got != shared {
		t.Errorf("a shared-vocabulary table was rewritten:\ngot  %q\nwant %q", got, shared)
	}
}

func TestFormatPrintsEveryByteBack(t *testing.T) {
	for name, text := range map[string]string{
		"tree":       treeSample,
		"df":         dfSample,
		"use%":       useSample,
		"one row":    oneRowSample,
		"read only":  readOnlySample,
		"fs options": fsOptionsSample,
		"ascii":      asciiSample,
	} {
		if got := format(text); strip(got) != text {
			t.Errorf("%s moved a byte:\ngot  %q\nwant %q", name, strip(got), text)
		}
	}
}

func TestFormatBoldensTheHeadingInPlace(t *testing.T) {
	got := format(treeSample)
	if !strings.Contains(got, "<bold>TARGET</>") || !strings.Contains(got, "<bold>OPTIONS</>") {
		t.Errorf("heading not bolded: %q", strings.SplitN(got, "\n", 2)[0])
	}
	// SIZE is right-aligned over its values, so its heading may not sit
	// where a left-aligned column's heading does.
	if use := format(useSample); !strings.Contains(use, "<bold>SIZE</>") {
		t.Errorf("a right-aligned heading column was not bolded: %q", strings.SplitN(use, "\n", 2)[0])
	}
}

func TestFormatDimsTheTreeAndLeavesThePath(t *testing.T) {
	got := format(treeSample)
	for _, want := range []string{"<dim>├─</>/proc", "<dim>│ └─</>/proc/sys", "<dim>└─</>/sys"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	// The root of the tree has no drawing in front of it.
	if !strings.Contains(got, "\n/  ") {
		t.Errorf("the root row lost its target: %q", strings.SplitN(got, "\n", 3)[1])
	}
	// The ascii spellings are drawings too.
	if ascii := format(asciiSample); !strings.Contains(ascii, "<dim>|-</>/proc") {
		t.Errorf("the ascii tree was not dimmed: %q", ascii)
	}
}

func TestFormatColorsFilesystemTypesByWhereTheyLive(t *testing.T) {
	got := format(treeSample)
	// btrfs is on a disk; proc and sysfs belong to the kernel.
	for _, want := range []string{"<cyan>btrfs</>", "<dim>proc</>", "<dim>sysfs</>"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

func TestFormatQuietensOptionsAndMarksAReadOnlyMount(t *testing.T) {
	got := format(treeSample)
	if !strings.Contains(got, "<dim>rw,relatime,compress=zstd:3,ssd,space_cache=v2,subvolid=256,subvol=/@</>") {
		t.Errorf("options not quietened: %q", strings.SplitN(got, "\n", 3)[1])
	}
	if fs := format(fsOptionsSample); !strings.Contains(fs, "<dim>rw,compress=zstd:3,ssd,space_cache=v2,subvolid=256,subvol=/@</>") {
		t.Errorf("FS-OPTIONS was not quietened: %q", fs)
	}
	// A lone OPTIONS column is the whole table, so quietening it would be
	// quietening the output itself: only the heading is painted.
	one := format(oneOptionsSample)
	if strings.Contains(one, "<dim>") {
		t.Errorf("a single options column was quietened: %q", one)
	}
	if !strings.Contains(one, "<bold>OPTIONS</>") {
		t.Errorf("the heading was not bolded: %q", one)
	}
	ro := format(readOnlySample)
	if !strings.Contains(ro, "<yellow>ro</><dim>,nosuid,nodev,noexec,relatime") {
		t.Errorf("a read-only mount did not stand out: %q", ro)
	}
}

func TestFormatTintsHowFullAFilesystemIs(t *testing.T) {
	got := format(useSample)
	if !strings.Contains(got, "<yellow>85%</>") {
		t.Errorf("a full filesystem was not tinted:\n%s", got)
	}
	// Below the threshold there is nothing to say, and a mount with no
	// percentage at all is left alone.
	if strings.Contains(got, "<red>63%</>") || strings.Contains(got, "<red>-</>") {
		t.Errorf("colored a figure that was fine: %s", got)
	}
	df := format(dfSample)
	if !strings.Contains(df, "<red>93%</>") || !strings.Contains(df, " /\n") {
		t.Errorf("the df form lost its alarming figure: %q", df)
	}
}

func TestFormatLeavesUnpaddedOutputAlone(t *testing.T) {
	// Raw output separates its fields with one space and no padding, so its
	// columns cannot be found — not even its heading may be bolded, because
	// a script reads the bytes.
	if got := format(rawSample); got != rawSample {
		t.Errorf("raw output was rewritten:\ngot  %q\nwant %q", got, rawSample)
	}
	if got := format(noHeadingSample); got != noHeadingSample {
		t.Errorf("output without a heading was rewritten:\ngot  %q\nwant %q", got, noHeadingSample)
	}
}

func TestScriptFormRefusesFormsScriptsRead(t *testing.T) {
	for _, args := range [][]string{
		{"-J"}, {"--json"}, {"-P"}, {"--pairs"}, {"-y"}, {"--shell"}, {"-r"}, {"--raw"},
	} {
		if !scriptForm(args) {
			t.Errorf("scriptForm(%q) = false, want true", args)
		}
	}
	for _, args := range [][]string{
		nil, {"-D"}, {"--df"}, {"-T", "/tmp"}, {"-t", "btrfs"}, {"-o", "TARGET,SIZE"},
		{"-l"}, {"-n"}, {"--bytes"},
	} {
		if scriptForm(args) {
			t.Errorf("scriptForm(%q) = true, want false", args)
		}
	}
}

// Only output that ends may be captured: a machine form must keep its bytes,
// and --poll would leave Capture waiting forever with nothing printed.
func TestCapturableRefusesScriptFormsAndPolls(t *testing.T) {
	for _, args := range [][]string{
		{"-r"}, {"-J"}, {"-p"}, {"--poll"}, {"--poll=umount"}, {"-p", "-o", "TARGET"},
	} {
		if capturable(args) {
			t.Errorf("capturable(%q) = true, want false", args)
		}
	}
	for _, args := range [][]string{
		nil, {"-D"}, {"-T", "/tmp"}, {"-t", "btrfs"}, {"-o", "TARGET,SIZE"},
		{"-l"}, {"--pseudo"}, {"-s"},
	} {
		if !capturable(args) {
			t.Errorf("capturable(%q) = false, want true", args)
		}
	}
}

func TestNeedsTerminalOnlyForAListingThatNeverEnds(t *testing.T) {
	if !NeedsTerminal([]string{"-p"}) || !NeedsTerminal([]string{"--poll"}) ||
		!NeedsTerminal([]string{"--poll=umount"}) {
		t.Error("a poll must be handed the terminal")
	}
	// --pseudo is not --poll, and -P is the pairs format, which ends.
	if NeedsTerminal([]string{"--pseudo"}) || NeedsTerminal([]string{"-P"}) ||
		NeedsTerminal([]string{"-D"}) {
		t.Error("an ordinary listing asked for the terminal")
	}
}
