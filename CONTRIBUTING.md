# Contributing

Thanks for wanting to improve parse. This document is everything you need
before opening a pull request.

## The one rule

parse formats output that is hard to read and leaves output that already
reads well exactly as the tool printed it. Every contribution is judged
against the contract in the README:

- Piped output comes back **byte for byte** unless parse confidently
  recognizes the table. If there is any doubt, the text passes through.
- Machine-readable formats (`-r`, `-P`, `-J`, `-y`, `-n`, …) and anything
  interactive or endless (`--poll`, `-f`, `+r`) are never captured or
  rewritten.
- Exit codes are the tool's own. stderr stays on stderr.
- A flood of output pages even when the tool is unsupported; a screenful
  prints straight through.

A false positive — claiming text printed by a *different* tool — is worse
than a missed formatting opportunity. Detectors are deliberately strict
about the shape they accept.

## Development setup

- Linux (parse wraps Linux-only tools), Go 1.21 or newer.
- Clone, then run the full gate:

```sh
make check
```

`make check` is `gofmt`, `go vet`, `go test ./...`, a build, and an install
to `~/.local/bin/parse`. Run it before calling anything done so the
installed binary is never behind the source.

Individual targets: `make test`, `make fmt`, `make vet`, `make build`.

## Adding support for a tool

`COMMAND-TO-ADD.md` is the roadmap: it ranks candidate tools and the
argument forms worth claiming. Pick from there and keep the change to one
tool.

1. **Create `cmd/<tool>/`** with an internal test beside the source:
   - `Run(args []string) int` — the direct path: `tool.Passthrough` for
     script forms, `tool.Capture` + `Format` otherwise. Tools that never
     end need a `NeedsTerminal(args)` guard so they are handed the
     terminal instead of captured forever.
   - `Pipe(w io.Writer, text string)` — the piped path, called only when
     `Detect` claimed the text.
   - `Detect(text string) string` — claims by the first line or heading
     alone. It cannot see flags, only text, so refuse anything whose
     shape another tool could also print.
   - `Format(w io.Writer, text string)` — paints cells by column
     position. Never move bytes: rebuild lines from the original runes
     and paint in place. Fail closed (print the input untouched) when
     the layout is not a table you understand.
2. **Register it** in `cmd/cmd.go` (import, wrapper function,
   `NeedsTerminal` case, and the detector order) and in `main.go`
   (usage line, `toolList`, dispatch switch). Detector order matters:
   each tool is tried in sequence, so a specific detector goes before a
   general one.
3. **Test it**:
   - `cmd/<tool>/<tool>_internal_test.go` — unexported helpers with real
     captured output as fixtures: every claimed argument form, byte
     identity with colors stripped, and the refusals.
   - `test/` — end-to-end claims for the exported entry point.
   - `test/pipe_test.go` — the pipe contract: machine forms and foreign
     tables must be byte-identical through the full dispatcher.
   - `pager_test.go` — if the tool can flood or run forever, assert
     when it pages and when it must not.
4. **Document it** in the README: one row per claimed form in the
   supported-tools table, and a short note on what passes through.

## Testing notes

- Fixtures are copy-pasted from real output (tree drawings, right
  alignment and all), never hand-made — real padding is where bugs hide.
- Assert byte identity by stripping colors and comparing with the raw
  input; assert colors by looking for the exact escape the design wants.
- When two detectors could claim the same text, add a test pinning down
  who wins.

## Pull requests

- One tool or one fix per PR; keep the diff reviewable.
- Include the commands you tested with and a couple of before/after
  samples of the output.
- `make check` must be green.

## Reporting bugs

Same four things the README asks for: the exact command (or the piped
source), the tool's raw output, what parse printed instead, and your
distro plus tool version. Real output from different distros is the most
useful thing you can send.

## License

MIT — by contributing you agree your changes are licensed under the same
terms.
