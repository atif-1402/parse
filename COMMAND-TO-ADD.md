# Top 50 Developer CLI Commands

### Legend

- ✅ — Strong `parse` candidate
- 🟡 — Potentially useful, but output is already reasonably readable
- ❌ — Generally not worth a dedicated parser
- ⭐ — Especially strong candidate for `parse`

| # | Command | Complex outputs / arguments worth parsing |
|---:|---|---|
| 1 | `git` | ⭐ `status`, ⭐ `diff`, ⭐ `log`, `show`, `branch -vv`, `worktree list`, `stash list`, `remote -v`, `tag -l`, `shortlog` |
| 2 | `docker` | ⭐ `ps`, ⭐ `images`, ⭐ `stats`, ⭐ `inspect`, `network inspect`, `volume inspect`, `system df`, `info`, `events` |
| 3 | `kubectl` | ⭐ `get`, ⭐ `describe`, ⭐ `logs`, ⭐ `top`, `events`, `config view`, `api-resources`, `cluster-info`, `diff` |
| 4 | `systemctl` | ⭐ `status`, ⭐ `list-units`, ⭐ `list-unit-files`, `list-dependencies`, `show`, `--failed`, `list-timers`, `list-sockets` |
| 5 | `journalctl` | ⭐ `-u`, ⭐ `-b`, ⭐ `-p`, ⭐ `-f`, `--list-boots`, `--disk-usage`, `-k`, `-g` |
| 6 | `ip` | ⭐ `addr`, ⭐ `route`, ⭐ `link`, ⭐ `neigh`, `rule`, `maddr`, `tunnel` |
| 7 | `ss` | ⭐ `-tulpn`, ⭐ `-tanp`, ⭐ `-s`, `-o`, `-i`, `-m`, `-K` |
| 8 | `ps` | ⭐ `aux`, ⭐ `-ef`, ⭐ `-eo`, `--forest`, `-eLf`, `-ww` |
| 9 | `find` | 🟡 `-ls`, `-printf`, `-exec` output depends on command; no dedicated parser priority |
| 10 | `ls` | ⭐ `-l`, ⭐ `-lah`, `-li`, `-lR`, `--time-style`, `-Z` |
| 11 | `du` | ⭐ `-h`, ⭐ `-ah`, ⭐ `--max-depth`, ⭐ `-d`, `--apparent-size`, `--inodes` |
| 12 | `df` | ❌ already aligns its own columns and names its headings — parse has nothing to add |
| 13 | `lsblk` | ⭐ default, ⭐ `-f`, ⭐ `-o`, ⭐ `-a`, `-t`, `-m`, `-S` |
| 14 | `free` | ⭐ default, ⭐ `-h`, `-w`, `-t`, `-s` |
| 15 | `top` | ⭐ interactive output, `-b`, `-H`, `-p` |
| 16 | `htop` | ⭐ interactive process view |
| 17 | `lsof` | ⭐ default, ⭐ `-i`, ⭐ `-p`, `-u`, `+L1`, `-nP` |
| 18 | `mount` | ⭐ default, `-l`, `-t`, `-v` |
| 19 | `findmnt` | ⭐ default, ⭐ `-D`, ⭐ `-T`, `-t`, `-o`, `--df` |
| 20 | `blkid` | ⭐ default, `-o`, `-s`, `-p` |
| 21 | `nmcli` | ⭐ `device`, ⭐ `connection`, ⭐ `general`, ⭐ `device show`, `connection show`, `radio`, `networking` |
| 22 | `curl` | ⭐ `-I`, ⭐ `-v`, ⭐ `-i`, ⭐ JSON/API output, `-w`, `--trace`, `--trace-ascii` |
| 23 | `wget` | 🟡 recursive/download status output, `--server-response`, `--spider` |
| 24 | `ssh` | 🟡 `-v`, `-vv`, `-vvv` — extremely noisy, but parser usefulness depends on goal |
| 25 | `scp` | ❌ Mostly progress/output rather than structured information |
| 26 | `rsync` | ⭐ `-av`, ⭐ `--stats`, ⭐ `--progress`, `-n`, `-i` |
| 27 | `tar` | 🟡 `-tv`, `-tvf`, verbose archive listings |
| 28 | `gzip` | ❌ Generally simple output |
| 29 | `openssl` | ⭐ `x509 -text`, ⭐ `s_client`, `req -text`, `verify`, `x509 -noout`, `version -a` |
| 30 | `dig` | ⭐ default, ⭐ `+trace`, ⭐ `+stats`, `+answer`, `+short` |
| 31 | `nslookup` | 🟡 Mostly readable already |
| 32 | `ping` | 🟡 `-D`, `-i`, `-c` — useful but not particularly complex |
| 33 | `traceroute` | ⭐ default, ⭐ `-n`, `-I`, `-T`, `-A` |
| 34 | `nmap` | ⭐ default scans, ⭐ `-sV`, ⭐ `-O`, ⭐ `-A`, `-sC`, `--script` |
| 35 | `make` | ⭐ `-n`, verbose/build output; parser depends heavily on build system |
| 36 | `gcc` | ⭐ compiler diagnostics, ⭐ `-v`, `-###`, `-H`, linker output |
| 37 | `clang` | ⭐ diagnostics, ⭐ `-v`, `-###`, `-ftime-report`, analyzer output |
| 38 | `cargo` | ⭐ `build`, ⭐ `test`, ⭐ `check`, ⭐ `tree`, `metadata`, `install`, `update` |
| 39 | `npm` | ⭐ `list`, ⭐ `outdated`, ⭐ `audit`, ⭐ `ls`, `fund`, `doctor`, `view` |
| 40 | `pnpm` | ⭐ `list`, ⭐ `outdated`, ⭐ `audit`, `why`, `store status`, `licenses` |
| 41 | `yarn` | ⭐ `list`, ⭐ `why`, `outdated`, `audit`, `info` |
| 42 | `pip` | ⭐ `list`, ⭐ `show`, ⭐ `check`, ⭐ `freeze`, `index versions` |
| 43 | `uv` | ⭐ `pip list`, ⭐ `pip show`, ⭐ `tree`, `lock`, `sync` |
| 44 | `go` | ⭐ `test`, ⭐ `list`, ⭐ `mod graph`, ⭐ `mod why`, ⭐ `env`, `version -m` |
| 45 | `java` | ⭐ `-XshowSettings`, `jcmd`, `jps`, `jstack`, `jmap`, `jstat` |
| 46 | `mvn` | ⭐ dependency tree, ⭐ dependency analyze, test/build diagnostics |
| 47 | `gradle` | ⭐ `dependencies`, ⭐ `tasks`, ⭐ `properties`, `projects`, `buildEnvironment`, build output |
| 48 | `terraform` | ⭐ `plan`, ⭐ `show`, ⭐ `state list`, ⭐ `state show`, `providers`, `graph`, `validate` |
| 49 | `git-lfs` | ⭐ `ls-files`, ⭐ `status`, `env`, `logs`, `prune`, `locks` |
| 50 | `jq` | 🟡 Output is already deliberately structured; `parse` is less valuable unless transforming huge JSON |

