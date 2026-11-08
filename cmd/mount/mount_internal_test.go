// Tests for the mount helpers: the row shape mount always prints, the
// table parse lines it up into, and the colors it gets. The samples are
// mount's own words, copied as it printed them — kernel plumbing, a btrfs
// root, a read-only credential tmpfs, a label after the brackets and a
// mountpoint holding a space.
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

const readOnlySample = `none on /run/credentials/systemd-journald.service type tmpfs (ro,nosuid,nodev,noexec,relatime,nosymfollow,size=1024k,mode=700,inode64,huge=advise,noswap)
`

const loneReadOnlySample = `none on /sys/firmware/efi/efivars type efivarfs (ro,nosuid,nodev,noexec,relatime)
`

const labelSample = `UUID=8f7e-21ab on /boot/efi type vfat (rw,relatime,fmask=0077,dmask=0077,utf8,shortname=winnt,errors=remount-ro) LABEL=EFI
`

const spaceSample = `//nas.example.com/share on /mnt/Backup Share type cifs (rw,vers=3.0,uid=1000,gid=1000)
`

const unknownFsSample = `fuse.gocryptfs on /home/atif/private type fuse.gocryptfs (rw,nosuid,nodev,relatime,user_id=1000,group_id=1000)
`

// Three rows small enough to hold the whole table, including its padding,
// in one expectation. Column widths are DEVICE 16, MOUNTPOINT 26, FSTYPE 6,
// and OPTIONS is last so it is never padded.
const threeRowSample = `proc on /proc type proc (rw,nosuid)
/dev/mapper/root on / type btrfs (rw,relatime,compress=zstd:3)
none on /run/credentials/x.service type tmpfs (ro,nosuid,nodev,mode=700)
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

// The whole table, padding and all, for a sample small enough to write
// out. Every column starts where the heading puts it.
func TestFormatLinesTheRowsUpAsATable(t *testing.T) {
	want := "<bold>DEVICE</>" + strings.Repeat(" ", 12) +
		"<bold>MOUNTPOINT</>" + strings.Repeat(" ", 18) +
		"<bold>FSTYPE</>" + "  " +
		"<bold>OPTIONS</>\n" +
		"proc" + strings.Repeat(" ", 14) + "/proc" + strings.Repeat(" ", 23) + "<dim>proc</>" + strings.Repeat(" ", 4) + "<dim>rw,nosuid</>\n" +
		"/dev/mapper/root  /" + strings.Repeat(" ", 27) + "<cyan>btrfs</>" + strings.Repeat(" ", 3) + "<dim>rw,relatime,compress=zstd:3</>\n" +
		"none" + strings.Repeat(" ", 14) + "/run/credentials/x.service  " + "<dim>tmpfs</>" + strings.Repeat(" ", 3) + "<yellow>ro</><dim>,nosuid,nodev,mode=700</>\n"
	if got := format(threeRowSample); got != want {
		t.Errorf("table mismatch\ngot  %q\nwant %q", got, want)
	}
}

// Every field mount printed survives into the table; only the words "on"
// and "type" that glue the line together are gone.
func TestFormatKeepsEveryFieldAndDropsOnlyTheGlue(t *testing.T) {
	for name, text := range map[string]string{
		"plain":     plainSample,
		"space":     spaceSample,
		"label":     labelSample,
		"read only": readOnlySample,
	} {
		got := format(text)
		// strip first: a painted value is wrapped in markers, and a
		// read-only option is split across two of them.
		plain := strip(got)
		for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
			m := reMount.FindStringSubmatch(line)
			if m == nil {
				t.Fatalf("%s: sample row no longer parses: %q", name, line)
			}
			for i, field := range []string{m[1], m[2], m[3], m[4]} {
				if !strings.Contains(plain, field) {
					t.Errorf("%s: field %d %q missing from the table:\n%s", name, i, field, got)
				}
			}
		}
		if strings.Contains(plain, " on / type ") || strings.Contains(plain, " type btrfs ") {
			t.Errorf("%s: the glue words survived:\n%s", name, got)
		}
	}
	// A plain mount has no labels, so it must not grow a LABEL column.
	if plain := strip(format(plainSample)); strings.Contains(plain, "LABEL") {
		t.Errorf("a LABEL column appeared with no labels:\n%s", plain)
	}
	// mount -l's label gets its own column instead of hiding in the options.
	if lab := format(labelSample); !strings.Contains(lab, "<bold>LABEL</>") || !strings.Contains(lab, "LABEL=EFI") {
		t.Errorf("the label was not given a column:\n%s", lab)
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
	if !strings.Contains(unknown, "fuse.gocryptfs") ||
		strings.Contains(unknown, "<dim>fuse.gocryptfs") ||
		strings.Contains(unknown, "<cyan>fuse.gocryptfs") {
		t.Errorf("an unknown filesystem was repainted: %q", unknown)
	}
}

func TestFormatQuietensOptionsAndMarksAReadOnlyMount(t *testing.T) {
	got := format(plainSample)
	if !strings.Contains(got, "<dim>rw,relatime,compress=zstd:3,ssd,space_cache=v2,subvolid=295,subvol=/@</>") {
		t.Errorf("options not quietened:\n%s", got)
	}
	// The device and the mountpoint are the row's anchor and keep their
	// bytes: /dev/mapper/root sits in a 16-wide column, the / in a 10-wide
	// one, and btrfs padded to the 6-wide FSTYPE column.
	if !strings.Contains(got, "/dev/mapper/root  /"+strings.Repeat(" ", 11)+"<cyan>btrfs</>   <dim>rw,relatime,compress=zstd:3,ssd,space_cache=v2,subvolid=295,subvol=/@</>") {
		t.Errorf("the anchor moved or got painted:\n%s", got)
	}
	ro := format(readOnlySample)
	if !strings.Contains(ro, "<yellow>ro</><dim>,nosuid,nodev,noexec,relatime,nosymfollow,size=1024k,mode=700,inode64,huge=advise,noswap</>") {
		t.Errorf("a read-only mount did not stand out: %q", ro)
	}
	// A lone `ro` is marked and nothing is dimmed after it.
	if lone := format(loneReadOnlySample); !strings.Contains(lone, "<yellow>ro</><dim>,nosuid") {
		t.Errorf("a lone ro was not marked: %q", lone)
	}
}

func TestFormatLeavesAnUnmatchedLineAlone(t *testing.T) {
	// An error that leaked onto stdout next to a real row: no table is
	// built, the row is painted where it stands, the error is not touched.
	text := "proc on /proc type proc (rw)\nmount: /mnt: must be superuser.\n"
	got := format(text)
	if !strings.Contains(got, "mount: /mnt: must be superuser.\n") {
		t.Errorf("the error line was rewritten: %q", got)
	}
	if !strings.Contains(got, "<dim>rw</>") {
		t.Errorf("the real row was not painted: %q", got)
	}
	if strings.Contains(got, "<bold>DEVICE</>") {
		t.Errorf("a table was built from mixed text: %q", got)
	}
}

// Text with no rows at all — an empty pipe, blank lines — comes back as
// it came in.
func TestFormatLeavesEmptyTextAlone(t *testing.T) {
	for _, text := range []string{"", "\n", "\n\n"} {
		if got := format(text); got != text {
			t.Errorf("empty text was rewritten: got %q, want %q", got, text)
		}
	}
}
