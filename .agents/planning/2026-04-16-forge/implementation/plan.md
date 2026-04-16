# Forge v1 — Implementation Plan

**Instructions to the implementer:**

> Convert the design into a series of implementation steps that will build each component in a test-driven manner following agile best practices. Each step must result in a working, demoable increment of functionality. Prioritize best practices, incremental progress, and early testing, ensuring no big jumps in complexity at any stage. Make sure that each step builds on the previous steps, and ends with wiring things together. There should be no hanging or orphaned code that isn't integrated into a previous step.

**Context documents assumed available during implementation:**

- `../rough-idea.md`
- `../idea-honing.md`
- `../research/claude-code-cli.md`
- `../research/gemini-cli.md`
- `../research/kiro-cli.md`
- `../research/go-library-survey.md`
- `../research/security-patterns.md`
- `../research/process-lifecycle.md`
- `../research/fsnotify-patterns.md`
- `../research/notification-fallbacks.md`
- `../design/detailed-design.md` — **single source of truth for behavior**

When details are missing here, consult the design. This plan intentionally stays concise on behavior and focuses on sequencing.

---

## Progress checklist

Each checkbox corresponds 1:1 to a step below. Tick as you complete each step's demo.

- [x] **Step 1:** Project scaffolding + CLI skeleton
- [x] **Step 2:** Logger (slog + lipgloss) + terminal output modes
- [x] **Step 3:** Config system (koanf) with layered precedence
- [x] **Step 4:** State manager — run directories + lifecycle markers
- [x] **Step 5:** Single-run lock with PID + start-time tuple
- [x] **Step 6:** Git helper (shell-out wrapper)
- [x] **Step 7:** Process wrapper (Job Object / setsid + SIGTERM→SIGKILL escalation)
- [x] **Step 8:** Backend interface + fake-backend test binary
- [x] **Step 9:** Claude Code backend adapter
- [x] **Step 10:** Intent Router — keyword + LLM classifier + escalation ladder
- [x] **Step 11:** Plan Phase (Create path) + confirmation UI
- [x] **Step 12:** Loop Engine — minimal Ralph loop, first end-to-end demo
- [x] **Step 13:** Policy scanners — Security (gitleaks) + Placeholder + Gate
- [x] **Step 14:** Escalation Manager — mailbox pair + `awaiting-human.md` / `answer.md` flow
- [x] **Step 15:** Notifier — 5-channel cascade + env-probe
- [x] **Step 16:** Stuck Detector (hybrid scoring) + Completion Detector
- [x] **Step 17:** Context Manager + Brain primitives (Classify / Judge / Distill / Diagnose / Draft)
- [x] **Step 18:** Gemini + Kiro backend adapters
- [ ] **Step 19:** Add, Fix, Refactor paths (brownfield loop-mode variants)
- [ ] **Step 20:** One-shot modes — Review, Document, Explain, Research
- [ ] **Step 21:** Upgrade mode (dep-gate-inverted loop)
- [ ] **Step 22:** Test mode (scope-restricted loop with production-touch escalation)
- [ ] **Step 23:** Composite chaining — stage lifecycle + inter-stage contracts
- [ ] **Step 24:** First-run onboarding + `forge doctor` + remaining CLI commands
- [ ] **Step 25:** CI + release pipeline + distribution (GitHub Releases / Homebrew / curl / go install)

---

## Step 1: Project scaffolding + CLI skeleton

**Objective:** initialize the Go project, set up the cobra CLI with all subcommand stubs. Every `forge <command>` should compile and return sensible help/version output even though behavior is stubbed.

**Implementation guidance:**
- `go mod init github.com/<owner>/forge` (owner decided at initialization).
- Directory layout: `cmd/forge/main.go`, `internal/cli/`, `internal/version/`, `testdata/`.
- Use `spf13/cobra` for the command tree per design §4.1. Register all subcommand stubs: root (task), `plan`, `status`, `stop`, `resume`, `history`, `show`, `clean`, `backend`, `backend set`, `config`, `config get/set/unset/edit`, `doctor`.
- Register all documented global flags (`--verbose`, `--quiet`, `--json`, `--yes`, `--auto-resolve`, `--timeout`, `--path`, `--branch`, `--no-branch`, `--brain`, `--backend`, `--chain`, `--subagents`).
- Stubs return a `not implemented in step N` error with the step that will implement them.
- Set up `golangci-lint` config; enable `gofmt`/`govet`/`revive`/`errcheck`/`staticcheck`.
- Set up a Makefile or `Justfile` with `build`, `test`, `lint`, `run` targets.

**Tests:**
- Smoke test per command: `forge --help`, `forge <subcommand> --help` each produce non-empty output, exit 0.
- `forge --version` prints a parseable semver.
- Stubs exit nonzero with the expected "not implemented" message for each unimplemented command.

**Integration:** this is the foundation. Future steps replace stubs one at a time.

**Demo:**
```
$ forge --help
# Shows command tree with all subcommands documented.

$ forge --version
forge 0.0.1

$ forge status
Error: not implemented yet (scheduled for step 4).
```

---

## Step 2: Logger (slog + lipgloss) + terminal output modes

**Objective:** structured logging for diagnostics + styled user-facing output with `--verbose` / `--quiet` / `--json` modes and TTY detection.

**Implementation guidance:**
- `internal/log` package.
- Use stdlib `log/slog` with `slog.NewTextHandler` for default; swap to `slog.NewJSONHandler` under `--json` mode.
- Custom `ReplaceAttr` to strip timestamps from text handler for clean CLI output.
- Separate user-facing output stream via `fmt.Fprintln` + `charmbracelet/lipgloss` for styling.
- Respect `NO_COLOR`, `CI`, and non-TTY (via `golang.org/x/term.IsTerminal`) — disable color + spinner when any is set.
- Global logger singleton initialized from root command's persistent-pre-run based on flags.
- Per-run `forge.log` file sink added later when State Manager lands (step 4).

**Tests:**
- Golden tests: for each mode combination (default / `--verbose` / `--quiet` / `--json` / `NO_COLOR=1`), capture output, assert shape.
- TTY detection toggle test: force `IsTerminal=false` and assert styling disabled.
- JSON mode emits valid NDJSON parseable by `encoding/json` line-by-line.

**Integration:** replaces naive `fmt.Println` in step 1 stubs. All later components log through this.

**Demo:**
```
$ forge --json status
{"time":"...","level":"INFO","msg":"status requested","implemented":false}

$ NO_COLOR=1 forge --help
# Help output without ANSI color codes.
```

---

## Step 3: Config system (koanf) with layered precedence

**Objective:** global + per-repo YAML config with documented precedence; `forge config get/set/unset/edit` fully functional.

**Implementation guidance:**
- `internal/config` package.
- Use `knadh/koanf/v2` with providers: file (YAML via `goccy/go-yaml`), env, posflag (from cobra/pflag).
- Layered load order: built-in defaults → `~/.config/forge/config.yml` → `<repo>/.forge/config.yml` → env vars (`FORGE_*`) → CLI flags. Last wins.
- Define the full schema as Go structs per design §5.2.
- Implement config commands:
  - `config` prints merged YAML.
  - `config get <key>` prints single value.
  - `config set <key> <value>` writes to per-repo by default; `--global` flag writes to global.
  - `config unset <key>` removes override.
  - `config edit` opens effective file in `$EDITOR`.
- Unknown keys → log a warning, proceed (forward compatibility).

**Tests:**
- Layer precedence: set same key at each layer with different values; assert flag wins.
- Round-trip: marshal config, reload, deep-equal.
- Unknown-key: add `foo: bar` to config; assert warning logged, no crash.
- Edit cmd: mock `$EDITOR=true` (no-op); assert file opened and saved.

**Integration:** every later component reads from this config.

**Demo:**
```
$ forge config set backend.default claude
$ forge config get backend.default
claude
$ forge config
backend:
  default: claude
# (full merged output)
```

---

## Step 4: State manager — run directories + lifecycle markers

**Objective:** create/manage `.forge/` and per-run directories; write/read lifecycle state markers; manage `current` symlink. No lock yet.

**Implementation guidance:**
- `internal/state` package.
- `RunDir` type encapsulates a run's path + metadata.
- Lifecycle markers are marker files at the run root: `RUNNING`, `AWAITING_HUMAN`, `PAUSED`, `DONE`, `ABORTED`, `FAILED`. Single-writer discipline: only the main Forge process writes markers.
- Atomic marker transitions: write new marker first via `renameio`/`natefinch atomic`, then delete old.
- `current` handling: symlink on Unix, plain text file on Windows. Abstraction behind `currentRunRef`.
- `forge status` (basic version): read `current`, show marker state + paths. Full output shape from §4.19.1 comes in step 24 after ledger is available.
- Write `forge.log` sink into the run dir via logger from step 2.
- Initialize `.gitignore` if missing: add `.forge/` line.

**Tests:**
- Run directory creation with correct permissions.
- Marker atomic transitions — no "two markers present" state visible to a concurrent reader.
- `current` symlink creation + resolution; Windows text-file fallback path (can be test-flag toggled on any OS).
- `.gitignore` idempotent augmentation.

**Integration:** loop engine (step 12+), escalation manager, etc. all live inside this directory structure.

**Demo:**
```
$ forge status
No active run.

# (manually: forge test-utility create-test-run — to be removed before v1 ship)
$ forge status
Run:   test-2026-04-16-001
State: RUNNING (started 2s ago)
```

---

## Step 5: Single-run lock with PID + start-time tuple

**Objective:** enforce one-active-run-per-repo via `gofrs/flock` + sidecar tuple; stale-lock recovery via liveness + start-time comparison.

**Implementation guidance:**
- `internal/state/lock` subpackage.
- Acquire lock via `gofrs/flock` on `.forge/lock`. Keep fd open for Forge's entire lifetime.
- Sidecar `.forge/lock.json` records `{pid, run_id, start_time_ns, hostname}` atomically via the atomic-write shim.
- Platform-specific start-time queries:
  - Linux: parse `/proc/<pid>/stat` field 22 (starttime in jiffies since boot; convert via `/proc/stat btime` + CLK_TCK).
  - macOS: `unix.Sysctl` on `KERN_PROC_PID`.
  - Windows: `windows.GetProcessTimes`.
- Liveness: `syscall.Kill(pid, 0)` on Unix (ESRCH = dead; EPERM = alive); `windows.OpenProcess` + `GetExitCodeProcess` on Windows.
- Stale-lock recovery algorithm per design §4.3. Retry once after removing confirmed-stale sidecar.
- Network FS detection (Linux `unix.Statfs` magic numbers, macOS `Fstypename`, Windows `GetDriveType DRIVE_REMOTE`). On network FS, skip `flock`, use PID-file-only mode with warning.

**Tests:**
- Acquire + release: single-instance works.
- Concurrent acquisition: second process refused with correct message.
- Stale-lock recovery: write sidecar for dead PID; assert cleanup + acquisition.
- PID-reuse simulation: write sidecar for live-but-unrelated PID with wrong start time; assert treated as stale.
- Hostname mismatch: write sidecar with different hostname; assert refusal.
- Network-FS fallback: mock `statfs` return; assert PID-file-only path.

**Integration:** `forge "<task>"` and every long-running command acquires the lock before creating a run directory.

**Demo:**
```
$ forge "Create a hello"  # (still stubbed end-to-end, but now holds lock)
(running...)

# In another terminal:
$ forge "Fix the bug"
Error: another forge run is active
  Run: forge/2026-04-16-143022-create-a-hello (PID 48231)
  Run 'forge status' to inspect.
```

---

## Step 6: Git helper (shell-out wrapper)

**Objective:** wrap all git operations via `os/exec`. Cover: `IsRepo`, `HEAD` (sha + branch), `IsDirty`, `Commit` (with trailer), `CreateBranch`, `Checkout`, `ResetHard`, `DiffSinceLastCommit`, `Log`, `Tag`, tiered `IsProtected`.

**Implementation guidance:**
- `internal/git` package; never import `go-git` (reasoning in design §Appendix A).
- Parse `git status --porcelain=v2` (stable machine-readable format) for dirty detection.
- `IsProtected(branch)` tiered strategy per design §Appendix B.4: config file → GitHub rulesets API (via `gh api` if `gh` CLI available) → legacy protection → GitLab/Bitbucket API → default-branch → `.github/rulesets/*.json` → pre-commit-config grep → offline convention fallback. Degraded-mode logging when auth unavailable.
- Commit message template with `Run-Id:` / `Iteration:` / `Path:` trailers. Message itself comes from Brain call later (step 17); for now accept pre-rendered message string.
- All operations timeout via `context.Context` (default 30s).

**Tests:**
- Each operation against a `t.TempDir()`-initialized repo.
- `IsProtected` per tier (mock `gh` / API responses); assert fallback sequence.
- Dirty detection against a freshly-created file.
- `Commit` writes correct trailer; `Run-Id` is a UUID-v7.
- `ResetHard` rejects when caller doesn't pass an `IHaveHumanConfirmation: true` struct flag (type-level safety gate).

**Integration:** used by Plan Phase (dirty check), Loop Engine (auto-commit), Escalation Manager (reset option).

**Demo:**
```
$ forge doctor  # (partially wired now)
git: OK (version 2.43.0)
repo: OK (HEAD=abc123 on main)
protected branches: main, master (detected via github rulesets API)
```

---

## Step 7: Process wrapper (Job Object / setsid + SIGTERM→SIGKILL escalation)

**Objective:** subprocess lifecycle with guaranteed tree-kill — Windows Job Object + Unix `Setpgid`/`Setsid`. External-signal classification.

**Implementation guidance:**
- `internal/proc` package.
- Unix: `SysProcAttr{Setpgid: true, Setsid: true}`. Kill via `syscall.Kill(-pgid, SIGTERM)`.
- Windows: Job Object with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` via `golang.org/x/sys/windows` directly (no third-party wrapper — reasoning in research `process-lifecycle.md`). Keep job handle open for Forge lifetime.
- Graceful escalation: `Cmd.Cancel` + `Cmd.WaitDelay = 10s` (Go 1.20+ pattern) plus custom `terminateTree` helper for Unix process groups.
- External-signal classification: on subprocess exit, inspect `ProcessState.Sys().(syscall.WaitStatus).Signal()` on Unix, exit code on Windows. Distinguish Forge-initiated TERM/KILL from OS SIGCONT/SIGHUP/SIGPIPE/SIGSEGV.
- Tee stdout/stderr: `io.MultiWriter(liveDecoder, logFile, ringBuffer)` with configurable ring buffer size (default 64KiB).
- Deadlock-free `Wait`: only after all pipe readers return.

**Tests:**
- Spawn `bash -c 'sleep 100 & sleep 200'` (Unix) / `cmd /c "start /b sleep 100 & sleep 200"` (Windows); SIGTERM; assert both children die within grace period.
- External-signal simulation: spawn process that self-sends SIGHUP; assert classification as `external`, not `forge-initiated`.
- SIGTERM → SIGKILL escalation timing: spawn unkillable-via-TERM process (ignore handler); assert SIGKILL after 10s.
- Ring buffer: dump 1MB to stdout, assert last 64KiB retained.

**Integration:** every backend CLI invocation (step 8+) goes through this wrapper.

**Demo:**
```go
// Test harness
wrapper := proc.New(cmd, proc.WithGracePeriod(10*time.Second))
wrapper.Start()
// ... 500ms later
wrapper.Terminate()  // entire tree dies cleanly
```

---

## Step 8: Backend interface + fake-backend test binary

**Objective:** define `Backend` interface + implement `testutil/fake-backend` binary that emits canned responses in text, stream-json, and ACP modes.

**Implementation guidance:**
- `internal/backend` package: interface definitions per design §4.5 (with cost tracking fields removed per v1 scope).
- `Capabilities` struct with fields for structured-output / subagent / skip-permissions-flag / effective-window.
- `cmd/fake-backend/main.go`: reads CSV or YAML script file mapping prompt patterns → canned responses; emits in the requested `--mode {text|stream-json|acp}` format.
  - `text` mode: print response to stdout + exit code.
  - `stream-json` mode: emit NDJSON events matching Claude Code's shape (system/init, assistant, result).
  - `acp` mode: JSON-RPC 2.0 over stdio (Kiro ACP) using `sourcegraph/jsonrpc2`. Handles `initialize`, `session/new`, `session/prompt`.
- Event types, field names, and completion sentinels match each real backend's published contract (verified in research notes).
- Fake-backend used by all later integration tests.

**Tests:**
- Round-trip test per mode: script → fake-backend → parse → assert content.
- Schema validation: canned stream-json events pass a JSON schema matching Claude Code docs.
- ACP mode: full `initialize` → `session/new` → `session/prompt` → response sequence.

**Integration:** enables end-to-end testing of backend-driven logic without live API.

**Demo:**
```
$ fake-backend --mode stream-json --script testdata/trivial.csv <<<"create a hello-world"
{"type":"system","subtype":"init","session_id":"..."}
{"type":"assistant","message":{"content":[{"type":"text","text":"Here's a hello.go..."}]}}
{"type":"result","subtype":"success","result":"done","duration_ms":42,...}
```

---

## Step 9: Claude Code backend adapter

**Objective:** first real backend adapter. Invokes `claude` CLI with proper flags, parses stream-json, detects completion.

**Implementation guidance:**
- `internal/backend/claude` package.
- Invocation: `claude --bare -p --output-format stream-json --verbose --permission-mode dontAsk --allowedTools <list> --session-id <uuid> --no-session-persistence`, prompt via stdin.
- Stream parser: scanner on stdout, JSON-decode each line; dispatch on `type` field. Completion signal: `type == "result"` event.
- Token usage extracted from `result.usage` for context-budget tracking.
- External-signal handling via Process wrapper from step 7.
- Test against fake-backend (step 8) emitting Claude-shaped stream-json.

**Tests:**
- End-to-end via fake-backend: send prompt, parse stream, assert `IterationResult` correctness.
- Error subtype handling: `error_max_turns`, `error_max_budget_usd` (even though we don't use budget, it can still appear).
- Timeout: slow fake-backend; assert SIGTERM after 30min cap (test uses shorter cap).
- Session-leak protection: assert `--session-id` is unique per call + `--no-session-persistence` always set.

**Integration:** plugs into the Backend interface from step 8.

**Demo:**
```
$ forge probe-backend claude testdata/trivial-prompt.md
# (test utility — sends prompt through adapter, prints parsed result)
Response: PONG
Tokens: 4 in / 7 out
Exit: success (duration 1.2s)
```

---

## Step 10: Intent Router — keyword + LLM classifier + escalation ladder

**Objective:** map `<task>` → mode via the 4-step ladder. Keyword fast-path first; LLM classifier via Backend on miss; human escalation on low confidence; confirmation safety net downstream.

**Implementation guidance:**
- `internal/router` package.
- Static keyword table covering all 10 modes from §2.1.2 (Create/Add/Fix/Refactor/Upgrade/Test/Review/Document/Explain/Research).
- First-token lowercase match; case-insensitive.
- On miss: invoke Brain-backed classifier (calls the configured backend via step 9's adapter) with a strict prompt demanding `path=<name>`/`confidence=<low|medium|high>` shape.
- Low confidence or tie → return `NeedsHumanEscalation` marker. The caller (Plan Phase in step 11) handles UI.
- `--path <name>` flag short-circuits the ladder.
- Chain detection: also detect multi-verb patterns ("review AND fix") and propose chain from §2.1.17 predefined contracts. Return either single mode or chain list.

**Tests:**
- Each known verb → expected mode.
- Unknown verb → falls through to LLM classifier.
- LLM-low-confidence return value.
- `--path create` overrides even on an unambiguous verb.
- Multi-verb chain detection: "Review and fix the login bug" → `review:fix`.
- Ambiguous multi-verb (not in predefined contracts) → warns but still returns the chain.

**Integration:** Plan Phase (step 11) calls this; `forge plan` command also uses it.

**Demo:**
```
$ forge plan "Fix the login redirect bug"
Detected: fix path (confidence: high; keyword match)
(...plan phase would continue in step 11)

$ forge plan "Review and fix the auth module"
Detected: review:fix chain
Stages: 1/Review → 2/Fix
```

---

## Step 11: Plan Phase (Create path) + confirmation UI

**Objective:** full plan-phase pipeline for the Create path — pre-gates, research, artifact generation, confirmation UI with y/n/e/r keystroke handling.

**Implementation guidance:**
- `internal/planphase` package with per-path dispatcher; step 11 implements **only** Create.
- Flow per §4.7: intent detect (via router) → pre-gates (dirty tree check via git helper; protected-branch check) → research (1–2 subagents spawned via Backend) → synthesize into `specs.md` + `plan.md` + `task.md` → render confirmation UI.
- Confirmation UI via lipgloss; keystroke input via `golang.org/x/term` (raw mode for single-char read).
- Handlers: `y` → transition state to RUNNING, return to caller; `n` → ABORTED; `e` → spawn `$EDITOR` on `plan.md`, re-render on save; `r` → rerun research.
- `--yes` flag bypasses prompt.
- Cost/iteration estimate: iteration-range only (no USD per v1 scope decision).

**Tests:**
- Happy path end-to-end via fake-backend: task → plan rendered → `y` → returns RUNNING handle.
- Dirty-tree escalation: uncommitted file → pre-gate halt.
- Protected-branch: on `main` → auto-switches to `forge/...` branch.
- Keystroke handling: mocked TTY input; each key produces correct transition.
- `e` key opens `$EDITOR=cat` (no-op); re-renders.

**Integration:** Loop Engine (step 12) consumes Plan Phase output.

**Demo:**
```
$ forge plan "Create a trivial hello-world Go CLI"
Researching codebase... done
Drafting plan... done

Path:     Create
Estimate: ~3–6 iterations (hard cap: 100)
Branch:   forge/2026-04-16-150032-create-trivial-hello

Plan:
  1. Initialize Go module
  2. Write main.go with flag parsing
  3. Add README

[y] go  [n] abort  [e] edit  [r] redo plan

> n
Aborted.
```

---

## Step 12: Loop Engine — minimal Ralph loop, first end-to-end demo

**Objective:** wire plan-phase output + backend + git + basic ledger into a working per-iteration loop. **This is the first end-to-end working demo of Forge.**

**Implementation guidance:**
- `internal/loopengine` package — minimal version.
- Per-iteration sequence:
  1. Assemble `prompt.md` naively (task.md + plan.md + state.md concatenated — real context manager in step 17).
  2. Invoke `Backend.RunIteration`.
  3. Append ledger entry with minimal fields (iteration, exit, files_changed, duration).
  4. Commit diff via git helper if non-empty (message = `forge(create): iter N`; real Brain-drafted messages come in step 17).
  5. Decrement `max_iterations`; enforce `max_duration`.
  6. Check naive completion: if backend output contains `TASK_COMPLETE` or all tests pass (for Create), exit loop.
  7. Emit terminal summary line via logger from step 2.
- No stuck detection, no distillation, no policy scanning — those come in steps 13–17.
- On cap reached: escalate as Tier-3 (stub — full escalation in step 14; for now print + exit).

**Tests:**
- End-to-end via fake-backend scripted to emit "TASK_COMPLETE" after 3 iterations. Assert: 3 iterations ran, 3 commits, ledger has 3 entries, DONE marker.
- Cap enforcement: `max_iterations=2`; script with 5 needed; assert halt at iter 2.

**Integration:** `forge "<task>"` now runs end-to-end for the Create path against a real Claude Code backend.

**Demo:**
```
$ forge "Create a hello-world Go CLI"
# (Plan phase runs, user confirms)
[14:30:01] iter 1 · create · files=2 · duration=24s
[14:30:45] iter 2 · create · files=3 · duration=31s
[14:31:22] iter 3 · create · files=0 · TASK_COMPLETE
DONE — 3 iterations, 3 commits
```

This is the **minimum viable Ralph**. Everything after this tightens safety, adds modes, and polishes UX.

---

## Step 13: Policy scanners — Security (gitleaks) + Placeholder + Gate

**Objective:** secret-in-diff detection (gitleaks), placeholder scanner (regex + per-file ledger), gate scanner (file-path policy gates).

**Implementation guidance:**
- `internal/policy` package with three sub-scanners.
- **Security Scanner:** embed `github.com/zricethezav/gitleaks/v8/detect`. Use `DetectBytes([]byte)` on diff output. Default ruleset gitleaks built-in 222. Respect `.gitleaksignore` + inline `gitleaks:allow`. `.gitleaks.toml` override.
- **Placeholder Scanner:** regex table per design §Appendix B.3 + research `security-patterns.md`. Go struct with per-language pattern groups. Two phases: per-iteration diff-mode (added lines) and pre-completion full-file-mode (touched files). `placeholders.jsonl` tracks per-file detections across iterations with `active|resolved|pre-existing` status.
- **Gate Scanner:** file-path matcher per §4.10.3 + research. Hard-stop classes + lockfile conditional class. `GateHit` struct with `{class, file, reason}`. `gates.additional_*` config respected.
- All three run between backend result and git commit in Loop Engine; on any hit, loop halts and triggers escalation (stubbed here; real escalation in step 14).

**Tests:**
- Security: inject AWS key regex in diff → hard stop. Inline `gitleaks:allow` → no stop.
- Placeholder: Go `panic("not implemented")` in added lines → block completion. `TODO(#123)` → counted, not blocked. Pre-existing placeholders → excluded.
- Gate: `package.json` change → hit with class=dependency. `yarn.lock` alone with tests passing → classed as `lockfile-only-ok`. `.github/workflows/ci.yml` → hit with class=ci.
- Integration test: end-to-end loop where fake-backend introduces each hit type; assert correct halt + escalation-marker set.

**Integration:** Loop Engine invokes scanners in the per-iteration sequence before commit.

**Demo:**
```
$ forge "Add AWS integration"
[14:30:01] iter 1 · create · files=2 · duration=24s
[14:30:45] iter 2 · ESCALATION — Gate Scanner hit
  File: package.json (class: dependency)
  Reason: added: aws-sdk@^2.1400.0
  Loop halted; run 'forge resume' or respond via .forge/current/answer.md
```

---

## Step 14: Escalation Manager — mailbox pair + `awaiting-human.md` / `answer.md` flow

**Objective:** two-file mailbox with atomic writes, fsnotify directory-watch, id validation, debounce, graceful resume.

**Implementation guidance:**
- `internal/escalate` package.
- **Write path:** compose `awaiting-human.md` per §5.4.1, atomically write via `renameio`/`natefinch atomic` shim.
- **Directory watcher** via `fsnotify/fsnotify` on the run dir; filter by exact basename `answer.md`.
- **Debounce 250ms** + size-stability check (`Stat` twice 20ms apart).
- **Editor-sidecar ignore patterns** per §4.15.4.
- **Parse `answer.md`:** CRLF→LF normalize, require `id:` + `answer:` + `---` terminator; strict validation.
- **Id mismatch:** rename to `answer.stale.md.<timestamp>`, log, continue waiting.
- **On successful consume:** `os.Remove("answer.md")`.
- Network-FS fallback: polling (2s interval) when `statfs` detects NFS/sshfs/SMB/FUSE.
- Option-dispatch table: each escalation type declares its valid options + action handler callbacks.
- Integrates with `--yes`, `--auto-resolve {accept-recommended|abort|never}`.

**Tests:**
- Each editor-save pattern (truncate-overwrite, atomic-rename, vim backup-style, JetBrains) drives `answer.md`; assert parse + action + delete.
- Id mismatch → rename to `answer.stale.md.<timestamp>`.
- Partial write race: write half-file, sleep, complete; assert stability-check rejects partial, reads complete.
- Network-FS polling path via mocked `statfs`.
- `--auto-resolve accept-recommended`: assert recommended option chosen after 5s delay.

**Integration:** Gate Scanner (step 13) escalations now have a real UI; Stuck Detector (step 16) will too.

**Demo:**
```
$ forge "Add AWS integration"
[14:30:45] iter 2 · ESCALATION ...
(user writes answer.md via editor)
  id: esc-...-001
  answer: a
  ---
[14:32:18] escalation resolved: applied (a)
[14:32:19] iter 3 · create · files=1
```

---

## Step 15: Notifier — 5-channel cascade + env-probe

**Objective:** layered notification cascade (FileSink, BannerSink, OSCSink, TmuxSink, BeepSink); env-probe in `forge doctor`.

**Implementation guidance:**
- `internal/notify` package.
- `Channel` interface with `Name()`, `Available()`, `Notify(ctx, msg)`.
- Five built-in channels per §4.17:
  - **FileSink** writes `.forge/current/ESCALATION` sentinel.
  - **BannerSink** writes loud ASCII banner to stdout AND `/dev/tty`.
  - **OSCSink** emits OSC 9 + bell to stdout + `/dev/tty`.
  - **TmuxSink** shells to `tmux display-message` if `$TMUX` set.
  - **BeepSink** via `gen2brain/beeep`.
- Escalation Manager fires all channels in order unconditionally. Per-channel result logged to `forge.log`.
- `forge doctor` (partial in step 24) invokes env-probe: `$DBUS_SESSION_BUS_ADDRESS`, `$DISPLAY`, `$SSH_TTY`, `$TMUX`, `/proc/version` WSL match, `$CI`, etc.
- Test-notify subcommand (hidden flag `forge doctor --test-notify`) with user consent per §4.17.1.

**Tests:**
- Per-channel unit tests with mocked outputs.
- Fail-loud: force `BeepSink` to return error; assert other channels still fire; assert `forge.log` records failure.
- `/dev/tty` write bypass under `--quiet`.
- Env-probe matrix: various environment combinations produce expected availability map.

**Integration:** Escalation Manager (step 14) now fires the full cascade on every escalation.

**Demo:**
```
$ forge --quiet "Add AWS integration"  # quiet mode — no iteration lines
(iterations run silently)

# When escalation hits:
========================================================================
ESCALATION — Forge needs your decision
Run: forge/2026-04-16-143022-add-aws · Iter 2 · Tier 3
Gate Scanner hit: package.json modification
Options: [a] apply  [s] revert  [p] pivot  [d] defer  ·  Recommended: [s]
========================================================================
# (OS notification also fires; .forge/current/ESCALATION sentinel written)
```

---

## Step 16: Stuck Detector (hybrid scoring) + Completion Detector

**Objective:** full stuck-detection (hard-signal gates + soft-signal additive sum) + completion-detection (multi-signal + judge veto + placeholder block).

**Implementation guidance:**
- `internal/stuckdet` package. Implement each signal as a pure predicate per §4.13. Hard signals → direct tier; soft signals → weighted sum → tier. Final tier = `max(hard, soft, ceiling)`.
- Worked examples from §2.1.8.1 become fixture-based golden tests.
- Tier actions:
  - Tier 1: Brain-Diagnose (stubbed until step 17; use fake-backend in tests).
  - Tier 2: Brain-Draft regenerate plan.md (stubbed until step 17).
  - Tier 3: invoke Escalation Manager (step 14).
- `internal/compdet` package. Implement per-mode programmatic checks + LLM judge (stubbed until step 17) + Placeholder scan integration + veto logic.
- External-signal deaths and rate-limit backoffs are **excluded** from stuck signals — they use separate retry pathways.
- Ledger entry schema extended with `stuck_tier`, `stuck_hard_triggers`, `stuck_soft_sum`, `completion_score` fields.

**Tests:**
- Three worked examples produce expected tiers.
- Signal predicates unit-tested against hand-crafted ledger windows.
- Completion: force various signal combinations; assert threshold behavior.
- External-signal deaths during an iteration don't increment stuck signals.
- Rate-limit 3× then success → no stuck accumulation.

**Integration:** Loop Engine (step 12) now invokes Stuck Detector post-commit + Completion Detector mid-loop.

**Demo:** Fake-backend scripted to produce each of the three worked-example scenarios; Forge runs them end-to-end and the user sees:
- Example A → Tier 0 → continues.
- Example B → Tier 2 → plan regenerated (visible in `state.md`).
- Example C → Tier 3 → escalation fires (cascade from step 15).

---

## Step 17: Context Manager + Brain primitives

**Objective:** real prompt composition with token budgets + automatic distillation; Brain primitives (Classify / Judge / Distill / Diagnose / Draft / Spawn) all backed by backend CLI calls.

**Implementation guidance:**
- `internal/context` package. Full `prompt.md` composition per §4.4 composition order with token-budget enforcement (approximate via 4 chars/token heuristic; exact via backend's reported usage post-invocation, reconciled in ledger).
- Distillation triggers on file-size thresholds (`state.md > 8k`, `notes.md > 10k`, `plan.md > 6k`). Archive compressed output; original moved to `state-<iter>.md`/etc.
- Emergency compression mid-iteration (on context-exhaustion signal from backend): git-stash incomplete diff → distill → regenerate prompt → retry; max 2 attempts.
- `internal/brain` package. Each primitive builds a scoped prompt template + invokes Backend. Parse structured response via prompt-engineered sentinels; retry once on parse failure with correction prompt.
- Primitives power: Intent Router (step 10)'s Classify; Stuck Detector (step 16)'s Diagnose; Completion Detector (step 16)'s Judge; Loop Engine auto-commit messages; Plan Phase research; this step wires them all together.

**Tests:**
- Prompt composition: assemble for each mode; assert token budget respected; assert path-specific sections present.
- Distillation: grow `notes.md` past threshold; assert distill fires + original archived.
- Each Brain primitive: feed known input to fake-backend, assert correct parse.
- Parse-failure retry: force malformed response; assert one correction-prompt retry before giving up.
- Emergency compression path: simulate truncated output; assert stash → distill → retry.

**Integration:** Loop Engine (step 12) now has real context management; Stuck Detector (step 16) calls Diagnose; Completion Detector calls Judge.

**Demo:**
```
$ forge "Create a CLI to convert markdown to HTML"
(long task — notes.md grows)
[14:35:12] iter 14 · create · files=1 · notes_distilled (was 10.2k → 3.8k)
```

---

## Step 18: Gemini + Kiro backend adapters

**Objective:** second and third backend adapters to match capability coverage.

**Implementation guidance:**
- `internal/backend/gemini`: invocation per research `gemini-cli.md`. Stream-json parse; completion on `result` event. `--approval-mode=yolo -s` for sandboxed unattended run. Pin tested CLI version via `--version` probe.
- `internal/backend/kiro`: two sub-modes:
  - **ACP (preferred):** `kiro-cli acp` via `sourcegraph/jsonrpc2`. Handle `initialize` + `session/new` + `session/prompt`. Parse events including `_kiro.dev/compaction/status`.
  - **Text fallback:** `kiro-cli chat --no-interactive --trust-all-tools`. Prompt is positional arg. Completion via `▸ Credits:` marker (documented as fragile; ACP preferred).
- `KIRO_API_KEY` required for headless — surfaced in `forge doctor` + onboarding if backend selected.
- Capability matrix entries updated.

**Tests:**
- Gemini: fake-backend emits Gemini-shaped stream-json; adapter parses; `IterationResult` correct.
- Kiro ACP: fake-backend ACP mode; full JSON-RPC handshake; session/prompt round-trip.
- Kiro text: fake-backend text mode with `▸ Credits: X • Time: Ys` footer; completion detected.
- Per-backend skip-permissions flag correctly passed.

**Integration:** `forge backend set gemini` / `forge backend set kiro` now work; users can actually use these backends.

**Demo:**
```
$ forge backend set gemini
Default backend set to gemini.

$ forge "Create a hello-world"
(runs end-to-end against Gemini CLI)
```

---

## Step 19: Add, Fix, Refactor paths (brownfield loop-mode variants)

**Objective:** three more loop-lifecycle paths with path-specific artifacts + completion criteria.

**Implementation guidance:**
- Extend Plan Phase dispatcher (step 11) with Add / Fix / Refactor variants.
  - **Add:** 2–3 researchers; produces `codebase-map.md` + `specs.md`.
  - **Fix:** 2–3 researchers (reproduce + root-cause candidates + adjacent-risk); produces `bug.md` with repro script section.
  - **Refactor:** 2–3 researchers (current-shape + invariants + affected-tests); produces `target-shape.md` + `invariants.md`.
- Path-specific completion criteria per §2.1.9 wired into Completion Detector (step 16).
- Refactor's pre-loop invariant-confirmation gate (separate prompt before main plan-confirm) per §4.7 pre-loop gates.
- Path-specific prompt templates for the Loop Engine (used by Context Manager's composition).

**Tests:**
- Per-path happy-path via fake-backend: each produces correct artifacts + correct completion signal.
- Refactor invariant gate: user declines invariants → ABORTED; user confirms → proceeds.
- Fix path's regression-test addition verified in completion scoring.

**Integration:** Intent Router (step 10) now routes all 4 brownfield loop paths to real implementations.

**Demo:**
```
$ forge "Fix the off-by-one in parser.go at line 42"
Researching bug... done (reproduced locally)

Path:     Fix
Bug:      parser.go:42 loops from 0 to len(items) not len(items)-1
Estimate: ~3–6 iterations

Plan:
  1. Add regression test capturing the off-by-one
  2. Fix the loop bound
  3. Verify full test suite

[y] go  ...
> y
(runs; fixes; commits; DONE)
```

---

## Step 20: One-shot modes — Review, Document, Explain, Research

**Objective:** implement the One-Shot Engine with parallel-subagent fan-out for all 4 non-loop modes.

**Implementation guidance:**
- `internal/oneshot` package. Shared flow per §4.9.
- Per-mode specifics:
  - **Review:** 3–6 subagents (security / architecture / correctness / style / performance). Output `report.md`. Read-only — no commits.
  - **Document:** 2–4 subagents (api-surface / internals / examples / README). Output docstrings + `.md` files. **Commits** (traceability). Production-code touches only for inline docstrings.
  - **Explain:** 1–3 subagents (small scope → 1; large → fan-out). Output `explanation.md`. Read-only.
  - **Research:** 2–4 subagents (alternatives / pros-cons / prior-art / cost). Web tools permitted. Output `research-report.md`. Read-only.
- On per-subagent failure: retry once; if retry fails, include `[Subagent failed: <area>]` stub in output.
- Synthesis via final Brain.Draft call.
- Chain-hook: at completion, suggest relevant next-stage chains from the predefined contracts list (step 23 implements chaining, but this step registers the suggestion).

**Tests:**
- Per-mode happy-path via fake-backend: each produces expected output artifact with expected sections.
- Subagent failure + retry + `[Subagent failed]` stub in output.
- Document mode writes inline docstrings; assert production code NOT touched otherwise (regex scan of non-docstring diff → must be empty).
- Explain/Research: assert no commits made.

**Integration:** Intent Router (step 10) routes the 4 one-shot verbs to One-Shot Engine.

**Demo:**
```
$ forge "Review the auth module"
Spawning 5 subagents (security / architecture / correctness / style / perf)...
  security: 3 findings
  architecture: 2 findings
  correctness: 0 findings
  style: 4 findings
  performance: 1 finding
Synthesizing report...
Report: .forge/runs/.../stage-1-review/report.md
Suggested next: forge --chain review:fix "the auth module"
```

---

## Step 21: Upgrade mode (dep-gate-inverted loop)

**Objective:** full Upgrade mode — loop variant with inverted dep-gate behavior + target-version confirmation.

**Implementation guidance:**
- Extend Plan Phase (step 11) with Upgrade variant:
  - 2–4 researchers: fetch release notes + migration guides + affected-imports inventory (web tools via Backend).
  - Produces `upgrade-scope.md` (target version, breaking changes, affected files).
  - Pre-loop confirmation: "Upgrading X from Y to Z — expected to modify these dep manifests: [package.json, package-lock.json]. OK? [y/n]"
- Loop Engine per-iteration config carries `dep_gate_inverted: true` flag for Upgrade runs. Gate Scanner (step 13) respects it: dep-manifest changes log-only, don't escalate (other gates still apply).
- Completion: target version present in manifests + tests pass + empty regression list.
- Default chain hook: suggest `upgrade:fix` or `upgrade:test` at completion.

**Tests:**
- Upgrade Next.js 13 → 14 against fake-backend scripted to modify `package.json`, `package-lock.json`, source files — assert no gate-escalation on manifest changes.
- Non-dep mandatory gates still fire (e.g., secret in diff).
- Target-version confirmation gate fires pre-loop.
- Completion when target version in manifest + tests green.

**Integration:** 5th loop-lifecycle path complete.

**Demo:**
```
$ forge "Upgrade Next.js from 13 to 14"
Researching migration guide... done (found 12 breaking changes)
Upgrade scope:
  Source: next@13.5.6
  Target: next@14.x
  Breaking changes: 12 documented
  Expected file changes: package.json, package-lock.json, src/app/**/*.tsx

Confirm upgrade? [y/n]
> y
(iterations run; dep-manifest changes auto-committed without gate halt)
```

---

## Step 22: Test mode (scope-restricted loop with production-touch escalation)

**Objective:** full Test mode — loop variant with framework detection + production-code-touch escalation gate.

**Implementation guidance:**
- Extend Plan Phase with Test variant:
  - 1–2 researchers: detect test framework (Go: `go test`; JS/TS: `jest`/`vitest` from `package.json`; Python: `pytest`/`unittest`; Rust: `cargo test`; Ruby: `rspec`/`minitest`). Scan existing tests for patterns. Identify coverage gaps.
  - Produces `test-scope.md` with coverage target, framework, test patterns.
- Loop Engine per-iteration check: classify each file in diff as "test", "test-infra" (`jest.config.js`, `pytest.ini`, `.gitignore` for coverage reports), or "production". Any production-file modification triggers mandatory escalation with `[m] modify with confirmation · [s] skip this test · [a] abort`.
- Completion: coverage delta meets target + new tests pass + no existing-test regressions.

**Tests:**
- Test mode attempts to modify production code → escalation fires → user picks `[m]` → proceeds once; user picks `[s]` → test skipped; `[a]` → abort.
- Framework detection: provide fixture repos with each framework; assert correct detection.
- Completion: scripted coverage delta → match target → DONE.

**Integration:** 6th loop-lifecycle path complete.

**Demo:**
```
$ forge "Add tests for the checkout flow"
Test framework: vitest
Coverage target: +15% over current 62%
Test scope: src/checkout/**/*.ts

Plan:
  1. Test cart-addition flow
  2. Test total calculation
  3. Test payment handoff

[y] go
> y

[14:35:12] iter 3 · ESCALATION — Test mode needs to touch production code
  File: src/checkout/payment.ts
  Proposed change: export internal `calculateTax` helper for direct test
  [m] modify with confirmation  [s] skip this test  [a] abort
> m
(iteration proceeds; production change + new test committed)
```

---

## Step 23: Composite chaining — stage lifecycle + inter-stage contracts

**Objective:** full chaining — natural-language multi-verb detection + `--chain` flag + stage directories + inter-stage contracts + confirmation gates + resume.

**Implementation guidance:**
- `internal/chain` package.
- Intent Router (step 10) already detects multi-verb chains and resolves against predefined contracts (step 10 left this stubbed; step 23 completes it).
- `chain.yml` written at run creation: stage list, current index, contract name, user's original task.
- Stage directories: `stage-N-<mode>/` each with full single-stage layout (task.md, plan.md, etc.). `current` symlink always points at active stage.
- **Inter-stage data flow** implemented per the predefined-contract table (§2.1.17):
  - `review:fix` — Review's findings parsed into Fix task list; one Fix stage with all findings OR user chooses per-finding-cluster granularity.
  - Other contracts per table.
- Inter-stage confirmation: at stage-boundary, render deliverable summary + next-stage preview + `[y] go / [e] edit which items / [n] stop`.
- `--yes` flag applies per stage.
- Resume: `forge resume` picks up the active stage (via `STAGE` pointer file at run root).
- Chain-terminated-at-stage-N = `DONE` at that stage; remaining stages stubbed with their `task.md` but never entered.

**Tests:**
- `review:fix` chain: Review finds 3 items → Fix processes 3 → both stages committed under the same `run_id`.
- `upgrade:fix:test` 3-stage chain: end-to-end via fake-backend; assert correct stage sequence + data flow + final DONE.
- Inter-stage decline: at stage-2 prompt, `n` → Run ends cleanly with stage-1 DONE + stage-2 never started.
- Resume mid-chain: kill mid-stage-2; re-run `forge resume`; continues stage-2 from PAUSED.
- Unknown-contract warning: `forge --chain review:explain "foo"` → warns "no predefined data-flow contract" and asks to proceed.
- `chaining.max_stages_warn`: 4-stage chain triggers warning.

**Integration:** Intent Router + Plan Phase + Loop Engine + One-Shot Engine + Escalation Manager all already support being stage-aware from prior steps; this step ties them together.

**Demo:**
```
$ forge "Review and fix the auth module"
Detected chain: review:fix (stage 1/2)

Stage 1/2: Review the auth module
(Review runs; produces 5 findings)

Stage 1 complete. Findings:
  1. Missing rate limiting on /login
  2. JWT exp parsed in local time
  3. Session fixation possible on upgrade path
  4. Timing side-channel in password comparison
  5. Missing CSRF protection on /logout

Stage 2/2: Fix these 5 findings (~15 iterations estimated)
  [y] start  [e] edit which to fix  [n] stop here
> y

(Fix runs per-finding; 5 fixes committed; DONE)
```

---

## Step 24: First-run onboarding + `forge doctor` + remaining CLI commands

**Objective:** user-facing polish — first-run UX, full `forge doctor`, remaining status/history/show/clean/stop/resume commands.

**Implementation guidance:**
- **First-run flow:** in root command pre-run, if `config.backend.default` unset:
  1. Scan `$PATH` for `claude`, `gemini`, `kiro-cli`.
  2. Zero found → print install pointers + exit.
  3. One or more → always prompt (§2.1.15 amendment — never silent even with one).
  4. Save selection to `~/.config/forge/config.yml`.
- **`forge doctor`:** all checks per §2.1.15 + §4.17.1 — config validity / backend install + version / git / protected-branch detection / `.forge/` writable / notification probe (with consent) / run-dir integrity / disk space / network-FS detection. `OK/WARN/FAIL` per check; `--verbose` for detail; suggestions inline; never auto-fixes.
- **`forge status`:** full output per §4.19.1 for each lifecycle state. `--verbose` adds ledger excerpt + stuck/completion breakdown + notify-attempts. `--run <id>` inspects any past run.
- **`forge history [--full]`:** scan `.forge/runs/*`; sort by timestamp (UUID-v7 ensures lexical sort = chronological); show summary.
- **`forge show <run-id> [--iter N]`:** dump artifacts; with `--iter`, the per-iteration transcript log.
- **`forge clean`:** delete terminal-state runs (`DONE`/`ABORTED`/`FAILED`) older than `retention.max_runs`. Forge-stash cleanup (stashes labeled `forge-emergency-<run-id>-*`).
- **`forge stop`:** send cooperative SIGTERM to current run's PID; wait for `PAUSED` marker.
- **`forge resume [<run-id>]`:** validate resumable state; git HEAD re-read; manual-edit detection via distillation refresh; apply pending `answer.md` if present; continue.

**Tests:**
- First-run: wipe config → zero backends → exit with install pointers. One backend → prompt. Multiple → prompt with list.
- Doctor: mock each failure type; assert correct classification.
- Status per state: RUNNING, AWAITING_HUMAN, PAUSED, DONE, ABORTED, FAILED → assert output matches spec.
- Stop + resume round-trip.
- Clean with retention=3: four terminal runs → oldest removed.

**Integration:** all CLI commands now functional.

**Demo:**
```
$ rm -rf ~/.config/forge  # fresh install
$ forge "Create a hello-world"
Welcome to Forge. Which backend should I use?
  1. claude (claude v2.1.110) [installed]
  2. gemini (gemini v0.38.1)  [installed]
  3. kiro-cli (kiro-cli v2.0.3) [installed]
> 1
Saved backend.default = claude.

(proceeds to plan phase)

$ forge doctor
✓ Config: OK
✓ Backend claude: OK (v2.1.110)
⚠ Notifications: no D-Bus session detected (headless?)
  → Consider --auto-resolve accept-recommended for unattended runs.
✓ Git: OK (2.43.0)
✓ .forge/ writable
(...)
```

---

## Step 25: CI + release pipeline + distribution

**Objective:** ship v1. Automated test matrix, cross-platform binaries, GitHub Releases on tag, Homebrew tap, curl installer, `go install`.

**Implementation guidance:**
- **`.github/workflows/ci.yml`:** matrix `{linux, macos, windows} × {amd64, arm64}`. Run `go test ./...` + `golangci-lint` + integration tests + backend-live tests skipped by default (tagged `-tags=live`).
- **`.github/workflows/release.yml`:** on tag `v*.*.*`, build binaries for all targets, upload to GitHub Release, generate SHA256 checksums.
- **Homebrew tap:** separate repo `<owner>/homebrew-forge` with `Formula/forge.rb` — pulls from GitHub Releases. Formula tests install + `forge --version`.
- **curl installer:** `scripts/install.sh` in repo — detects OS/arch, fetches matching GitHub Release binary, installs to `~/.local/bin` (or `/usr/local/bin` with sudo). Published at a stable URL.
- **`go install github.com/<owner>/forge@latest`** — works as soon as repo is public and tagged.
- **Version embedding:** via `-ldflags -X main.version=...` at build time.
- **CHANGELOG.md** maintained from CI-extracted commits since last tag.

**Tests:**
- CI matrix passes on all targets.
- Smoke-test release-built binaries on each platform: `forge --version` + `forge --help` work + trivial `forge "Create a hello"` runs.
- Homebrew Formula installs cleanly in a fresh Brew tap test env.
- Installer script runs on fresh Linux + macOS Docker/VM.

**Integration:** v1 is shipped.

**Demo:**
```
# From a fresh macOS:
$ brew tap <owner>/forge
$ brew install forge
$ forge "Create a trivial Go CLI"
(runs end-to-end — fresh install, zero config, working Forge)
```

---

## Post-implementation sanity pass

After Step 25, before declaring v1 shipped, run the full §2.3 done-criteria acceptance suite:

1. ✅ All 10 modes' happy paths work end-to-end from a fresh brew install.
2. ✅ ≥80% autonomous recovery on Tier 1/2 stuck scenarios.
3. ✅ Every mandatory gate has an integration test that exercises it.
4. ✅ Crash recovery round-trip works (kill -9 mid-iteration → resume).
5. ✅ Iteration cap and wall-clock cap both enforced with clean Tier-3 halt.
6. ✅ `upgrade:fix:test` 3-stage chain runs end-to-end including mid-chain resume.
7. ✅ Test mode's production-touch escalation fires + is resolvable.
8. ✅ Upgrade mode's dep-gate-inversion works (no halt on manifest touches).

Any failure → treat as a hot-fix before ship, then re-verify.
