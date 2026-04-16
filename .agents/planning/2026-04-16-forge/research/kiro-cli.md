# Research: Kiro CLI capabilities

**Date:** 2026-04-16
**Verified against:** Kiro CLI docs at `kiro.dev/docs/cli/*` + CLI 2.0 (April 2026) release notes
**Purpose:** Ground Forge's Kiro backend adapter in verified capabilities and flags.

## Key implications for Forge

- **Verdict up front:** Kiro has a scriptable standalone CLI (`kiro-cli`). Headless mode was added in CLI 2.0 (April 2026), making Kiro a viable Forge backend. The binary is the rebranded/evolved Amazon Q Developer CLI.
- **Two invocation modes to choose between:**
  - `kiro-cli chat --no-interactive --trust-all-tools "<prompt>"` — **text output only** with a `▸ Credits: X • Time: Ys` completion marker at the end.
  - `kiro-cli acp` — **JSON-RPC 2.0 over stdio** (recommended for Forge — structured events).
- **Recommendation: Forge should default to `kiro-cli acp` mode for richer stream events.** Text mode (`chat --no-interactive`) is the fallback.
- **Authentication:** `KIRO_API_KEY` env var is the headless-compatible auth method. **Requires Kiro Pro/Pro+/Power subscription** — Forge `doctor` should detect and advise.
- **Prompt is a positional argument in text mode** (not stdin — unlike Claude Code / Gemini CLI). Forge's adapter must pass prompts as args, escaping appropriately.
- **Subagents/delegation:** `/spawn` slash command in interactive mode creates parallel sessions. Not directly applicable to headless — Forge should spawn multiple `kiro-cli` subprocesses for its own subagent pool (matches Q16's multi-spawn decision).
- **Context windows are large on recent Claude models:** `claude-opus-4.6` and `claude-sonnet-4.6` have **1M** windows; older models 200k. Credit multipliers: Opus 2.2x, Sonnet 1.3x, Haiku 0.4x.
- **Completion signal in text mode is fragile:** the `▸ Credits:` footer is a UX string, not an API. Prefer ACP mode whenever possible.

---

# Kiro CLI — Forge Backend Research

**Verdict up front:** Kiro **does** ship a scriptable standalone CLI (`kiro-cli`). Headless/non-interactive mode was added in Kiro CLI 2.0 (April 2026) and is explicitly designed for CI/CD and automation. A Forge backend is feasible. For best structured I/O, prefer the `kiro-cli acp` subcommand (JSON-RPC 2.0 over stdio); for simpler text-in/text-out, use `kiro-cli chat --no-interactive`.

Origin note: per the `kiro.dev` docs and multiple search results, "The Q Developer CLI has become the Kiro CLI" — Kiro CLI is the rebranded/evolved Amazon Q Developer CLI. ([kiro.dev/docs/cli](https://kiro.dev/docs/cli/))

## 1. Does Kiro have a standalone CLI?

**Yes.** The binary is `kiro-cli`, installed to `~/.local/bin` on macOS/Linux via:

```bash
curl -fsSL https://cli.kiro.dev/install | bash
```

It runs natively on macOS, Linux, and Windows 11 (the latter since 2.0).

Confirmed externally by `ralph-orchestrator`, which spawns `kiro-cli` as a subprocess: `command: "kiro-cli".to_string()` ([crates/ralph-adapters/src/cli_backend.rs L160](https://github.com/mikeyobrien/ralph-orchestrator/blob/main/crates/ralph-adapters/src/cli_backend.rs)).

## 2. Invocation modes

### 2a. `kiro-cli chat --no-interactive` (text mode)

Prompt is passed as a **positional argument** (not stdin):

> `--no-interactive` — "Run without an interactive session. Requires a prompt as an argument"

Canonical command (exactly what ralph-orchestrator uses):

```bash
kiro-cli chat --no-interactive --trust-all-tools "your prompt here"
```

Stdin is supported only for **piping extra context** alongside the argument prompt:

```bash
git diff | kiro-cli chat --no-interactive "Review these changes"
```

> *"You must provide an initial prompt as an argument. No mid-session user input is possible."*

Ralph's adapter records prompt-mode for Kiro as `PromptMode::Arg` (positional), not stdin.

### 2b. `kiro-cli acp` (structured mode, recommended for Forge)

```bash
kiro-cli acp
kiro-cli acp --agent my-agent
kiro-cli acp --agent developer --model claude-opus-4.6
```

Speaks **JSON-RPC 2.0 over stdin/stdout**. Ralph's `kiro-acp` backend uses exactly this with `PromptMode::Stdin` and `OutputFormat::Acp`.

ACP: *"an open standard for AI agent communication, similar to how LSP standardized language servers."*

## 3. Output formats

- **`kiro-cli chat --no-interactive`**: **plain text** to stdout with inline status markers (ANSI); no `--output-format=json` flag for `chat`.
- **`kiro-cli acp`**: **JSON-RPC 2.0** messages over stdout with "streaming text chunks and tool invocations." Supported methods: `initialize`, `session/new`, `session/load`, `session/prompt`, `session/cancel`, `session/set_mode`, `session/set_model`, plus Kiro extensions `_kiro.dev/commands/execute`, `_kiro.dev/mcp/oauth_request`, `_kiro.dev/compaction/status`.
- **`kiro-cli whoami --format json|json-pretty|plain`** exists for some subcommands but not for `chat`.

## 4. Non-interactive / permission skip

Kiro normally prompts `Allow this action? [y/n/t]:` for sensitive ops. Two flags bypass:

| Flag | Docs quote |
|------|------------|
| `--trust-all-tools` | *"Auto-approve all tool calls without prompting"* |
| `--trust-tools=<categories>` | *"Auto-approve specific tool categories (e.g., `read`, `grep`, `write`)"* |

For CI robustness: `--require-mcp-startup` = *"Fail immediately if any MCP server fails to connect"* (otherwise warnings, exit 0).

## 5. Session semantics

- **Fresh process per iteration is fine.** `--no-interactive` prints response to stdout and exits.
- **Hidden state / caches exist but aren't required.** ACP sessions persist to `~/.kiro/sessions/cli/` with metadata `.json` and event logs `.jsonl`. For `chat`, sessions addressable via `--resume`, `-r`, `--resume-picker`, `--resume-id <ID>`, `--list-sessions`, `--delete-session <ID>` — persistence is opt-in; omit `--resume*` for a clean slate each iteration.
- **Agent profiles** live at `~/.kiro/agents/<name>/agent-spec.json` or `.md`.

## 6. Tool ecosystem

Built-in tool **categories** (via `--trust-tools`): `read`, `grep`, `write`. Shell exec implied ("file edits, command execution"). Full category list not published on headless page.

**MCP extensibility**: `kiro-cli mcp {add, remove, list, import, status}` — Kiro is a first-class MCP client.

**Subagents/delegation**: Yes. CLI 2.0: *"Subagents now support task dependencies"* and `/spawn` creates parallel sessions. Agents via `kiro-cli agent {list, create, edit, validate, migrate, set-default}`.

## 7. Token / cost reporting

Kiro reports credits + time after each response:

```
▸ Credits: 0.39 • Time: 22s
▸ Credits: 0.13 • Time: 21s
```

AWS Labs provider detects completion via the `"▸ Credits:"` marker.

Kiro uses a **credit system** (not raw tokens); per-model multipliers published. In ACP mode, usage likely surfaces through `session/prompt` responses — not explicitly documented.

## 8. Context window

Per-model:

| Model ID | Context | Credit mult. |
|---|---|---|
| `claude-opus-4.6` | **1M** | 2.2x |
| `claude-opus-4.5` | 200K | 2.2x |
| `claude-sonnet-4.6` | **1M** | 1.3x |
| `claude-sonnet-4.5` | 200K | 1.3x |
| `claude-sonnet-4.0` | 200K | 1.3x |
| `claude-haiku-4.5` | 200K | 0.4x |
| `deepseek-3.2` | 128K | 0.25x |
| `minimax-m2.5` | 200K | 0.25x |
| `glm-5` | 200K | 0.5x |
| `minimax-m2.1` | 200K | 0.15x |
| `qwen3-coder-next` | 256K | 0.05x |
| `auto` | — | 1.0x baseline |

Kiro runs automatic context compaction — exposed in ACP as `_kiro.dev/compaction/status` notifications.

## 9. Model selection

1. **Default via settings**: `kiro-cli settings chat.defaultModel claude-opus-4.6`
2. **Per-session slash command**: `> /model set-current-as-default`
3. **ACP per-invocation flag**: `kiro-cli acp --model claude-opus-4.6`

`kiro-cli chat` exposes `--list-models`. **A `--model` flag on `chat` itself is not confirmed** — ACP mode is the documented per-call model override.

## 10. Authentication model

- GitHub, Google, AWS Builder ID, AWS IAM Identity Center (browser auth)
- External identity provider
- **`KIRO_API_KEY` env var** — the only headless-compatible method

*"For CI/CD pipelines and automation scripts, you can authenticate using an API key... Set the `KIRO_API_KEY` environment variable and run Kiro CLI in non-interactive mode."*

API keys require **Kiro Pro, Pro+, or Power subscription**.

**Historical note:** Pre-2.0 Kiro CLI auth was AWS-cred based. As of CLI 2.0 (2026-04-13), `KIRO_API_KEY` is canonical headless auth.

## 11. Version reporting

Two forms:
- **Global flag**: `--version` / `-V`
- **Subcommand**: `kiro-cli version` with `--changelog`, `--changelog=all`, `--changelog=x.x.x`

Ralph probes: `kiro-cli --version`.

## 12. Config precedence

(Composed from docs; explicit precedence ordering not stated on one page)

1. **Command-line flags** (highest)
2. **Environment variables** — `KIRO_API_KEY`, AWS env vars for IAM-IdC flows
3. **Project/agent profile** — `~/.kiro/agents/<name>/agent-spec.{json,md}`; `--agent <name>`
4. **User settings** — `~/.kiro/settings/cli.json` via `kiro-cli settings list|open|list --all`
5. **Built-in defaults** (lowest)

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | "Command completed successfully" |
| 1 | "General failure (auth error, invalid args, operation failed)" |
| 3 | "MCP server failed to start (requires `--require-mcp-startup`)" |

Hook exit codes: 0 success, 2 block tool (PreToolUse only, STDERR returned to LLM), other = warning.

## Recommended Forge invocation

**Text backend (simplest):**
```bash
KIRO_API_KEY=$KIRO_API_KEY \
kiro-cli chat --no-interactive --trust-all-tools --require-mcp-startup \
  "<prompt text>"
```

**Structured backend (recommended):**
```bash
KIRO_API_KEY=$KIRO_API_KEY \
kiro-cli acp --agent <profile> --model claude-opus-4.6
# then speak JSON-RPC 2.0 over stdio: initialize → session/new → session/prompt
```

Probe availability with `kiro-cli --version` (exit-0 = installed).

---

## Gaps / to verify in implementation phase

1. **Exact `chat --no-interactive` stdout schema**: no published grammar. AWS Labs approach: strip ANSI, delimit on `▸ Credits:` marker.
2. **`--model` flag on `kiro-cli chat`**: not listed in public commands reference. Use `acp` or pre-set `chat.defaultModel`.
3. **Precise ACP token/usage reporting shape**: needs capture from real run.
4. **Full tool-category list for `--trust-tools`**: only `read`, `grep`, `write` mentioned.
5. **Stdin piping semantics with `--no-interactive`**: prepended, replaced, or attached? Test.
6. **Credentials-on-disk path**: unconfirmed; likely `~/.kiro/`.
7. **`--version` output grammar**: undocumented; treat as free-form.
8. **Config precedence ordering**: inferred, not quoted.
9. **Windows paths** for `~/.kiro`: undocumented.
10. **Rate limits / concurrent invocations**: not published for parallel `kiro-cli` processes.
11. **Subscription gating**: free-tier users will fail at auth — Forge `doctor` must detect.
12. **Stability of `▸ Credits:` marker**: UX string, could change. ACP mode doesn't have this fragility.

## Primary sources

- [Kiro CLI docs hub](https://kiro.dev/docs/cli/)
- [Headless mode](https://kiro.dev/docs/cli/headless/)
- [CLI commands reference](https://kiro.dev/docs/cli/reference/cli-commands/)
- [Exit codes](https://kiro.dev/docs/cli/reference/exit-codes)
- [Authentication](https://kiro.dev/docs/cli/authentication/)
- [Models](https://kiro.dev/docs/cli/models/)
- [ACP](https://kiro.dev/docs/cli/acp/)
- [CLI 2.0 changelog](https://kiro.dev/changelog/cli/2-0/)
- [Headless mode launch blog](https://kiro.dev/blog/introducing-headless-mode/)
- [Ralph adapter (kiro invocation)](https://github.com/mikeyobrien/ralph-orchestrator/blob/main/crates/ralph-adapters/src/cli_backend.rs)
- [AWS Labs CLI Agent Orchestrator](https://github.com/awslabs/cli-agent-orchestrator/blob/main/docs/kiro-cli.md)
- [Classmethod walkthrough (real-run transcripts)](https://dev.classmethod.jp/en/articles/kiro-cli-2-0-headless-mode-api-key-auth/)
