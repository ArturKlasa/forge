package backend

import (
	"context"
	"time"
)

// Backend abstracts over Claude Code / Gemini / Kiro. One interface, three implementations.
type Backend interface {
	Name() string
	Capabilities() Capabilities
	RunIteration(ctx context.Context, prompt Prompt, opts IterationOpts) (IterationResult, error)
	Probe(ctx context.Context) error
}

// Capabilities describes what a backend supports.
type Capabilities struct {
	StructuredOutput    bool
	NativeSubagents     bool
	SkipPermissionsFlag string
	EffectiveWindow     int
}

// Prompt is the input to a single iteration.
type Prompt struct {
	Path       string // filesystem path to prompt.md
	Body       string // or inline body
	SystemHint string // role/persona hint
}

// IterationOpts controls a single backend iteration.
type IterationOpts struct {
	Model         string
	Timeout       time.Duration
	AllowedTools  []string
	DenyAllOthers bool
	MaxTurns      int
}

// IterationResult is the output from a single backend iteration.
type IterationResult struct {
	ExitCode           int
	Events             []Event
	RawStdout          string
	RawStderr          string
	FinalText          string
	TokensUsage        TokenUsage
	CompletionSentinel bool // "TASK_COMPLETE" detected
	Truncated          bool
	Error              error
}

// Event is a parsed stream event from a backend.
type Event struct {
	Type    string
	Subtype string
	Payload map[string]any
	At      time.Time
}

// TokenUsage tracks token consumption for context-budget purposes.
type TokenUsage struct {
	Input      int
	Output     int
	CacheRead  int
	CacheWrite int
	Model      string
}
