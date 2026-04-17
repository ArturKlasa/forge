# Memories

## Patterns

### mem-1776391797-55c6
> step 24: First-run onboarding scans PATH for claude/gemini/kiro-cli, prompts if unset, saves to ~/.config/forge/config.yml. Full forge doctor: 6 checks (Config/Backend/Git/ForgeDir/DiskSpace/Notifications). lifecycle_cmds.go: history/show/clean/stop/resume all implemented. Stop reads lock.json PID, sends SIGTERM, waits for PAUSED. Resume reads ledger to count done iterations, restarts loop with StartIteration offset.
<!-- tags: cli, step24, doctor | created: 2026-04-17 -->

### mem-1776381855-da74
> internal/planphase: Test path added. researchTest→parses framework/coverage fields. artifacts writes test-scope.md with framework+coverage target+scope. internal/loopengine: TestMode option=true fires production-touch escalation (options: m/s/a) when diff touches non-test files. isTestFile detects *_test.go, tests/, spec/, __tests__/, jest.config.js, etc. internal/compdet: PathCriteriaCheck test case = test-scope.md present + diffAddsTest. planphase.Result.TestMode wired to loopengine.Options.TestMode in commands.go.
<!-- tags: planphase, loopengine, compdet | created: 2026-04-16 -->

### mem-1776380628-4526
> internal/planphase: Add/Fix/Refactor paths fully implemented. researchAdd→codebase-map.md+specs.md; researchFix→bug.md+repro section; researchRefactor→invariants.md+target-shape.md. Refactor pre-loop gate fires before main confirm; 'n' at gate→ABORTED. internal/compdet: PathCriteriaCheck(path, runDir, diff) wired into loop engine (iterDiff variable). Fix path checks: bug.md present + diff adds test func/file.
<!-- tags: planphase, compdet, loopengine | created: 2026-04-16 -->

### mem-1776379807-0c80
> internal/brain package: Brain struct, 6 primitives (Classify/Judge/Distill/Diagnose/Draft/Spawn). Each: scoped prompt template + Backend call (120s timeout) + parse key=value sentinels + 1 retry. internal/ctxmgr: Manager.AssemblePrompt(ctx, path, budgetTokens) uses 7-section order (sys prompt, task.md, path artifact, plan.md, state.md, notes.md, instructions). Distill triggers: state>8k/notes>10k/plan>6k tokens (4 chars/token heuristic). With Brain: LLM compress + archive. Without: truncate + archive. Loop engine Options.Brain + Options.ContextBudgetTokens wired to all primitives.
<!-- tags: brain, ctxmgr, loopengine | created: 2026-04-16 -->

### mem-1776379375-2f38
> internal/stuckdet package: Evaluate([]Entry) Result. Hard signals (4): off_topic_drift→Tier2, placeholder_accumulation→Tier2, same_error_fingerprint_4plus→Tier3, build_broken_5plus→Tier3. Soft signals (5) over 3-iter window: no_files(+2), no_plan_items(+2), no_state_delta(+2), regression(+3), self_report_stuck(+2). Soft thresholds: <3→Tier0, 3-5→Tier1, ≥6→Tier2. External deaths excluded. Engine parses <!--FORGE:build_status/self_report/error_fp/regression=--> from FinalText. Tier1→append state.md, Tier2→append plan.md, Tier3→escalate. internal/compdet: Evaluate(Signals) Result. Weighted sum; ≥8+judge_medium→Complete, 5-7→Audit, <5→Continue.
<!-- tags: stuckdet, compdet, loopengine | created: 2026-04-16 -->

### mem-1776378866-7fa8
> internal/notify package: Channel interface (Name/Available/Notify), Message{Title,Summary,Body}, NotifyAll (fail-loud, all channels fire independently). 5 channels: FileSink (ESCALATION sentinel), BannerSink (ASCII banner → output + /dev/tty), OSCSink (OSC 9 + bell), TmuxSink ($TMUX → tmux display-message), BeepSink (gen2brain/beeep). DefaultChannels(runDir, output). EnvProbe: DBUS/DISPLAY/SSH/TMUX/WSL/CI detection. EscalationManager.Channels field for injection; lazily built at Escalate() time. forge doctor --test-notify (hidden).
<!-- tags: notify, escalate, loopengine | created: 2026-04-16 -->

### mem-1776378502-9119
> internal/escalate package: Manager.Escalate() writes awaiting-human.md (AtomicWrite shim: renameio on Unix, natefinch/atomic on Windows), writes ESCALATION sentinel, displays banner, then blocks on fsnotify dir-watch (debounce 250ms + stability check) or 2s polling on network-FS. ParseAnswer: CRLF→LF, requires id:/answer:/--- fields; ID mismatch→answer.stale.md.<ts>. --auto-resolve accept-recommended waits 5s for non-mandatory. GateScannerEscalation() builds escalation from loop engine; answer dispatch: a=commit, s=unstage+continue, p/d=break.
<!-- tags: escalate, loopengine, policy | created: 2026-04-16 -->

### mem-1776378075-0202
> internal/policy package: SecurityScanner (gitleaks v8 via isolated viper, DetectBytes on diff), PlaceholderScanner (11 regex patterns, 9 high-conf + 2 low-conf, diff-mode added-lines only, skips test files + forge:allow-todo + TODO(#N)), GateScanner (hardcoded tables for manifest/CI/secret-env/lockfile paths). Loop engine now stages-then-scans-then-commits (git add -A → diff --cached → policy scan → commit or unstage). gitleaks allowlists 'EXAMPLE'-ending keys via stopword regex.
<!-- tags: policy, security, loopengine | created: 2026-04-16 -->

### mem-1776377616-5e0e
> internal/loopengine package: Run() assembles prompt (task.md+plan.md+state.md), calls Backend.RunIteration, appends ledger.jsonl (NDJSON), commits via git CommitAll if dirty, enforces MaxIterations/MaxDuration cap, detects TASK_COMPLETE in FinalText/CompletionSentinel. LedgerEntry has run_id,iteration,started_at,finished_at,duration_sec,exit,files_changed,commit_sha,tokens,complete. Root cmd now runs plan phase → lock → loop instead of stub.
<!-- tags: loopengine, cli | created: 2026-04-16 -->

### mem-1776377191-a93c
> internal/planphase package: Run() implements pre-gates (dirty tree + protected branch auto-switch) → research (1-2 subagents for Create; stub when no backend) → artifacts (task.md, target-shape.md, plan.md, state.md, notes.md) → confirmation UI (y/n/e/r via x/term raw mode). TermReader interface for test mocking. Chain detection returns early (before pre-gates). KEY: plan subcommand uses --mode flag (not --path) to avoid conflict with root persistent --path (workdir).
<!-- tags: planphase, cli, testing | created: 2026-04-16 -->

### mem-1776376378-882f
> internal/router package: Router.Route() implements 4-step ladder (keyword→LLM→human escalation). keywordTable covers all 10 paths (§2.1.2). detectChain uses regex for 'X and Y' multi-verb; PredefinedChains map lists v1 contracts. WithPathOverride() short-circuits (--path flag). LLM prompt returns path= + confidence= fields; low→NeedsHumanEscalation. plan cmd uses it; forge plan shows detected path/chain.
<!-- tags: router, intent, backend | created: 2026-04-16 -->

### mem-1776376101-eb89
> internal/backend/claude package: Adapter struct implements Backend interface. WithExecutable() option lets tests swap in fake-backend. proc.Wrapper handles SIGTERM→SIGKILL. parseStreamJSON scans NDJSON stdout; completion signal is type==result; error_max_turns→Truncated=true. Tests skip when sandbox blocks setpgid (same probe as proc package). probe-backend test-utility command wired in commands.go.
<!-- tags: backend, claude, testing | created: 2026-04-16 -->

### mem-1776375776-fbfd
> cmd/fake-backend: 3-mode test binary (text/stream-json/acp). Script in CSV or YAML maps patterns→responses. stream-json emits system/init+assistant+result NDJSON matching Claude Code shape. acp is manual JSON-RPC 2.0 (initialize/session/new/session/prompt). Used by integration tests for all later steps.
<!-- tags: backend, testing, fake-backend | created: 2026-04-16 -->

### mem-1776375438-f1c7
> internal/proc package: Wrapper type with New/Start/Terminate/Kill/Wait. Unix: SysProcAttr{Setpgid,Setsid} + SIGTERM→SIGKILL to process group. Windows: Job Object JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE via x/sys/windows. RingBuffer (io.Writer, default 64KiB). ExitClassification: Normal/IterationFail/ForgeTerminated/ExternalSignal. Tests probe setpgid capability in TestMain and skip when blocked.
<!-- tags: proc, subprocess, lifecycle | created: 2026-04-16 -->

### mem-1776375003-eb71
> internal/git package: shell-out wrapper, Git{dir,timeout,ghCommand}. IsProtected 8-tier strategy. Log uses \x1f separator (null bytes rejected by OS exec). HumanConfirmation gate on ResetHard. DetectProtectedBranches for forge doctor. google/uuid for UUID-v7 run IDs.
<!-- tags: git, state | created: 2026-04-16 -->

### mem-1776374423-f48c
> internal/state/lock package: gofrs/flock + PID/start_time sidecar. Acquire(forgeDir, runID) does TryLock + sidecar write. handleConflict: hostname mismatch→refuse; dead PID→stale; PID alive+start_time mismatch→stale. Network FS fallback (PID-file-only). SetNetworkFSOverride for tests. Linux: /proc/<pid>/stat field 19 (0-indexed after ')') + /proc/stat btime, CLK_TCK=100.
<!-- tags: state, lock | created: 2026-04-16 -->

### mem-1776373623-cac8
> internal/state package: RunDir type + lifecycle markers (RUNNING/AWAITING_HUMAN/PAUSED/DONE/ABORTED/FAILED) as empty files. Atomic transitions via renameio (write new first, then remove old). currentRunRef is symlink on Unix, text file on Windows. Manager.Init() creates .forge/runs/ and idempotently adds .forge/ to .gitignore.
<!-- tags: state, lifecycle | created: 2026-04-16 -->

### mem-1776373388-5b16
> Config system uses koanf/v2 with 4-layer precedence: defaults→global→repo→env. mapProvider with koanf/maps Unflatten bridges flat dot-key map to koanf. Structs need both koanf and yaml tags for correct goyaml marshalling.
<!-- tags: config, koanf | created: 2026-04-16 -->

### mem-1776372810-4b61
> internal/log package uses slog (text→stderr, JSON→stdout) + lipgloss. Init(Config) sets global. G() accessor. isInteractive gates on NO_COLOR, CI, and term.IsTerminal.
<!-- tags: logging, cli | created: 2026-04-16 -->

## Decisions

## Fixes

## Context
