# Forge — Planning Summary

**Date:** 2026-04-16
**Status:** Planning phase complete; ready for implementation handoff.

## What was created

```
.agents/planning/2026-04-16-forge/
├── rough-idea.md                           (original concept + source inspirations)
├── idea-honing.md                          (19 Q&A rounds of requirements clarification)
├── research/
│   ├── claude-code-cli.md                  (verified Claude Code CLI capabilities + flags)
│   ├── gemini-cli.md                       (verified Gemini CLI capabilities + flags)
│   ├── kiro-cli.md                         (verified Kiro CLI capabilities + flags)
│   ├── go-library-survey.md                (13-category library recommendations for 2026)
│   ├── security-patterns.md                (secret scan / placeholder / protected-branch patterns)
│   ├── process-lifecycle.md                (cross-platform process + file lifecycle mechanics)
│   ├── fsnotify-patterns.md                (editor-save patterns + reliable mailbox protocol)
│   └── notification-fallbacks.md           (notification probes + in-band terminal fallbacks)
├── design/
│   └── detailed-design.md                  (2198 lines; stand-alone v1 design; mermaid diagrams)
├── implementation/
│   └── plan.md                             (25 incremental steps with per-step demo)
└── summary.md                              (this file)
```

## What Forge is

A single-binary Go CLI that orchestrates long-running AI coding tasks by driving existing AI CLI backends (Claude Code / Gemini CLI / Kiro CLI) in Ralph-style loops, with automated context management, stuck-loop prevention, and a minimal human-intervention policy.

The core invocation is one line:

```
forge "Create a REST API for todos"
forge "Fix the login bug"
forge "Review the auth module"
forge "Upgrade Next.js 13 to 14"
forge "Review and fix the auth module"      # composite chain
```

## Key design decisions

**Scope:**
- **10 modes** — 6 loop (Create, Add, Fix, Refactor, Upgrade, Test) + 4 one-shot (Review, Document, Explain, Research).
- **Composite chaining** — natural-language multi-verb detection + explicit `--chain` flag. Up to 3-stage chains with predefined inter-stage contracts.
- **3 backends** — Claude Code / Kiro / Gemini CLI. Manual selection (never silent auto-select).

**Architecture:**
- Language: **Go 1.22+** (single binary; rock-solid subprocess model).
- No `ralph-orchestrator` dependency — built from scratch.
- Brain (Forge's meta-LLM calls) uses the configured backend CLI; no separate Anthropic API key in v1.
- No USD cost management — `max_iterations` + `max_duration` are the loop-control caps.

**UX:**
- One primary command + inline confirmation gate (`y/n/e/r`).
- ~12 total CLI commands.
- Intent-based routing from task's opening verb; 4-step decision ladder (keyword → LLM → human → confirmation) becomes the canonical pattern for every Forge decision.
- Two-file mailbox (`awaiting-human.md` Forge-written + `answer.md` user-written) for async/remote human responses.
- 5-channel notification cascade (file sentinel → loud banner → OSC 9/bell → tmux → OS notify) — fail-loud, never silent.

**Safety:**
- Mandatory human gates on destructive ops, external-facing actions, secrets, credentials, protected branches, CI/CD files, dependency manifests (inverted in Upgrade mode), mid-loop scope changes.
- `gofrs/flock` + PID+start-time tuple for single-run lock; stale-lock recovery.
- Windows Job Object + Unix `Setsid` for guaranteed subprocess-tree kill.
- Embedded `gitleaks` library for secret scanning (222 rules out of the box).
- Embedded regex table for placeholder detection across Go/Python/TS/Rust/Java/etc.

**Mechanics:**
- Per-iteration reset context (Ralph discipline).
- Automatic distillation when `state.md` / `notes.md` / `plan.md` cross thresholds.
- Hybrid stuck-detection: hard-signal gates (any single trigger → direct-to-tier) + soft-signal additive sum. Final tier = max.
- External-signal subprocess deaths (SIGCONT post-suspend, SIGHUP, SIGPIPE) classified as transparent retries, not stuck events.
- Rate-limit responses trigger exponential backoff, NOT stuck escalation.

## Implementation plan

25 steps, each producing a working demoable increment. Broad phases:

- **Steps 1–7: Foundation** — project scaffolding, logger, config, state manager + lock, git helper, process wrapper. No AI yet; pure infrastructure.
- **Steps 8–9: Backend plumbing** — interface + fake-backend test binary + Claude Code adapter.
- **Steps 10–12: First end-to-end demo** — intent router, Plan Phase for Create, minimal Loop Engine. **Step 12 is the first working Ralph.**
- **Steps 13–17: Safety + intelligence** — policy scanners, escalation mailbox + cascade, stuck/completion detectors, context manager + brain primitives.
- **Steps 18–22: Backend + mode breadth** — Gemini/Kiro adapters, Add/Fix/Refactor, Upgrade (dep-gate-inverted), Test (scope-restricted), one-shot modes (Review/Document/Explain/Research).
- **Step 23: Composite chaining** — stage directories + inter-stage contracts + confirmation gates.
- **Steps 24–25: Polish + ship** — first-run onboarding, `forge doctor`, remaining CLI commands, CI + release pipeline + distribution (Homebrew / curl / GitHub Releases / `go install`).

## Suggested next steps

1. **Read the design doc** at `design/detailed-design.md` to confirm the v1 spec matches your intent.
2. **Review the implementation plan** at `implementation/plan.md` and adjust any step's scope or sequence.
3. **Hand off to the Ralph loop** to execute the plan. Per the PDD SOP's handoff guidance, start with one of:
   - `ralph run --config presets/pdd-to-code-assist.yml --prompt "<task>"`
   - `ralph run -c ralph.yml -H builtin:pdd-to-code-assist -p "<task>"`

The planning session ends here. The next phase is implementation, which you run yourself.

## Areas that may need further refinement during implementation

These are honest gaps — surface them early when the first step hits them:

- **Kiro ACP `session/prompt` response schema** — documented method names but not the exact payload shapes; capture from a real run in step 18.
- **Claude Code `stream-json` error subtypes** — docs mention `error_max_turns` and `error_max_budget_usd`; full enumeration needs empirical cataloging during step 9.
- **Gemini CLI pre-1.0 flag churn** — breaking changes in minor versions (e.g., `--checkpointing` removed in 0.11.0). Pin a version; re-verify during step 18.
- **Per-backend context-window table** — advertised vs. effective windows need empirical filling at step 18.
- **Placeholder regex tuning** — the shipped regex table will produce false positives on test files using `raise NotImplementedError` for "pending" scenarios; tune exclusions during step 13.
- **Protected-branch detection in unauth contexts** — on GitHub repos without `gh auth login`, detection falls through to offline convention; consider a louder warning during step 6.

## Questions to anticipate from the implementer

- *"Why `gofrs/flock` over `rogpeppe/go-internal/lockedfile`?"* — Community-canonical, higher-level ergonomics. See research `go-library-survey.md`.
- *"Why embed gitleaks vs. shell out to the binary?"* — MIT license, clean Go API, saves a fork overhead per iteration. See research `security-patterns.md`.
- *"Why not hard-cap chain length at 2?"* — Real workflows hit 3 (`upgrade:fix:test`); N-stage costs almost nothing over 2-stage.
- *"Why separate Explain and Research as modes if implementation is ~95% shared?"* — User mental model is different; two rows in a table is cheap.
