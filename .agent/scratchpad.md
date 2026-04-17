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

## 2026-04-16 — Iteration 15

### Completed: Step 15 — Notifier — 5-channel cascade + env-probe

**What was done:**
- Created `internal/notify` package with 5 channels:
  - `FileSink`: writes `.forge/current/ESCALATION` sentinel file
  - `BannerSink`: writes loud ASCII banner to m.Output AND /dev/tty (bypasses --quiet)
  - `OSCSink`: emits OSC 9 escape + bell to stdout + /dev/tty (iTerm2/WezTerm/Kitty)
  - `TmuxSink`: calls `tmux display-message` when $TMUX set
  - `BeepSink`: OS-native via gen2brain/beeep
- `Channel` interface, `Message` type, `NotifyAll()` (fail-loud: each channel independent)
- `EnvProbe` struct + `Probe()` function: reads $DBUS_SESSION_BUS_ADDRESS, $DISPLAY, $WAYLAND_DISPLAY, $SSH_TTY, $TMUX, /proc/version WSL detection, $CI
- `DefaultChannels(runDir, output)` builds standard cascade order
- `SendTestNotify()` for `forge doctor --test-notify` (hidden flag)
- Integrated into EscalationManager: replaced inline sentinel write + printBanner() with notify.NotifyAll() using lazily-built DefaultChannels
- Manager.Channels field for test injection; lazy build uses m.Output at Escalate() time
- Added env-probe output to `forge doctor`; `--test-notify` hidden flag fires all channels
- 11 notify tests pass + all existing escalate/cli tests still green

**Next: Step 16** — Stuck Detector (hybrid scoring) + Completion Detector

## 2026-04-16 — Iteration 16

### Completed: Step 16 — Stuck Detector + Completion Detector

**What was done:**
- Created `internal/stuckdet` package: 4 hard signals (off_topic_drift→Tier2, placeholder_accumulation→Tier2, same_error_fingerprint_4plus→Tier3, build_broken_5plus→Tier3), 5 soft signals over 3-iter rolling window. External deaths excluded from scoring.
- Created `internal/compdet` package: weighted multi-signal completion detector; ≥8+judge_medium→Complete, 5-7→Audit, <5→Continue.
- Extended `LedgerEntry` with 10 new fields: error_fingerprint, build_status, plan_items_completed, state_semantic_delta, agent_self_report, regressions, off_topic_drift, new_high_confidence_placeholders, stuck_tier, stuck_hard_triggers, stuck_soft_sum, completion_score.
- Added `parseFinalTextSignals()`: parses `<!--FORGE:build_status/self_report/error_fp/regression=...-->` from FinalText.
- Wired stuck detector + completion detector into loop engine post-commit.
- Tier 1 action: append diagnostic to state.md. Tier 2: append plan regeneration notice to plan.md. Tier 3: escalate via EscalationManager.
- Added `HighConfidencePlaceholderCount()` to policy.ScanResult.
- 12 stuckdet unit tests + 9 compdet unit tests + 3 integration tests (Examples A/B/C) — all pass.
- Full suite: all 20 packages green.

**Next: Step 17** — Context Manager + Brain primitives

## 2026-04-16 — Iteration 17

### Completed: Step 17 — Context Manager + Brain primitives

**What was done:**
- Created `internal/brain` package: Brain struct with 6 primitives (Classify, Judge, Distill, Diagnose, Draft, Spawn)
  - Each builds a scoped prompt + invokes Backend with 120s timeout
  - Parse via prompt-engineered sentinels (key=value format); one retry on parse failure with correction prompt
  - 7 brain tests (skip in sandbox, compile + run correctly)
- Created `internal/ctxmgr` package: Context Manager with real prompt assembly + distillation
  - AssemblePrompt: 7-section composition order (system prompt, task.md, path artifact, plan.md, state.md, notes.md, per-iteration instructions)
  - Token budget enforcement via 4 chars/token heuristic; sections truncated when budget exhausted
  - Distillation triggers: state.md > 8k, notes.md > 10k, plan.md > 6k tokens
  - No Brain: archive + truncate fallback. With Brain: archive + LLM compress
  - 9 ctxmgr tests all pass
- Updated loop engine:
  - Options.Brain + Options.ContextBudgetTokens fields added
  - AssemblePrompt now uses ctxmgr.Manager instead of naive concatenation
  - Brain.Draft used for commit messages when available
  - Brain.Judge called post-iteration; verdict fed into completion detector (replaces JudgeUnknown stub)
  - Brain.Diagnose called in Tier 1 stuck handler; Brain.Draft called in Tier 2 plan regeneration
- Full suite: all 20 packages green

**Next: Step 18** — Gemini + Kiro backend adapters

## 2026-04-16 — Iteration 18

### Completed: Step 18 — Gemini + Kiro backend adapters

**What was done:**
- Created `internal/backend/gemini/adapter.go`: Gemini CLI adapter
  - Invokes `gemini -p "$PROMPT" -o stream-json --approval-mode=yolo -s`
  - Parses Gemini-shaped NDJSON: init/message/result events
  - Stats from `result.stats.models.<model>.tokens`
  - Exit code mapping: 53→Truncated, 42→input error, 1→general error
  - Capabilities: StructuredOutput=true, NativeSubagents=false, window=1M
- Created `internal/backend/kiro/adapter.go`: Kiro CLI adapter (ACP + text)
  - ACP mode: JSON-RPC 2.0 over stdio (initialize → session/new → session/prompt)
  - Text mode: `kiro-cli chat --no-interactive --trust-all-tools <prompt>`
  - Text completion via `▸ Credits:` marker detection
  - Capabilities: ACP=StructuredOutput=true, Text=false; skip flag=--trust-all-tools
- Added `gemini-stream-json` and `kiro-text` modes to fake-backend
- Updated `internal/cli/commands.go`: gemini + kiro wired into probe-backend utility
- `forge backend set gemini` / `forge backend set kiro` both work
- All 22 packages green

**Next: Step 19** — Add, Fix, Refactor paths (brownfield loop-mode variants)

## 2026-04-16 — Iteration 19

### Current Focus: Step 19 — Add, Fix, Refactor paths (brownfield loop-mode variants)

**Plan:**
1. Extend `research.go`: add `researchAdd`, `researchFix`, `researchRefactor` functions with 2-3 researchers each
2. Extend `artifacts.go`: path-specific artifacts (codebase-map.md + specs.md for Add; bug.md for Fix; target-shape.md + invariants.md for Refactor)
3. Extend `planphase.go`: Refactor pre-loop invariant gate (renders invariants, asks confirmation before main plan confirm)
4. Add `compdet/path_criteria.go`: `PathSpecificCheck(path, runDir)` function that evaluates path-specific programmatic completion criteria
5. Add `planphase_test.go`: tests for Add/Fix/Refactor happy paths + Refactor invariant gate abort + Fix completion criteria

**Key design decisions (from §4.7 and §2.1.9):**
- Add: codebase-map.md (where to integrate) + specs.md
- Fix: bug.md with repro script section; 3 researchers (reproduce, root-cause, adjacent-risk)
- Refactor: target-shape.md + invariants.md; 3 researchers (current-shape, invariants, affected-tests)
- Refactor gate: render invariants.md contents, ask "Are these behaviors preserved? [y/e/n]" before main plan confirm
- PathSpecificProgrammatic for Fix: checks if regression test added (new test file / test function in diff)
- PathSpecificProgrammatic for Add: checks if specs.md items all referenced in plan
- PathSpecificProgrammatic for Refactor: checks invariants.md presence (invariants confirmed)

## 2026-04-16 — Iteration 21

### Completed: Step 21 — Upgrade mode (dep-gate-inverted loop)

**What was done:**
- Extended `researchOutput` with Upgrade fields: UpgradeSourceVersion, UpgradeTargetVersion, UpgradeBreakingCount, UpgradeManifests
- Added `researchUpgrade()` in research.go: 2 researchers (scope + migration plan), parses key=value sentinel fields
- Added `parseUpgradeScopeFields()` to extract source_version/target_version/breaking_changes/manifests from text
- Added Upgrade artifact writing in artifacts.go: `upgrade-scope.md` + `upgrade-target.md`
- Added `runUpgradeGate()` in planphase.go: pre-loop confirmation showing source→target version + dep manifests + [y/n] prompt
- Added `DepGateInverted bool` to planphase.Result (true when path=upgrade)
- Added `DepGateInverted bool` to loopengine.Options
- Added `filterDepGateHits()` in engine.go: removes GateClassDependency/Lockfile hits when DepGateInverted=true
- Added `printChainHookSuggestion()` for upgrade:fix/upgrade:test suggestions on completion
- Added "upgrade" case in compdet/path_criteria.go: checks upgrade-scope.md exists
- Wired DepGateInverted from planphase result → loopengine options in commands.go
- Tests: TestUpgradeHappyPath, TestUpgradeGateDeclineAborts, TestUpgradeForceYes, TestDepGateInverted, TestDepGateInverted_NonDepGatesStillFire, TestPathCriteriaCheck_Upgrade
- All 23 packages green

**Next: Step 22** — Test mode (scope-restricted loop with production-touch escalation)

## 2026-04-16 — Iteration 23

### Completed: Step 23 — Composite chaining package + wiring

**What was done:**
- `internal/chain` package: chain.go (Run, ChainYML, stage lifecycle, inter-stage gates), contracts.go (reviewFix/reviewRefactor/upgradeFix/upgradeTest/fixTest and generic passthrough), ui_unix/windows.go
- Added `Predefined bool` field to `planphase.Result` struct and wired from `routerRes.Predefined`
- Updated `internal/cli/commands.go`: replaced `ActionChain` stub with real `chain.Run()` call
- Created `internal/chain/chain_test.go`: 5 tests (review:fix chain, inter-stage decline, unknown-contract warning, 4-stage warn, chain.yml written)
- Updated `cli_test.go` chain_detection test to use real chain path with --yes
- All 27 packages green

**Next: Step 24** — First-run onboarding + forge doctor + remaining CLI commands

## 2026-04-16 — Iteration 24

### Completed: Step 24 — First-run onboarding + forge doctor + remaining CLI commands

**What was done:**
- Added `ReadSidecar(forgeDir)` to `internal/state/lock` for stop command
- Added `ListRuns()` + `RunEntry` to `internal/state` for history/show/clean
- Exported `ReadLedger()` from `internal/loopengine` for status/show/resume
- Added `StartIteration int` to `loopengine.Options` for resume
- Created `internal/cli/onboarding.go`: `runFirstRunOnboarding()` scans PATH for claude/gemini/kiro-cli, prompts user if backend unset, saves to global config
- Created `internal/cli/doctor_full.go`: full `forge doctor` with 6 checks (Config/Backend/Git/ForgeDir/DiskSpace/Notifications), OK/WARN/FAIL per check, --verbose for details
- Created `internal/cli/lifecycle_cmds.go`: `selectBackend()`, `printFullStatus()`, `runHistory()`, `runShow()`, `runClean()`, `runStop()`, `runResume()`
- Updated `internal/cli/commands.go`: replaced stubs for stop/resume/history/show/clean with real implementations; replaced partial doctor with full implementation; upgraded status to use printFullStatus
- Updated `internal/cli/root.go`: added first-run onboarding in PersistentPreRunE (skipped for doctor/config/backend/help/version/lifecycle commands)
- Updated `internal/cli/cli_test.go`: replaced TestUnimplementedStubs with TestLifecycleCommands; added TestDoctorCommand and TestHistoryWithRuns
- All 27 packages pass

**Demo verified:**
- `forge history` → "No runs found." / shows run table with ID+STATE+STARTED
- `forge clean` → "Nothing to clean."
- `forge stop` → "No active run to stop."
- `forge status` → "No active run." / shows run details
- `forge show <id>` → shows run artifacts
- `forge doctor` → all checks with OK/WARN/FAIL icons
- `forge doctor --verbose` → detailed check info

**Next: Step 25** — CI + release pipeline + distribution
