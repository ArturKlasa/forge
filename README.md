# Forge

Forge orchestrates long-running AI coding tasks by driving AI CLIs (Claude Code, Kiro, Gemini CLI) in automated loops. It handles the automation brain around those loops — intent routing, plan-phase research, context management, stuck detection, safety gates, and human escalation.

## Install

**go install** (requires Go 1.22+):
```bash
go install github.com/ArturKlasa/forge/cmd/forge@latest
```

**curl installer** (Linux / macOS):
```bash
curl -fsSL https://raw.githubusercontent.com/ArturKlasa/forge/main/scripts/install.sh | bash
```

**Homebrew** (macOS / Linux):
```bash
brew tap ArturKlasa/forge
brew install forge
```

Or download a pre-built binary from [GitHub Releases](https://github.com/ArturKlasa/forge/releases).

## Prerequisites

Forge drives one of three AI CLI backends. Install at least one:

| Backend | Install |
|---|---|
| [Claude Code](https://claude.ai/code) | `npm install -g @anthropic-ai/claude-code` |
| [Gemini CLI](https://github.com/google-gemini/gemini-cli) | `npm install -g @google/gemini-cli` |
| [Kiro](https://kiro.dev) | Download from kiro.dev (Pro+ subscription required) |

## Quick start

```bash
# First run: Forge will prompt you to pick a backend
forge "Create a REST API for todos"

# Or specify the backend explicitly
forge --backend claude "Create a REST API for todos"
```

Forge detects your intent from the opening verb and routes to the right mode automatically.

## Modes

### Loop modes (run until done)

| Mode | Trigger words | Description |
|---|---|---|
| **Create** | create, build, write, implement, scaffold | Build something new from scratch |
| **Add** | add, extend, introduce, integrate | Add a feature to an existing codebase |
| **Fix** | fix, repair, debug, resolve, patch | Fix a bug or broken behavior |
| **Refactor** | refactor, restructure, reorganize, clean | Improve structure without changing behavior |
| **Upgrade** | upgrade, update, migrate, bump | Upgrade a dependency or framework version |
| **Test** | test, spec, coverage, tdd | Write or improve tests (production code is read-only) |

### One-shot modes (single pass, no loop)

| Mode | Trigger words | Description |
|---|---|---|
| **Review** | review, audit, check, inspect | Review code with parallel subagents |
| **Document** | document, docs, readme, comment | Generate documentation |
| **Explain** | explain, describe, summarize, what does | Explain how code works |
| **Research** | research, investigate, explore, analyse | Research a topic or question |

### Composite chaining

Chain modes with `:` for multi-stage workflows:

```bash
forge "review and fix the auth module"        # auto-detected chain
forge --chain review:fix "auth module"        # explicit chain
forge --chain upgrade:fix:test "Next.js 14"  # 3-stage chain
```

Supported combinations: `review:fix`, `fix:test`, `upgrade:fix`, `upgrade:fix:test`, `add:test`, `refactor:test`.

## Usage

```bash
# Run a task (intent auto-detected)
forge "<task description>"

# Research + plan first, confirm before running
forge plan "<task description>"

# Force a specific mode
forge --mode fix "<task description>"

# Use a specific backend
forge --backend gemini "<task description>"

# Auto-confirm all prompts (non-interactive / CI)
forge -y "<task description>"

# Resume an interrupted run
forge resume

# Check status of a running task
forge status

# List past runs
forge history

# Show details of a specific run
forge show <run-id>

# Stop a running task
forge stop
```

## Configuration

Forge uses a layered config system. Highest precedence first:

1. CLI flags (e.g. `--backend claude`)
2. Environment variables (e.g. `FORGE_BACKEND=claude`)
3. Project config: `.forge/config.yaml` in the working directory
4. User config: `~/.config/forge/config.yaml`
5. Built-in defaults

**Set your default backend:**
```bash
forge config set backend claude
```

**View / edit config:**
```bash
forge config get backend
forge config edit          # opens in $EDITOR
```

**Key config options:**

| Key | Default | Description |
|---|---|---|
| `backend` | — | Backend driver: `claude`, `kiro`, or `gemini` |
| `max_iterations` | 50 | Max loop iterations before stopping |
| `timeout` | 0 | Global timeout in seconds (0 = unlimited) |
| `auto_confirm` | false | Skip confirmation prompts |
| `subagents` | 3 | Parallel subagents for Review/Research |

## Human escalation

Forge escalates to you when it can't decide autonomously:

1. Forge writes `.forge/awaiting-human.md` with the question + options + recommendation
2. A loud in-terminal banner appears
3. OS notification fires (if available)
4. You write your answer to `.forge/answer.md`
5. Forge resumes automatically

You can also respond inline during a running task — press `Enter` to open the input prompt.

## Safety gates

Forge requires explicit confirmation before:

- Pushing to protected branches (`main`, `master`, etc.)
- Modifying CI/CD files (`.github/workflows/`, `Jenkinsfile`, etc.)
- Modifying dependency manifests (`package.json`, `go.mod`, etc.) — except in **Upgrade** mode where this is the point
- Touching production code in **Test** mode
- Any operation that detects secrets or credentials (via embedded gitleaks, 222 rules)
- External-facing actions (deploys, publishes, API calls)

## Diagnostics

```bash
forge doctor
```

Checks: `git` installed, you're in a git repo, at least one backend is on PATH, notification system probe, protected-branch detection.

## Single-run guarantee

Only one Forge instance can run per repo at a time. The lock is stored in `.forge/forge.lock` and uses PID + start-time to detect stale locks from crashed processes. If you see a stale lock error, run `forge clean` to remove it.

## Project layout (runtime)

```
.forge/
├── forge.lock          # single-run lock
├── runs/
│   └── <run-id>/
│       ├── state.md    # live loop state (auto-distilled when large)
│       ├── notes.md    # accumulated notes
│       ├── plan.md     # task plan
│       ├── run.json    # run metadata
│       └── iteration-NNN.json
├── awaiting-human.md   # escalation question (written by Forge)
└── answer.md           # your answer (you write this)
```

## License

MIT
