// Tests for the mount helpers: the row shape mount always prints, and the
// colors it gets. The samples are mount's own words, copied as it printed
// them — kernel plumbing, a btrfs root, a read-only credential tmpfs, a
// label after the brackets and a mountpoint holding a space.
package mount

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

const plainSample = `proc on /proc type proc (rw,nosuid,nodev,noexec,relatime)
sys on /sys type sysfs (rw,nosuid,nodev,noexec,relatime)
/dev/mapper/root on / type btrfs (rw,relatime,compress=zstd:3,ssd,space_cache=v2,subvolid=295,subvol=/@)
/dev/nvme0n1p2 on /boot type ext4 (rw,relatime)
`

const readOnlySample = `none on /run/credentials/systemd-journald.service type tmpfs (ro,nosuid,nodev,noexec,relatime,nosymfollow,size=1024k,mode=700,inode64,huge=advise)
`

const loneReadOnlySample = `none on /sys/firmware/efi/efivars type efivarfs (ro,nosuid,nodev,noexec,relatime)
`

const labelSample = `UUID=8f7e-21ab on /boot/efi type vfat (rw,relatime,fmask=0077,dmask=0077,utf8,shortname=winnt,errors=remount-ro) LABEL=EFI
`

const spaceSample = `//nas.example.com/share on /mnt/Backup Share type cifs (rw,vers=3.0,uid=1000,gid=1000)
`

const unknownFsSample = `fuse.gocryptfs on /home/atif/private type fuse.gocryptfs (rw,nosuid,nodev,relatime,user_id=1000,group_id=1000)
`

func TestDetectClaimsMountRows(t *testing.T) {
	for name, text := range map[string]string{
		"plain":         plainSample,
		"read only":     readOnlySample,
		"label":         labelSample,
		"space in path": spaceSample,
		"one row":       "proc on /proc type proc (rw)\n",
		// mount's table is the whole of stdout; a leading blank line is
		// nothing but an accident of the terminal.
		"leading blank":  "\n\nproc on /proc type proc (rw)\n",
		"unknown fs":     unknownFsSample,
		"no trailing nl": "proc on /proc type proc (rw)",
	} {
		if got := Detect(text); got != "mount" {
			t.Errorf("%s: Detect = %q, want mount", name, got)
		}
	}
}

func TestDetectLeavesOtherOutputAlone(t *testing.T) {
	for name, text := range map[string]string{
		"df":         "Filesystem        1K-blocks      Used Available Use% Mounted on\n/dev/nvme0n1p1     104857600  69106688  35750912  66% /\n",
		"findmnt":    "TARGET      SOURCE FSTYPE OPTIONS\n/           /dev/1 ext4   rw,relatime\n",
		"raw find":   "/dev/sda1 /mnt ext4 rw,relatime 0 0\n",
		"lsblk":      "NAME MAJ:MIN RM SIZE RO TYPE MOUNTPOINTS\nsda    8:0    0   50G  0 disk /\n",
		"mountpoint": "/proc is a mountpoint\n",
		"prose":      "the changelog on every line type says so (really)\n",
		"no type":    "proc on /proc rw,nosuid,nodev,noexec,relatime\n",
		"no parens":  "proc on /proc type proc rw,nosuid\n",
		"empty":      "",
		"blank":      "\n\n",
	} {
		if got := Detect(text); got != "" {
			t.Errorf("%s: Detect = %q, want \"\"", name, got)
		}
	}
}

func TestFormatPrintsEveryByteBack(t *testing.T) {
	for name, text := range map[string]string{
		"plain":         plainSample,
		"read only":     readOnlySample,
		"lone ro":       loneReadOnlySample,
		"label":         labelSample,
		"space in path": spaceSample,
		"unknown fs":    unknownFsSample,
	} {
		if got := format(text); strip(got) != text {
			t.Errorf("%s moved a byte:\ngot  %q\nwant %q", name, strip(got), text)
		}
	}
}

func TestFormatColorsFilesystemTypesByWhereTheyLive(t *testing.T) {
	got := format(plainSample)
	// btrfs and ext4 are on a disk; proc and sysfs belong to the kernel.
	for _, want := range []string{"<cyan>btrfs</>", "<cyan>ext4</>", "<dim>proc</>", "<dim>sysfs</>"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	// A type this list has never heard of keeps its color: being unsure is
	// not the same as being noise.
	unknown := format(unknownFsSample)
	if !strings.Contains(unknown, "type fuse.gocryptfs (") || strings.Contains(unknown, "<dim>fuse.gocryptfs") {
		t.Errorf("an unknown filesystem was repainted: %q", unknown)
	}
}

func TestFormatQuietensOptionsAndMarksAReadOnlyMount(t *testing.T) {
	got := format(plainSample)
	if !strings.Contains(got, "<dim>rw,relatime,compress=zstd:3,ssd,space_cache=v2,subvolid=295,subvol=/@</>") {
		t.Errorf("options not quietened: %q", strings.SplitN(got, "\n", 4)[3])
	}
	// The device and the mountpoint are the row's anchor and keep their bytes.
	if !strings.Contains(got, "/dev/mapper/root on / type <cyan>btrfs</>") {
		t.Errorf("the anchor moved or got painted: %q", strings.SplitN(got, "\n", 4)[3])
	}
	ro := format(readOnlySample)
	if !strings.Contains(ro, "<yellow>ro</><dim>,nosuid,nodev,noexec,relatime,nosymfollow,size=1024k,mode=700,inode64,huge=advise</>") {
		t.Errorf("a read-only mount did not stand out: %q", ro)
	}
	// A lone `ro` is marked and nothing is dimmed after it.
	if lone := format(loneReadOnlySample); !strings.Contains(lone, "<yellow>ro</><dim>,nosuid") {
		t.Errorf("a lone ro was not marked: %q", lone)
	}
}

func TestFormatLeavesAnUnmatchedLineAlone(t *testing.T) {
	// An error that leaked onto stdout next to a real row: the row is
	// painted, the error is not touched.
	text := "proc on /proc type proc (rw)\nmount: /mnt: must be superuser.\n"
	got := format(text)
	if !strings.Contains(got, "mount: /mnt: must be superuser.\n") {
		t.Errorf("the error line was rewritten: %q", got)
	}
	if !strings.Contains(got, "<dim>rw</>") {
		t.Errorf("the real row was not painted: %q", got)
	}
}
