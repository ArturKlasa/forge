# Forge v1 — Implementation Loop

You are implementing **Forge v1**, a single-binary Go CLI that orchestrates long-running AI coding tasks by driving other AI CLIs (Claude Code / Kiro / Gemini CLI) in Ralph-style loops. This Ralph run executes the 25-step implementation plan.

## Primary reference documents

Always consult these in this order when in doubt:

| File | Role |
|---|---|
| `.agents/planning/2026-04-16-forge/implementation/plan.md` | **Active checklist. Your next step is the first unchecked `- [ ]` box.** |
| `.agents/planning/2026-04-16-forge/design/detailed-design.md` | Source of truth for v1 behavior. Every component, data model, and error mode is defined here. |
| `.agents/planning/2026-04-16-forge/research/go-library-survey.md` | Pinned Go library choices (cobra, koanf, gofrs/flock, lipgloss, fsnotify, gitleaks-as-library, etc.). |
| `.agents/planning/2026-04-16-forge/research/process-lifecycle.md` | Cross-platform lock + subprocess + Job Object specifics. |
| `.agents/planning/2026-04-16-forge/research/fsnotify-patterns.md` | Editor-save patterns + mailbox protocol details. |
| `.agents/planning/2026-04-16-forge/research/notification-fallbacks.md` | Notification cascade channels + probes. |
| `.agents/planning/2026-04-16-forge/research/security-patterns.md` | gitleaks, placeholder regex, protected-branch detection. |
| `.agents/planning/2026-04-16-forge/research/claude-code-cli.md` | Verified Claude Code CLI flags + stream-json schema. |
| `.agents/planning/2026-04-16-forge/research/gemini-cli.md` | Verified Gemini CLI flags + stream-json schema. |
| `.agents/planning/2026-04-16-forge/research/kiro-cli.md` | Verified Kiro CLI flags + ACP JSON-RPC schema. |
| `.agents/planning/2026-04-16-forge/rough-idea.md` | Original concept (context only). |
| `.agents/planning/2026-04-16-forge/idea-honing.md` | Decision history (context only). |

## Per-iteration workflow

1. **Re-read `plan.md`.** Find the progress checklist at the top. Your target is the first unchecked box.
2. **Read the target step's full section.** Each step has: Objective / Implementation guidance / Tests / Integration / Demo. The Demo describes what "done" looks like.
3. **Consult the design doc.** Every behavioral detail is defined in `detailed-design.md`. If the plan says "per design §4.7", open §4.7 and follow it.
4. **Consult the relevant research notes.** When the step touches locks, use `process-lifecycle.md`. When it touches fsnotify/mailbox, use `fsnotify-patterns.md`. When it touches libraries, use `go-library-survey.md`.
5. **Write tests first or alongside** the implementation, per the step's Tests section.
6. **Run the full test suite** (`go test ./...` once the Go module exists). Fix any regressions before proceeding.
7. **Run the step's Demo command(s)** from the plan. Confirm the described output.
8. **Commit** with message format `step N: <short title>`. One step = one commit.
9. **Tick the checkbox** in `plan.md`: change `- [ ] **Step N:**` → `- [x] **Step N:**`. Stage the tick in the same commit.
10. **Iterate.** Re-read plan.md at the top of the next iteration — the checklist is the authoritative state.

## Completion

When (and only when) **all 25 boxes are ticked** AND the **post-implementation sanity pass** at the bottom of `plan.md` passes (every criterion verified with evidence), output the exact string `LOOP_COMPLETE` on its own line.

Do **not** output `LOOP_COMPLETE` for partial progress. Do not output it if any sanity-pass criterion fails.

## Rules

- **Steps run in order.** Do not skip ahead. Each step depends on prior steps.
- **Each step ends with a working, demoable increment.** No orphaned code. No "will be wired in step N+1" stubs unless that specific handoff is documented in the next step's Integration section.
- **Planning files are frozen.** Never modify files under `.agents/planning/` with one exception: ticking checkbox boxes in `plan.md` as you complete steps. If the design looks wrong or impossible, write a note to `.agent/scratchpad.md` with the specific concern; do not silently deviate.
- **Tests first or alongside code.** A step's Tests section defines the minimum test coverage. Run them. If they don't pass, keep iterating on the step — don't move on.
- **One commit per step.** Commit message: `step N: <short title>`. If you made scratch/experimental changes before the real implementation, squash them before committing the step.
- **Forge's own state directories (`.forge/`) are runtime-created**, not part of source. Ralph's state lives in `.agent/` and `.ralph/`. Do not manually create `.forge/` during implementation — it's created by Forge itself at runtime.
- **If blocked**, write the specific blocker (what's missing, what was tried, what's needed) to `.agent/scratchpad.md` and continue. The next iteration will get fresh context and try a different angle. Don't loop on the same approach.
- **Use the research notes for specifics** — they contain verified flag strings, exact JSON shapes, and library-version-pinned recommendations. Don't re-derive.

## Project context (one-paragraph orientation)

Forge is an orchestrator that runs OTHER AI CLIs (Claude Code, Kiro, Gemini CLI) in Ralph-style loops. It provides 10 modes (6 loop: Create / Add / Fix / Refactor / Upgrade / Test; 4 one-shot: Review / Document / Explain / Research) plus composite chaining (`review:fix`, `upgrade:fix:test`). Its value is the automation brain around the loops — intent routing, plan-phase research, context management with automatic distillation, hybrid stuck detection, multi-signal completion detection, policy scanners (secrets / placeholders / file-path gates), two-file mailbox for async human input, and a layered notification cascade with fail-loud guarantees. Written in Go 1.22+, distributed as a single binary via GitHub Releases + Homebrew + curl-installer + `go install`.

## Start

Read `.agents/planning/2026-04-16-forge/implementation/plan.md`. Find the first unchecked box. Begin.
