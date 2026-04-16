# Forge v1 Implementation Scratchpad

## 2026-04-16 — Iteration 1

### Current Focus: Step 1 — Project scaffolding + CLI skeleton

**Understanding:**
- All 25 steps unchecked; this is the first iteration
- Need to create a complete Go CLI with cobra, all subcommand stubs, global flags
- Module path: `github.com/arturklasa/forge` (using ArturKlasa git user)
- Directory layout: `cmd/forge/main.go`, `internal/cli/`, `internal/version/`, `testdata/`
- All commands return "not implemented yet (scheduled for step N)" errors
- Need golangci-lint config + Makefile

**Plan:**
1. Create directory structure
2. Initialize Go module
3. Write version package
4. Write cobra command tree with all stubs
5. Write tests (smoke tests per command)
6. Set up golangci-lint config
7. Set up Makefile
8. Run tests + verify demo works
9. Commit

## 2026-04-16 — Iteration 2

### Completed: Step 2 — Logger (slog + lipgloss) + terminal output modes

**What was done:**
- Created `internal/log/log.go`: Logger with slog text/JSON handlers, lipgloss renderer
- Global singleton via `Init(Config)` + `G()` accessor
- Strips timestamps in text mode via ReplaceAttr
- JSON mode routes slog to UserOut (stdout) for machine parsing
- NO_COLOR, CI, non-TTY all disable interactive/color mode
- PersistentPreRunE in root.go wires flags → log.Init before any subcommand
- Status stub now logs "status requested" via slog
- 12 tests all passing

**Demo verified:**
- `forge --json status` → valid NDJSON to stdout
- `NO_COLOR=1 forge --help` → no ANSI codes

## 2026-04-16 — Iteration 3

### Completed: Step 3 — Config system (koanf) with layered precedence

**What was done:**
- Created `internal/config/config.go`: full Config schema (all §5.2 fields with koanf+yaml tags), Manager type, Load() with 4-layer precedence (defaults → global → repo → env), goyamlParser using goccy/go-yaml, mapProvider using koanf/maps Unflatten
- Created `internal/config/write.go`: SetKey/UnsetKey for writing nested YAML key updates to target files
- Added InitWithHandler to `internal/log` for test log capture
- Wired `config`, `config get/set/unset/edit` commands in commands.go - fully functional
- Wired `backend set` command using config system
- 7 config tests pass + CLI tests updated (removed now-implemented commands from stubs test, added TestConfigCommands)
- CLI loads config via --path flag for test isolation

**Demo verified:**
- `forge config set backend.default claude` → sets key in repo config
- `forge config get backend.default` → `claude`
- `forge config` → prints full merged YAML with all defaults
