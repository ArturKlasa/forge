# Research: Claude Code CLI capabilities

**Date:** 2026-04-16
**Verified against:** `claude` CLI v2.1.110 (Linux) + docs at `code.claude.com/docs/en/*`
**Purpose:** Ground Forge's Claude Code backend adapter in verified capabilities and flags.

## Key implications for Forge

- **Invocation pattern for Forge iterations:**
  `claude --bare -p --output-format stream-json --verbose --permission-mode dontAsk --allowedTools "Bash(git diff *),Read,Edit,..." --session-id $(uuidgen) --no-session-persistence < prompt.md`
- **Completion signal in stream-json:** the line where `type == "result"`. `subtype` disambiguates (`success`, `error_max_turns`, `error_max_budget_usd`, etc.).
- **Default window is 200k; opt into 1M via `--model opus[1m]` / `sonnet[1m]` aliases.** Effective window is affected by auto-compact — use env vars (`CLAUDE_AUTOCOMPACT_PCT_OVERRIDE`, `CLAUDE_CODE_MAX_CONTEXT_TOKENS`) for fine control.
- **Session leak hazard:** sessions persist by default to `~/.claude/projects/<encoded-cwd>/*.jsonl`. Forge MUST pass `--no-session-persistence` or a unique `--session-id` per iteration to enforce Q8's reset discipline.
- **Permission strategy:** Do not use `--dangerously-skip-permissions`. Use `--permission-mode dontAsk` + explicit `--allowedTools` per iteration — keeps Forge's Q10 policy enforcement as the single source of truth.
- **Cost tracking:** `total_cost_usd` ships in every JSON `result` event. `modelUsage` has per-model token counts and context-window/max-output-tokens metadata. Forge gets accurate cost data for free.
- **Subagent support is first-class via CLI:** `Agent` tool + `--agents '{...}'` JSON lets Forge declare custom subagents. Main agent can spawn them with fresh contexts; they can't nest. Parent cost rolls up.

---

# Claude Code CLI — Research Report for Forge

Claude Code CLI (`claude`, a.k.a. claude-code) — driving it as a subprocess in a Ralph-style loop. Tested locally on `claude` v2.1.110 (Linux), cross-referenced against the official docs at `code.claude.com/docs/en/...`. Docs recently moved off `docs.anthropic.com` — every `/en/docs/claude-code/*` URL now 301-redirects to `code.claude.com/docs/en/*`.

## 1. Invocation modes

Claude Code has four de-facto entry modes, all through the single `claude` binary (from `cli-reference` and `headless`):

- **Interactive REPL**: `claude` or `claude "initial prompt"` — TUI session, not useful for Forge.
- **One-shot / headless**: `claude -p "query"` (long form `--print`). "The CLI was previously called 'headless mode.' The `-p` flag and all CLI options work the same way." ([headless](https://code.claude.com/docs/en/headless))
- **stdin pipe**: `cat file | claude -p "query"` is the documented pattern. `claude < prompt.md` works but the content is treated as **prompt input only when combined with `-p`**; without `-p`, the binary tries to attach a real TTY and exits or falls back. The doc example is verbatim: `cat logs.txt | claude -p "explain"` ([cli-reference](https://code.claude.com/docs/en/cli-reference)).
- **Streaming input** (advanced): `--input-format stream-json` — accepts NDJSON user messages on stdin and requires `--output-format stream-json`. My live probe confirmed: `--input-format=stream-json requires output-format=stream-json` (exit 1).

**For Forge**: use `claude --bare -p --output-format stream-json --verbose --permission-mode ... "$(cat prompt.md)"`, or pipe via stdin with `cat prompt.md | claude --bare -p --output-format stream-json --verbose`. The doc explicitly recommends `--bare` for scripted/SDK calls: *"`--bare` is the recommended mode for scripted and SDK calls, and will become the default for `-p` in a future release."* ([headless](https://code.claude.com/docs/en/headless))

## 2. Output formats

`--output-format` only works with `-p` and takes three values ([headless](https://code.claude.com/docs/en/headless)):

- **`text`** (default): plain prose, no metadata.
- **`json`**: single JSON object printed at completion. Live-captured shape from `claude -p "reply PONG" --output-format json`:

```json
{"type":"result","subtype":"success","is_error":false,"api_error_status":null,
 "duration_ms":1234,"duration_api_ms":2256,"num_turns":1,
 "result":"PONG","stop_reason":"end_turn",
 "session_id":"59d002df-8034-4d19-88ed-d96f78f2aafe",
 "total_cost_usd":0.051654,
 "usage":{"input_tokens":5,"cache_creation_input_tokens":6876,
          "cache_read_input_tokens":16158,"output_tokens":7,
          "server_tool_use":{"web_search_requests":0,"web_fetch_requests":0},
          "service_tier":"standard",
          "cache_creation":{"ephemeral_1h_input_tokens":6876,"ephemeral_5m_input_tokens":0}},
 "modelUsage":{"claude-haiku-4-5-20251001":{"inputTokens":345,"outputTokens":11,"contextWindow":200000,"maxOutputTokens":32000},
               "claude-opus-4-6[1m]":{"inputTokens":5,"outputTokens":7,"contextWindow":1000000,"maxOutputTokens":64000}},
 "permission_denials":[],"terminal_reason":"completed","uuid":"..."}
```

- **`stream-json`**: NDJSON, one event per line. Event types (from [streaming-output](https://code.claude.com/docs/en/agent-sdk/streaming-output)):

| Event        | Shape / meaning                                                                        |
| ------------ | -------------------------------------------------------------------------------------- |
| `system` (subtype `init`)   | First event; contains `session_id`, model, tools, cwd. |
| `assistant`  | Complete `AssistantMessage` after each model turn (content blocks: text, tool_use). |
| `user`       | Tool-result messages the agent produced internally. |
| `stream_event` | Raw Anthropic API chunks when `--include-partial-messages` is passed. Sub-types: `message_start`, `content_block_start`, `content_block_delta`, `content_block_stop`, `message_delta`, `message_stop`. |
| `system` (subtype `compact_boundary`) | Marks where conversation was auto-compacted mid-run. |
| `system` (subtype `api_retry`) | Emitted on retryable errors; fields `attempt`, `max_retries`, `retry_delay_ms`, `error_status`, `error` (one of `authentication_failed`, `billing_error`, `rate_limit`, `invalid_request`, `server_error`, `max_output_tokens`, `unknown`). |
| `result`     | **Final event. Signals iteration completion.** Same schema as `--output-format json`. `subtype`: `success`, `error_max_turns`, `error_max_budget_usd`, or other error subtypes. |

**Detect completion in Forge**: line where `type == "result"`. The `subtype` tells you whether it was clean (`success`) or hit a limit.

## 3. Non-interactive / permission-skip

From [permission-modes](https://code.claude.com/docs/en/permission-modes):

- `--dangerously-skip-permissions` — equivalent to `--permission-mode bypassPermissions`. *"Writes to protected paths are the only actions that still prompt."* Protected paths: `.git`, `.vscode`, `.idea`, `.husky`, most of `.claude/`, plus files like `.gitconfig`, `.bashrc`, `.zshrc`, `.mcp.json`, `.claude.json`. **It does NOT sandbox** — any Bash/Edit/Write runs. *"`bypassPermissions` offers no protection against prompt injection or unintended actions."*
- **Safer middle grounds**:
  - `--permission-mode acceptEdits` — auto-approves file edits + the filesystem Bash commands `mkdir, touch, rm, rmdir, mv, cp, sed` (scoped to cwd/additionalDirectories). Other shells/network still prompt.
  - `--permission-mode dontAsk` — auto-denies anything not in `permissions.allow`. *"fully non-interactive for CI pipelines."* **This is the cleanest locked-down headless mode.**
  - `--allowedTools "Bash(git diff *),Read,Edit"` — uses [permission rule syntax](https://code.claude.com/docs/en/settings#permission-rule-syntax); space before `*` matters.
  - `--disallowedTools` — removes tools from the model's context entirely.
  - `--tools "Bash,Edit,Read"` — restricts the **available** built-in set; `""` disables all, `"default"` enables all.
- `--max-turns N` and `--max-budget-usd 5.00` — cap loop length / cost; both print-mode only.

**Forge recommendation**: `--permission-mode dontAsk` + explicit `--allowedTools` list per iteration, not `--dangerously-skip-permissions`.

## 4. Session semantics

From [agent-sdk/sessions](https://code.claude.com/docs/en/agent-sdk/sessions) and live inspection:

- **Default persistence is ON**. Every `claude -p` invocation writes a transcript to `~/.claude/projects/<encoded-cwd>/<session-id>.jsonl`, where `<encoded-cwd>` is the absolute cwd with non-alphanumerics replaced by `-`.
- **To prevent leakage across iterations**: pass `--no-session-persistence` (print mode only). *"sessions will not be saved to disk and cannot be resumed."* Even safer: `--bare` + `--no-session-persistence` + fresh `--session-id "$(uuidgen)"` each iteration.
- **Fresh process with no flags ≠ fresh context.** Without `--bare`, Claude Code still auto-discovers: `~/.claude/settings.json`, project `.claude/settings.json`, `.claude/settings.local.json`, `CLAUDE.md`, `.mcp.json`, hooks, skills, plugins, agent definitions, `~/.claude.json`, shell snapshots in `~/.claude/session-env/`.
- **`--session-id <uuid>`**: forces a specific UUID. Useful for Forge to control file naming if Forge ever wants sessions on disk.
- **`--fork-session`** (with `--resume`/`--continue`): creates a new session ID seeded with a copy of original history.
- Sessions persist **by cwd**. If two Forge workers share a cwd, `--continue` could pick up the wrong one — always use `--session-id` explicitly or `--no-session-persistence`.

## 5. Tool ecosystem

Default built-in tools:

| Tool | Purpose |
| ---- | ------- |
| `Read`, `Write`, `Edit` | File operations |
| `Bash` | Shell commands |
| `Glob`, `Grep` | File/content search |
| `WebSearch`, `WebFetch` | Web access |
| `Monitor` | Watch background scripts |
| `AskUserQuestion` | Interactive clarification (no-op in `-p` unless a handler is wired) |
| `Agent` (a.k.a. Task) | **Spawns subagents — exposed through CLI.** Include `"Agent"` in `allowedTools` to enable. |
| `NotebookEdit` | Jupyter support |

Flags to control tool access:
- `--tools ""` disables all; `--tools "default"` enables all; `--tools "Bash,Edit,Read"` restricts to a list.
- `--allowedTools` / `--disallowedTools` for fine-grained permission rules.
- `--agents '{"reviewer":{...}}'` defines custom subagents inline as JSON.
- `--bare` restricts to **Bash, file read, and file edit tools** only.

## 6. Token / cost reporting

Confirmed live: the `json` and `stream-json` result objects always include rich usage fields. `text` mode has **no metadata** — use JSON for Forge.

Fields in `usage`: `input_tokens`, `output_tokens`, `cache_creation_input_tokens`, `cache_read_input_tokens`, `cache_creation.ephemeral_5m_input_tokens`, `cache_creation.ephemeral_1h_input_tokens`, `server_tool_use.web_search_requests`, `web_fetch_requests`, `service_tier`, `speed`, `iterations[]`.

Top-level: `total_cost_usd`, `duration_ms`, `duration_api_ms`, `num_turns`, `stop_reason`, `terminal_reason`, `permission_denials`.

`modelUsage` breaks down token + cost per model used, with `contextWindow` and `maxOutputTokens` per model.

## 7. Context window specifics

From [model-config](https://code.claude.com/docs/en/model-config) and live data:

- **Default context**: 200,000 tokens.
- **1M-token window**: available on `opus[1m]` and `sonnet[1m]` aliases. Live probe showed `claude-opus-4-6[1m]` with `contextWindow: 1000000, maxOutputTokens: 64000`.
- **Auto-compact**: Claude Code auto-compacts mid-conversation. Env vars ([env-vars](https://code.claude.com/docs/en/env-vars)):
  - `CLAUDE_CODE_AUTO_COMPACT_WINDOW` — capacity used for calculation
  - `CLAUDE_AUTOCOMPACT_PCT_OVERRIDE` — percentage (1–100) at which it triggers
  - `CLAUDE_CODE_MAX_CONTEXT_TOKENS` — override the model's assumed window
  - `CLAUDE_CODE_MAX_OUTPUT_TOKENS` — cap output tokens
- Stream emits `system` / subtype `compact_boundary` at the compaction point.
- `maxOutputTokens` defaults: 32000 (Haiku), 64000 (Opus 4.6 [1m]).

## 8. Model selection

Precedence:
1. `/model <alias>` during session
2. `--model <alias|name>` at startup (e.g. `--model opus`, `--model opus[1m]`)
3. `ANTHROPIC_MODEL` env var
4. `model` field in `settings.json`

Aliases: `default`, `best`, `sonnet`, `opus`, `haiku`, `sonnet[1m]`, `opus[1m]`, `opusplan`.

Alias resolution: `ANTHROPIC_DEFAULT_{OPUS,SONNET,HAIKU}_MODEL`. Subagent model: `CLAUDE_CODE_SUBAGENT_MODEL`. `--fallback-model sonnet` provides auto-fallback when primary is overloaded.

## 9. Subagent (Task / Agent tool) semantics

From [sub-agents](https://code.claude.com/docs/en/sub-agents):

- **Fresh context window**: *"Each subagent runs in its own context window with a custom system prompt, specific tool access, and independent permissions."* Only final summary returns to parent.
- **Tool inheritance**: if `tools` frontmatter omitted, inherits all parent tools. `disallowedTools` subtracts. Built-in Explore/Plan subagents inherit parent permissions but are read-only.
- **Permission mode**: subagents can set their own `permissionMode` but under auto-mode the parent classifier overrides.
- **No nesting**: *"subagents cannot spawn other subagents"* — prevents infinite recursion.
- **Cost**: rolls up to parent `total_cost_usd`; per-model breakdown via `modelUsage`. Stream events include `parent_tool_use_id` for attribution.
- **CLI exposure**: `--agents '{...}'` passes JSON definitions; `Agent` must be in `allowedTools`.
- **Isolation option**: frontmatter `isolation: worktree` gives subagent a temporary git worktree.

## 10. Error modes and exit codes

| Scenario | Behavior |
| -------- | -------- |
| Not logged in | `is_error: true`, `result: "Not logged in · Please run /login"`, exit code 0. |
| `--input-format=stream-json` without matching output | Stderr error, exit 1. |
| `--max-turns N` reached | `subtype: "error_max_turns"`. |
| `--max-budget-usd` hit | `subtype: "error_max_budget_usd"`. |
| Rate-limit, 5xx, network | `system` / subtype `api_retry` events. `CLAUDE_CODE_MAX_RETRIES` (default 10), `API_TIMEOUT_MS` (default 600000 ms). |
| Auth failure | `error: "authentication_failed"` in retry events. |
| Billing / quota | `error: "billing_error"`. |

**Docs do not publish a full exit-code table.** Observed: 0 for any completion (even soft errors with `is_error: true` inside JSON), 1 for CLI validation errors. Forge should parse JSON `is_error` / `subtype` fields, not rely on exit codes.

## 11. Version reporting

- `claude --version` / `-v` → `2.1.110 (Claude Code)`.
- Semver-ish. Release channel controlled by `autoUpdatesChannel`: `"stable"` or `"latest"` (default).
- `claude install stable|latest|<version>` installs a specific build.

## 12. Config precedence

Highest to lowest:
1. **Managed** (system policy, MDM, registry).
2. **Command-line arguments** — session-only.
3. **Local** — `.claude/settings.local.json` (gitignored).
4. **Project** — `.claude/settings.json`.
5. **User** — `~/.claude/settings.json`.

Relevant env vars that beat settings: `ANTHROPIC_MODEL`, `ANTHROPIC_API_KEY`, `CLAUDE_CODE_EFFORT_LEVEL` (*"takes precedence over all other methods"*), `CLAUDE_CONFIG_DIR`, `CLAUDE_CODE_SKIP_PROMPT_HISTORY`, `CLAUDE_CODE_DISABLE_AUTO_MEMORY`, `CLAUDE_CODE_DISABLE_CLAUDE_MDS`.

`--setting-sources user,project,local` whitelists which scopes load — critical for reproducible Forge runs.

---

## Gaps / to verify later

- Full exit-code table not published. Verify empirically under rate-limit and quota-exhaust.
- Full `result` event subtypes for error cases.
- Exact `assistant` / `user` event JSON shape in stream-json — capture fixture from real run.
- Shell-snapshot leakage (`~/.claude/session-env/`) — confirm `--bare` skips it.
- `claude < prompt.md` (no `-p`) behavior not tested live.
- `CLAUDE_CODE_EXIT_AFTER_STOP_DELAY` semantics.
- Per-subagent cost attribution granularity.

## Sources

- [cli-reference](https://code.claude.com/docs/en/cli-reference)
- [headless](https://code.claude.com/docs/en/headless)
- [settings](https://code.claude.com/docs/en/settings)
- [env-vars](https://code.claude.com/docs/en/env-vars)
- [permission-modes](https://code.claude.com/docs/en/permission-modes)
- [sub-agents](https://code.claude.com/docs/en/sub-agents)
- [model-config](https://code.claude.com/docs/en/model-config)
- [context-window](https://code.claude.com/docs/en/context-window)
- [agent-sdk/overview](https://code.claude.com/docs/en/agent-sdk/overview)
- [agent-sdk/streaming-output](https://code.claude.com/docs/en/agent-sdk/streaming-output)
- [agent-sdk/sessions](https://code.claude.com/docs/en/agent-sdk/sessions)
