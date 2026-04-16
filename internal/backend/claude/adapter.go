package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/arturklasa/forge/internal/backend"
	"github.com/arturklasa/forge/internal/proc"
	"github.com/google/uuid"
)

const (
	defaultExecutable  = "claude"
	defaultTimeout     = 30 * time.Minute
	defaultGracePeriod = 15 * time.Second
)

// Adapter implements backend.Backend for the Claude Code CLI.
type Adapter struct {
	executable  string
	gracePeriod time.Duration
}

// Option configures an Adapter.
type Option func(*Adapter)

// WithExecutable overrides the path to the claude binary (useful in tests).
func WithExecutable(path string) Option {
	return func(a *Adapter) { a.executable = path }
}

// WithGracePeriod sets the SIGTERM→SIGKILL grace period.
func WithGracePeriod(d time.Duration) Option {
	return func(a *Adapter) { a.gracePeriod = d }
}

// New returns a new Adapter with the given options applied.
func New(opts ...Option) *Adapter {
	a := &Adapter{
		executable:  defaultExecutable,
		gracePeriod: defaultGracePeriod,
	}
	for _, o := range opts {
		o(a)
	}
	return a
}

// Name implements backend.Backend.
func (a *Adapter) Name() string { return "claude" }

// Capabilities implements backend.Backend.
func (a *Adapter) Capabilities() backend.Capabilities {
	return backend.Capabilities{
		StructuredOutput:    true,
		NativeSubagents:     true,
		SkipPermissionsFlag: "--permission-mode dontAsk",
		EffectiveWindow:     200_000,
	}
}

// Probe implements backend.Backend — verifies the claude binary is on PATH.
func (a *Adapter) Probe(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, a.executable, "--version")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("claude probe: %w", err)
	}
	version := strings.TrimSpace(string(out))
	if version == "" {
		return fmt.Errorf("claude --version returned empty output")
	}
	return nil
}

// RunIteration implements backend.Backend.
func (a *Adapter) RunIteration(ctx context.Context, prompt backend.Prompt, opts backend.IterationOpts) (backend.IterationResult, error) {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	sessionID := uuid.New().String()

	args := a.buildArgs(sessionID, opts)

	cmd := exec.CommandContext(ctx, a.executable, args...)

	// Feed prompt via stdin.
	promptBody, err := resolvePrompt(prompt)
	if err != nil {
		return backend.IterationResult{}, fmt.Errorf("resolve prompt: %w", err)
	}
	cmd.Stdin = strings.NewReader(promptBody)

	// Capture stdout separately for streaming; stderr goes to ring buffer.
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer

	wrapper := proc.New(cmd,
		proc.WithGracePeriod(a.gracePeriod),
		proc.WithStdoutWriter(&stdoutBuf),
		proc.WithStderrWriter(&stderrBuf),
	)

	if err := wrapper.Start(); err != nil {
		return backend.IterationResult{}, fmt.Errorf("start claude: %w", err)
	}

	result := wrapper.Wait()

	return parseStreamJSON(stdoutBuf.String(), stderrBuf.String(), result), nil
}

// buildArgs assembles the claude CLI argument list.
func (a *Adapter) buildArgs(sessionID string, opts backend.IterationOpts) []string {
	args := []string{
		"--bare",
		"-p",
		"--output-format", "stream-json",
		"--verbose",
		"--permission-mode", "dontAsk",
		"--session-id", sessionID,
		"--no-session-persistence",
	}

	if len(opts.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(opts.AllowedTools, ","))
	}

	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}

	if opts.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", opts.MaxTurns))
	}

	return args
}

// resolvePrompt returns the prompt body as a string.
func resolvePrompt(p backend.Prompt) (string, error) {
	if p.Body != "" {
		return p.Body, nil
	}
	if p.Path != "" {
		data, err := readFile(p.Path)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	return "", fmt.Errorf("prompt has no body and no path")
}

// readFile reads a file from disk. Replaced in tests via the Adapter's promptLoader field.
func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// parseStreamJSON scans the NDJSON stdout and builds an IterationResult.
func parseStreamJSON(stdout, stderr string, procResult proc.Result) backend.IterationResult {
	result := backend.IterationResult{
		ExitCode:  procResult.ExitCode,
		RawStdout: stdout,
		RawStderr: stderr,
	}

	if procResult.Classification == proc.ExitForgeTerminated {
		result.Error = fmt.Errorf("iteration terminated by forge (SIGTERM/SIGKILL)")
	} else if procResult.Classification == proc.ExitExternalSignal {
		result.Error = fmt.Errorf("iteration killed by external signal")
	}

	scanner := bufio.NewScanner(strings.NewReader(stdout))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}

		var typStr string
		if err := json.Unmarshal(ev["type"], &typStr); err != nil {
			continue
		}

		event := backend.Event{
			Type: typStr,
			At:   time.Now(),
		}

		if raw, ok := ev["subtype"]; ok {
			json.Unmarshal(raw, &event.Subtype) //nolint:errcheck
		}

		// Build generic payload map.
		payload := make(map[string]any)
		for k, v := range ev {
			var val any
			json.Unmarshal(v, &val) //nolint:errcheck
			payload[k] = val
		}
		event.Payload = payload
		result.Events = append(result.Events, event)

		switch typStr {
		case "assistant":
			text := extractAssistantText(ev)
			if text != "" {
				result.FinalText = text
			}

		case "result":
			applyResultEvent(&result, ev)
		}
	}

	return result
}

// extractAssistantText pulls the first text block from an assistant event.
func extractAssistantText(ev map[string]json.RawMessage) string {
	rawMsg, ok := ev["message"]
	if !ok {
		return ""
	}
	var msg struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(rawMsg, &msg); err != nil {
		return ""
	}
	for _, block := range msg.Content {
		if block.Type == "text" {
			return block.Text
		}
	}
	return ""
}

// applyResultEvent extracts the result event fields into the IterationResult.
func applyResultEvent(r *backend.IterationResult, ev map[string]json.RawMessage) {
	var subtype string
	if raw, ok := ev["subtype"]; ok {
		json.Unmarshal(raw, &subtype) //nolint:errcheck
	}

	var isError bool
	if raw, ok := ev["is_error"]; ok {
		json.Unmarshal(raw, &isError) //nolint:errcheck
	}

	// Override FinalText from result.result field if assistant event not seen.
	if raw, ok := ev["result"]; ok {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil && s != "" && r.FinalText == "" {
			r.FinalText = s
		}
	}

	switch subtype {
	case "success":
		// clean exit
	case "error_max_turns":
		r.Truncated = true
		r.Error = fmt.Errorf("claude: max turns reached")
	case "error_max_budget_usd":
		r.Truncated = true
		r.Error = fmt.Errorf("claude: budget limit reached")
	default:
		if isError {
			r.Error = fmt.Errorf("claude: error subtype %q", subtype)
		}
	}

	if raw, ok := ev["usage"]; ok {
		var usage struct {
			InputTokens               int `json:"input_tokens"`
			OutputTokens              int `json:"output_tokens"`
			CacheCreationInputTokens  int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens      int `json:"cache_read_input_tokens"`
		}
		if err := json.Unmarshal(raw, &usage); err == nil {
			r.TokensUsage = backend.TokenUsage{
				Input:      usage.InputTokens,
				Output:     usage.OutputTokens,
				CacheRead:  usage.CacheReadInputTokens,
				CacheWrite: usage.CacheCreationInputTokens,
			}
		}
	}
}
