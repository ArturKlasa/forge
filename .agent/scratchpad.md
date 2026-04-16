# Forge v1 Implementation Scratchpad

## 2026-04-16 — Iteration 1

### Current Focus: Step 1 — Project scaffolding + CLI skeleton

**Understanding:**
- All 25 steps unchecked; this is the first iteration
- Need to create a complete Go CLI with cobra, all subcommand stubs, global flags
- Module path: `github.com/arturklasa/forge` (using ArturKlasa git user)
- Directory layout: `cmd/forge/main.go`, `internal/cli/`, `internal/version/`, `testdata/`
- All commands return "not implemented yet (scheduled for step N)" errors
- Need golangci-lint config + Makefile

**Plan:**
1. Create directory structure
2. Initialize Go module
3. Write version package
4. Write cobra command tree with all stubs
5. Write tests (smoke tests per command)
6. Set up golangci-lint config
7. Set up Makefile
8. Run tests + verify demo works
9. Commit

## 2026-04-16 — Iteration 2

### Completed: Step 2 — Logger (slog + lipgloss) + terminal output modes

**What was done:**
- Created `internal/log/log.go`: Logger with slog text/JSON handlers, lipgloss renderer
- Global singleton via `Init(Config)` + `G()` accessor
- Strips timestamps in text mode via ReplaceAttr
- JSON mode routes slog to UserOut (stdout) for machine parsing
- NO_COLOR, CI, non-TTY all disable interactive/color mode
- PersistentPreRunE in root.go wires flags → log.Init before any subcommand
- Status stub now logs "status requested" via slog
- 12 tests all passing

**Demo verified:**
- `forge --json status` → valid NDJSON to stdout
- `NO_COLOR=1 forge --help` → no ANSI codes

## 2026-04-16 — Iteration 3

### Completed: Step 3 — Config system (koanf) with layered precedence

**What was done:**
- Created `internal/config/config.go`: full Config schema (all §5.2 fields with koanf+yaml tags), Manager type, Load() with 4-layer precedence (defaults → global → repo → env), goyamlParser using goccy/go-yaml, mapProvider using koanf/maps Unflatten
- Created `internal/config/write.go`: SetKey/UnsetKey for writing nested YAML key updates to target files
- Added InitWithHandler to `internal/log` for test log capture
- Wired `config`, `config get/set/unset/edit` commands in commands.go - fully functional
- Wired `backend set` command using config system
- 7 config tests pass + CLI tests updated (removed now-implemented commands from stubs test, added TestConfigCommands)
- CLI loads config via --path flag for test isolation

**Demo verified:**
- `forge config set backend.default claude` → sets key in repo config
- `forge config get backend.default` → `claude`
- `forge config` → prints full merged YAML with all defaults

## 2026-04-16 — Iteration 4

### Completed: Step 4 — State manager — run directories + lifecycle markers

**What was done:**
- Created `internal/state` package with `Manager`, `RunDir`, lifecycle `Marker` constants
- Atomic marker transitions via `renameio` (write new → remove old), ensuring no two markers visible simultaneously
- `currentRunRef` abstraction: symlink on Unix, text file on Windows (with fallback detection)
- `Manager.Init()` creates `.forge/runs/` and idempotently adds `.forge/` to `.gitignore`
- `forge status` now reads current run and displays ID + marker + elapsed time
- Added hidden `forge test-utility create-test-run` dev command (to be removed before v1 ship)
- 13 state tests + 2 CLI tests pass; full suite green

**Demo verified:**
- `forge status` → "No active run." when no run
- `forge test-utility create-test-run test-2026-04-16-001 && forge status` → shows RUNNING

## 2026-04-16 — Iteration 5

### Completed: Step 5 — Single-run lock with PID + start-time tuple

**What was done:**
- Created `internal/state/lock` package with `Lock`, `Sidecar`, `ErrLocked` types
- `Acquire(forgeDir, runID)` → `gofrs/flock` advisory lock + atomic sidecar JSON `{pid, run_id, start_time_ns, hostname}`
- Stale-lock recovery in `handleConflict`: hostname mismatch → refuse; dead PID → stale; PID alive but start_time mismatch → PID reuse → stale; all match → ErrLocked
- Linux start time via `/proc/<pid>/stat` field 22 + `/proc/stat btime`, CLK_TCK hardcoded to 100
- Darwin start time via `unix.SysctlRaw("kern.proc.pid")` → KinfoProc.P_starttime
- Windows start time via `GetProcessTimes`; Windows liveness via `OpenProcess + GetExitCodeProcess`
- Network FS detection: Linux statfs magic numbers, Darwin Fstypename, Windows GetDriveType; fallback to PID-file-only mode
- 7 tests all pass; concurrent test uses subprocess helper via TestMain env var pattern
- Wired lock into `forge "<task>"` RunE; added `forge test-utility hold-lock` for demo
- Demo verified: two terminals, lock contention shows correct ErrLocked message

**Demo verified:**
- `forge test-utility hold-lock demo-2026-04-16-143022` → "(running... lock held...)"
- `forge "Fix the bug"` → "Error: another forge run is active / Run: demo-... (PID ...) / Run 'forge status' to inspect."

## 2026-04-16 — Iteration 6

### Completed: Step 6 — Git helper (shell-out wrapper)

**What was done:**
- Created `internal/git/git.go`: Git struct, all core operations (IsRepo, HEAD, IsDirty, CreateBranch, Checkout, Commit, ResetHard, DiffSinceLastCommit, Log, Tag, Version)
- Created `internal/git/protected.go`: IsProtected with 8-tier strategy (config → GitHub rulesets API → legacy protection → GitLab → default branch → .github/rulesets/*.json → pre-commit hooks → offline convention fallback)
- HumanConfirmation type-level safety gate on ResetHard
- Log format uses \x1f separator (null bytes rejected by OS exec)
- Added google/uuid dependency for UUID-v7 run IDs
- 17 tests all pass covering all tiers + all core ops
- Wired forge doctor to show git version, HEAD, protected branches
- Demo: `forge doctor` → git version, HEAD, protected branches detected via offline convention

## 2026-04-16 — Iteration 7

### Completed: Step 7 — Process wrapper (Job Object / setsid + SIGTERM→SIGKILL escalation)

**What was done:**
- Created `internal/proc` package with 6 files
- `ring_buffer.go`: thread-safe RingBuffer (default 64KiB), io.Writer implementation with wrap-around
- `exit_classification.go`: ExitClassification enum (Normal/IterationFail/ForgeTerminated/ExternalSignal) + Result struct
- `proc.go`: Wrapper type with New/Start/Terminate/Kill/Wait/RingBuffer/Cmd; stdout+stderr tee via io.MultiWriter; deadlock-free Wait (drains pipe goroutines first); idempotent Wait via sync.Once
- `proc_unix.go`: SysProcAttr{Setpgid:true, Setsid:true}; Terminate sends SIGTERM to -pgid, SIGKILL after grace; classifyExit uses WaitStatus.Signaled() + forgeKilled flag
- `proc_windows.go`: Job Object with JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE; CREATE_SUSPENDED → AssignProcessToJobObject → ResumeThread
- Tests skip gracefully when setpgid/setsid is blocked (sandbox detection via TestMain probe)
- All tests pass (2 run, 7 skipped in sandbox due to setpgid restriction)

**Environment note:**
- This sandbox blocks setpgid/setsid syscalls. Added TestMain probe + skipIfNoSetpgid helper.
- In a real environment, all subprocess tests would run.

## 2026-04-16 — Iteration 8

### Completed: Step 8 — Backend interface + fake-backend test binary

**What was done:**
- Created `internal/backend/backend.go`: Backend interface, Capabilities, Prompt, IterationOpts, IterationResult, Event, TokenUsage types per design §4.5
- Created `cmd/fake-backend/` binary with 3 modes:
  - `text`: reads prompt from stdin, matches against script, prints canned response
  - `stream-json`: emits NDJSON matching Claude Code's shape (system/init, assistant, result events)
  - `acp`: minimal JSON-RPC 2.0 server over stdio handling initialize/session/new/session/prompt
- Script loading supports CSV and YAML formats; pattern matching is case-insensitive substring
- 11 tests all pass including round-trip tests per mode, schema validation, and ACP sequence test
- testdata/trivial.csv and trivial.yaml created

**Decision note:** Used manual JSON-RPC 2.0 for fake-backend ACP mode (not sourcegraph/jsonrpc2) since the fake-backend only needs simple request-response. The real Kiro adapter (step 18) will use jsonrpc2 library.

**Demo verified:**
- `fake-backend --mode stream-json --script testdata/trivial.csv <<<"create a hello-world"` → 3 NDJSON lines with system/init, assistant, result events

## 2026-04-16 — Iteration 9

### Current Focus: Step 9 — Claude Code backend adapter

**Plan:**
1. Create `internal/backend/claude/adapter.go` — Adapter struct implementing Backend interface
2. Build args: `claude --bare -p --output-format stream-json --verbose --permission-mode dontAsk --allowedTools <list> --session-id <uuid> --no-session-persistence`
3. Stream parser: scan stdout line-by-line, JSON-decode, dispatch on `type` field
4. Completion signal: `type == "result"`; extract TokenUsage from result.usage
5. Timeout: use proc.Wrapper SIGTERM→SIGKILL; accept IterationOpts.Timeout
6. Tests: use fake-backend binary as the `claude` executable via env var or adapter option
7. Add `forge test-utility probe-backend claude <prompt-file>` command for demo
8. Add `testdata/trivial-prompt.md` for demo
9. Tick checkbox and commit

**Key design decisions:**
- Tests use fake-backend by overriding the executable path (ClaudeExecutable option)
- Session ID: uuid.New() per call, always pass --no-session-persistence
- Error subtypes: success, error_max_turns, error_max_budget_usd → map to IterationResult fields

## 2026-04-16 — Iteration 10

### Completed: Step 10 — Intent Router

**What was done:**
- Created `internal/router/` package with `Router` type
- `Route()` implements the 4-step ladder: keyword fast-path → LLM classifier → human escalation
- `keywordTable` maps all 10 path trigger verbs per §2.1.2
- `detectChain()` uses regex to find "X and Y" multi-verb patterns; `PredefinedChains` map for v1 inter-stage contracts
- `WithPathOverride(p)` option short-circuits (--path flag equivalent)
- LLM classifier parses `path=<name>` and `confidence=<low|medium|high>` from backend response
- `forge plan` command updated to use Router and show detected path/chain
- 8 router tests + 4 CLI integration tests all pass
- Demo verified: "Fix the login redirect bug" → fix path; "Review and fix the auth module" → review:fix chain

**Next: Step 11** — Plan Phase (Create path) + confirmation UI

## 2026-04-16 — Iteration 11

### Completed: Step 11 — Plan Phase (Create path) + confirmation UI

**What was done:**
- Created `internal/planphase/` package with 5 files:
  - `planphase.go`: Run() function, Options/Result types, confirmation UI rendering
  - `pregates.go`: dirty tree + protected branch checks with auto-switch
  - `research.go`: 1-2 research subagents for Create path (stub when no backend)
  - `artifacts.go`: writes task.md, target-shape.md, plan.md, state.md, notes.md
  - `ui_unix.go` + `ui_windows.go`: platform-specific raw terminal key reading
- 10 tests all pass: happy path, abort, --yes, dirty tree gate, protected branch auto-switch, edit key, backend research, redo research, run ID format, task slug
- Fixed flag conflict: plan command's local `--path` flag renamed to `--mode` to avoid conflict with root persistent `--path` (working directory)
- Chain detection returns early (before pre-gates) — chains handled in step 23
- Updated CLI tests to use new plan command behavior
- Demo verified: protected branch auto-switch, --yes bypass, n abort, full plan display

**Key bug fixed:** Local `--path` flag in plan subcommand conflicted with root persistent `--path` (working dir), causing wrong workDir when --path was set. Renamed to --mode.

**Next: Step 12** — Loop Engine — minimal Ralph loop, first end-to-end demo

## 2026-04-16 — Iteration 13

### Completed: Step 13 — Policy Scanners (Security + Placeholder + Gate)

**What was done:**
- Created `internal/policy` package with 3 sub-scanners + coordinator:
  - `security.go`: SecurityScanner wrapping gitleaks v8 `detect` package. Uses isolated viper instance (not global). Supports custom `.gitleaks.toml` override. `DetectBytes()` on diff → SecretFinding list.
  - `placeholder.go`: PlaceholderScanner with compiled regex table (11 patterns: 9 high-conf + 2 low-conf). Parses unified diff added lines; skips test files, `forge:allow-todo` inline suppression, `TODO(#N)` tracked forms. Hunk header parsing for accurate line numbers.
  - `gate.go`: GateScanner with hardcoded path tables for dependency manifests, CI/CD pipelines, secret-env files, lockfiles. `testsPassed` flag controls lockfile-only auto-OK vs hard-stop classification.
  - `policy.go`: Scanner coordinator, ScanResult, AppendPlaceholderLedger (writes placeholders.jsonl).
- Added git helper methods: StageAll, DiffCached, UnstageAll, CommitStaged
- Updated loop engine (engine.go): stage-then-scan-then-commit flow instead of diff-then-commit. Policy scanner wired between backend result and git commit. Hard stop → unstage, print ESCALATION, break loop.
- 16 policy tests (all pass) + 2 new loop engine integration tests (PolicyScannerGateHalt, PolicyScannerSecretHalt)
- Key finding: gitleaks allowlists `AKIAIOSFODNN7EXAMPLE` via stopword `.+EXAMPLE$` (AWS docs example key)
- Key design: `git diff HEAD` doesn't show untracked files → must stage first for complete policy scan

**Next: Step 14** — Escalation Manager (mailbox pair + fsnotify)

## 2026-04-16 — Iteration 14

### Completed: Step 14 — Escalation Manager (mailbox pair + fsnotify)

**What was done:**
- Created `internal/escalate` package (11 files):
  - `escalate.go`: Manager type, Escalate() method, GateScannerEscalation() builder, --auto-resolve logic
  - `mailbox.go`: renderAwaitingHuman() (YAML frontmatter format), ParseAnswer() with strict validation
  - `atomic.go` + `atomic_unix.go` + `atomic_windows.go`: AtomicWrite shim (renameio on Unix, natefinch/atomic on Windows)
  - `watcher.go`: fsnotify directory watch + 250ms debounce + size-stability check + 2s polling fallback
  - `sidecar.go`: regex patterns for vim/JetBrains/emacs sidecar ignore
  - `netfs_linux.go` + `netfs_other.go` + `netfs_windows.go`: network FS detection via statfs
- 13 tests all pass: all 4 editor save styles, ID mismatch→stale rename, partial write race, network-FS polling, auto-resolve 5s, awaiting-human.md written, sidecar ignored, ParseAnswer valid/invalid
- Loop engine updated: EscalationManager option; gate hard-stops now call Escalate() and dispatch on answer (a=commit, s=unstage+continue, p/d=break)
- Added natefinch/atomic v1.0.1 dependency

**Next: Step 15** — Notifier — 5-channel cascade + env-probe
