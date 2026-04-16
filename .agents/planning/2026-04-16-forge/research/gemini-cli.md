# Research: Gemini CLI capabilities

**Date:** 2026-04-16
**Verified against:** `google-gemini/gemini-cli` v0.38.1 (2026-04-15)
**Purpose:** Ground Forge's Gemini CLI backend adapter in verified capabilities and flags.

## Key implications for Forge

- **Invocation pattern for Forge iterations:**
  `gemini -p "$PROMPT" -o stream-json --approval-mode=yolo -s -m <pinned-model>`
- **Completion signal:** the `result` event in stream-json NDJSON stream.
- **Checkpointing is OFF by default** — perfect for Ralph-reset discipline. Just fire-and-exit each iteration.
- **Pre-1.0 version churn.** Breaking changes (e.g., `--checkpointing` removed in 0.11.0, `--yolo` deprecated in favor of `--approval-mode=yolo`). Forge MUST pin a tested CLI version and gate behavior on `--version` at startup.
- **No native subagent/Task tool.** If Forge wants delegation on Gemini, Forge itself must spawn additional subprocesses (matches Q16's multi-spawn decision).
- **Exit codes published:** 0 success, 1 general/API, 42 input error, 53 turn-limit. Cleaner than Claude Code's "parse is_error from JSON" pattern.
- **Rate-limit hazard:** CLI has been reported stuck in retry loops on 429s. Forge must wrap with its own wall-clock timeout (already have — Q16.5 30-min default per iteration).
- **Cost:** Gemini emits token counts but NOT dollar values. Forge computes cost from its own price table.
- **`ask-user` tool is dangerous in headless mode.** Forge should exclude it via `tools.exclude` in a Forge-written `.gemini/settings.json`.
- **Config path differs from platform norms:** `~/.gemini/` (not `~/.config/gemini/`).

---

# Gemini CLI Research Report (for Forge integration)

Target: `gemini` CLI from `google-gemini/gemini-cli`. All facts cited from docs fetched 2026-04-16. Current stable release: **v0.38.1** (2026-04-15), with nightly builds daily.

## 1. Invocation modes

Three modes:

- **Headless / one-shot**: `-p` / `--prompt` or piped stdin. Quote: *"Headless mode is triggered when the CLI is run in a non-TTY environment or when providing a query with the `-p` (or `--prompt`) flag."* `--prompt` "Forces non-interactive mode" and is "Appended to stdin input if provided".
- **Interactive REPL**: bare `gemini`, or `-i` / `--prompt-interactive "<seed>"`.
- **Streaming**: `-o stream-json` emits NDJSON events.

```bash
gemini -p "Refactor this module" -o json        # one-shot, JSON
echo "summarize" | gemini                        # stdin, non-TTY -> headless
cat prompt.md | gemini -p "apply the plan above" # stdin concatenated with -p
```

Piping a file via `gemini < prompt.md` works because stdin in a non-TTY triggers headless mode. **Note:** GH issue #16025 (Jan 2026) now *requires* `-p` for non-interactive runs in some scenarios — verify against installed version before shipping.

## 2. Output formats

Three formats via `--output-format` / `-o`: `text` (default), `json`, `stream-json`.

**JSON schema:**

```json
{
  "response": "string",
  "stats": {
    "models": {
      "gemini-2.5-pro": {
        "api":    {"totalRequests": 2, "totalErrors": 0, "totalLatencyMs": 5053},
        "tokens": {"prompt": 24939, "candidates": 20, "total": 25113,
                   "cached": 21263, "thoughts": 154, "tool": 0}
      }
    },
    "tools": {
      "totalCalls": 0, "totalSuccess": 0, "totalFail": 0, "totalDurationMs": 0,
      "totalDecisions": {"accept":0,"reject":0,"modify":0,"auto_accept":0},
      "byName": { "<tool>": {"count":0,"success":0,"fail":0,"durationMs":0,"decisions":{}} }
    },
    "files": {"totalLinesAdded": 0, "totalLinesRemoved": 0}
  },
  "error": {"type":"string","message":"string","code":0}
}
```

**stream-json event types** (NDJSON): `init`, `message`, `tool_use`, `tool_result`, `error`, `result`. For Forge, `stream-json` is the right channel — tool-call events inline and a terminal `result` envelope.

## 3. Non-interactive / permission-skip

- `--approval-mode <default|auto_edit|yolo|plan>` — canonical flag. `yolo` = auto-approve all tool calls; `auto_edit` = auto-approve edits only; `plan` = read-only planning.
- `--yolo` / `-y` — deprecated alias.
- `--allowed-tools <list>` — comma-separated tool names that bypass confirmation without enabling full yolo.

Sandbox is **independent** of approval mode but auto-enables with yolo. Flags: `--sandbox` / `-s`, `--sandbox-image <uri>`, env `GEMINI_SANDBOX=true|false|docker|podman|<cmd>`. On macOS: `SEATBELT_PROFILE=permissive-open|strict|<custom>`.

**Forge recommendation:** `gemini -p "$PROMPT" -o stream-json --approval-mode=yolo -s` — unattended execution inside a container-sandboxed process.

## 4. Session semantics

- **Fresh process is well-supported and the default**: checkpointing is **off by default**. *"Checkpointing is disabled by default. You must explicitly enable it by adding this to `settings.json`"*.
- When enabled: `~/.gemini/history/<project_hash>` + `~/.gemini/tmp/<project_hash>/checkpoints`.
- `--resume <id|"latest">` (`-r`) to opt into resume. `-w` / `--worktree <name>` starts in fresh git worktree.
- `GEMINI.md` (project-level memory) — read every run, intended behavior.
- `.env` in project directory is auto-loaded.

**Note:** `--checkpointing` CLI flag was **removed in v0.11.0**; use settings.json if needed. For Ralph-loop, leave off.

## 5. Tool ecosystem

Documented categories (at `docs/tools/`):
- `file-system.md` — read/write/edit
- `shell.md` — shell exec
- `web-fetch.md`, `web-search.md`
- `memory.md` — persistent memory
- `todos.md`
- `planning.md` — `enter_plan_mode` / `exit_plan_mode`
- `ask-user.md` — interactive prompt to user (**problematic in headless!**)
- `mcp-server.md` — MCP bridge
- `activate-skill.md`, `internal-docs.md`

**No subagent/Task tool.** *"Planning tools let Gemini CLI switch into a safe, read-only 'Plan Mode'… This is fundamentally different from a task-spawning mechanism"*. If Forge needs delegation, Forge itself must spawn sub-processes.

**Restriction keys** (settings.json under `tools`): `tools.core` (allowlist), `tools.exclude` (denylist), `tools.allowed` (bypass-confirmation allowlist), `tools.discoveryCommand`, `tools.callCommand`, `tools.sandbox`.

## 6. Token/cost reporting

Per-invocation stats in `stats.models.<model>.tokens`: `prompt`, `candidates` (output), `total`, `cached` (cache hits), `thoughts`, `tool`. Latency in `.api.totalLatencyMs`. **No dollar-cost field** — Forge must multiply by its own price table. Cache-hit reporting is explicit.

## 7. Context-window specifics

**Not consistently documented from first-party pages.** Numbers from secondary sources:
- Gemini 2.5 Pro: **1,048,576** input tokens.
- Other model limits: not verified.

Gemini CLI has `model.compressionThresholds` and auto-compaction behavior. Effective usable context is lower once tool-output summarization kicks in.

## 8. Model selection

Three inputs, highest-wins:
- CLI: `--model <name>` / `-m <name>`, e.g. `-m gemini-2.5-flash`
- Env: `GEMINI_MODEL`
- Settings: `model.name` in `~/.gemini/settings.json` or `.gemini/settings.json`
- Fallback: hardcoded default

## 9. MCP / extensions

Full MCP support via `mcpServers` in `settings.json`:

```json
{
  "mcpServers": {
    "my-tool": {
      "command": "bin/mcp_server.py",
      "args": ["--verbose"],
      "env": {"VAR": "value"},
      "cwd": "/path/to/dir",
      "url": "https://example.com/sse",
      "httpUrl": "https://example.com/http",
      "headers": {"Authorization": "Bearer token"},
      "timeout": 5000,
      "trust": false,
      "includeTools": ["tool1"],
      "excludeTools": ["tool3"]
    }
  }
}
```

Precedence: `httpUrl` > `url` > `command`. `trust: true` skips confirmations for that server. Also: `--extensions <names>` / `-e`, `--list-extensions` / `-l`. Shell-command-as-tool: `tools.discoveryCommand` + `tools.callCommand`.

## 10. Error modes and exit codes

- `0` — Success
- `1` — General error or API failure
- `42` — Input error (invalid prompt or arguments)
- `53` — Turn limit exceeded

Rate limits surface as HTTP 429 in the `error` field. Multiple open GH issues (#10513, #1881, #24396, #18050) describe stuck retry loops — **Forge should impose its own wall-clock timeout** and treat nonzero exit + 429 in stderr as retry-with-backoff signal.

## 11. Version reporting

`--version` / `-v` prints semver (pre-1.0 currently `0.38.1`). Cadence: **daily nightlies**, **preview** tags, **stable patch releases roughly weekly**. Breaking flag changes do happen (e.g., `--checkpointing` removed in 0.11.0). Forge should pin a tested CLI version and gate on `--version` at startup.

## 12. Config precedence

Lowest to highest:
1. Hardcoded defaults
2. `/etc/gemini-cli/system-defaults.json`
3. `~/.gemini/settings.json` (user)
4. `.gemini/settings.json` (project)
5. `/etc/gemini-cli/settings.json` (system override)
6. Env vars (including auto-loaded `.env`)
7. CLI flags (win)

Platform paths: Linux `/etc/gemini-cli/…`, Windows `C:\ProgramData\gemini-cli\…`, macOS `/Library/Application Support/GeminiCli/…`. Config is under `~/.gemini/`, **not** `~/.config/gemini/`.

Key env vars: `GEMINI_API_KEY`, `GEMINI_MODEL`, `GOOGLE_API_KEY`, `GOOGLE_CLOUD_PROJECT`, `GOOGLE_APPLICATION_CREDENTIALS`, `GOOGLE_CLOUD_LOCATION`, `GEMINI_SANDBOX`, `GEMINI_TELEMETRY_*`.

---

## Forge-specific recommendations

- Per iteration: `gemini -p "$PROMPT" -o stream-json --approval-mode=yolo -s -m <pinned-model>` in a clean `cwd`.
- Parse NDJSON stream; treat `result` event as termination signal and source-of-truth for stats.
- Leave checkpointing off; rely on git in the workspace for undo.
- Exclude `ask-user` via `tools.exclude` in project-level `.gemini/settings.json` Forge writes.
- Wrap subprocess with own timeout; watch for 429 in `error` channel and back off.
- Pin CLI version; re-check flags on upgrade (pre-1.0 churn).

## Gaps / to verify later

- **Exact context windows** for 2.5 Flash, 2.0 Flash, 1.5 Pro/Flash, 3.x — not captured first-party.
- **Stderr format** — no authoritative doc.
- **stream-json schema per-event field shapes** — capture from real output and pin parser.
- **`--resume` interplay with `-p`** — test before relying.
- **Rate-limit retry semantics** — confirm empirically.
- **Breaking change #16025** — confirm version and whether stdin-pipe-without-`-p` still works.
- **`allowed-tools` flag spelling** — verify with `gemini --help`.

## Sources

- [google-gemini/gemini-cli README](https://github.com/google-gemini/gemini-cli)
- [Headless Mode docs (github.io)](https://google-gemini.github.io/gemini-cli/docs/cli/headless.html)
- [Headless Mode reference (geminicli.com)](https://geminicli.com/docs/cli/headless/)
- [Configuration docs](https://google-gemini.github.io/gemini-cli/docs/get-started/configuration.html)
- [CLI Reference cheatsheet](https://geminicli.com/docs/cli/cli-reference/)
- [Checkpointing docs](https://geminicli.com/docs/cli/checkpointing/)
- [Tools docs directory](https://github.com/google-gemini/gemini-cli/tree/main/docs/tools)
- [Releases page](https://github.com/google-gemini/gemini-cli/releases)
- [GH issue #16025 — require `-p`](https://github.com/google-gemini/gemini-cli/issues/16025)
- [GH issue #22417](https://github.com/google-gemini/gemini-cli/issues/22417)
- [Rate-limit issues](https://github.com/google-gemini/gemini-cli/issues/24396)
- [Long-context page](https://ai.google.dev/gemini-api/docs/long-context)
- [Gemini Code Assist quotas](https://developers.google.com/gemini-code-assist/resources/quotas)
