# Changelog

All notable changes to Forge are documented here.

## [Unreleased]

### Added
- Full Forge v1 implementation: 25-step plan complete
- 10 modes: Create, Add, Fix, Refactor, Upgrade, Test (loop), Review, Document, Explain, Research (one-shot)
- Composite chaining (review:fix, upgrade:fix:test, etc.)
- Intent router with keyword + LLM classification
- Plan phase with AI-assisted research subagents
- Loop engine with stuck detection, completion detection, and context management
- Policy scanners: secrets (gitleaks), placeholders, and file-path gates
- Two-file mailbox escalation with fsnotify watch
- 5-channel notification cascade (file sentinel, ASCII banner, OSC9, tmux, beep)
- Brain primitives: Classify, Judge, Distill, Diagnose, Draft, Spawn
- Context manager with automatic distillation (state.md / notes.md / plan.md)
- Claude Code, Gemini CLI, and Kiro (ACP JSON-RPC) backend adapters
- First-run onboarding with PATH scanning for backends
- forge doctor with 6 health checks
- CI matrix for Linux/macOS/Windows × amd64/arm64
- GitHub Releases with cross-platform binaries and SHA256 checksums
- curl installer script
- Homebrew tap support
- go install support
