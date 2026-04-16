# Idea Honing: Forge

This document captures the interactive Q&A used to refine the Forge concept from the rough idea into a concrete specification.

Format: each entry is `## Q<N>: <question>` followed by the asked-and-answered content, including any alternatives considered.

---

## Q1: Who is Forge built for, and what's the deployment scope?

**Options considered:**

- **A. Personal tool, Claude Code–first.** Built primarily for you and users with the same workflow. Claude Code as the default/only backend initially. Distributed casually (npm/cargo) if it proves useful to others. Narrow scope = less config surface = more room for aggressive automation.
- **B. Public, single-backend.** Polished for public release. Claude-first but designed with extensibility in mind. Wider audience means more UX edges to handle; more config flags creep in.
- **C. Public, multi-backend from day one.** Parity with `ralph-orchestrator`'s support for Claude Code, Kiro, Gemini CLI, Codex, Amp, Copilot CLI, OpenCode. Big surface area; conflicts with "minimal CLI commands" and "AI decides setup" because each backend has its own conventions.

**Recommendation: A.** Your rough idea emphasizes minimal human burden and AI-decided setup — both are easier when the tool targets one backend and one workflow. Starting narrow is also how Ralph (a bash one-liner) and GSD (a Claude Code skill pack) started; both grew scope only after the core was proven. You can design Forge's internals so a future multi-backend mode is possible without rewriting, but you don't pay that tax now. This also aligns with Ralph's guidance that LLMs are "mirrors of operator skill" — tuning one backend well is better than tuning several poorly.

**Answer: C (narrowed).** Public, multi-backend from day one, but scope is reduced to three backends for now: **Claude Code, Kiro, Gemini CLI**. Rationale from the user: different users have access to different backends, so backend choice is a legitimate human decision and should not be hidden by automation — but the full seven-backend scope isn't needed initially.

**Implications for subsequent questions:**
- Backend selection is a human decision (not AI-decided) — but *setup within a backend* is AI-driven.
- Forge needs a backend-agnostic internal abstraction; three per-backend adapters (Claude Code, Kiro, Gemini CLI) must be built.
- Forge must handle prompt idioms and context-window differences across the three backends.
- Additional backends (Codex, Amp, Copilot CLI, OpenCode) are deferred to post-v1.

---

## Q2: What's Forge's architectural relationship to `ralph-orchestrator`?

Since we're now supporting 7+ backends, the "how do we actually implement multi-backend" question becomes central. `ralph-orchestrator` already implements all seven. Options:

- **A. Wrap `ralph-orchestrator` (Forge is an automation layer).** Forge is a separate CLI. It auto-generates `ralph.yml`, picks hats/presets, invokes `ralph run`, monitors output, handles escalation and stuck detection. Forge's novel value is the "brain" (research, auto-decide, context/stuck mgmt); execution is delegated.
- **B. Fork `ralph-orchestrator`.** Clone the Rust codebase, embed Forge's automation inside, ship as a single tool. Maximum control, but you own a Rust codebase and carry merge burden from upstream.
- **C. Build from scratch.** New codebase (any language), write 7 backend adapters ourselves, own loop engine. Biggest lift; no external dependency. Full design freedom.
- **D. Hybrid: use `ralph-orchestrator` as an engine subprocess, but keep Forge UX/brain totally separate.** Essentially the same as A but with an explicit design commitment that Forge never exposes `ralph-orchestrator`'s surface to end users — it's an implementation detail, swappable later.

**Research note:** `ralph-orchestrator` is 82.6% Rust (install via `cargo install ralph-cli` or npm-wrapped binary), supports MCP server mode, has a Telegram HITL bot, supports the exact backend list you want. Its web dashboard is Alpha-quality and iterating; its core CLI is stable.

**Recommendation: D (hybrid with explicit separation).** Reasons:
1. **Free multi-backend support.** `ralph-orchestrator` already implemented this and you want parity — reimplementing seven adapters is weeks of work for zero differentiating value.
2. **Forge's value prop is the automation brain, not the loop engine.** Original Ralph is a bash one-liner; the engine is trivial, the intelligence around it is everything.
3. **Explicit separation > tight wrap.** If `ralph-orchestrator` evolves incompatibly or you want to swap engines later, you're not locked in. Forge should depend on ralph-orchestrator via a narrow, versioned interface (spawn `ralph run`, parse known output sentinels like `LOOP_COMPLETE`, read `ralph.yml` we wrote).
4. **The friction you named — "`ralph-orchestrator` requires `ralph.yml`" — is solved by Forge generating the yml, not by avoiding ralph-orchestrator.** That's exactly the abstraction: Forge's users never write `ralph.yml`; Forge writes it.

Cost accepted with D: users need both `forge` and `ralph-cli` binaries installed; Forge carries the install/version-check burden (detect missing ralph-cli, prompt to install).

Alternatives worth naming: **A** is essentially the same as D with less intent; **B** is a maintenance trap (ralph-orchestrator has 41 releases — you'd rebase forever); **C** is appealing for purity but doubles the scope of Forge.

**Answer: C.** Build Forge from scratch — no dependency on `ralph-orchestrator`. Rationale from the user: avoid tying Forge's lifecycle, installation, and upgrade path to an external Rust toolchain.

**Implications for subsequent questions:**
- Forge owns its own Ralph-loop engine (writing PROMPT.md, spawning the backend CLI, monitoring output, reading completion sentinels).
- Forge owns three backend adapters (Claude Code, Kiro, Gemini CLI).
- Implementation language is now a first-class question — no need to match `ralph-orchestrator`'s Rust.
- Distribution must be self-contained (no Rust toolchain prerequisite).
- Feature parity with `ralph-orchestrator` features (MCP server, Telegram HITL, hats/events) is NOT a requirement — we can pick and choose which concepts to adopt.

---

## Q3: What's the implementation language for Forge?

Now that we're building from scratch, we have full language freedom. Forge's runtime needs:
1. **Easy install** (single-binary is ideal — matches "minimal human burden").
2. **Solid subprocess management** — spawn backend CLIs, stream their stdout/stderr, handle timeouts/signals, kill stuck processes.
3. **File I/O** — read/write PROMPT.md, state files, config.
4. **LLM SDK** — Forge will likely make its own API calls for meta-tasks (pre-question research, escalation decisions, stuck detection). Needs an official Anthropic SDK at minimum.
5. **Cross-platform** — Linux, macOS, Windows.
6. **Reasonable dev velocity** — this is greenfield; fast iteration matters.

**Options:**

- **A. Go.** Single binary (zero runtime). Best-in-class distribution (`go install`, Homebrew, curl-installer, GitHub releases). Rock-solid subprocess management (`os/exec`, goroutines). Official Anthropic Go SDK exists. Cross-platform native builds out of the box. Moderate dev velocity.
- **B. Rust.** Single binary too. Best perf, strictest type system. Same language as `ralph-orchestrator` (could borrow patterns or migrate code later). No official Anthropic Rust SDK — community crates only. Slower dev velocity.
- **C. TypeScript/Node.** npm distribution (familiar, like GSD). Rich ecosystem. Official Anthropic TS SDK. Requires a Node runtime on the user's machine (minor install friction). Fastest dev velocity for this kind of orchestration work.
- **D. Python.** Best AI/LLM ecosystem and SDK support. Distribution is painful (pyenv/venv/pip). Requires Python runtime.
- **E. Bash/POSIX shell.** Ralph's native language. Zero-install on Unix. Impossible to maintain anything with stuck detection, context mgmt, and escalation logic.

**Recommendation: A (Go).** Reasoning:
1. **Distribution.** User wants "easier use" — a single binary installed via `brew install forge` or `curl | sh` is dramatically lower friction than `npm install -g` (requires Node), `cargo install` (requires Rust toolchain), or `pip install` (Python version hell). This is exactly the friction Forge is trying to eliminate.
2. **Subprocess model.** Forge's core job is orchestrating long-lived backend CLI subprocesses — streaming their output, detecting stalls, killing and restarting them. Go's `os/exec` + goroutines is the cleanest model for this; Node's is workable but more callback-heavy; Rust's async runtime is overkill and slower to iterate.
3. **Official Anthropic SDK.** `github.com/anthropics/anthropic-sdk-go` exists and is maintained. Forge will call Claude for meta-tasks (research, stuck detection) — first-party SDK is table stakes.
4. **No runtime required.** Users installing Forge shouldn't need to install a language runtime first.

Strong runner-up: **C (TypeScript/Node).** Faster to iterate, better for prompt-heavy code where you're shuffling strings. Loses on distribution (npm requires Node) and on single-binary story. If you prefer Node for familiarity, it's a defensible choice.

Weakest: **B (Rust)** — slow to iterate, no official Anthropic SDK, ralph-orchestrator is already the Rust incumbent. **D (Python)** — distribution is the opposite of "minimal human burden." **E (Bash)** — not viable at Forge's complexity.

**Answer: A (Go).** Decided after a pros/cons table comparing A, B, and C across ~18 dimensions (distribution, subprocess mgmt, SDK availability, dev velocity, etc.). Go wins on the two load-bearing dimensions for Forge — single-binary distribution and subprocess orchestration — without any disqualifying weakness.

**Implications for subsequent questions:**
- Distribution targets: `brew`, `curl | sh` installer, GitHub releases binaries, `go install`.
- No Node/Python/Rust runtime required for end users.
- Forge will use `github.com/anthropics/anthropic-sdk-go` for its own LLM calls (meta-tasks like research, stuck detection).
- Cross-compile for Linux/macOS/Windows natively.
- CLI framework candidates: `cobra` (most common), `urfave/cli`, `kong`.
- TUI/prompt libs: `bubbletea` / `charmbracelet/*` for interactive escalation UX.

---

## Q4: End-to-end UX and CLI command shape

How does a user actually interact with Forge? This determines the CLI surface, the number of commands, where the AI makes decisions, and where humans are prompted. Your rough idea emphasizes "limited CLI commands" and "AI to do previous research before asking" — so this question is about shaping the primary flow.

**Options:**

- **A. Single-command "describe it, it builds it".** One command: `forge "build me X"`. Forge researches, plans, configures the Ralph loop, runs it, ships. No intermediate steps. Maximum automation. Risk: user has zero visibility until it's done (or broken).

- **B. Plan-then-run.** Two commands: `forge plan "build me X"` generates a plan (research + decisions + draft PROMPT.md), user reviews/edits, then `forge run` starts the loop. Familiar pattern from `ralph-orchestrator` (`ralph plan` → `ralph run`) and Terraform (`plan` → `apply`).

- **C. Multi-stage project-based.** More commands: `forge init`, `forge plan <feature>`, `forge run`, `forge verify`, `forge ship`. GSD-style. Explicit control at every stage. More CLI surface — contradicts "limited commands".

- **D. Single-command with inline confirmation (describe → auto-research → show plan → confirm → run).** One command: `forge "build me X"`. Forge does research, drafts a plan, prints it, pauses for human confirmation (one keypress: `y` / `n` / `e` for edit), then starts the loop. All in one invocation; no separate `plan` phase.

- **E. Interactive TUI.** `forge` with no args launches a full TUI that walks the user through task entry, plan review, and loop monitoring. Most polished; biggest implementation cost.

**Orthogonal secondary commands** (all options assume these exist regardless of primary flow):
- `forge status` — what's the loop doing right now
- `forge stop` / `forge resume` — pause/resume a running loop
- `forge backend` — view/select backend (Claude Code / Kiro / Gemini CLI)
- `forge doctor` — diagnose install issues, missing backends, stuck states

**Recommendation: D (single command with inline confirmation).** Reasons:
1. **Matches your "limited CLI commands" constraint.** One primary verb (`forge "<task>"`) + a handful of status/control commands. Compare with GSD's 40+ commands.
2. **Matches your "AI researches then asks" philosophy.** Forge does the research, proposes a plan, and only asks when the human's input is actually needed — which is at the decision point where you approve the plan before burning tokens.
3. **Keeps visibility without ceremony.** A has no visibility; B splits into two invocations (more friction); C has too many commands. D gives the human an approval gate without requiring them to manage explicit phases.
4. **Mirrors the best of Ralph's philosophy.** Ralph is one bash loop; Forge's primary command is one invocation. The loop is the engine; everything else is delegated to the AI.
5. **Escalation is additive.** Mid-loop, if Forge gets stuck or needs a human decision, it pauses and prompts (via the same TUI used for initial confirmation). Same UX everywhere.

**Proposed CLI surface (under D):**

```
forge "<task>"              # Primary command: research + plan + confirm + run
forge status                # What's happening now
forge stop                  # Gracefully stop running loop
forge resume                # Resume last paused loop
forge backend [set <name>]  # View or change backend
forge doctor                # Diagnose install / backend issues
forge --version / --help    # Standard
```

That's ~6 commands total. Tight.

**Answer: D, with two additions.**

1. **Optional dry-run sub-command.** `forge plan "<task>"` is supported as an explicit dry-run that stops after the plan is printed. Gives users B's flow when they want it, without paying for a separate phase in the happy path.

2. **Intent-based routing from the task's opening verb.** `forge "<task>"` inspects the natural-language task description and routes to a specific path (workflow mode) based on the verb. Example routings from the user:
   - `forge "Review the auth module..."` → **review path**
   - `forge "Create a CLI for..."` → **build path**
   - (Additional verbs and their paths — TBD in Q5.)

   This means the primary command is still one invocation, but internally Forge picks different workflows based on what the human is asking for. The set of paths and how routing works are specified in Q5 and Q6.

**Implications for subsequent questions:**
- There is no single "the loop" — different paths have different lifecycles (some are Ralph loops, some are one-shot).
- Path inventory must be decided (Q5): what paths exist in v1, and what does each do.
- Intent-detection mechanism must be decided (Q6): pure keyword matching, LLM classification, or hybrid.
- Each path needs its own prompt template, completion criteria, and context-management strategy — these are downstream design decisions.
- The "which situations need human intervention" escalation policy (rough-idea open question) is per-path.

---

## Q5: What paths (task modes) does Forge support in v1?

A "path" is a named workflow with its own prompt template, lifecycle (loop vs. one-shot), completion criteria, and escalation policy. The task's opening verb selects the path.

**Core observation:** Not every task is a Ralph loop. Some tasks (review, explain, research) produce an output artifact and are done. Others (create, fix, refactor) are genuinely iterative and benefit from looping. The path system makes this explicit.

**Proposed v1 path inventory (5 paths):**

| Path | Trigger verbs (examples) | Lifecycle | Completion criterion | Output |
|---|---|---|---|---|
| **Create** | create, build, generate, make, scaffold, start | Ralph loop | Specs met, tests pass, demo-able increment | New code + commits |
| **Add** | add, implement, extend, introduce | Ralph loop | Feature merged, tests pass, no regressions | New/modified code + commits |
| **Fix** | fix, debug, repair, resolve, patch | Ralph loop (diagnostic-first) | Bug no longer reproduces, regression test added | Minimal diff + test + commit |
| **Refactor** | refactor, restructure, rename, reorganize, simplify, cleanup, modernize | Ralph loop (behavior-preserving) | Target shape achieved, all tests still pass | Refactored code + commits |
| **Review** | review, audit, analyze, inspect, check, critique | One-shot (no Ralph loop) | Report produced | Markdown report; no code changes |

**Why this split:**
- **Create vs. Add.** Greenfield and brownfield differ in a way that matters — Ralph's original author explicitly advises against Ralph on existing codebases because the non-determinism is worse. Create path can be more aggressive (spec-driven); Add path must be more cautious (preserve existing behavior, read before write). Same loop primitive, different prompts and guardrails.
- **Fix vs. Refactor.** Both operate on existing code, but Fix's completion criterion is "bug gone + regression test" while Refactor's is "tests still pass + shape achieved." Different success signals → different prompts.
- **Review is one-shot.** No loop — analysis produces a report and exits. Matches your constraint that "git operations don't need Ralph loop" (review, like git ops, is a single action not an iteration).

**Deferred paths (not in v1):**
- **Document** — writes docs; can be loop-driven, but often one-shot. Add post-v1.
- **Test** — writes tests; usually subsumed by Add or Fix. Add post-v1 if it proves worth its own lifecycle.
- **Upgrade / Migrate** — dependency bumps, framework migrations. High value but lots of edge cases; defer.
- **Explain** — one-shot exposition. Close to Review; could merge into Review path or add later.
- **Research** — gather info without touching code. Nice-to-have but not core.

**Alternatives considered:**
- **Fewer paths (3):** Create, Fix, Review. Merge Add into Create, merge Refactor into Fix. Simpler, but loses the behavior-preservation signal that Refactor needs.
- **More paths (7+):** Add Document, Test, Upgrade to v1. More coverage, but every added path is more prompt engineering and more lifecycle logic to maintain — contradicts the "ship something tight first" instinct.
- **No path system (one universal workflow):** Let the AI figure out from the prompt what to do. Rejected because it puts the intent inference into the Ralph loop itself, which conflicts with "Forge makes decisions before the loop starts."

**Recommendation: the 5-path inventory above.**
- Covers the vast majority of day-to-day dev tasks.
- Each path has a clear, distinct completion criterion (crucial for stuck detection later — a stuck Fix looks different from a stuck Create).
- Small enough that each path can be prompt-engineered well rather than all being generic.
- Additions (Document, Test, etc.) can be layered in post-v1 without breaking the abstraction.

**Answer: confirmed — the 5-path inventory above, with Review as one-shot + parallel subagents.**

Sub-decision on Review lifecycle: **one-shot with parallel subagents** (GSD-style fan-out), not a Ralph loop. Rationale: Review produces a report, not an iteratively-built artifact; there's no accumulating state that benefits from Ralph's per-iteration reset; review benefits from holding the whole mental model in one invocation (either main-agent or fanned-out sub-agents), not from restarting. If a future need arises (codebases so large even fan-out can't cover), a looped-review variant can be added post-v1.

**Implications for subsequent questions:**
- Forge's execution engine needs two lifecycle primitives: **Ralph loop** (Create, Add, Fix, Refactor) and **one-shot with parallel subagents** (Review).
- Completion detection is per-path — each path needs its own "am I done" logic.
- Prompt templates are per-path — 5 distinct templates in v1.
- Stuck detection (a later question) only applies to loop-lifecycle paths.
- Escalation triggers (a later question) are per-path.

---

## Q6: How does Forge detect intent and pick a path?

The task description is free-form natural language. Forge must map it to one of the 5 paths (Create / Add / Fix / Refactor / Review). This is Forge's first research-then-decide-then-escalate moment — the pattern should mirror how Forge handles every decision.

**Options:**

- **A. Pure keyword match on the opening verb.** Forge keeps a static verb-to-path table and matches the first (or first few) words of the task. Fast, deterministic, transparent. Rigid: fails on unusual phrasing like "Modernize the auth module" (Refactor?) or "Work on the login bug" (Fix?).

- **B. Pure LLM classification.** Every task goes through a small LLM call ("which of these 5 paths?"). Flexible, handles any phrasing. Adds ~500ms + a tiny token cost per invocation. Non-deterministic (same input can in theory route differently across invocations).

- **C. Hybrid — keyword fast path, LLM fallback.** Try the keyword table first. If no match, ambiguous, or multiple paths match, fall through to an LLM classifier. If the LLM is confident, proceed. If the LLM is uncertain (low confidence or ties), escalate to the human with options and a recommended one.

- **D. Always LLM + always confirm.** Every invocation runs the LLM classifier, and the detected path is always shown to the user at the confirmation step for approval/override.

**Recommendation: C (hybrid), with an explicit 4-step escalation ladder.**

This mirrors Forge's general decision-making pattern:

```
1. Keyword table match        →  path selected (fast path)
2. LLM classifier             →  path selected (research)
3. LLM low confidence or tie  →  human prompt with options + recommendation (escalation)
4. Human confirmation step    →  user sees detected path and can override (safety net)
```

**Why C over the alternatives:**
- A alone is too rigid. Real tasks don't all start with the five canonical verbs.
- B alone is wasteful (LLM call for `forge "Create a CLI..."` when keyword match is trivially correct) and non-deterministic.
- D is B plus extra user friction (always confirm) — fine, but we already have a confirmation step from Q4, so the classifier result just shows up there.
- C degrades gracefully: unambiguous tasks are instant and free; ambiguous tasks get LLM help; genuinely uncertain tasks ask the human.

**Proposed keyword table (v1):**

| Path | Trigger verbs (case-insensitive, match first word) |
|---|---|
| **Create** | create, build, generate, make, scaffold, start, initialize, bootstrap, new |
| **Add** | add, implement, extend, introduce, support, enable, integrate |
| **Fix** | fix, debug, repair, resolve, patch, address, troubleshoot |
| **Refactor** | refactor, restructure, rename, reorganize, simplify, cleanup, modernize, tidy, rewrite |
| **Review** | review, audit, analyze, inspect, check, critique, examine, assess |

**Escape hatch:** `forge --path=<name> "<task>"` explicit override flag for power users. Zero cost to add.

**Edge cases and their handling:**
- **Multi-intent tasks** ("Review and fix the auth module"): LLM classifier picks the dominant intent; the confirmation step lets the user accept or redirect. No multi-path execution in v1.
- **No verb at all** ("The login flow is broken"): keyword miss → LLM call → likely classifies as Fix.
- **Metaphorical verbs** ("Tighten up the login flow"): LLM handles what keyword table can't.
- **User disagrees with auto-detected path**: confirmation step's `e` (edit) option lets them change the path before the loop starts.

**Answer: C (hybrid).** 4-step escalation ladder confirmed: keyword match → LLM classifier → human prompt (if LLM low-confidence) → confirmation checkpoint as safety net. `--path=<name>` flag added as explicit override for power users.

**Implications for subsequent questions:**
- This 4-step ladder is Forge's **canonical decision-making pattern**, not just for path selection. It should be reused wherever Forge needs to make a decision (backend selection for a given task, next-step priority in a loop, escalation vs. retry when stuck, etc.). Establishing this pattern now avoids redesigning escalation for every decision site.
- Forge needs an "LLM classifier" primitive: a cheap Claude API call (likely Haiku-class, short output, structured response). This becomes a reusable building block.
- Confidence thresholds must be decided later (what numeric threshold constitutes "low confidence" for human escalation).
- The user's confirmation step (Q4) already surfaces the detected path — no extra UI is needed for path selection.

---

## Q7: What happens in the "plan phase," and what artifacts does it produce?

Between `forge "<task>"` and the confirmation prompt, Forge has a window to research, decide, and produce a plan. This is where research-before-asking actually happens. The decisions here shape what the user sees at confirmation, what files persist on disk, what the loop reads per iteration, and ultimately what "context management" even means for Forge.

**Core tension:** more research = better plan but slower/costlier; less research = faster/cheaper but plan quality suffers.

**Options:**

- **A. Lightweight — task.md + plan.md only.** LLM reads the task, answers a few internal structured questions, writes a short plan.md listing steps. Minimal codebase research (just file listing if brownfield). Fast. Weakest plan quality for brownfield.

- **B. Research-heavy PDD — full requirements + design + implementation-plan.** Spawn research subagents (codebase, docs, relevant prior art). Synthesize into requirements.md + design.md + implementation-plan.md (ralph-orchestrator's PDD model). Heaviest. Highest-quality plan. Slowest; arguably over-engineered for small tasks.

- **C. Path-specific right-sized.** Each path gets what it needs, nothing more:
  - **Create** — research light (it's greenfield); produce specs.md + plan.md.
  - **Add** — codebase research (what exists, integration points); produce codebase-map.md + plan.md.
  - **Fix** — reproduction + diagnostic research; produce bug.md (repro, expected/actual, suspects) + plan.md.
  - **Refactor** — current-shape + invariants research; produce target-shape.md + invariants.md + plan.md.
  - **Review** — scope analysis + subagent assignments; produce review-scope.md + subagent-plan.md (Review is one-shot, no iterative plan.md needed).

- **D. Streaming/incremental.** Forge shows the plan as it builds it (research → outline → refinement all visible in real time). Nicest UX, most implementation complexity (streaming rendering, progress updates).

**Proposed v1 artifact layout (shared across loop paths):**

```
.forge/
├── config.yml                    # global user prefs (default backend, etc.)
├── runs/
│   └── 2026-04-16-143022-add-dark-mode/
│       ├── task.md               # original + refined task understanding
│       ├── plan.md               # prioritized fix-plan (Ralph's fix_plan.md)
│       ├── state.md              # progress, decisions, blockers (updated per iteration)
│       ├── notes.md              # accumulated learnings (build commands, codebase map, invariants)
│       ├── path-specific.md      # per-path artifact (bug.md / target-shape.md / specs.md / etc.)
│       └── prompt.md             # rendered fresh each iteration, what backend CLI sees
└── current -> runs/2026-04-16-143022-add-dark-mode    # symlink to active run
```

Review (one-shot) variant:

```
.forge/runs/2026-04-16-143022-review-auth/
├── task.md
├── scope.md            # what's being reviewed, by which subagents
├── report.md           # the deliverable (what the user cares about)
└── transcripts/        # optional: per-subagent findings
```

**Why this layout:**
- **Shared base (`task.md`, `plan.md`, `state.md`) across loop paths** — Forge's runtime only needs to know these three; path-specific artifacts are read by the LLM via prompt composition.
- **`notes.md` replaces Ralph's `AGENT.md`** — same idea: accumulated learnings that survive loop iterations (build commands discovered, codebase patterns, invariants) but are pruned/distilled over time.
- **`prompt.md` is regenerated, not manually edited.** Forge assembles it from the other files each iteration. This is how context management actually happens.
- **Runs are directories, not single files.** Multiple runs can coexist; resume/inspect/discard is folder-granular.

**Recommendation: C (path-specific right-sized), with the artifact layout above.**

- A is too thin for brownfield paths (Add/Fix/Refactor). You can't produce a good plan for "Fix the login bug" without understanding the codebase.
- B is over-engineered. Most tasks don't need a formal requirements doc. PDD's heavyweight structure is useful when humans have to communicate — but Forge's primary consumer is an LLM, and LLMs can work from terser structured inputs.
- D is nice UX but adds weeks to implementation for questionable first-user value. Can be added post-v1 without breaking anything.

C lets Forge do the right amount of work per path: expensive reconnaissance for Refactor (invariants matter), minimal research for Create (no codebase to read), focused reproduction-hunting for Fix.

**What the user sees at the confirmation step:**
- Rendered summary of plan.md (the prioritized steps).
- Path-specific highlights (e.g., Fix: "Reproduces locally? ✓" + suspected root cause; Refactor: "Behavioral invariants I found: <list>").
- Cost estimate (rough token budget for expected iterations, based on path).
- `y` (go) / `n` (abort) / `e` (open plan in $EDITOR) / `r` (redo the plan phase, maybe with extra instructions).

**Answer: C (path-specific right-sized) + the proposed artifact layout.** The `.forge/runs/<timestamp>-<slug>/` directory model is confirmed, with shared base files (`task.md`, `plan.md`, `state.md`, `notes.md`) and path-specific additions (`bug.md`, `target-shape.md`, `invariants.md`, `specs.md`, `codebase-map.md`, `scope.md`). The confirmation prompt's `y/n/e/r` keys are confirmed.

**Implications for subsequent questions:**
- Forge needs a prompt-composition engine that assembles `prompt.md` from the persisted files on each iteration.
- `notes.md` growth must be managed (distillation) — detail belongs in Q8 (context management).
- `state.md` is the primary progress-tracking file and the source of truth for "is the loop stuck?" — detail belongs in Q9 (stuck detection).
- Per-path prompt templates are needed (5 of them in v1).
- The plan phase itself uses Forge's subagent mechanism (parallel researchers) — the same mechanism the loop will use. This implies subagents are a cross-cutting concern, not loop-specific.

---

## Q8: What's Forge's context-management strategy?

Forge's stated differentiator is automated context management. This is where that gets concrete: what's in the prompt every iteration, how it stays under the window, and how artifacts don't bloat.

**Background from the research:**
- Ralph's approach: reset context every iteration (fresh agent, same persisted state files). Ralph's author warns "the more you use the context window, the worse the outcomes" and measures Claude's *effective* window at ~147–152k (vs. 200k advertised).
- GSD's approach: externalize everything to markdown, spawn fresh-context subagents for heavy work, keep main context at 30–40% utilization.

**Options:**

- **A. Ralph-style: fresh context per iteration.** Every loop iteration is a brand-new agent invocation with `prompt.md` assembled from persisted files. No continuous context. Clean, predictable, matches Ralph's proven pattern. Cost: no conversational memory; state must be fully captured on disk.

- **B. GSD-style: externalize + parallel subagents, no true loop reset.** Main agent stays running; heavy work is delegated to fresh-context subagents. Context discipline comes from offloading, not resetting.

- **C. Hybrid: per-iteration reset (Ralph) + subagents on demand (GSD) + automatic distillation.** Each loop iteration resets. Expensive or parallelizable subtasks spawn subagents. `state.md` and `notes.md` are auto-compressed by Forge when they exceed per-file token budgets.

- **D. Continuous with aggressive trimming.** Main agent keeps context across iterations; Forge trims oldest messages when approaching limits. Contradicts Ralph's evidence that context bloat degrades quality.

**Proposed v1 strategy (C, with specifics):**

**1. Token budget enforcement per iteration**

Each iteration, Forge computes:

```
available_budget = effective_window − reserved_response − safety_margin
                 = 150k − 20k − 10k = 120k for prompt.md
```

Backend-specific effective windows (Claude Code uses Sonnet by default, so ~150k; Gemini CLI differs; Kiro differs). Forge tracks these per-backend.

**2. prompt.md composition order**

Assembled fresh every iteration, in this priority order until budget exhausted:
1. **System prompt** (path-specific; ~2–5k). Always included.
2. **task.md** (~<1k). Always included.
3. **Path-specific artifact** (bug.md / target-shape.md / etc.; ~2–10k). Always included.
4. **Top-N items of plan.md** (not the whole file — only the next priorities Forge selected). ~1–3k.
5. **state.md** (distilled if over budget). ~3–10k.
6. **Relevant notes from notes.md** (dynamically selected — semantic match to current priorities, not whole file). ~2–8k.
7. **Per-iteration instructions** — what Ralph is asked to do *this* iteration. ~1–2k.

Everything else (history, archived notes, transcripts) lives on disk but isn't sent.

**3. Automatic distillation**

When any of these thresholds trip:
- `state.md` > 8k tokens → distill into a compressed summary + archive full version as `state-<iter>.md`.
- `notes.md` > 10k tokens → LLM-driven pruning (keep still-relevant, archive the rest).
- `plan.md` > 6k tokens → re-prioritize (Ralph's "I have deleted the TODO list multiple times" pattern: regenerate fresh from task + current state).

Distillation is a Forge subagent call (Haiku-class; cheap, fast) run between iterations. The user doesn't see this unless they opt into verbose mode.

**4. Subagent spawning triggers**

Forge spawns subagents (either via backend-native tools like Claude Code's Task tool, or via direct API calls) when:
- **Codebase search** — spawn parallel searchers, each with clean context.
- **Independent subtasks** — e.g., "implement feature A and feature B in parallel" when dependencies allow.
- **Expensive reasoning** — distillation, plan re-prioritization, stuck diagnosis (Q9).
- **Plan-phase research** — parallel researchers per topic (same mechanism used by Review path).

**5. Emergency compression**

If the main agent's reply signals it ran out of context mid-iteration (detected by truncated output, specific error signatures, or self-report), Forge:
1. Aborts that iteration's changes (git stash or git reset).
2. Runs distillation on `state.md` and `notes.md` immediately.
3. Regenerates `prompt.md` with compressed state.
4. Retries the iteration.

**6. User-visible knobs (minimal by default)**

- `forge config context.budget <tokens>` — override the default budget.
- `forge config context.verbose true` — log every prompt assembly + distillation event.
- Everything else is automatic (per the "minimal human burden" principle).

**Recommendation: C, with the specifics above.**

- A (pure Ralph) is 80% of the right answer but leaves artifact bloat (`notes.md` growing unbounded) and no subagent story. The additions are small and high-value.
- B (pure GSD) keeps a long-running main context, which conflicts with Ralph's evidence that iteration-reset is what keeps quality high in long loops. GSD mitigates this with phase boundaries; Forge doesn't have those in the loop paths (Create/Add/Fix/Refactor loop without phase breaks).
- D is explicitly what Ralph warns against.

C inherits Ralph's proven reset discipline, adds GSD's externalization and subagent pattern where it fits, and makes distillation automatic so the user never sees the machinery.

**Answer: C with all proposed specifics confirmed.** Per-iteration reset; path-specific prompt composition order; automatic distillation at 8k/10k/6k thresholds for state/notes/plan; subagents for search, independent subtasks, expensive reasoning, and plan-phase research; emergency compression on mid-iteration context exhaustion; user knobs limited to `context.budget` and `context.verbose`.

**Implications for subsequent questions:**
- Distillation-subagent is a Forge primitive with a defined contract (input file → compressed output + archive). Reused in several places.
- Per-iteration artifact monitoring (file size, token count, semantic change) is needed as a runtime mechanism — this becomes part of the "progress ledger" used by stuck detection in Q9.
- Backend-specific context windows must be tracked in a registry (Claude Sonnet / Kiro / Gemini have different effective windows).
- The "iteration" is Forge's fundamental unit of work — everything is measured per-iteration.

---

## Q9: How does Forge detect and handle stuck loops?

The user explicitly called this out in the rough idea. Failure to detect stuck loops is how Ralph burns money on thrashing agents. This question covers both detection (what signals stuck?) and handling (what does Forge do when stuck?).

**Failure modes to detect:**

| Mode | What it looks like |
|---|---|
| **No progress** | `plan.md` items not checked off; `state.md` doesn't meaningfully change |
| **Oscillation** | Files change but get reverted; same code rewritten repeatedly |
| **Repeated error** | Same test fails same way ≥N iterations |
| **Build broken** | Codebase won't compile/run for ≥N iterations |
| **Test regression** | Previously-passing tests now fail and aren't being fixed |
| **Off-topic drift** | Agent working on things unrelated to the task |
| **Placeholder accumulation** | Items marked "done" but are stubs (Ralph's explicit concern) |
| **Agent self-loop** | Agent repeats same reasoning, produces same output |
| **Budget blown** | Token/cost budget exceeded without completion |
| **False completion refusal** | Task looks done but agent won't emit `LOOP_COMPLETE` |

**Options:**

- **A. Simple iteration cap (ralph-orchestrator's approach).** `max_iterations` config; abort when hit. Cheap to implement, useless at detecting *why* it's stuck.

- **B. Objective multi-signal progress tracker.** Per-iteration metrics (files changed, plan items completed, build/test status, state semantic delta, error fingerprints). Compute a stuck score; threshold-triggered response.

- **C. Agent self-report.** Agent declares itself stuck via a sentinel ("I cannot make progress because..."). Forge listens for it.

- **D. Hybrid B + C with graduated response.** Objective signals are primary (don't trust a stuck agent to know it's stuck); self-report is a secondary signal that raises the stuck score; responses graduate by severity tier.

**Proposed mechanism (D, with specifics):**

**Progress ledger (per iteration)**

Forge records, after each iteration:
- `files_changed` — set of files modified, added, deleted (with hashes)
- `plan_items_delta` — which items in `plan.md` got checked off or added
- `state_semantic_delta` — LLM-judged: did `state.md` meaningfully change? (boolean + short rationale)
- `build_status` — one of `pass | fail | unknown` (via path-specific check commands from `notes.md`)
- `test_status` — `pass | fail | partial | unknown` + count of failures
- `regressions` — tests that passed last iteration and fail this one
- `error_fingerprint` — hash of normalized error output (detects repetition)
- `agent_self_report` — "stuck" | "progressing" | "uncertain" (agent prompted to self-assess each iteration)
- `iteration_cost` — tokens consumed this iteration

**Stuck score calculation**

Each iteration, Forge computes a stuck score from a rolling window (last 3 iterations). Heuristic weights (tunable):

| Signal | Contribution |
|---|---|
| No files changed in window | +3 |
| No plan items closed in window | +2 |
| Semantic state delta = false across window | +3 |
| Build broken for ≥2 consecutive iters | +3 |
| Same error fingerprint ≥2 consecutive iters | +4 |
| Test regression introduced | +2 |
| Agent self-reports stuck | +2 |
| Off-topic drift detected (LLM-judged) | +5 |
| Placeholder accumulation detected (LLM-scan of diff) | +3 |

**Graduated response (tiered)**

| Tier | Score threshold | Action |
|---|---|---|
| **0. Progressing** | < 3 | Continue normally |
| **1. Soft stuck** | 3–5 | Spawn diagnostic subagent → analyze what's blocking → inject finding into `state.md` → continue |
| **2. Hard stuck** | 6–9 | Regenerate `plan.md` from scratch (Ralph's delete-the-TODO pattern) using task + current state; also try: switch to alternative backend model (e.g., Opus for one iteration) if available |
| **3. Dead stuck** | ≥ 10 OR diagnostic subagent says "no viable path" OR iteration budget exhausted | Pause loop; escalate to human with diagnostic report, options (reset-to-last-green / pivot approach / split task / abort), and a recommendation |

**Hard ceilings (absolute, regardless of score):**
- `max_iterations` (configurable; default e.g. 100)
- `max_cost_usd` (configurable; default e.g. $10)
- `max_duration` (configurable; default e.g. 4h)
- Build broken continuously for ≥5 iterations → auto-escalate to Tier 3
- Same `error_fingerprint` ≥4 iterations → auto-escalate to Tier 2 then 3

**Escape hatches always available:**
- **Git safety net.** Every iteration that changes files is committed (Forge-managed commit). Rollback to any previous iteration is a `git reset --hard <sha>`.
- **Ralph's "just reset" escape.** Tier 3 recommendation can include "reset to start and try different approach" as an option.
- **User Ctrl-C at any time** surfaces a Tier 3 prompt (what to preserve / what to discard / resume point).

**Per-path tuning:**
- **Create** — fresh codebase, so some early iterations may have no test/build status (unknown, not fail). Don't penalize `unknown`.
- **Fix** — success = test added + existing test suite still green. Regression introduction weighted heavier.
- **Refactor** — success = invariants preserved + shape achieved. Behavior change penalized heavily.
- **Add** — similar to Fix for regression weighting.
- **Review** — not a loop, not subject to stuck detection. Has its own "subagent timeout" policy instead.

**Recommendation: D, with the mechanism above.**

- A alone is useless (no diagnosis, just death).
- B alone is robust but slow to act in cases where the agent *knows* it's stuck.
- C alone is fragile — stuck agents often don't know they're stuck.
- D combines them with graduated response, which is the right shape: cheap intervention first, expensive intervention (human) last.

The key design commitment: **Forge acts before it escalates.** Tier 1 and Tier 2 try to recover autonomously; only Tier 3 interrupts the user. This matches the rough idea's "AI decides if possible, human only if needed."

**Answer: D with all proposed specifics confirmed.** Progress ledger + stuck score + graduated tiers + hard ceilings + git safety net + per-path tuning are all accepted.

**Implications for subsequent questions:**
- Every file-changing iteration requires an auto-commit (git workflow is implicit — needs explicit design in a later question).
- Escalation mechanism (Tier 3 "pause and ask human") needs concrete UX — detail belongs in Q11.
- Diagnostic subagent and plan-regeneration subagent are Forge primitives, used by Q8 (context mgmt) and Q9 (stuck handling).
- Forge needs to run build/test commands on the user's codebase — requires learning them per project (stored in `notes.md`, Ralph-style `AGENT.md` pattern).
- Rolling-window metrics imply Forge persists per-iteration ledger entries on disk (likely `.forge/runs/<run>/ledger.jsonl`).

---

## Q10: What situations always require human intervention (escalation policy)?

The rough idea explicitly demands we specify this. Forge's philosophy is "AI decides if it can, human only if needed" — but some decisions should *never* be made autonomously, regardless of how confident Forge is. This question enumerates those, plus the second tier of "escalate if research is insufficient."

**Categories:**

### 1. Always requires human — mandatory gates (AI cannot override)

| Situation | Why mandatory |
|---|---|
| **Initial task submission** | By definition — Forge can't start without a task from a human. |
| **Plan confirmation (Q4 checkpoint)** | Approving the plan before the loop burns tokens. Skipped only with explicit `--yes`. |
| **Tier-3 stuck-loop escalation (Q9)** | Loop is dead-stuck; autonomous recovery failed. Human must decide pivot / reset / abort. |
| **Destructive git ops: `push --force`, `reset --hard` (on user-visible branches), `branch -D`, `clean -fd`** | Irreversible or visible to others. Never automate. |
| **Git push to remote** | External-facing (visible to teammates/CI). Requires explicit approval, even in autonomous modes. |
| **Creating/closing PRs, issues, commenting** | External-facing, stakeholder-visible. |
| **Sending messages (Slack, email, etc.)** | External-facing. |
| **Installing system packages (`apt install`, `brew install`, etc.)** | Affects the user's machine outside the project. |
| **Modifying CI/CD pipelines** | Affects shared infrastructure. |
| **Modifying credentials, secrets, env vars, or `.env*` files** | Security-sensitive. |
| **Dependency changes in locked manifests** (`package-lock.json`, `Cargo.lock`, `go.sum`, etc. churn beyond expected delta) | Supply-chain surface. Auto-resolvable for minor/patch bumps with passing tests; major/breaking requires approval. |
| **Detected secret in diff** (API keys, tokens, credentials matched by regex/scanner) | Must never be auto-committed. Hard stop. |
| **Branch-protection-protected operations** (direct writes to `main`/`master` in repos that block it) | Forge refuses and escalates. |
| **Ambiguous task resolution when LLM classifier is low-confidence** (Q6 Tier 3) | Can't pick a path confidently → ask. |
| **Mid-loop scope change** (user types something new while loop is running) | Never implicitly redirect; confirm pivot. |

### 2. Escalation after autonomous research fails — tier-based

Forge first tries to decide autonomously (keyword lookup → LLM classifier → decision). If confidence is below threshold after research, escalate.

| Situation | Autonomous first step | Escalation when… |
|---|---|---|
| **Path selection (Q6)** | Keyword → LLM classifier | LLM confidence low or multiple paths tied |
| **Backend selection** | Read `.forge/config.yml` default, else check available CLIs, else prompt | No default + multiple backends installed + can't infer from task |
| **Ambiguous completion** | LLM judge: "does the output match task goals?" | Judge uncertain |
| **Conflicting design alternatives** | LLM picks the best per research | No clear winner after research |
| **Merge conflict** | Attempt auto-resolution (semantic merge via LLM); run tests | Tests fail post-auto-resolve, or semantic merge declines |
| **Test failure of ambiguous cause** | Diagnostic subagent tries to localize | Diagnosis inconclusive |
| **Build/runtime command unknown** | Read `README.md`, `package.json`, `Cargo.toml`, etc. | No idiomatic command found |

### 3. Interrupt-on-demand (human-initiated)

- **Ctrl-C** at any time → pause cleanly, preserve state, prompt for what to do.
- **`forge stop`** → graceful shutdown + state preserved.
- **Out-of-band signal** (file flag `.forge/current/INTERRUPT`, for remote/notification-driven halt) → pause at next iteration boundary.

### 4. Per-path defaults

| Path | Mandatory human gates (in addition to category 1) |
|---|---|
| **Create** | None beyond category 1. Greenfield: aggressive automation is fine. |
| **Add** | None beyond category 1. |
| **Fix** | None beyond category 1. |
| **Refactor** | **One extra:** before starting, confirm the list of behavioral invariants Forge will preserve. Matches the user's "situations needing default human guidance." Refactors that break invariants are the classic foot-gun. |
| **Review** | None — Review is read-only by design. |

### 5. Escalation prompt anatomy (what the human sees)

Every escalation prompt (categories 1–3) contains:
- **What Forge tried** — research steps and their outcomes.
- **The decision Forge can't make** — one-sentence framing.
- **Options** — 2–4 concrete choices, labeled and short-keyed.
- **Recommendation** — Forge's best guess with one-line justification (when there is one; sometimes explicitly "no recommendation — this is your call").
- **Defer option** — `d` to leave it, pause the loop, come back later.

**Options considered for the policy itself:**

- **A. No mandatory gates — trust the AI fully.** Rejected: supply-chain attacks, secret leaks, and irreversible actions are not places where "research then decide" is acceptable.
- **B. Minimal gates (only irreversible stuff).** Tempting but misses destructive git operations and external-facing actions.
- **C. Comprehensive gates as proposed above.** Maps to standard ops/security best practices.
- **D. Everything always asks.** Would contradict the "minimal human burden" design goal.

**Recommendation: C.** The list above is the right balance — mandatory gates on security, destructiveness, and external-facing actions; tier-based escalation for ambiguous decisions; per-path defaults where the failure mode differs (Refactor's invariant check is path-specific). Everything else runs autonomously.

**Answer: C confirmed.** Full policy list accepted as proposed: 4 categories (mandatory gates, tier-based research-first escalation, interrupt-on-demand, per-path defaults with Refactor's invariant-confirmation gate). Escalation prompt anatomy (tried / decision / options / recommendation / defer) accepted.

**Implications for subsequent questions:**
- Forge needs a secret-scanning mechanism (regex + common heuristics) running on every diff.
- Forge needs a branch-protection awareness check (query `git` for protected branches; respect `.github/branch-protection.yml` if present).
- Forge needs a dependency-change detector distinguishing patch/minor (auto-OK when tests pass) from major/breaking (escalate).
- The `awaiting-human` state must persist to disk so it survives terminal disconnection — belongs in Q11 (escalation UX) and Q13 (state/resume).
- The "what Forge tried + options + recommendation" payload format is standardized — belongs in Q11.
- Refactor's invariant-confirmation gate is a per-path feature of the plan phase; gets surfaced before the confirmation checkpoint, not during it.

---

## Q11: Escalation UX — how does Forge notify and prompt the human?

Now that we know *when* Forge escalates, we need to decide *how*. The design constraint is still "minimal human burden" — which pulls in two directions: the user shouldn't have to babysit a terminal, but extra integrations add setup friction.

**Options:**

- **A. Terminal-only.** Inline prompt when user is at TTY. If Forge is detached/backgrounded, loop pauses silently and waits. User must remember to check.

- **B. Terminal + OS-native notification.** Inline prompt plus a desktop notification (macOS Notification Center / Linux `notify-send` / Windows toast) when escalation triggers. Zero setup. Cross-platform. Gets attention without babysitting.

- **C. Terminal + OS notification + webhook.** Add an optional outgoing webhook POST on escalation. Lets users wire Slack/Discord/Telegram/email themselves without Forge shipping N integrations. Still zero setup for basic use.

- **D. Terminal + OS notification + Telegram bot (ralph-orchestrator's RObot).** First-party Telegram integration. Richest out-of-band UX; most implementation work.

- **E. Everything (C + D + email + web UI).** Most flexible, most bloat — contradicts "limited CLI commands" and "minimal burden" spirit.

**Proposed mechanism (C, with specifics):**

### 5.1 File-based source of truth

When an escalation triggers, Forge writes `.forge/current/awaiting-human.md` with a structured format:

```markdown
---
id: esc-2026-04-16-143022-001
raised_at: 2026-04-16T14:30:22Z
tier: 3
path: fix
iteration: 14
resolved: false
---

## What Forge tried
- Ran failing test `login_test.go::TestValidCredentials` — reproduces consistently (14/14 iterations).
- Root-cause candidates explored: (a) bcrypt cost param mismatch, (b) clock skew in JWT exp, (c) session-store race.
- Each candidate patched and tested; all 14 iterations reintroduced the same error fingerprint.

## Decision
Should Forge pivot approach, reset to the last-green commit and try differently, split the task, or abort?

## Options
- **[p] Pivot** — keep current code; try suggesting a new diagnostic direction I add to state.md.
- **[r] Reset + retry** — `git reset --hard <last-green-sha>`; fresh plan.
- **[s] Split task** — task is too broad; narrow to "fix JWT exp handling only" and defer the rest.
- **[a] Abort** — stop the run, preserve artifacts for manual debug.
- **[d] Defer** — pause, come back later.

## Recommendation
**[r] Reset + retry.** The error fingerprint hasn't shifted in 14 iterations, which means current approach is on a dead-end path. Reset is cheap (42 lines of changes to discard). A fresh plan gets a clean start.

## How to answer
- Interactively: reply with a single letter in the terminal.
- Out-of-band: edit this file, fill in the `resolved` frontmatter and an `answer: <letter>` line, save, run `forge resume`.
```

### 5.2 Notification channels (layered)

1. **Terminal inline prompt** (if TTY attached) — keystroke response (`p` / `r` / `s` / `a` / `d`). If user hits `?`, expand into a fuller transcript.
2. **OS-native notification** (always on, cross-platform via Go libraries — no user setup). Short summary + clickable open action that focuses the terminal.
3. **Optional outgoing webhook** — if `config.webhook.url` is set, Forge POSTs a compact JSON payload (`id`, `tier`, `summary`, `recommendation`, `url_to_file`) on every escalation. Users wire this to Slack/Discord/Telegram/SMS/pager themselves.
4. **Bell on escalation** (terminal `\a`), as a light attention-grabber when user is at the terminal but not watching.

### 5.3 Response channels

Primary:
- **Terminal keystroke** — single letter.
- **File edit + `forge resume`** — for detached/remote workflows. User edits `awaiting-human.md`, adds `answer: <letter>`, runs `forge resume`.

Forge watches the file when not in interactive mode (`inotify`/`fsnotify`) and auto-detects the answer without requiring `forge resume`.

### 5.4 Timeout behavior

- Escalations **never time out by default**. The loop pauses indefinitely; the user can come back tomorrow.
- `--timeout <duration>` flag forces a default answer after N minutes (for CI-like scenarios). If the recommendation has a confidence, timeout defaults to that recommendation; otherwise, it defaults to `abort`.

### 5.5 Non-interactive modes

For CI and autonomous scenarios:
- `forge --yes "<task>"` — auto-accept the plan confirmation.
- `forge --auto-resolve <policy>` — how to handle mid-loop escalations:
  - `accept-recommended` — auto-pick Forge's recommendation for non-mandatory-gate escalations; mandatory gates still halt.
  - `abort` — any escalation aborts the run (strict CI mode).
  - `never` *(default)* — pause and wait for human.
- `forge --hook <command>` — run a user-provided command at escalation time (receives JSON on stdin; exit 0 to proceed, nonzero to abort). Lets user write custom handlers.

### 5.6 Deferred post-v1

- First-party Telegram integration (D) — valuable, but users can reach it via webhook in v1.
- Web UI console — post-v1 polish.
- Mobile app — no.

**Recommendation: C (terminal + OS notification + webhook).**

- A leaves the user babysitting the terminal — fails the "minimal burden" test.
- B is the right floor but denies users a way to route alerts to where they actually are (Slack, phone).
- D commits to a specific third-party service; webhook (C) is strictly more flexible for the same implementation cost.
- E is scope creep.

C + the file-based source of truth gives us:
- No-setup default UX (terminal + desktop notification).
- Power-user extensibility (webhook).
- Remote/async workflows (file-edit + auto-detect).
- CI/automation story (`--yes`, `--auto-resolve`, `--hook`).

**Answer: B.** Terminal + OS-native notification only. No webhook integration in v1. No `--hook` flag. Simpler surface; users who need external integrations (Slack, Telegram, pager) can add them post-v1.

**Retained from original recommendation:**
- File-based source of truth (`.forge/current/awaiting-human.md`) — orthogonal to notification channels; needed for remote/async responses and terminal-disconnection survival.
- Terminal inline prompt + OS-native desktop notification + terminal bell.
- Response via keystroke **or** file edit (fsnotify auto-detects saved answer).
- Timeout: never by default; `--timeout <duration>` flag for CI.
- Non-interactive flags: `--yes` (skip plan confirmation), `--auto-resolve {accept-recommended|abort|never}` (default: `never`).

**Dropped from original recommendation:**
- Outgoing webhook support (was in C).
- `--hook <command>` flag for custom escalation handlers (was in C).

**Deferred post-v1:**
- First-party Telegram bot.
- Web UI console.
- Webhook and hook-script integrations (can be re-added if demanded).

**Implications for subsequent questions:**
- OS-notification implementation: cross-platform Go library (e.g., `beeep`, `gen2brain/beeep`, or native syscalls per OS). Cost is tiny.
- The escalation payload format (front-mattered markdown with the anatomy fields) is a Forge primitive — documented as part of the spec.
- CI story is narrower than originally proposed — `--yes` + `--auto-resolve` + `--timeout` are the only automation knobs; no user-defined hooks.

---

## Q12: Where does Forge's "brain" run — direct API calls, or through the backend CLI?

Throughout Q5–Q11 we've committed Forge to making frequent autonomous LLM calls: the intent classifier, the semantic state-delta judge, the distillation subagent, the diagnostic subagent, the plan-regeneration subagent, the research subagents (plan phase + Review path). These happen *in addition* to the main loop's backend-CLI invocations.

The open question is: **where do those meta-calls run?**

**Options:**

- **A. Direct Anthropic API calls.** Forge uses `anthropic-sdk-go` directly. Requires user to set `ANTHROPIC_API_KEY` separately from their backend CLI (e.g., Claude Code's subscription). Faster (no subprocess overhead), cleaner structured responses (JSON mode), but adds a credential-setup step the user didn't ask for.

- **B. Shell out to the configured backend CLI for meta-tasks too.** Forge invokes `claude-code` / `gemini` / `kiro` as subprocesses for every meta-call. Zero extra credentials — if the user's backend works, Forge's brain works. Per-call overhead is higher (subprocess spawn ~100–500ms + LLM latency). Works for Gemini/Kiro users who have no Anthropic access.

- **C. Prefer direct API, fall back to backend CLI.** If `ANTHROPIC_API_KEY` is set, use direct API for meta-tasks; otherwise shell out. Zero setup by default, faster for users who opt in. Two code paths to maintain.

- **D. Always backend CLI, add direct-API acceleration post-v1.** Same as B for v1; C's hybrid gets deferred.

**Trade-off dimensions:**

| Dimension | **A (Direct API)** | **B (Backend CLI)** | **C (Hybrid)** | **D (B now, C later)** |
|---|---|---|---|---|
| Extra credential setup | Required | None | Optional | None in v1 |
| Works for Gemini/Kiro users without Anthropic account | ❌ | ✅ | ✅ | ✅ |
| Meta-call latency | ~300ms | ~1–3s | Best of both | v1: slower; v2: both |
| Cost model consistency with user's backend | Different bill | Same bill | Mixed | Same in v1 |
| Implementation complexity | Low | Low | Medium (two paths) | Low in v1 |
| Small-model availability (Haiku-class for cheap classifiers) | ✅ native | ⚠️ depends on backend CLI's model controls | ✅ | ⚠️ v1 |
| Parallel subagent spawning | ✅ SDK-native | ⚠️ spawn multiple subprocesses (heavier) | ✅ | ⚠️ v1 |

**Hidden concerns:**

1. **Subagent parallelism cost.** Plan-phase research spawns 3–4 parallel researchers. Via direct API, these are async HTTP calls — trivial. Via backend CLI, they're 3–4 separate subprocesses — each with its own startup cost and its own terminal/PTY handling. At Ralph's recommended scale (up to 500 parallel subagents for search), CLI spawning becomes untenable.

2. **Model selection granularity.** Forge's brain benefits from cheap models (Haiku) for classifiers and expensive models (Sonnet/Opus) for diagnosis. Direct API lets Forge pick per-call. Backend CLI limits Forge to whatever model the CLI is configured with.

3. **Structured output.** JSON-mode / tool-use is clean via direct API; forcing structured output through a CLI is hit-or-miss (need to parse free-form text).

4. **Credential story is real.** Forcing ANTHROPIC_API_KEY on every user violates "minimal human burden" — especially for Gemini/Kiro users who may not have one.

**Recommendation: C (hybrid — direct API preferred, backend CLI fallback).**

- B pushes parallel subagents and structured-output handling into subprocess-land, which is painful at scale. Also limits the brain to the main backend's model tier.
- A assumes every user has an Anthropic key; wrong assumption for a multi-backend tool.
- D defers the right answer without a good reason — the hybrid code path isn't large.
- C is the honest answer: use the fast/structured path when available, degrade gracefully when not.

**Specifics for C:**

1. **Detection at startup.** Forge checks `ANTHROPIC_API_KEY` env var (and `config.brain.api_key` as fallback). If present → direct-API mode. Otherwise → backend-CLI mode. Mode is logged once at startup; user can override with `--brain api|cli`.

2. **Contract at the brain-primitive layer.** Forge has one internal interface (`Classify`, `Judge`, `Distill`, `Spawn`, etc.); two implementations (SDK-backed, subprocess-backed) plug in. Code that uses the brain doesn't care.

3. **Non-Claude backends (Gemini, Kiro) are CLI-mode-only for v1.** Adding direct Gemini/Kiro API support is scope creep. Users of those backends get B's experience.

4. **Model selection rule.**
   - Direct-API mode: Forge picks Haiku for classifiers/distillation, Sonnet for diagnosis, Opus for plan regeneration (tunable).
   - CLI mode: Forge uses whatever the backend CLI is configured with. Classifiers and distillation become more expensive per call, but still cheap in absolute terms (they're short prompts).

5. **Cost tracking.** Forge logs estimated cost per meta-call to the run's ledger (approximate; based on prompt+response token counts). `forge status` shows running total.

**Answer: B.** Forge's brain always shells out to the configured backend CLI. No direct Anthropic API calls, no separate `ANTHROPIC_API_KEY` requirement, no hybrid mode in v1. One credential system (the backend CLI's), one code path, consistent behavior across all three supported backends (Claude Code, Kiro, Gemini CLI).

**Implications for subsequent questions:**
- **All brain primitives (`Classify`, `Judge`, `Distill`, `Spawn`, etc.) are implemented as backend-CLI subprocess invocations.** No SDK dependency.
- **Subagent parallelism is capped by practical subprocess-spawn overhead.** Ralph's "500 parallel searchers" pattern is not feasible via CLI; cap at ~3–8 parallel subagents (backend- and hardware-dependent).
- **Model-tier selection is not possible per brain call.** Forge uses whatever the backend CLI is configured with. Classifier calls on a Sonnet-class model are slightly wasteful but tolerable; the keyword fast path (Q6) absorbs most classifier volume before Forge even calls the brain.
- **Structured output relies on prompt engineering + parsing** (no JSON mode). Brain calls must prompt the backend to emit responses in parseable shape (e.g., "Respond with exactly one line: `path=<name>`"). Forge validates; retries on parse failure with a clarifying correction prompt.
- **Keyword fast path (Q6) is now more valuable** — it saves a full CLI subprocess spawn per forge invocation.
- **Distillation and diagnosis happen between iterations** — not during — to avoid competing with the main loop's CLI process.
- **Cost tracking is simpler:** all meta-calls bill through the backend CLI's existing account; Forge can still log estimates but doesn't manage two cost streams.
- **Brain primitive contract is single-implementation** — no interface/adapter split needed. The abstraction layer survives in case the decision gets revisited post-v1.

---

## Q13: Git workflow — when, where, and how does Forge interact with git?

Referenced in Q9 (every file-changing iteration is auto-committed → safety net for `git reset --hard`) and Q10 (push/PR/force-push/reset are mandatory human gates). But the actual workflow hasn't been specified. Several concrete decisions:

### 13.1 Branch strategy (main choice)

Where does Forge's work land?

- **A. Current branch.** Forge commits directly to whatever branch the user is on. Lowest friction. Risky if the user is on `main`/`master` or a protected branch.
- **B. Always a new `forge/<timestamp>-<slug>` branch** branched from current HEAD at plan time. Safest. One extra step for the user to merge/PR afterward.
- **C. Smart default — new branch if current is protected/main, else current branch.** Split the difference. User can override with a flag.
- **D. Ask at plan confirmation.** Explicit choice every time. More friction but no surprises.

**Recommendation: C (smart default with override).** `main`/`master`/any protected branch → auto-create `forge/<timestamp>-<slug>` branch. Any other branch (feature/topic) → commit directly. Override via `--branch <name>` or `--no-branch`. Rationale: power users on feature branches don't want extra branching ceremony; safety for users on main is critical; C gets both.

### 13.2 Uncommitted work in the user's tree at start time

- Detect dirty tree at plan phase.
- If dirty: **pause and escalate** (this is a mandatory gate — Forge never silently incorporates or discards user's uncommitted work).
- Options presented: `c` commit it (with user-provided message or Forge-drafted one), `s` stash it (restored on `forge stop`), `a` abort until resolved.

### 13.3 Commit cadence

- **Every iteration that changes files** is auto-committed at iteration boundary.
- **No commit** if diff is empty (prevents no-op commits on stuck iterations).
- **Atomic** — one commit per iteration, not per file or per test-pass.
- **Final "run complete" commit** is an empty commit (or amended last commit) with a summary of what the run did, tagged in message body.

### 13.4 Commit message format

LLM-drafted by Forge's brain (backend CLI via Q12). Template:

```
forge(<path>): <one-line summary generated from diff>

Iteration <N> of run <run-id>.
<optional 1-3 line context>

Run-Id: <run-id>
Iteration: <N>
Path: <create|add|fix|refactor>
```

- First line under 72 chars.
- Conventional-commits-friendly prefix (`forge(fix):`, `forge(create):`, etc.).
- `Run-Id` trailer lets Forge find its own commits programmatically later.

### 13.5 Tags

- **No automatic tagging by default.** Ralph's author tags releases from 0.0.0 upward on successful builds; valuable for library-style greenfield work but invasive and opinionated for most tasks.
- **Opt-in via `config.git.auto_tag true`.** When enabled, Forge tags on successful Create-path completion (not Add/Fix/Refactor).

### 13.6 Push / PR / force-push

All mandatory human gates (per Q10). Forge can *suggest* these at run completion:
- "Run complete. Push `forge/2026-04-16-dark-mode` to origin? [y/N]"
- "Open PR against `main`? [y/N]" (with Forge-drafted title/body)

User answers once at completion; nothing silent.

### 13.7 Safety-net rollback

Per Q9, "Tier 3 dead stuck → reset to last-green commit" option is available. Implementation:
- "Last-green" = last commit where `build_status=pass` and `test_status=pass|partial` in the iteration ledger.
- `git reset --hard <last-green-sha>` is **still a mandatory gate** — user must confirm the specific SHA. Forge proposes, user accepts.

### 13.8 Rebasing / upstream changes

- Forge does **not** attempt to rebase against upstream mid-run. If the branch falls behind `main` during a long loop, Forge warns at next plan/completion checkpoint but does not auto-rebase.
- If user manually pulls during a paused run, Forge re-reads HEAD at resume time and adapts.

### 13.9 Interaction with user's in-flight manual edits

- User can manually edit files on the forge branch during a pause.
- On resume, Forge detects new commits (or uncommitted changes) and either incorporates them (refreshing state.md via a distillation call) or prompts if conflicts arise.

### 13.10 Git-operations bypass

Per the rough idea: "git operations don't need Ralph loop." Forge runs git commands directly (via `os/exec`), not by prompting the backend CLI. Exception: commit-message generation, which *does* go through the brain (Q12 ⇒ backend CLI).

**Options considered for the overall model:**

- **Model X: Ralph-style (direct commits to current branch, aggressive tagging).** Too opinionated; doesn't match Forge's safer defaults.
- **Model Y: PR-workflow-first (always branch, always draft PR at end).** Too heavyweight for solo/local work.
- **Model Z: The hybrid above.** Smart branching, per-iteration auto-commit, opt-in tagging, human-gated push/PR.

**Recommendation: Model Z (the full spec above).** Defaults are safe (auto-branch on protected branches, mandatory gates on push/PR/force). Power-user behavior (feature-branch iteration, optional tagging) is easy. Matches every earlier decision (safety net commits from Q9, human gates from Q10).

**Answer: Model Z (full spec) confirmed.** Smart branching, per-iteration auto-commit, opt-in tagging, human-gated push/PR, dirty-tree escalation, LLM-drafted commit messages with `Run-Id:` trailers, safety-net rollback as Tier-3 option with mandatory SHA confirmation, no auto-rebase, post-pause resume detects manual edits via distillation.

**Implications for subsequent questions:**
- Forge needs a git helper module — branch detection, dirty-tree check, protected-branch inference, atomic commit, reset to SHA, HEAD read on resume.
- Commit-message generation is a brain call — classifier+drafter contract: input (diff + run context), output (one-line summary + optional body).
- The `Run-Id`/`Iteration`/`Path` trailer convention is a public-ish format; should be documented.
- "Last-green-sha" is computed from the iteration ledger, which now has first-class status.
- Protected-branch detection: query `git` for branch protection, optionally respect repo-level config files (`.github/branch-protection.yml`, etc.) — exact source of truth needs picking in implementation.

---

## Q14: Completion detection — how does Forge know the task is actually done?

The other side of stuck detection (Q9). Fire too eagerly → Forge quits with incomplete work. Fire too slowly → burns iterations after real completion. Per-path, because completion signals differ.

### 14.1 Options for the detection mechanism

- **A. Pure sentinel from the agent.** Rely on `LOOP_COMPLETE` in the agent's output (ralph-orchestrator's pattern). Unreliable alone — agents declare done too early (placeholder implementations, unfinished scope) or refuse to declare done (Ralph's "false completion refusal" failure mode from Q9).

- **B. Pure programmatic.** Plan items all closed + tests pass + build green. Deterministic. Brittle — sometimes the project has no test suite; sometimes plan.md isn't the source of truth at completion time; greenfield projects often have undefined "test pass" in early iterations.

- **C. Multi-signal with LLM judge.** Agent sentinel + programmatic check + LLM judge ("given task.md and current state, is the task complete?"). Weighted. Matches Forge's philosophy of multi-signal decisions (same pattern as stuck detection).

- **D. Agent + judge (skip programmatic).** Cleaner when tests/build aren't available (early greenfield), but loses deterministic backstop.

### 14.2 Per-path completion criteria

Signals combine into a completion score. ≥ threshold → declare complete.

| Path | Agent sentinel | Programmatic signals | LLM judge focus |
|---|---|---|---|
| **Create** | `TASK_COMPLETE` emitted | Build passes + tests pass (if any) + all specs.md items checked | "Does the codebase deliver the behaviors described in task.md?" |
| **Add** | `TASK_COMPLETE` emitted | Build passes + tests pass + new feature's tests pass + no regressions | "Is the new feature integrated and working? Anything missing from task.md?" |
| **Fix** | `TASK_COMPLETE` emitted | Bug's regression test passes + full suite green + bug-repro now fails to reproduce | "Does the current state resolve the bug described in task.md without side effects?" |
| **Refactor** | `TASK_COMPLETE` emitted | All tests pass (behavior preserved) + target shape achieved per target-shape.md + invariants from invariants.md hold | "Has the refactor reached its target without behavioral drift?" |
| **Review** | N/A (one-shot, not a loop) | All assigned subagents returned + report.md written | N/A — completion is compilation of subagent outputs |

### 14.3 Scoring model (for looped paths)

Each completion check (run between iterations, after the stuck-detection pass) computes:

| Signal | Weight | Value |
|---|---|---|
| Agent sentinel `TASK_COMPLETE` present | 3 | 0 or 3 |
| Build passes (when available) | 2 | 0 or 2 |
| Tests pass (full suite, when available) | 2 | 0 or 2 |
| Path-specific programmatic (see 14.2) | 2 | 0 or 2 |
| All plan.md items checked off | 2 | 0 or 2 |
| LLM judge says "complete, high confidence" | 3 | 0 or 3 |
| LLM judge says "complete, medium confidence" | 2 | 0 or 2 |
| LLM judge says "incomplete" | −4 | 0 or −4 (vetoes false positives) |

**Thresholds:**
- Score ≥ 8 **and** judge ≥ medium confidence → declare complete, stop loop, finalize.
- Score 5–7 → one more iteration with prompt: "you seem nearly done; audit for placeholders and unfinished edges."
- Score < 5 → continue normally.
- Agent sentinel present + judge says "incomplete" → log the discrepancy to ledger, continue (prevents premature completion).

### 14.4 Special-case rules

- **"No test suite" projects.** If Forge can't discover test/build commands after 2 iterations of searching (README, `notes.md`, common conventions), it drops those signals from the score and reweights the judge's weight higher. Log notice to ledger.
- **Placeholder scan.** Before declaring complete, Forge runs a diff-vs-task grep for placeholders (`TODO`, `FIXME`, `unimplemented!`, `pass # XXX`, empty function bodies, etc.). Hits → reduce score by 4 and push an iteration with "resolve these placeholders" as the next prompt. This matches Ralph's explicit "DO NOT IMPLEMENT PLACEHOLDERS" discipline.
- **Review path** doesn't use scoring. Done = subagents all returned + report.md compiled. If any subagent failed, Forge retries that one; if retry fails, it includes a "[Subagent failed: <area>]" section in the report and returns.

### 14.5 Completion ceremony (what happens when score threshold hit)

1. Run a final full test/build pass.
2. Forge-commit any last-iteration changes.
3. Generate a run-summary commit (empty or amended last commit) summarizing the work done across all iterations.
4. Suggest next actions: push branch (if branched) / open PR / tag release (if `auto_tag`).
5. Update `.forge/runs/<run>/DONE` flag file with summary + stats.
6. Deactivate `current` symlink.

### 14.6 Early-exit path (user aborts)

If user aborts mid-run (Ctrl-C → abort option), completion ceremony is **not** triggered. Instead: work-in-progress state is frozen, a `PAUSED` (not `DONE`) marker is written, and the symlink is kept so `forge resume` can pick up.

**Options considered at the mechanism level:**

- **A alone** — sentinel only. Rejected: unreliable.
- **B alone** — programmatic only. Rejected: brittle in greenfield and projects without strong test harnesses.
- **C (multi-signal)** — robust. Accepted.
- **D (agent + judge only)** — cleaner for early greenfield but loses the programmatic backstop that catches "agent says done, tests fail."

**Recommendation: C with the per-path criteria and scoring above.** Same shape as stuck detection (multi-signal, threshold, graduated response) which gives the codebase a consistent pattern. The placeholder scan is non-negotiable — it's the single biggest failure mode in agent-driven code.

**Answer: C with all per-path criteria and scoring confirmed.** Placeholder scan and judge veto accepted. Completion ceremony, early-exit behavior, and special cases (no-test-suite, Review path) all accepted as proposed.

**Implications for subsequent questions:**
- Forge needs a "placeholder scanner" primitive — regex+AST-light heuristics across common languages, plus a fallback brain call for exotic cases. Cost-cheap by design.
- Build/test-command discovery is a separate Forge primitive: reads README, `package.json`, `Cargo.toml`, `Makefile`, etc., and tries common conventions. Persists learned commands to `notes.md`.
- Per-path completion hooks (e.g., Fix-path's "bug-repro no longer reproduces" check) require structured info from the path-specific artifact. Means `bug.md` needs a repro-script section that Forge can replay.
- The LLM judge has a defined contract: input (`task.md`, current state snapshot, diff since start) → output (complete/incomplete + confidence + rationale).
- `DONE` / `PAUSED` marker files are part of the run directory convention; should be documented.

---

## Q15: State persistence, resume behavior, and concurrent runs

Q7 established the artifact layout (`.forge/runs/<ts>-<slug>/` + `current` symlink). Q9 referenced the iteration ledger. Q11 referenced pause/resume. Q13 referenced resume-with-manual-edits. None of these covered the full lifecycle, concurrency, or resume mechanics. This question ties them together.

### 15.1 Scope of active state

- `.forge/` is **per-repo** (lives inside the working tree, or in its root — see 15.3).
- Global user config (default backend, OS-notification prefs) lives at `~/.config/forge/config.yml`.
- No daemon / no long-running process. Forge is invocation-based. State lives in files; resume re-reads files.

### 15.2 `.forge/` location

Two candidates:
- **At repo root** (`<repo>/.forge/`) — tracked or git-ignored per user preference. Easy to inspect, natural for per-repo runs.
- **At a user-global home cache** (`~/.cache/forge/<repo-hash>/`) — keeps repo clean, harder to inspect.

**Proposal: `.forge/` at repo root, added to `.gitignore` by default on first run** (Forge writes `.forge/` entry if missing). User can un-ignore if they want to track (e.g., for team handoff).

### 15.3 Concurrency model

Three options:

- **A. One active run per repo, serial.** `forge "..."` while another run is active → refuses (with "forge status" hint) or offers to abort-previous-and-start-new. Simplest. Matches "minimal human burden" — no juggling.
- **B. Multiple concurrent runs per repo, isolated by branch.** Each run owns its own `forge/<ts>-<slug>` branch; multiple can progress in parallel. Matches GSD's workstreams. Useful for "implement A and B concurrently." Adds meaningful complexity (which run is "current"? which `.forge/current/`? scheduler? git worktrees?).
- **C. One active run per *branch* in a repo.** Enables parallelism without the full complexity of B. Still requires thinking about which run is active "here and now."

**Recommendation: A for v1.** Concurrency of multiple agent-driven loops in the same repo is a significant complexity multiplier (conflicting writes, stuck-detection cross-talk, escalation queueing, subprocess resource contention). Ship A in v1; consider B post-v1 if real demand materializes.

### 15.4 Run lifecycle states

A run directory always has a state marker file. States:

| State | Marker | What it means |
|---|---|---|
| `RUNNING` | `RUNNING` file + PID | Forge is actively looping (or doing plan phase) |
| `AWAITING_HUMAN` | `AWAITING_HUMAN` file + `awaiting-human.md` | Paused for human input |
| `PAUSED` | `PAUSED` file | User ran `forge stop` or Ctrl-C'd — can resume |
| `DONE` | `DONE` file + summary | Completed successfully |
| `ABORTED` | `ABORTED` file + reason | User aborted, no resume intent |
| `FAILED` | `FAILED` file + diagnostic | Unrecoverable error (not Tier 3 — a real crash) |

Only states `RUNNING` and `AWAITING_HUMAN` are "active." `current` symlink points to the active run if any; cleared on `DONE`/`ABORTED`/`FAILED`.

### 15.5 `forge resume` behavior

```
forge resume           # resume whatever .forge/current points to
forge resume <run-id>  # resume a specific past run (must be in PAUSED or AWAITING_HUMAN)
```

Behavior:
1. Validate run's state marker is resumable (`PAUSED` / `AWAITING_HUMAN`).
2. Re-read git HEAD — if moved since pause, note it in state.md.
3. Re-detect dirty tree — if user edited files manually, run distillation subagent to incorporate changes into state.md (Q13's manual-edits behavior).
4. For `AWAITING_HUMAN`: check if `awaiting-human.md` has an answer (either key in frontmatter or answer line); if yes, apply and continue; if no, re-prompt.
5. Resume loop at next iteration.

### 15.6 `forge status`

```
forge status                 # terse summary of current run (if any)
forge status --verbose       # full diagnostic: iteration count, cost, latest ledger, open questions
forge status --run <run-id>  # status of any run by id
```

Shows: run id, path, iteration count, state marker, elapsed time, estimated cost, top-3 plan items, latest stuck-score (if running).

### 15.7 `forge history`

```
forge history          # list past runs with summaries (last N by default)
forge history --full   # all runs
forge show <run-id>    # dump the run's key artifacts
```

### 15.8 Crash recovery

If Forge's process dies mid-iteration (OS kill, segfault, machine reboot):
- `RUNNING` marker remains on disk but PID is stale.
- On next `forge` invocation, Forge detects stale `RUNNING` (PID absent), demotes to `PAUSED`, logs the event, and prompts whether to resume.

### 15.9 Run retention

- Recent N=50 runs kept under `.forge/runs/` by default.
- `forge clean` deletes `DONE`/`ABORTED`/`FAILED` runs older than a threshold (configurable).
- Never auto-deletes active runs.

### 15.10 Multi-repo

Out of scope for v1. Users running Forge in two different repos at once is fine (each has its own `.forge/`); Forge doesn't coordinate across repos. Simpler mental model.

**Options recap:**

- **A (one active run per repo, serial) + the lifecycle/resume/history model above.** Recommended.
- **B (concurrent runs).** Deferred post-v1.
- **C (per-branch concurrency).** Deferred post-v1.

**Recommendation: A, with the specifics in 15.1–15.10.** This gets us a working runtime with resume, history, crash recovery, and clear state transitions — without the concurrency complexity we don't yet need.

**Answer: A confirmed** with all specifics in 15.1–15.10. One active run per repo, `.forge/` at repo root (auto-added to `.gitignore`), six lifecycle states, `current` symlink, resume/status/history/show/clean commands, crash recovery via PID check, 50-run retention default, no multi-repo orchestration in v1.

**Implications for subsequent questions:**
- Forge's CLI surface now includes: `forge "<task>"`, `forge plan "<task>"`, `forge status`, `forge stop`, `forge resume`, `forge history`, `forge show <run-id>`, `forge clean`, `forge backend`, `forge doctor`, `forge --help`, `forge --version` — still ~11 commands, within "limited CLI" constraint.
- PID-based process tracking is needed for crash recovery.
- `current` symlink behavior is platform-sensitive (Windows doesn't have POSIX symlinks) — use a plain file with the run-id on Windows; same contract either way.
- `.gitignore` auto-addition should be idempotent and announce itself once.

---

## Q16: Backend driver protocol — how does Forge actually talk to Claude Code / Kiro / Gemini CLI?

Q12 answered *where* Forge's brain runs (backend CLI, not direct API). This question is *how* — at the protocol level — Forge invokes the backend CLI for iteration loops, subagents, and meta-calls. Several architectural decisions follow.

### 16.1 Iteration invocation model

- **Session-based** (persistent CLI process, send messages over time). Efficient but contradicts Q8's per-iteration-reset commitment.
- **One-shot per iteration** (spawn fresh CLI process each iteration, pipe prompt.md, capture output, process exits). Matches Q8. Clear lifecycle. Higher per-iteration startup cost but predictable.

**Recommendation: one-shot per iteration.** Aligns with Q8's Ralph-style reset discipline.

### 16.2 Output protocol

- **A. Text-only.** Pipe prompt to stdin, capture stdout as text. Simplest, most portable across backends. Post-iteration, Forge infers changes from `git diff` and parses free-form text for sentinels.
- **B. Structured streaming output.** Use backend's JSON event stream when available (Claude Code: `--output-format stream-json`; similar modes on Gemini CLI). Forge receives tool-use events, text deltas, token counts, errors in real time.
- **C. Hybrid — structured when available, text fallback.** Best of both; one code path per mode. Per-backend capability matrix maintained by Forge.

**Recommendation: C.** Structured output powers:
- Live progress in `forge status` ("agent is editing `src/auth.go`…").
- Accurate token/cost tracking without estimation.
- Tool-use-event-based signals for the progress ledger (Q9 signals become richer).
- Early detection of loop pathology (agent is re-reading the same file 20x → stuck signal).

Text-only fallback keeps the door open for backends without structured output or for debugging.

### 16.3 Subagent dispatch mechanism

From Q8, Forge spawns subagents for: codebase search, independent subtasks, expensive reasoning, plan-phase research. Three ways to dispatch:

- **A. Multiple backend-CLI spawns** (N concurrent processes, each with its own prompt). Backend-agnostic. Cost: subprocess startup + some OS resource contention. Parallelism cap ~3–8.
- **B. Backend's native Task/subagent tool** (e.g., Claude Code's Task tool). Efficient — one CLI process dispatches internally. Backend-specific: Gemini/Kiro may not have equivalents.
- **C. Hybrid.** Use native when available; fall back to multi-spawn. Two code paths.

**Recommendation: A (multi-spawn).** Rationale:
- **Backend-agnostic.** One mechanism works for Claude Code, Kiro, Gemini CLI identically. No per-backend subagent code.
- **Forge controls each subagent's context.** With native Task tools, Forge hands control to the main agent to decide subagent prompts — reduces Forge's visibility and control.
- **Parallelism cap of 3–8 is enough for v1.** Plan-phase research (3–4 researchers), Review-path fan-out (4–6 reviewers), distillation (sequential, not parallel). Ralph's "up to 500" pattern is aspirational; practical AI-dev tool usage is well under that.
- **Backend's Task tool is still available to the main agent**, just not to Forge itself. If the main agent wants to spawn a subagent mid-iteration via Claude Code's Task tool, that's fine — it's happening inside the iteration. Forge doesn't need to drive it.

### 16.4 Permission / non-interactive mode per backend

Forge runs loops autonomously; we don't want the backend CLI to prompt for file-write approval every iteration. Each backend has a non-interactive / skip-approval mode:
- **Claude Code:** `--dangerously-skip-permissions` or equivalent.
- **Kiro:** equivalent flag.
- **Gemini CLI:** equivalent flag (plus possible sandbox mode).

**Rule:** Forge runs the backend in its most-permissive non-interactive mode **and enforces its own Q10 policy gates at the orchestration layer.** The backend's permission system is bypassed because Forge has a stricter policy system already.

This is the same pattern GSD recommends: skip backend permissions, rely on the orchestrator's rules.

### 16.5 Per-iteration timeout

- Default per-iteration timeout: 30 minutes (configurable).
- On timeout: Forge SIGTERMs the backend CLI, allows 30s graceful shutdown, then SIGKILLs. Marks iteration as `FAILED_TIMEOUT` in ledger. Counts toward stuck score.

### 16.6 Capability matrix per backend

Forge maintains a built-in table:

| Backend | Streaming JSON | Native subagents | Skip-permissions flag | Effective context window |
|---|---|---|---|---|
| Claude Code | ✅ `stream-json` | ✅ Task tool | `--dangerously-skip-permissions` | ~150k (Sonnet), backend-configured |
| Kiro | (TBD — verify during implementation) | (TBD) | (TBD) | (TBD) |
| Gemini CLI | ✅ (verify mode name) | ⚠️ limited | (verify) | ~1M (Gemini 2.x), backend-configured |

Forge falls back to text-only protocol on any backend where structured output isn't working, logging the degradation.

### 16.7 Backend abstraction layer

Forge defines a single internal interface (illustrative Go signature — not a design commitment to this exact shape):

```go
type Backend interface {
    Name() string
    Capabilities() Capabilities
    RunIteration(ctx context.Context, promptPath string, opts IterationOpts) (IterationResult, error)
    // RunIteration handles: spawn + pipe prompt + stream events + wait + kill-on-timeout
}
```

Three implementations (Claude Code, Kiro, Gemini CLI). Each handles its own invocation, flags, and output parsing. The rest of Forge doesn't care which backend is active.

### 16.8 Installed-backend detection

- At startup, Forge checks `$PATH` for each supported binary (`claude`, `gemini`, `kiro`).
- If none found → `forge doctor` explains install options.
- If one found → default backend is it.
- If multiple found → use `config.backend.default` if set, else prompt user on first run.

### 16.9 Version pinning

- Forge maintains a "known-good version range" per backend. On backend invocation, Forge queries `claude --version` etc. and warns if version is outside the tested range (doesn't block — just warns in the run log).

**Options recap:**

- **Main-loop invocation:** one-shot per iteration.
- **Output protocol:** C (hybrid — structured when available, text fallback).
- **Subagents:** A (multi-spawn, backend-agnostic).
- **Permissions:** skip at backend; enforce at Forge.

**Recommendation: the full spec above.**

**Answer: full spec confirmed.** One-shot per iteration; hybrid output protocol (structured when available, text fallback); multi-spawn subagents; skip-permissions at backend with Forge policy enforcement at orchestration layer; 30-minute iteration timeout with graceful→forceful shutdown; built-in capability matrix with degraded-fallback logging; single `Backend` interface with three adapters; startup install detection with `forge doctor` fallback; version warnings.

**Implications for subsequent questions:**
- Kiro and Gemini CLI capability entries in the matrix are TBD — implementation phase must verify during adapter construction.
- Forge needs a stream parser per backend (for structured output mode).
- The distinction between "Forge's subagents" and "main agent's subagents" is now a documented boundary that prompt templates must respect.
- `forge doctor` scope expands — install detection, version checks, per-backend capability probes, missing-tool advice.
- The capability matrix becomes part of the internal design doc.

---

## Q17: First-run onboarding, config system, and `forge doctor`

This is where the "minimal human burden" philosophy gets tested hardest — the first time a user runs Forge on a machine. Three coupled concerns.

### 17.1 First-run flow

When `forge "<task>"` or `forge` is invoked on a machine with no prior Forge state (`~/.config/forge/config.yml` missing):

**Option A: Just-in-time setup.** Ask only what's needed right now. Detect backends on `$PATH`. If exactly one found → use it. If none → explain install options, exit. If multiple → prompt once for default. Save config, continue.

**Option B: Upfront wizard.** Walk user through backend selection, default preferences, OS-notification opt-in, etc. Heavier but more thorough.

**Option C: Zero-touch + lazy prompts.** Create config with sensible defaults; ask only when Forge hits an actual decision it can't default.

**Recommendation: A (just-in-time).** Matches "AI decides if possible, ask only if needed." Wizard-style setup (B) is the opposite of minimal burden. C's "silent defaults" is tempting but risks choosing a backend the user didn't expect.

Specifically:
1. Detect installed backends via `$PATH` (`claude`, `gemini`, `kiro`).
2. If zero installed → print install pointers for each + exit with a friendly message ("Install one of these to get started: …").
3. If one installed → use it silently; write to config; proceed.
4. If multiple installed → prompt once: "Which backend should Forge use by default?" with keystroke selection; save; proceed.
5. For the actual task, proceed to plan phase normally. No extra wizarding.

Everything else (OS notifications, auto-tag, timeouts, etc.) uses built-in defaults. Users override with `forge config set <key> <value>` as needed.

### 17.2 Config system

**Locations:**
- **Global:** `~/.config/forge/config.yml` — user-wide defaults (backend preference, OS notifications, default timeouts).
- **Per-repo:** `.forge/config.yml` — repo-specific overrides (e.g., `git.branching = always-new`, `context.budget = 100000`).

**Precedence:** CLI flags > per-repo config > global config > built-in defaults.

**Format:** YAML (readable, Forge can read/write via standard Go YAML library).

**Access:**
```
forge config                         # print merged effective config
forge config get <key>               # single value
forge config set <key> <value>       # write to per-repo by default, --global to write global
forge config unset <key>             # remove override
forge config edit                    # open in $EDITOR
```

**Config schema (v1, illustrative):**
```yaml
backend:
  default: claude         # claude | gemini | kiro | (interactive prompt if unset)

brain:
  mode: cli               # cli-only in v1 (Q12 = B)

context:
  budget: null            # auto-detected from backend; override with integer
  verbose: false

git:
  branching: smart        # smart | always-new | always-current
  auto_tag: false

iteration:
  timeout_sec: 1800
  max_iterations: 100
  max_cost_usd: 10.00
  max_duration_sec: 14400

notifications:
  terminal_bell: true
  os_notify: true

retention:
  max_runs: 50

paths:
  refactor_gate: true     # per-path invariant-confirmation gate (Q10)
```

Unknown keys → Forge warns but proceeds (forward compatibility).

### 17.3 `forge doctor`

Diagnostic command. Runs on-demand; user invokes when something feels wrong.

**Checks:**
1. **Config validity.** Parse global and per-repo configs. Flag unknown keys, type mismatches.
2. **Backend installation.** For each supported backend, check `$PATH`, run `--version`, check version against known-good range.
3. **Backend capability probe.** Spawn a tiny no-op invocation per installed backend. Verify: process exits cleanly, can accept stdin, structured output works (if expected).
4. **Git available.** `git --version` works; current directory is a git repo (or warn).
5. **Protected-branch detection.** Verify git config / GitHub config reads work.
6. **Write access.** Ensure `.forge/` is writable at repo root and `~/.config/forge/` at home.
7. **OS notification.** Send a test notification (with consent: "send test notification? [y/N]").
8. **Run directory integrity.** Scan `.forge/runs/`; flag runs with stale `RUNNING` markers, missing state markers, orphaned symlinks.
9. **Disk space.** Warn if `.forge/` is huge (retention policy failing?).

**Output:**
- Terse summary: `OK` / `WARN` / `FAIL` per check.
- Verbose mode (`--verbose`) shows full output per check.
- Exit 0 on all-OK-or-warn; nonzero if any FAIL.

Doctor is **read-only + user-consent**. It never auto-fixes. Suggestions printed for each issue (e.g., "install Claude Code: https://...").

### 17.4 Onboarding UX polish

- **First-run banner.** One-time terse greeting on first `forge` invocation, explaining the command surface (6–11 commands listed in Q15). Stored flag suppresses future banner.
- **Inline help.** `forge --help` gives the command list. `forge <command> --help` gives per-command help.
- **`forge doctor` suggestion** on any FAILED-state run recovery: "Run `forge doctor` for diagnostics."

### 17.5 Options recap

- **First-run:** A (just-in-time) recommended.
- **Config:** YAML at global + per-repo with documented precedence.
- **Doctor:** read-only diagnostic, suggest-only, never auto-fix.

**Recommendation: the full spec above.** Matches "minimal human burden" (no wizard) and "AI decides if possible, ask only if needed" (single prompt only if truly ambiguous — multiple backends installed).

**Answer: full spec confirmed with one amendment — backend selection is always manual.**

**Amended first-run flow (17.1):**

1. Detect installed backends on `$PATH` (for validation and to populate the prompt options).
2. If **zero** installed → print install pointers for each supported backend + exit with a friendly message.
3. If **one or more** installed → **always prompt** the user to pick the backend, even if only one is installed. Forge does not silently select. User's selection is saved to global config.
4. For the actual task, proceed to plan phase normally.

**Rationale for amendment:** the user explicitly asked for manual backend selection. Backend choice is a legitimate human decision (as originally stated in Q1 — "different users have access to different backends, so backend choice should not be hidden by automation"). Silent selection on single-install violates that principle. One-time prompt at first run is the right cost.

**Subsequent runs:** use the saved `config.backend.default` value. No re-prompt. Users change the backend later via `forge backend set <name>` or `forge config set backend.default <name>`.

**Everything else in Q17 stays as proposed:**
- Config at global (`~/.config/forge/config.yml`) + per-repo (`.forge/config.yml`); CLI > per-repo > global > built-in precedence.
- `forge config` / `get` / `set` / `unset` / `edit` subcommands.
- YAML schema as proposed.
- `forge doctor` as read-only, suggest-only, user-consent diagnostics (no auto-fix).
- First-run banner, inline `--help`, doctor suggestion on recovery.

**Implications for subsequent questions:**
- The onboarding prompt is the second mandatory human interaction (after initial task submission). Not invasive — one time only.
- `forge backend set <name>` command is now part of the CLI surface.
- `backend.default: null` (unset) in config is the trigger for first-run prompt; any other value skips it.

---

## Q18: Observability — what does the user see in the terminal during a running loop?

Forge loops can run for minutes to hours. What's on the user's terminal during that time matters for trust and debugging.

**Options:**

- **A. Silent.** Loop runs without output; user checks `forge status` in another terminal. Minimalist. Unnerving — looks frozen.
- **B. Full streaming backend output.** Every token the agent emits flows through the terminal, plus Forge's own lines interspersed. Maximum signal, maximum noise. Hard to scan, exhausting to read.
- **C. One terse summary line per iteration.** Forge prints a single line when each iteration starts/ends: `[14:32:10] iter 7 · fix · files=3 · cost=$0.42 · state=progressing`. Quiet enough to ignore, informative enough to trust. User can tail the per-iteration transcript in another terminal.
- **D. Live status line (in-place updating).** One line that rewrites itself as state changes: spinner + current file being edited + iteration + cost. Polished. Requires ANSI terminal and complicates logging.
- **E. Verbose mode + quiet mode as flags.** Ship C as default; `--verbose` gets B's streaming; `--quiet` gets A; optional `--tui` or post-v1 for D.

**Proposed default (C) with flags (E):**

- **Default terminal output** (`forge "<task>"`):
  - Plan-phase: live dots/spinner + milestones ("researching codebase…", "drafting plan…", "ready").
  - Confirmation: printed plan summary + keystroke prompt.
  - Per-iteration: one summary line at iteration end. Format:
    ```
    [HH:MM:SS] iter N · <path> · <score-delta or state> · files=<n> · cost=$<x.xx>
    ```
  - On escalation: full escalation prompt (from Q11) interrupts the terminal.
  - On completion: summary block with final stats.

- **`--verbose`**: stream every backend token + Forge internal events. For debugging or the user who wants to watch.
- **`--quiet`**: suppress per-iteration lines; print only plan-confirm, escalation prompts, and completion.
- **`--json`**: emit newline-delimited JSON events (for scripting/piping).

- **Per-iteration transcript on disk.** Regardless of terminal verbosity, every iteration's full backend output is written to `.forge/runs/<run>/iterations/<N>.log` for post-hoc inspection and debugging. `forge show <run-id> --iter <N>` retrieves it.

- **Live status line (D) deferred to post-v1.** Adds a real TUI dependency (bubbletea) and complicates log piping; not core for v1.

**Recommendation: E — ship C as default, B via `--verbose`, A via `--quiet`, machine-readable via `--json`.** Full iteration transcripts always persist on disk regardless of verbosity. Live status line is post-v1 polish.

**Answer: E confirmed.** C is default; `--verbose` / `--quiet` / `--json` flags override; full per-iteration transcripts always persist to `.forge/runs/<run>/iterations/<N>.log`. Live TUI status deferred post-v1.

**Implications for subsequent questions:**
- Forge logger has three verbosity levels (quiet/default/verbose) and a JSON mode — standard structured logger pattern.
- Spinner implementation is a minimal dependency (no full TUI); cross-platform.
- Transcript logs are per-iteration files (not one giant run log) for efficient inspection and `forge show --iter N`.

---

## Q19: Distribution channels and MVP scope

Wrapping up with two tightly-related questions.

### 19.1 Distribution channels for v1

Forge is a Go single-binary. Channels to ship through:

| Channel | Cost to set up | Audience | Recommendation for v1 |
|---|---|---|---|
| **GitHub Releases binaries** (Linux/macOS/Windows — amd64, arm64) | Near-zero (CI-generated on tag) | Everyone; always the source of truth | ✅ **Required** |
| **`curl \| sh` installer script** (wraps GH Releases download) | Low (one shell script + URL) | Quick-start users; docs copy-paste | ✅ **Required** |
| **Homebrew tap** (`brew install <tap>/forge`) | Low (Formula file in a separate repo) | macOS + Linuxbrew users | ✅ **Recommended** |
| **`go install github.com/<...>/forge@latest`** | Trivial (just publish the module) | Go devs | ✅ **Free for us** |
| **apt / dnf repos** | Medium (signing, hosting, maintenance) | Linux distribution users | ❌ Defer post-v1 |
| **Docker image** (`docker run <...>/forge`) | Medium (Dockerfile + multi-arch build) | CI pipelines, containerized workflows | ❌ Defer post-v1 |
| **Windows MSI / macOS .app** | High (code signing, certs) | Non-CLI-native users | ❌ Defer post-v1 |
| **WinGet / Scoop / Chocolatey** | Low–Medium | Windows users | ❌ Defer post-v1 |

**Recommendation:** Ship with **GitHub Releases + curl-installer + Homebrew tap + `go install`** in v1. Covers macOS and Linux well out of the gate; Windows users can use GH Releases or `go install`. The rest is post-v1 based on demand.

### 19.2 MVP scope — what ships in v1

Consolidating every decision so far. Features marked ✅ are v1; ⏳ are explicitly deferred.

**Core (v1):**
- ✅ Primary command `forge "<task>"` with inline plan confirmation
- ✅ Optional dry-run `forge plan "<task>"`
- ✅ Intent-based routing from opening verb (5 paths: Create / Add / Fix / Refactor / Review)
- ✅ Keyword-first + LLM-classifier + human-escalation 4-step decision ladder
- ✅ 3 backend adapters: Claude Code, Kiro, Gemini CLI
- ✅ `Backend` interface + one-shot per iteration + hybrid (structured/text) output protocol
- ✅ Multi-spawn subagent pool (cap 3–8 parallel)
- ✅ Path-specific plan artifacts + shared base files (task/plan/state/notes/prompt)
- ✅ Ralph-style per-iteration context reset with automatic distillation (8k / 10k / 6k thresholds for state/notes/plan)
- ✅ Progress ledger + 4-tier stuck detection + graduated response (soft / hard / dead)
- ✅ Multi-signal completion detection with path-specific scoring + placeholder scan + LLM judge veto
- ✅ Mandatory human-intervention gates (push/PR, force-push, reset --hard, secret-in-diff, branch protection, external-facing actions, dependency changes, etc.)
- ✅ Tier-based escalation for decisions Forge's brain can't resolve
- ✅ Refactor-path invariant-confirmation gate (only per-path gate in v1)
- ✅ Terminal + OS-native notification (no webhook/Telegram/bot)
- ✅ File-based `awaiting-human.md` source of truth with fsnotify auto-detect on edits
- ✅ `--yes` / `--auto-resolve {accept-recommended|abort|never}` / `--timeout` non-interactive flags
- ✅ Smart git branching (auto-new on protected branches, else current) with `--branch`/`--no-branch` overrides
- ✅ Per-iteration auto-commit with LLM-drafted messages + Run-Id trailer
- ✅ Optional `config.git.auto_tag` (Create path only)
- ✅ Safety-net `git reset` as Tier-3 option with mandatory SHA confirmation
- ✅ Dirty-tree escalation before starting any run
- ✅ Manual-edit detection on resume (distillation refresh)
- ✅ One active run per repo, serial; lifecycle states (RUNNING/AWAITING_HUMAN/PAUSED/DONE/ABORTED/FAILED)
- ✅ `.forge/` at repo root, auto-`.gitignore`'d; global config at `~/.config/forge/`
- ✅ `forge resume`, `forge status`, `forge stop`, `forge history`, `forge show`, `forge clean`
- ✅ Crash recovery (stale RUNNING → PAUSED)
- ✅ 50-run retention default
- ✅ Terminal output: C (one line per iteration) + `--verbose` / `--quiet` / `--json` flags
- ✅ Always-on per-iteration transcript logs
- ✅ Just-in-time first-run flow with **always-manual backend selection prompt**
- ✅ `forge backend set <name>`, `forge config get/set/unset/edit`
- ✅ `forge doctor` (read-only, suggest-only diagnostics)
- ✅ Distribution: GitHub Releases + curl-installer + Homebrew tap + `go install`
- ✅ Brain via backend CLI only (no direct Anthropic API)

**Explicitly deferred (post-v1):**
- ⏳ Additional backends: Codex, Amp, Copilot CLI, OpenCode
- ⏳ Direct Anthropic API mode (hybrid brain)
- ⏳ Additional paths: Document, Test, Upgrade/Migrate, Explain, Research
- ⏳ Webhook / hook-script / Telegram / Slack / email integrations
- ⏳ First-party Telegram bot (ralph-orchestrator's RObot)
- ⏳ Web UI console
- ⏳ Live TUI status line
- ⏳ Multi-run concurrency (single or per-branch)
- ⏳ Multi-repo orchestration
- ⏳ Composite paths (Review-then-Fix)
- ⏳ MCP server mode (ralph-orchestrator has this)
- ⏳ Docker image, apt/dnf repos, MSI, WinGet, Scoop
- ⏳ Streaming/incremental plan UX (option D in Q7)

### 19.3 What "done" looks like for v1

A successful v1 ships when:
1. A user can `brew install` or `curl | sh` Forge, run `forge "<any-task>"`, and get through the 5 paths' happy paths without reading more docs than `forge --help`.
2. Forge recovers autonomously from ≥80% of soft-stuck cases (Tier 1/2) without human input, based on the failure-mode taxonomy in Q9.
3. Mandatory gates from Q10 are enforced — no test passes without exercising each gate type.
4. Cost tracking (estimated via ledger) is within ±20% of actual on a validation set of tasks.
5. Crash recovery works — OS kill mid-iteration is resumable.

**Recommendation: adopt this scope for v1.** Trim or add as you wish.

**Answer: scope confirmed as proposed.** No additions, trims, or reassignments between v1 and deferred. Distribution channels: GitHub Releases + curl-installer + Homebrew tap + `go install`. "Done" criteria accepted.

**Implications for subsequent steps:**
- The implementation plan (Step 7 of this SOP) is bounded by the v1 scope above; deferred items do not appear in v1's numbered implementation steps.
- CI/release pipeline must produce multi-arch binaries at tag time.
- Homebrew tap lives in a separate repo (convention); implementation plan must include creating it.
- Success metrics (the 5 "done" criteria) become acceptance tests in the implementation plan.

---


















