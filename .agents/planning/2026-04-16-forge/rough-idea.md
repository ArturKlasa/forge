# Rough Idea: Forge

**Date captured:** 2026-04-16

## Source Inspirations (to study thoroughly)

- Ralph loops — https://ghuntley.com/ralph/
- ralph-orchestrator — https://github.com/mikeyobrien/ralph-orchestrator and https://mikeyobrien.github.io/ralph-orchestrator/
- get-shit-done (GSD) — https://github.com/gsd-build/get-shit-done

## Concept

Build a tool — **Forge** — similar in spirit to `ralph-orchestrator` and `get-shit-done`, using Ralph loops as the core execution primitive, but substantially more automated and less human-burdened than either.

## Goals / Differentiators vs. Existing Solutions

1. **Maximum automation, minimum human input.** Today, `ralph-orchestrator` requires the operator to manage `ralph.yml`, pick a backend, pick a hat/preset, choose a prompt, etc. GSD exposes 40+ slash commands. Forge should make these decisions for the human wherever it can.

2. **AI decides the setup.** Forge should automatically determine the appropriate Ralph-loop setup for a given task — backend choice, prompt structure, iteration limits, checkpoints, etc. The human shouldn't need to hand-write a `ralph.yml` to get started.

3. **Research-first decision making.** When Forge encounters a decision point, it must *first* conduct research (codebase, web, docs, existing state) and try to make a good decision autonomously from that research. Only when research is insufficient should it escalate.

4. **Explicit escalation policy.** We need to specify *which* situations always require human intervention vs. which can be decided autonomously. When escalating, Forge should present the question with options and a recommended option where possible.

5. **Default human-guidance situations.** Some situations require human input by default, even if Forge could guess (e.g., the initial idea; irreversible/destructive actions; stakeholder-visible changes). These must be enumerated.

6. **Limited CLI surface.** Few commands. Easier cognitive load. (Contrast with GSD's ~40 commands.)

7. **Selective use of Ralph loops.** Ralph loops are the default execution mode wherever applicable, but some tasks (e.g., routine git operations) are better as one-shot actions. Forge decides per task.

8. **Automated context management.** Forge must manage the AI context window automatically — pruning, summarizing, externalizing state to markdown, spawning subagents with fresh context when needed. This is inspired by GSD's "context rot" solution (externalize to markdown) combined with Ralph's subagent pattern.

9. **Stuck-loop prevention.** Forge must detect when a Ralph loop is stalling (making no progress, thrashing, repeating mistakes, drifting off-task) and intervene — reset context, regenerate plan, escalate to human, or abort.

## Hard Constraints

- Name: **Forge**.
- Initial task input is human-submitted (by definition — Forge can't read minds).
- Ralph loops are the execution primitive wherever they apply.
- Git operations should generally not require a full Ralph loop.

## Open Questions (to be honed in idea-honing)

- Language / runtime (Rust like ralph-orchestrator? Node like GSD? Python?)
- Which LLM backend(s) to support (Claude-only initially, or multi-backend from day one?)
- Distribution (npm? cargo? curl-installer? Homebrew?)
- Scope of "automated setup" — does Forge generate the `ralph.yml` equivalent and then hand off, or is it a full replacement for ralph-orchestrator?
- Human-escalation UX (terminal prompt? Telegram like ralph-orchestrator's RObot? Slack?)
- State/context storage location and format
- Concurrency model (single loop vs. waves like GSD)
- Planning methodology (PDD? GSD-style discuss→plan→execute→verify→ship? Something new?)
