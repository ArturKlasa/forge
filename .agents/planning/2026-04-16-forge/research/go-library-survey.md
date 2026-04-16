# Research: Go library survey for Forge

**Date:** 2026-04-16
**Scope:** Verified library recommendations for Forge's Go implementation, grounded in April 2026 ecosystem data (stars, recent releases, maintenance status, relevant issues).

## Summary matrix (decisions at a glance)

| # | Category | Pick | Alternative |
|---|---|---|---|
| 1 | CLI framework | `spf13/cobra` | `alecthomas/kong` if type-safe flags preferred |
| 2 | YAML | `goccy/go-yaml` | — |
| 3 | File watching | `fsnotify/fsnotify` | — |
| 4 | Notifications | `gen2brain/beeep` | — |
| 5 | Spinner | `briandowns/spinner` | — |
| 6 | Logging | stdlib `log/slog` | `rs/zerolog` if pretty console matters |
| 7 | Git | shell out via `os/exec` | `go-git/go-git` only if PATH-less required |
| 8 | UUID | `google/uuid` (v7) | `gofrs/uuid` |
| 9 | JSON-RPC 2.0 | `sourcegraph/jsonrpc2` | roll-your-own |
| 10 | Subprocess | stdlib `os/exec` + thin wrapper | `go-cmd/cmd` |
| 11 | TTY detect | `golang.org/x/term` | `mattn/go-isatty` |
| 12 | ANSI color | `charmbracelet/lipgloss` | `fatih/color` |
| 13 | Config merge | `knadh/koanf/v2` | `spf13/viper` |

**Total third-party surface:** 9 direct deps + `golang.org/x/term`.

---

# Forge Go Library Survey (April 2026)

A single-binary orchestration CLI that spawns AI backend processes, watches files, reads/writes YAML, emits notifications, and touches git. Target Go 1.22+, cross-platform.

## 1. CLI framework

| Lib | Stars (Apr 2026) | Latest release | Nested subcmds | Positional+flags | Shell completion | Man pages | Env binding |
|---|---|---|---|---|---|---|---|
| `spf13/cobra` | ~43.5k | active (point releases through 2026) | yes | yes | bash/zsh/fish/pwsh built-in | built-in | via viper or manual |
| `urfave/cli/v3` | ~22k (v3.8.0, Mar 25 2026) | active | yes | yes | built-in | no (third-party) | first-class (`Sources: cli.EnvVars(...)`) |
| `alecthomas/kong` | ~3k (1.x stable) | active | yes | yes, typed via struct tags | via `willabides/kongplete` | via `miekg/king` | via `env:""` tag |

**Recommendation: `spf13/cobra`.** Not because it's most elegant — `kong` is — but because Forge orchestrates other CLIs (`claude`, `gemini`, `gh`) whose users already have muscle memory for the cobra idiom. Dominates comparable-tool neighborhood (kubectl, hugo, gh, helm, docker). Help text universally recognizable, completion generation battle-tested across four shells, man page generation one function call. Main pitfall: pulls `spf13/pflag` whose maintenance has been questioned — but pflag is frozen-complete rather than abandoned.

Consider `kong` for compile-time type safety. Avoid `urfave/cli/v3` — v3 brought breaking changes from v2 and migration churn still visible.

- https://github.com/spf13/cobra

## 2. YAML

`gopkg.in/yaml.v3` is effectively **archived**; comment-preservation bug (go-yaml/yaml#709) is a showstopper for a user-visible config file.

`goccy/go-yaml` (~2,272 importers, latest release January 8, 2026) advertises **reversible round-tripping with anchors, aliases, and comments preserved**, passes all 402 YAML test suite cases plus ~60 extras, exposes an AST for surgical edits. Field order preserved on round-trip through AST.

**Recommendation: `goccy/go-yaml`.** High-level `Marshal`/`Unmarshal` for reads; drop to `ast` package when writing back so comments survive. Pitfall: issue #225 — naive AST-then-marshal can still mangle some comment placements. Write integration tests with golden-file fixture.

- https://github.com/goccy/go-yaml
- https://github.com/go-yaml/yaml/issues/709

## 3. File watching

**Recommendation: `fsnotify/fsnotify`.** v1.9.0 current, active in 2026. Backends: inotify (Linux), kqueue (BSD/macOS), ReadDirectoryChangesW (Windows), FEN (illumos).

Pitfalls:
- **Watch the directory, not the file.** Editors save atomically via tmp-then-rename; watcher on original inode dies silently. Watch `filepath.Dir(path)` and filter by `Event.Name`.
- **Debounce events.** One "save" can fire Create + Write + Rename + Chmod. Debounce 50–100 ms.
- **kqueue eats file descriptors** — one per watched file/dir.
- **Linux `inotify` has a watch-count limit** (`fs.inotify.max_user_watches`, default ~8192). Fail gracefully.
- Windows: tune `WithBufferSize` on high-churn directories.

- https://github.com/fsnotify/fsnotify
- https://github.com/fsnotify/fsnotify/issues/372

## 4. OS-native notifications

`martinlindhe/notify` is **deprecated** — README redirects to `gen2brain/beeep`.

**Recommendation: `gen2brain/beeep`.** Linux: D-Bus. macOS: `terminal-notifier` fallback to osascript. Windows 10/11: WinRT toast. Windows 7: win32.

Gotchas:
- macOS: without `terminal-notifier` installed, falls back to AppleScript (generic alert, no icon/buttons). Document `brew install terminal-notifier` path.
- Windows unpackaged apps: toast attribution may show generic "PowerShell" identity unless AppUserModelID registered.
- Linux headless / no D-Bus: call errors — swallow, don't crash.

Skip `nikoksr/notify` — multi-channel isn't what Forge needs locally.

- https://github.com/gen2brain/beeep

## 5. Spinner / progress indicators

**Recommendation: `briandowns/spinner`** for plan-phase spinner. Lightweight, no Bubble Tea event loop.

Pitfall: does **not** auto-detect non-TTY. Gate yourself: `isatty.IsTerminal(os.Stdout.Fd())` before constructing — otherwise `\r` lands in logs/CI as ugly noise.

`schollz/progressbar/v3` is for known-length progress (file downloads); overkill for indeterminate "planning…". `charmbracelet/bubbles` implies adopting full Bubble Tea architecture — skip for v1.

- https://github.com/briandowns/spinner

## 6. Structured logging

**Recommendation: stdlib `log/slog`.** In 2026, unambiguously good enough for a CLI. Benchmarks match zerolog on allocations (~40 B/op), beat zap on memory; throughput ~20–30% behind zerolog but irrelevant at CLI volumes.

Forge's requirements served cleanly:
- **Text for humans**: `slog.NewTextHandler` + custom `ReplaceAttr` to strip timestamps.
- **`--json` mode**: swap in `slog.NewJSONHandler`. One-line toggle.
- **Structured fields**: `slog.With("run_id", id)` propagation idiomatic.

Pitfall: slog's default text handler emits `level=INFO time=... msg=...` — fine for logs, ugly for user output. Keep slog for *diagnostics*, use direct `fmt.Fprintln` (styled via lipgloss) for *user-facing output*.

- https://pkg.go.dev/log/slog

## 7. Git operations

**Recommendation: shell out to `git` via `os/exec`.** This is what `gh`, `lazygit`, `glab` all do. Forge's requirements (read HEAD, detect dirty, commit, branch, `reset --hard`, protected-branch guard) are simple plumbing commands with stable text output.

Why not `go-git/go-git`:
- **Lacks porcelain.** README: covers "majority of plumbing read ops and some main write ops, but lacks the main porcelain operations such as merges." Wall on first weird user action.
- **Status is known-unreliable.** Issue #119 (unmodified files reported as untracked) is multi-year-open.
- **Security churn.** 2026 alone: CVE-2026-34165 (malicious idx file OOM), CVE-2026-33762 (Index v4 panic). Inherit CVE feed for no reason when `git` is on every dev machine.
- **Go version tax.** v5.14+ requires Go 1.23.
- **Co-existence with user config.** `~/.gitconfig`, signing keys, credential helpers, hooks, aliases all work transparently when you shell out, all fail when you don't.

Build thin `internal/git` package wrapping `exec.Command("git", ...)`, parse `git status --porcelain=v2` (stable machine format), use `git rev-parse HEAD`, etc.

- https://github.com/go-git/go-git
- https://github.com/cli/cli

## 8. UUID generation

**Recommendation: `google/uuid`** using **UUIDv7** via `uuid.NewV7()` (in stable API since v1.6.0). Time-ordered, k-sortable in filenames/logs, RFC 9562 standardized. Perfect for Forge run-ids/session-ids.

`google/uuid` vs `gofrs/uuid`: both well-maintained; `google/uuid` has v7 stable (gofrs still marks v6/v7 experimental), broader adoption, fixed 16-byte array.

- https://github.com/google/uuid

## 9. JSON-RPC 2.0 client

**Recommendation: `sourcegraph/jsonrpc2`** for line-oriented stdio protocol like Kiro ACP. Accepts any `io.ReadWriteCloser` via `NewConn`, supports LSP framing and plain line-delimited JSON. Battle-tested for a decade powering Sourcegraph's LSP clients.

Alternatives rejected:
- `golang.org/x/tools/internal/jsonrpc2` — internal, can't import.
- `ybbus/jsonrpc` — HTTP-only.
- Roll-your-own — 300–500 LOC of fiddly concurrency (request ID allocation, pending-request map, cancellation, notification/request dispatch). Start with the library; fall back if it fights you.

- https://github.com/sourcegraph/jsonrpc2

## 10. Process management / subprocess orchestration

**Recommendation: stdlib `os/exec` with ~150-line internal wrapper.** Not `go-cmd/cmd`.

Forge needs enough custom behavior (SIGTERM-then-SIGKILL escalation with grace, tee stdout to log+live pipe, track children) that you'll write the wrapper anyway — and it's simpler than adapting `go-cmd`'s model.

Wrapper must:
1. **Process group isolation.** Unix: `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}`. Windows: `CREATE_NEW_PROCESS_GROUP` via `SysProcAttr.CreationFlags`. Essential — without it, killing `claude` won't kill its MCP server children.
2. **Kill by `-pid` on Unix**, `taskkill /T` on Windows. Not `cmd.Process.Kill()`.
3. **Graceful escalation**: SIGTERM, wait N seconds, SIGKILL. Stdlib's `CommandContext` only supports SIGKILL (golang/go#21135); implement escalation yourself.
4. **Tee stdout/stderr**: `io.MultiWriter(liveDecoder, logFile, ringBuffer)`. Ring buffer captures last N KiB for error reporting.
5. **`cmd.Wait()` only after all pipe readers return** — known deadlock pitfall.

- https://pkg.go.dev/os/exec
- https://github.com/golang/go/issues/21135

## 11. Terminal detection

**Recommendation: `golang.org/x/term`.** `term.IsTerminal(int(os.Stdout.Fd()))`. Official x/ repo, Go compatibility promise in spirit, handles platform matrix internally.

`mattn/go-isatty` works fine (transitive dep of cobra/viper/lipgloss anyway) but no reason to import directly when `x/term` does the job.

Gate spinner AND color on a single `isInteractive()` helper: `term.IsTerminal(stdout)` AND `os.Getenv("NO_COLOR") == ""` AND `os.Getenv("CI") == ""`. Respect `NO_COLOR` (https://no-color.org).

- https://pkg.go.dev/golang.org/x/term

## 12. ANSI color

**Recommendation: `charmbracelet/lipgloss`.** v2 landed with explicit TTY-detection control, automatic color downsampling (truecolor → 256 → 16 → mono), clean `Style.Render(s)` ergonomics.

`fatih/color` (~7k stars, maintained) fine for basic `color.Red("…")`. But for Forge's plan-phase output, iteration summary boxes, escalation prompts — lipgloss's layout primitives (borders, padding, joining) pay for themselves. Honors `NO_COLOR`, `COLORTERM`/`TERM` automatically.

Pitfalls:
- Automatic downsampling reads stdout; writing to stderr? `lipgloss.NewRenderer(os.Stderr)`.
- Don't import both `fatih/color` and `lipgloss` — independent color-profile caches will disagree.

- https://github.com/charmbracelet/lipgloss

## 13. Config merging

**Recommendation: `knadh/koanf/v2`.**

`spf13/viper` functional/ubiquitous, but wrong tradeoff for 2026 greenfield:
- Forcibly lowercases keys (breaks YAML/TOML/JSON specs).
- Bloats binary substantially (~313% larger per koanf authors).
- Hardcoded file-format detection, transitive dep sprawl.
- Tight coupling to pflag.

`knadh/koanf/v2` does exactly what Forge needs:
- Layered `Load()` calls: defaults → global → repo → env → flags. Last-loaded wins, or `WithMergeFunc` for custom precedence.
- Providers/parsers separate modules — import only YAML + env + posflag. Small binary.
- Preserves key case.
- Works alongside cobra via `github.com/knadh/koanf/providers/posflag`.

Pitfall: no auto-reload on file change — wire fsnotify yourself if wanted (probably unnecessary for orchestration CLI).

- https://github.com/knadh/koanf
- https://github.com/knadh/koanf/wiki/Comparison-with-spf13-viper

---

## Sources

- [spf13/cobra](https://github.com/spf13/cobra)
- [alecthomas/kong](https://github.com/alecthomas/kong)
- [urfave/cli v3](https://github.com/urfave/cli/releases)
- [goccy/go-yaml](https://github.com/goccy/go-yaml)
- [go-yaml/yaml issue #709](https://github.com/go-yaml/yaml/issues/709)
- [fsnotify/fsnotify](https://github.com/fsnotify/fsnotify)
- [fsnotify issue #372](https://github.com/fsnotify/fsnotify/issues/372)
- [gen2brain/beeep](https://github.com/gen2brain/beeep)
- [briandowns/spinner](https://github.com/briandowns/spinner)
- [log/slog stdlib](https://pkg.go.dev/log/slog)
- [go-git/go-git](https://github.com/go-git/go-git)
- [cli/cli](https://github.com/cli/cli)
- [google/uuid](https://github.com/google/uuid)
- [sourcegraph/jsonrpc2](https://github.com/sourcegraph/jsonrpc2)
- [golang/go#21135](https://github.com/golang/go/issues/21135)
- [golang.org/x/term](https://pkg.go.dev/golang.org/x/term)
- [charmbracelet/lipgloss](https://github.com/charmbracelet/lipgloss)
- [knadh/koanf](https://github.com/knadh/koanf)
- [koanf vs viper](https://github.com/knadh/koanf/wiki/Comparison-with-spf13-viper)
