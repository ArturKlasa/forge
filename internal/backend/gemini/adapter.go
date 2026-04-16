package gemini

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
)

const (
	defaultExecutable  = "gemini"
	defaultTimeout     = 30 * time.Minute
	defaultGracePeriod = 15 * time.Second
)

// Adapter implements backend.Backend for the Gemini CLI.
type Adapter struct {
	executable  string
	gracePeriod time.Duration
}

// Option configures an Adapter.
type Option func(*Adapter)

// WithExecutable overrides the path to the gemini binary (useful in tests).
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
func (a *Adapter) Name() string { return "gemini" }

// Capabilities implements backend.Backend.
func (a *Adapter) Capabilities() backend.Capabilities {
	return backend.Capabilities{
		StructuredOutput:    true,
		NativeSubagents:     false,
		SkipPermissionsFlag: "--approval-mode=yolo",
		EffectiveWindow:     1_048_576,
	}
}

// Probe implements backend.Backend — verifies the gemini binary is on PATH.
func (a *Adapter) Probe(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, a.executable, "--version")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("gemini probe: %w", err)
	}
	if strings.TrimSpace(string(out)) == "" {
		return fmt.Errorf("gemini --version returned empty output")
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

	promptBody, err := resolvePrompt(prompt)
	if err != nil {
		return backend.IterationResult{}, fmt.Errorf("resolve prompt: %w", err)
	}

	args := a.buildArgs(opts, promptBody)
	cmd := exec.CommandContext(ctx, a.executable, args...)

	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer

	wrapper := proc.New(cmd,
		proc.WithGracePeriod(a.gracePeriod),
		proc.WithStdoutWriter(&stdoutBuf),
		proc.WithStderrWriter(&stderrBuf),
	)

	if err := wrapper.Start(); err != nil {
		return backend.IterationResult{}, fmt.Errorf("start gemini: %w", err)
	}

	result := wrapper.Wait()
	return parseGeminiStreamJSON(stdoutBuf.String(), stderrBuf.String(), result), nil
}

// buildArgs assembles the gemini CLI argument list.
// Prompt is passed via -p flag (Gemini headless mode requires it per GH issue #16025).
func (a *Adapter) buildArgs(opts backend.IterationOpts, promptBody string) []string {
	args := []string{
		"-p", promptBody,
		"-o", "stream-json",
		"--approval-mode=yolo",
		"-s",
	}
	if opts.Model != "" {
		args = append(args, "-m", opts.Model)
	}
	return args
}

// resolvePrompt returns the prompt body as a string.
func resolvePrompt(p backend.Prompt) (string, error) {
	if p.Body != "" {
		return p.Body, nil
	}
	if p.Path != "" {
		data, err := os.ReadFile(p.Path)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	return "", fmt.Errorf("prompt has no body and no path")
}

// parseGeminiStreamJSON scans Gemini NDJSON stdout and builds an IterationResult.
func parseGeminiStreamJSON(stdout, stderr string, procResult proc.Result) backend.IterationResult {
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

	// Map Gemini exit codes to error conditions.
	switch procResult.ExitCode {
	case 53:
		result.Truncated = true
		if result.Error == nil {
			result.Error = fmt.Errorf("gemini: turn limit exceeded (exit 53)")
		}
	case 42:
		if result.Error == nil {
			result.Error = fmt.Errorf("gemini: input error (exit 42)")
		}
	case 1:
		if result.Error == nil {
			result.Error = fmt.Errorf("gemini: general error (exit 1)")
		}
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
		payload := make(map[string]any)
		for k, v := range ev {
			var val any
			json.Unmarshal(v, &val) //nolint:errcheck
			payload[k] = val
		}
		event.Payload = payload
		result.Events = append(result.Events, event)

		switch typStr {
		case "message":
			var text string
			if raw, ok := ev["text"]; ok {
				json.Unmarshal(raw, &text) //nolint:errcheck
			}
			if text != "" {
				result.FinalText = text
			}

		case "result":
			applyGeminiResultEvent(&result, ev)
		}
	}

	return result
}

// applyGeminiResultEvent extracts stats and error info from the Gemini result event.
func applyGeminiResultEvent(r *backend.IterationResult, ev map[string]json.RawMessage) {
	// Check for error in result event.
	if raw, ok := ev["error"]; ok {
		var apiErr struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Type    string `json:"type"`
		}
		if err := json.Unmarshal(raw, &apiErr); err == nil && apiErr.Type != "" {
			if apiErr.Type == "turn_limit" {
				r.Truncated = true
				r.Error = fmt.Errorf("gemini: turn limit exceeded")
			} else if r.Error == nil {
				r.Error = fmt.Errorf("gemini: %s (%s)", apiErr.Type, apiErr.Message)
			}
		}
	}

	// Extract token stats: stats.models.<model>.tokens.{prompt,candidates}
	if raw, ok := ev["stats"]; ok {
		var stats struct {
			Models map[string]struct {
				Tokens struct {
					Prompt     int `json:"prompt"`
					Candidates int `json:"candidates"`
					Total      int `json:"total"`
					Cached     int `json:"cached"`
				} `json:"tokens"`
			} `json:"models"`
		}
		if err := json.Unmarshal(raw, &stats); err == nil {
			for model, ms := range stats.Models {
				r.TokensUsage = backend.TokenUsage{
					Input:     ms.Tokens.Prompt,
					Output:    ms.Tokens.Candidates,
					CacheRead: ms.Tokens.Cached,
					Model:     model,
				}
				break // use first model
			}
		}
	}
}
