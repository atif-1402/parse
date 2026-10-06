# parse

Make messy command output readable. Single binary, Go standard library only.

Supports **git**, **systemctl**, **journalctl**, **ps**, **find**, **du**,
**findmnt**, **free**, **lsof**, **ip**, **ss** and **lsblk**. **kubectl** is supported but experimental — see
[below](#kubectl).

The rule it follows: format output that is hard to read, and leave output that
already reads well exactly as the tool printed it. Machine-readable formats and
anything that prompts are never touched, so scripts keep working.

## Install

```bash
make build       # creates ./parse
make install     # or: go install github.com/atif-1402/parse@latest
```

## Usage

Two ways, same result:

```bash
parse -l                    # what parse supports, and what it changes
parse git diff -w           # a diff as old | new, in two columns
parse git status            # parse runs the tool for you
parse git log -n 10
parse systemctl --user status
parse journalctl --user -n 20
parse ps aux --sort=-%mem
parse ip addr
parse du -sh ~/src
parse free
parse findmnt -D
parse lsof -i
parse kubectl get pods

git status | parse          # or pipe into parse
systemctl --user list-units | parse
ps aux | parse
ip addr | parse
kubectl get pods | parse
```

`parse <tool> …` runs one of the supported commands and formats what comes
back. Any other command is refused with one line — `parse: ls is not
supported yet` — because parse would only be in the way. The pipe is the other
way round: parse never gets in the way of it at all. `ls | parse`,
`df -h | parse` and `find /usr | parse` show the tool's own output, formatted
when parse knows the tool and byte for byte when it does not.

`parse -l` lists every supported command with what parse changes for it:

```
  git         status, log, diff, branch, blame, grep, stash, remote, shortlog, reflog, and the rest
  systemctl   status, list-units, list-unit-files, list-dependencies, is-active, is-failed
  journalctl  the whole log line: prefix dimmed, severities colored
  ps          aux and friends: busy values tinted, long COMMAND dimmed
  find        -ls turned into named columns instead of eleven positional fields
  du          sizes always carry a unit; large entries tinted
  findmnt     the mount tree dimmed; disk filesystems cyan, options quietened, a filling filesystem tinted
  free        bare numbers get a unit; a filling machine is tinted
  lsof        the socket state at the end of a row colored; the heading bolded
  ip          addr and link collapsed per interface; route colored
  ss          colliding headers split; the process name given its own column
  lsblk       device tree dimmed; wrapped mountpoints indented under their device
  kubectl     experimental: get tables aligned with STATUS tinted; describe keys dimmed, states colored
```

`parse <tool> ...` accepts every normal argument for that tool and keeps the
tool's exit code. `-l` is only parse's when no tool has been named, so
`parse du -l` and `parse ip -l` still reach the tool's own `-l`. Run `parse`
with no arguments to see both forms, or `parse -h` for the full usage.

### When the command prints nothing

An empty pipe is an answer, not a mistake, and parse treats it as one:

```bash
git diff | parse        # clean tree: no output, exit 0
git stash list | parse  # no stashes: no output, exit 0
ipaddr | parse          # bash says "command not found"; parse says nothing
```

This is worth stating because "no output" is exactly what a successful command
prints when there is nothing to report: `git diff` on a clean tree,
`git status --porcelain` with nothing staged, `git log --author=nobody`,
`ip route show table nosuchtable`. parse used to print a hint about stderr and
exit 1 for all of them, which meant a clean tree looked like a failure and any
script checking `$?` broke for no reason.

A misspelled command is bash's error to report, and bash already reports it on
stderr where you can see it. parse gets an empty pipe and has nothing to add, so
it adds nothing.

### Side-by-side diffs

A unified diff makes you read it twice: once to match a `-` line with its `+`
line, once to understand the result. `parse git diff -w` puts the old version
on the left and the new one on the right, with the line number on each side so
you can jump straight to a line in your editor:

```
app.py  +3 -2
@@ -1,5 +1,6 @@
2 def process(data):                    │ 2 def process(data, opts):
3     result = compute(data)            │ 3     result = compute(data, opts)
                                        │ 4     logger.info(result)
3     return result                     │ 4     return result
4                                       │ 5
5 def other():                          │ 6 def other():
```

`-w` also works on piped input, as `git diff | parse -w`. It is opt-in because
the two columns are laid out to the terminal width: under about 27 columns
there is no honest way to show two columns, so parse falls back to one.

Lines longer than their column are cut with a `…` rather than wrapped, and
double-width characters count as two columns so CJK text stays aligned.

## Color

`--color=MODE` controls parse's own colors and works on either side of the
subcommand:

```sh
parse --color=always git log | less   # keep color through a pager
git status | parse --color=never     # plain text
```

`auto` (the default) colors only when parse writes to a terminal, and never when
`NO_COLOR` is set.

#### Side-by-side diffs

A unified diff makes you read it twice: once to match a `-` line with its `+`
line, once to understand the result. `parse git diff -w` puts the old version
on the left and the new one on the right, with the line number on each side so
you can jump straight to a line in your editor:

```
app.py  +3 -2
@@ -1,5 +1,6 @@
2 def process(data):                    │ 2 def process(data, opts):
3     result = compute(data)            │ 3     result = compute(data, opts)
                                        │ 4     logger.info(result)
3     return result                     │ 4     return result
4                                       │ 5
5 def other():                          │ 6 def other():
```

`-w` also works on piped input, as `git diff | parse -w`. It is opt-in because
the two columns are laid out to the terminal width: under about 27 columns
there is no honest way to show two columns, so parse falls back to one.

Lines longer than their column are cut with a `…` rather than wrapped, and
double-width characters count as two columns so CJK text stays aligned.

## Paging

Every command goes through a pager, so you get one screenful and the arrow keys,
`j`/`k`, `/` to search and `q` to leave.

This has to be parse's job rather than the tool's. Most tools switch their own
pager off when their output is not a terminal — and here it never is, because
parse runs them through a pipe so it can format what comes back. Without a pager
in parse, `parse journalctl` prints the whole backlog in one go, thousands of
lines past the bottom of the screen.

There is no list of which commands qualify. The rule is not "is this tool noisy"
but "did it fill more than a screen", and `less -F` already answers that: output
that fits is printed straight through without the pager ever taking the screen.
So `parse ps aux` does not flicker, and `parse du -h /usr` scrolls when there are
27,000 lines of it.

The rules it follows, all of which exist so parse cannot break a pipeline or
trap output nobody was going to read:

| Rule | Why |
|------|-----|
| Only when stdout is a terminal | `parse journalctl > log.txt` still writes the file, with no pager in the way |
| Never in front of something interactive | `git add -p` and a `git commit` with no message read the keyboard as they go, so they get the terminal untouched |
| Never in front of something endless | `journalctl -f`, `kubectl logs -f`, `systemctl watch` are passed straight through, so the tool pages them itself and Ctrl-C reaches the tool rather than the pager sitting in front of it |
| Never on `dumb` or missing `TERM` | less cannot draw there and would print a warning instead of your logs |
| Never when no pager is installed | a missing pager must never cost you the output |

The one that is easiest to forget is that parse's own output is not paged:
`parse -h` prints and exits, and a misspelled `parse gti status` prints its
one-line complaint without needing `q` to get past it.

`less` is the default, and gets `-R -F -X` when they are not already there: `-R`
keeps the colors, `-F` means output that fits on one screen is printed straight
through with no pager at all, and `-X` leaves your scrollback alone.

The piped form pages too, which takes a moment longer to explain. When the text
arrives on stdin there is no command line to inspect, so parse lets its
detectors name the tool that produced it:

```bash
journalctl -n 500 | parse            # paged
journalctl -n 500 | parse | less     # not paged: stdout is a pipe again
journalctl -n 500 | parse --no-pager # not paged
```

Text no detector names is measured rather than waved through, because a flood
is a flood whoever printed it. parse holds it back only until it has seen more
than a screenful — a screenful counted in rows, so a hundred short lines count
even though they are barely any bytes — then pages it as it would a supported
tool's. It holds nothing back after that, and it never opens a pager at all for
output that turns out to fit:

```bash
find /usr | parse    # paged: thousands of lines
df -h | parse        # printed straight through: one screenful
```

Quitting the pager stops parse immediately, even part-way through a journal of
millions of lines.

To choose a different pager, set `PARSE_PAGER` or `PAGER`:

```bash
PARSE_PAGER=most parse journalctl -n 200
PAGER='less -S' parse git log          # -S is kept; -R -F -X are added
parse --no-pager journalctl -n 200     # print it all, this once
parse journalctl -n 200 --no-pager     # the tool's own opt-out, also honored
```

`--no-pager` is parse's own flag and only counts before the tool's name. After
it, `--no-pager` belongs to the tool, and parse honors that one too — so
`parse journalctl --no-pager` does the same thing without parse's flag.

## Color the tool added itself

Piping normally strips a tool's color, because the tool sees a pipe and turns
its own color off. parse does not have that problem, since it cannot color what
it never receives. Two cases follow from it:

- **parse has a formatter for the command.** parse discards whatever color the
  tool produced and applies its own, so output is never half one palette and half
  another. `git -c color.ui=always diff | parse` still comes out as parse's diff.
- **parse has no formatter for the command.** the text is passed through byte
  for byte, color intact. `grep --color=always ... | parse` keeps its highlight,
  because the color was explicitly asked for and parse has no reason to remove
  it. This is also why a command parse does not know still works as a pipe sink.

Either way the piped and direct forms produce the same output for a command
parse formats; a test enforces that they cannot drift apart.

### Formats parse passes through untouched

Where a machine-readable format prints different bytes from the human one, parse
leaves it alone, because rewriting it breaks the thing you piped it into:

| Format | Why it is left alone |
|--------|----------------------|
| `git diff --numstat`, `--name-only`, `--name-status`, `--raw`, `--shortstat`, `--stat` | a script reads these. `--numstat` in particular is tab-separated counts, which used to be mistaken for a `du` listing and a `shortlog` summary, and came back with the counts rewritten as file sizes |
| `git diff --word-diff`, `--color-words` | same `diff --git` header as a patch, but the changes are inside brackets on a context line rather than on `+`/`-` lines. parse only claims a diff that actually records an edit |
| `findmnt -r`, `-P`, `-y`, `-J` | raw, pairs, shell and JSON are a script's input: raw has one space between its fields and no padding, so there are no columns to read |
| foreign color | `grep --color=always ... \| parse` keeps its highlight |

Two formats that print the *same bytes* cannot be told apart, and so cannot be
treated differently: `git status --porcelain` is byte-identical to
`git status -s`, and `git for-each-ref` looks like `git log --oneline`. parse
formats the readable one in each pair, and the machine form is unavoidably
formatted with it. The separation only exists where the bytes differ.

## What gets formatted

### git

| Command | Output |
|---------|--------|
| `status` | branch + sync line, then STAGED / UNSTAGED / UNTRACKED / CONFLICTS lists. `-s` / `-sb` / `--porcelain` get their status codes colored instead |
| `log` | aligned table: COMMIT, AUTHOR, DATE, REF, MESSAGE. REF holds the decorations — `HEAD -> master`, branch names, `tag: v1.0` — so you can see where you are without a second command. A merge commit is marked `(merge of abc1234, def5678)`. Also `--oneline`, `--graph` |
| `branch` | the branch you are on green, branches in other worktrees yellow, remotes red |
| `tag` | tag names highlighted |
| `grep` | file, line number and text in different colors |
| `blame` | commit, author, date and line number colored; the code itself untouched (`-s` too) |
| `shortlog` | `-s` as a COMMITS / AUTHOR table, otherwise each author's subjects under a heading |
| `reflog` | hash yellow, ref cyan, git's description of the move kept as is |
| `remote` | `-v` as an aligned REMOTE / URL / DIR table, bare `remote` as a list of names, `show` as labeled sections |
| `stash` | `list` as a STASH / COMMIT / BRANCH / MESSAGE table, `pop` as a status block |
| `submodule` | `status` with its unexplained ahead/behind flag colored |
| `diff` | git's four boilerplate lines per file (`diff --git`, `index`, `---`, `+++`) are replaced by one line with the file name and `+added -removed`, then the hunks. Renames show both ends, big changes are highlighted, binary files say so instead of printing nothing. `parse git diff -w` lays the same diff out as old \| new in two columns |
| `show` | the commit header kept as one colored line (hash, author, date, message), then the patch formatted exactly as `diff` is. `-w` works here too, in both the piped and direct forms |
| `checkout` | "Switched to branch" green, sync line green/yellow, detached HEAD advice dimmed |
| `clean` | removed files red, `Would remove` (dry run) yellow |
| `revert`, `cherry-pick` | commit summary highlighted, `Date:` dimmed |
| `add commit push pull fetch merge rebase reset switch restore rm mv clone init` | errors red, warnings yellow, success green, ref updates and commit summaries highlighted |

Everything else runs unchanged: `describe`, `worktree list`, `config`, `am`,
`rev-parse`, `ls-files`, `merge-base`, `cat-file`, `ls-tree`, `for-each-ref` and
the rest of git's 185 commands.

### systemctl

| Command | Output |
|---------|--------|
| `status` | the `●` bullet colored by state, unit name bold, labels dimmed. `Active` and `Loaded` states colored; `Invocation`, `Tasks`, `Memory`, `CPU` and `CGroup` dimmed so the state stops competing with reference detail. A failed unit's trailing log lines are formatted like journal output |
| `list-units` | UNIT / LOAD / ACTIVE / SUB / DESCRIPTION table, state colored, and the `\x2d` escapes systemd puts in path-derived unit names decoded |
| `list-unit-files` | UNIT FILE / STATE / PRESET table, state colored |
| `is-active`, `is-failed` | the single state word colored |

Everything else runs unchanged, including the mutating verbs (`start`, `stop`,
`enable`, `daemon-reload`, `edit`, `poweroff`, ...), so nothing parse does can
change the state of the system.

### journalctl

| Command | Output |
|---------|--------|
| `journalctl` | the timestamp, host, unit and pid dimmed so the message is the only bright text on the line; `error`, `failed`, `fatal`, `panic`, `critical` red and `warning` yellow, but only at the start of a message. `-- Boot <id> --` dividers dimmed |

`-f` / `--follow` runs unchanged (it never ends), and `-o json`, `-o export`,
`-o cat` and the other machine formats pass through untouched.

### ps

| Command | Output |
|---------|--------|
| `aux`, `ax`, `ef`, `-e`, `a` | the table keeps all 11 columns, but `%CPU` and `%MEM` above 1% are tinted (yellow at 10%+) and `STAT` is colored, so a busy process is visible without reading every row. The `COMMAND` column is dimmed past 60 characters rather than truncated, so a browser's worth of flags never buries the pid. `USER` and `PID` are dimmed and bold respectively |

`ps` in any other form (`-eo`, `--sort`, `-o`, `-p`) runs unchanged, because
those layouts choose their own columns. Already-short forms like
`ps -eo pid,user,%mem,comm` are left alone on purpose.

### find

| Command | Output |
|---------|--------|
| `find -ls` | the eleven positional fields become a named table: INODE, LINKS, MODE, OWNER, GROUP, SIZE, MODIFIED, PATH. Directories are cyan, executables green, sizes over 10M tinted, and the path stays rightmost |

Bare `find` and `find -printf` run unchanged. Bare `find` prints one path per
line with no columns at all, so there is nothing for parse to line up, and
`-printf` already formats itself.

### du

| Command | Output |
|---------|--------|
| `du` | sizes always carry a unit, so du's default bare `22972` reads as `22972K` instead of requiring you to remember the block size. Over 10M cyan, over 100M yellow, over 1G red, and directories are bold with their trailing `/` |

`-b` / `--apparent-size` pass through, because those are asked for in machine
terms on purpose.

### findmnt

| Command | Output |
|---------|--------|
| `findmnt` | color only: the heading bolded where it stands, the branch findmnt draws in front of each target dimmed so the mountpoints are what your eye lands on, filesystems on a disk (`btrfs`, `ext4`, `xfs`, …) cyan and the kernel's own plumbing (`proc`, `sysfs`, `tmpfs`, …) dim, the options column quietened on every row — except a `ro` at its front, which turns yellow — and `USE%` tinted from 80%, red from 90% |
| `findmnt -D`, `findmnt -T /tmp`, `findmnt -t btrfs`, `findmnt -o TARGET,SIZE,USE%` | the same table with different columns in it. findmnt lines every one of them up itself, right-aligned numbers included, so parse moves nothing and reads each cell by the column the heading puts it under |

`-r` / `--raw`, `-P` / `--pairs`, `-y` / `--shell` and `-J` / `--json` pass
straight through — a script reads those bytes. `-n` prints no heading to claim
the table by, `--poll` runs until the mount table changes, so it gets the
terminal with no pager in front of it, and a heading spelled only from names
lsblk prints too (`lsblk -o FSTYPE,SIZE`) belongs to neither tool and is left
alone.

### free

| Command | Output |
|---------|--------|
| `free` | the default numbers are counts of kibibytes saying nothing about it, so each one gets the unit `free -h` would give it (`8004464` reads as `7.6G`), and the used column is tinted by how much of the row's own total is gone: 80% yellow, 90% red. The grid free prints on — a label field of eight, every value right-aligned in twelve — is left exactly where free put it |

`free -h` has already done the work, so it comes back byte for byte, tint only.
The unit-pinned forms (`-b`, `-k`, `-m`, `-g`, `--bytes`, `--si`, …) and the
repeating ones (`-s`, `-c`) pass straight through: pinned numbers are read by
scripts that chose the unit on purpose, and a table printed every second until
you interrupt it is read as it arrives. The single-line `free -L` has no header
to claim it by and is left alone.

### lsof

| Command | Output |
|---------|--------|
| `lsof`, `lsof -i`, `lsof -p`, `lsof -u`, `lsof +L1` | color only: the heading bolded where it stands, and the parenthesis lsof writes at the end of a name colored — a socket's state (`LISTEN` cyan, `ESTABLISHED` green, `TIME_WAIT` and friends yellow, `UNCONNECTED` dim), a file that has gone (`deleted` yellow), or one of lsof's own remarks (`readlink:`, `path dev=` dim). A row whose cells read as the columns above them also gets its `FD` and `TYPE` marked |

lsof lays its own table out and a row can carry an empty cell in the middle of
it, so parse moves nothing: reading the cells positionally would color the
wrong word the first time a name holds a space. The heading is painted in
place, and a row that cannot be read keeps its note and loses nothing else.

`-F` / `-F…` (one field per line) and `-t` (pids and nothing else) pass
straight through — they are a script's input, not a table. `+r` / `-r` print
forever, so they are handed the terminal with no pager in front of them.

### ip

| Command | Output |
|---------|--------|
| `ip addr`, `ip link` | each interface collapses from six indented lines into a group: name and state on the first line, `mtu`/`qdisc`/MAC/`altname` dimmed below, addresses aligned on their own lines. The `<BROADCAST,MULTICAST,UP,LOWER_UP>` flag wall is dropped and the state is colored instead (UP green, DOWN red, UNKNOWN dim) |
| `ip route` | color only; the alignment `ip` already does is left alone. `default` bold, `via`/`dev`/`proto`/`src`/`metric` dim, `linkdown` yellow |

`ip -brief` passes through untouched: it is already exactly what parse aims for.
`-j` / `-json` passes through so scripts keep working.

`ip addr` drops `valid_lft forever`, which is the same on every line. A lifetime
that actually ends is kept and shown as `expires in 64900sec`, because a DHCP
lease counting down is real information.

### ss

| Command | Output |
|---------|--------|
| `ss` | the `Peer Address:PortProcess` header, where two column names are printed with no space between them, is split apart. The `users:(("chromium",pid=3568,fd=63))` field is lifted out of the address column into its own aligned column. `ESTAB` green, `LISTEN` cyan, `UNCONN` dim, `SYN-SENT` and `FIN-WAIT` yellow |

`-o json` and the other machine formats pass through.

### lsblk

| Command | Output |
|---------|--------|
| `lsblk` | the box-drawing tree characters are dimmed so the device names dominate, `TYPE` is colored (disk bold, part dim, crypt/lvm cyan), and a device with several mountpoints has them indented under it instead of floating at the far column edge |

`-J`, `-O`, `--output` and `-P` pass through, since those are read by scripts.

### kubectl

> **Experimental.** This one is wired up and works as a command and as a pipe,
> but it has never been run against a live cluster. It was written from the
> Kubernetes docs and tested against fixtures that were written by hand, so the
> column widths and the exact header text per resource are unconfirmed. It is
> marked experimental for that reason, not because it is known broken.
>
> When it is not sure about a piece of input it writes your bytes through
> untouched rather than guessing, so the worst case is that you get plain
> `kubectl` output. If you do run a cluster and find it formatting something
> wrongly — or not at all — that is a bug worth reporting, and the first thing to
> check is which of the guards below it tripped.

| Command | Output |
|---------|--------|
| `get` | the resource table lined up column by column, with the column that answers the question tinted: `STATUS` green/yellow/red, `READY` green when all containers are up and red when none are, a `RESTARTS` count that is not zero in yellow, `AGE` dimmed. Two-word column names in a `-o wide` header (`NOMINATED NODE`, `READINESS GATES`) are kept as one column |
| `describe` | the section headings at the left margin bold, the keys nested inside them dimmed, and the values that state a condition colored wherever they appear — `Running` green, `Pending` yellow, `ImagePullBackOff` red. The two container hashes and `<none>` are dimmed so the eighty lines are not eighty things to read |

Everything else runs unchanged: `logs`, `top`, `events`, `cluster-info`,
`api-resources`, `config view`, `apply`, `delete`, `scale` and the rest of
kubectl's verbs.

Passed through untouched, because the bytes are not for a person to read:

| Flag | Why it is left alone |
|------|----------------------|
| `-o json`, `-o yaml`, `-o name` | a script or a `jq` reads these |
| `-o go-template`, `-o jsonpath`, `-o custom-columns` | the result is whatever the template said |
| `--template=...` | same |
| `--no-headers` | there is no header to line the columns up to |
| `-w`, `--watch`, `--watch-only` | a watch never ends, so it is handed to the terminal where it can be stopped |

`kubectl get all` also passes through: it prints one table per resource kind,
each with its own header, so there is no single set of columns to lay out. A row
that has been reflowed by anything else is left alone too, because lining it out
from the header would cut the values in half.

Those two rules are deliberate. A `get` table is claimed only when its header
opens with `NAME` and carries a column only kubectl prints, which is what keeps
parse off `lsblk`, whose header is also capitals opening with `NAME`. And a data
row that has drifted out of line with its header is refused outright, because
slicing it on the header's column offsets would cut values in half.

## Notes

- Piped input is auto-detected. For git: `status` (including `-s`/`-sb`), `log`,
  `diff`/`show`, `branch`, `stash list`, `remote -v`, `blame`, `shortlog` and
  `reflog`. For kubectl: `get` and `describe`. The other tools are recognized by
  their header line or row shape. Anything parse does not recognize is passed
  through unchanged.
- `git log` gets `--decorate=short` added when you have not chosen a decoration
  mode yourself. git hides branch names, tags and `HEAD` markers when its
  output is not a terminal, and parse captures through a pipe, so without this
  `parse git log` would show less than `git log` in your terminal. Pass
  `--no-decorate` or any `--pretty` / `--format` to keep git's own behavior.
- Each detector is deliberately strict, because a false positive would rewrite
  output belonging to a different tool. `ps`, `ss`, `lsblk` and `find -ls` all
  require their exact header or field layout before parse will touch the text.
  `kubectl get` needs a header that opens with `NAME` and carries a column only
  kubectl prints, which is what keeps it off `lsblk`, whose header is also
  capitals opening with `NAME`.
- `git log -p` and `--stat` fall back to colored text instead of a table, except
  that `git show` gets its patch formatted as a diff, because that is the whole
  reason to run `show`.
- A `-w` before a tool is parse's side-by-side switch; after one it belongs to
  the tool, where `git log -w` means "ignore whitespace" and `git commit -m -w`
  means the message is the word `-w`. parse only claims `-w` for the commands
  that actually print a diff (`diff`, `show`, `format-patch`, `range-diff`),
  and never when it is the value of another flag.
  `--graph` dims its branch drawing so the commits stand out.
- Commands that need an editor or prompt (`git commit` without `-m`,
  `git add -p`, `git rebase -i`) also run unchanged.
- `git bisect` passes through unchanged, because `git bisect good`/`bad` can stop
  and ask a question.
- Parsing expects the tools' English output. A locale that changes a header or a
  state word will not be recognized, and parse will pass that output through
  unchanged rather than guess.
- systemctl colorizes its own output when it writes to a terminal. parse always
  captures through a pipe, so it never sees those codes and does not need to
  strip them; on the passthrough path systemd's own colors are left alone.
- A tool's stderr is left on stderr and never mixed into the text parse formats,
  so `parse git log` outside a repository writes git's `fatal:` to stderr where
  you can see it, rather than handing a program an error as if it were data. The
  exit code is always the tool's own.

## Layout

```
├── main.go           # CLI entry, stdin reading, dispatch
├── colors.go         # ANSI color helpers
├── go.mod
├── go.sum
├── README.md
├── LICENSE
├── Makefile
├── .gitignore
│
├── cmd/
│   ├── cmd.go        # front door: one entry point per tool + the Pipe dispatcher
│   ├── git/          # all git logic: run, detect, parse, format
│   │   ├── git.go
│   │   └── git_internal_test.go
│   ├── systemctl/    # systemctl logic
│   │   ├── systemctl.go
│   │   └── systemctl_internal_test.go
│   ├── journalctl/   # journalctl logic
│   │   ├── journalctl.go
│   │   └── journalctl_internal_test.go
│   ├── ps/           # ps, find, du, findmnt, free, lsof, ip, ss, lsblk, kubectl: one folder each
│   ├── find/
│   ├── du/
│   ├── findmnt/
│   ├── free/
│   ├── lsof/
│   ├── ip/
│   ├── ss/
│   ├── lsblk/
│   └── kubectl/      # kubectl get tables and describe blocks
│
├── internal/
│   └── tool/         # plumbing shared by every tool: hooks, capture, exit codes
│       └── tool.go
│
└── test/
    ├── git_test.go    # end-to-end git tests
    ├── systemd_test.go # end-to-end systemctl and journalctl tests
    └── basic_test.go  # end-to-end tests for ps, find, du, findmnt, free, lsof, ip, ss, lsblk
```

Each tool gets its own folder so the code for one command lives in one place.
No tool imports another except `systemctl`, which reuses `journalctl`'s line
formatter for the log lines it embeds in `status`.

`internal/tool` is a separate package because the tools cannot share unexported
helpers once they are in different folders. It holds only the plumbing:
color hooks, `Cell`, running a child process, and mapping its exit status.
`main.go` supplies the colors and the table printer at startup through
`cmd.SetHooks`.

Each tool has a `*_internal_test.go` beside its source. Those are not redundant
with the end-to-end tests: they live in the tool's own package, which is the
only place its unexported helpers (`Detect`, `needsTerminal`, the parsers) can
be reached at all. The end-to-end tests in `test/` are in package `test` and can
only use exported entry points.
# parse
