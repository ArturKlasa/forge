package kiro

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
	defaultExecutable  = "kiro-cli"
	defaultTimeout     = 30 * time.Minute
	defaultGracePeriod = 15 * time.Second
	creditsMarker      = "\u25b8 Credits:" // ▸ Credits:
)

// Mode selects the Kiro invocation strategy.
type Mode int

const (
	// ModeACP uses kiro-cli acp (JSON-RPC 2.0 over stdio) — preferred.
	ModeACP Mode = iota
	// ModeText uses kiro-cli chat --no-interactive (text output with ▸ Credits: footer).
	ModeText
)

// Adapter implements backend.Backend for the Kiro CLI.
type Adapter struct {
	executable  string
	gracePeriod time.Duration
	mode        Mode
	model       string
}

// Option configures an Adapter.
type Option func(*Adapter)

// WithExecutable overrides the path to the kiro-cli binary.
func WithExecutable(path string) Option {
	return func(a *Adapter) { a.executable = path }
}

// WithGracePeriod sets the SIGTERM→SIGKILL grace period.
func WithGracePeriod(d time.Duration) Option {
	return func(a *Adapter) { a.gracePeriod = d }
}

// WithMode sets the invocation mode (ACP or Text).
func WithMode(m Mode) Option {
	return func(a *Adapter) { a.mode = m }
}

// WithModel sets a default model override.
func WithModel(model string) Option {
	return func(a *Adapter) { a.model = model }
}

// New returns a new Adapter with the given options applied.
// Defaults to ACP mode.
func New(opts ...Option) *Adapter {
	a := &Adapter{
		executable:  defaultExecutable,
		gracePeriod: defaultGracePeriod,
		mode:        ModeACP,
	}
	for _, o := range opts {
		o(a)
	}
	return a
}

// Name implements backend.Backend.
func (a *Adapter) Name() string { return "kiro" }

// Capabilities implements backend.Backend.
func (a *Adapter) Capabilities() backend.Capabilities {
	return backend.Capabilities{
		StructuredOutput:    a.mode == ModeACP,
		NativeSubagents:     false,
		SkipPermissionsFlag: "--trust-all-tools",
		EffectiveWindow:     1_000_000,
	}
}

// Probe implements backend.Backend — verifies the kiro-cli binary is on PATH.
func (a *Adapter) Probe(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, a.executable, "--version")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("kiro probe: %w", err)
	}
	if strings.TrimSpace(string(out)) == "" {
		return fmt.Errorf("kiro-cli --version returned empty output")
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

	switch a.mode {
	case ModeACP:
		return a.runACP(ctx, promptBody, opts)
	case ModeText:
		return a.runText(ctx, promptBody, opts)
	default:
		return backend.IterationResult{}, fmt.Errorf("unknown kiro mode %d", a.mode)
	}
}

// runACP drives kiro-cli acp (JSON-RPC 2.0 over stdio).
func (a *Adapter) runACP(ctx context.Context, promptBody string, opts backend.IterationOpts) (backend.IterationResult, error) {
	model := opts.Model
	if model == "" {
		model = a.model
	}

	args := []string{"acp"}
	if model != "" {
		args = append(args, "--model", model)
	}

	cmd := exec.CommandContext(ctx, a.executable, args...)

	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer

	// Pipe stdin for JSON-RPC 2.0 messages.
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		return backend.IterationResult{}, fmt.Errorf("kiro acp: create pipe: %w", err)
	}
	cmd.Stdin = stdinR

	wrapper := proc.New(cmd,
		proc.WithGracePeriod(a.gracePeriod),
		proc.WithStdoutWriter(&stdoutBuf),
		proc.WithStderrWriter(&stderrBuf),
	)

	if err := wrapper.Start(); err != nil {
		stdinR.Close()
		stdinW.Close()
		return backend.IterationResult{}, fmt.Errorf("start kiro acp: %w", err)
	}
	stdinR.Close() // child owns the read end

	// Execute JSON-RPC 2.0 handshake.
	rpcResult, rpcErr := a.doACPSession(stdinW, &stdoutBuf, promptBody)
	stdinW.Close()

	procResult := wrapper.Wait()

	if rpcErr != nil {
		return backend.IterationResult{
			ExitCode:  procResult.ExitCode,
			RawStderr: stderrBuf.String(),
			Error:     rpcErr,
		}, nil
	}

	rpcResult.ExitCode = procResult.ExitCode
	rpcResult.RawStderr = stderrBuf.String()

	if procResult.Classification == proc.ExitForgeTerminated {
		rpcResult.Error = fmt.Errorf("iteration terminated by forge (SIGTERM/SIGKILL)")
	} else if procResult.Classification == proc.ExitExternalSignal {
		rpcResult.Error = fmt.Errorf("iteration killed by external signal")
	} else if procResult.ExitCode != 0 && rpcResult.Error == nil {
		rpcResult.Error = fmt.Errorf("kiro acp: exited with code %d", procResult.ExitCode)
	}

	return rpcResult, nil
}

// doACPSession writes JSON-RPC requests to w and reads from out as they accumulate.
// Returns when session/prompt completes or an error occurs.
func (a *Adapter) doACPSession(w *os.File, out *bytes.Buffer, promptBody string) (backend.IterationResult, error) {
	var result backend.IterationResult

	id := 1
	send := func(method string, params any) error {
		req := map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"method":  method,
			"params":  params,
		}
		id++
		b, err := json.Marshal(req)
		if err != nil {
			return err
		}
		b = append(b, '\n')
		_, err = w.Write(b)
		return err
	}

	// 1. initialize
	if err := send("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "forge", "version": "0.1.0"},
	}); err != nil {
		return result, fmt.Errorf("kiro acp initialize: %w", err)
	}

	// 2. session/new
	if err := send("session/new", map[string]any{}); err != nil {
		return result, fmt.Errorf("kiro acp session/new: %w", err)
	}

	// Read initialize + session/new responses to get session ID.
	sessionID, err := a.readACPSessionID(out)
	if err != nil {
		return result, fmt.Errorf("kiro acp: read session ID: %w", err)
	}

	// 3. session/prompt
	if err := send("session/prompt", map[string]any{
		"sessionId": sessionID,
		"message": map[string]any{
			"role":    "user",
			"content": promptBody,
		},
	}); err != nil {
		return result, fmt.Errorf("kiro acp session/prompt: %w", err)
	}

	// Read session/prompt response.
	if err := a.readACPPromptResult(out, &result); err != nil {
		return result, fmt.Errorf("kiro acp: read prompt result: %w", err)
	}

	result.CompletionSentinel = strings.Contains(result.FinalText, "TASK_COMPLETE")
	return result, nil
}

// readACPSessionID reads buffered stdout lines until it finds session/new result with id.
func (a *Adapter) readACPSessionID(out *bytes.Buffer) (string, error) {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		scanner := bufio.NewScanner(bytes.NewReader(out.Bytes()))
		var newID string
		lineCount := 0
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			lineCount++
			var resp map[string]json.RawMessage
			if err := json.Unmarshal([]byte(line), &resp); err != nil {
				continue
			}
			if resultRaw, ok := resp["result"]; ok {
				var r map[string]string
				if err := json.Unmarshal(resultRaw, &r); err == nil {
					if id, ok := r["id"]; ok && id != "" {
						newID = id
					}
				}
			}
		}
		if newID != "" {
			return newID, nil
		}
		if lineCount >= 2 {
			return "", fmt.Errorf("session/new response missing id field")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return "", fmt.Errorf("timeout waiting for session ID")
}

// readACPPromptResult reads until the session/prompt response arrives.
func (a *Adapter) readACPPromptResult(out *bytes.Buffer, result *backend.IterationResult) error {
	deadline := time.Now().Add(30 * time.Minute)
	seen := 0
	for time.Now().Before(deadline) {
		data := out.Bytes()
		scanner := bufio.NewScanner(bytes.NewReader(data))
		lineIdx := 0
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			lineIdx++
			if lineIdx <= seen || line == "" {
				continue
			}
			seen = lineIdx

			var resp map[string]json.RawMessage
			if err := json.Unmarshal([]byte(line), &resp); err != nil {
				continue
			}
			if resp["error"] != nil {
				var apiErr struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				}
				if err := json.Unmarshal(resp["error"], &apiErr); err == nil {
					result.Error = fmt.Errorf("kiro acp error %d: %s", apiErr.Code, apiErr.Message)
					return nil
				}
			}
			resultRaw, hasResult := resp["result"]
			if !hasResult {
				continue
			}
			var r struct {
				Content    string `json:"content"`
				StopReason string `json:"stopReason"`
				Usage      struct {
					InputTokens  int `json:"inputTokens"`
					OutputTokens int `json:"outputTokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal(resultRaw, &r); err != nil {
				continue
			}
			if r.Content != "" || r.StopReason != "" {
				result.FinalText = r.Content
				result.TokensUsage = backend.TokenUsage{
					Input:  r.Usage.InputTokens,
					Output: r.Usage.OutputTokens,
					Model:  "kiro",
				}
				result.Events = append(result.Events, backend.Event{
					Type:    "session/prompt",
					Subtype: r.StopReason,
					At:      time.Now(),
				})
				return nil
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for session/prompt response")
}

// runText drives kiro-cli chat --no-interactive <prompt>.
// Completion is detected via the ▸ Credits: footer.
func (a *Adapter) runText(ctx context.Context, promptBody string, opts backend.IterationOpts) (backend.IterationResult, error) {
	args := []string{"chat", "--no-interactive", "--trust-all-tools", promptBody}
	cmd := exec.CommandContext(ctx, a.executable, args...)

	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer

	wrapper := proc.New(cmd,
		proc.WithGracePeriod(a.gracePeriod),
		proc.WithStdoutWriter(&stdoutBuf),
		proc.WithStderrWriter(&stderrBuf),
	)

	if err := wrapper.Start(); err != nil {
		return backend.IterationResult{}, fmt.Errorf("start kiro text: %w", err)
	}

	procResult := wrapper.Wait()
	return parseKiroText(stdoutBuf.String(), stderrBuf.String(), procResult), nil
}

// parseKiroText builds an IterationResult from kiro-cli text output.
func parseKiroText(stdout, stderr string, procResult proc.Result) backend.IterationResult {
	result := backend.IterationResult{
		ExitCode:  procResult.ExitCode,
		RawStdout: stdout,
		RawStderr: stderr,
	}

	if procResult.Classification == proc.ExitForgeTerminated {
		result.Error = fmt.Errorf("iteration terminated by forge (SIGTERM/SIGKILL)")
	} else if procResult.Classification == proc.ExitExternalSignal {
		result.Error = fmt.Errorf("iteration killed by external signal")
	} else if procResult.ExitCode != 0 {
		result.Error = fmt.Errorf("kiro: exited with code %d", procResult.ExitCode)
	}

	// Split on the ▸ Credits: marker — everything before is the response.
	parts := strings.SplitN(stdout, creditsMarker, 2)
	body := strings.TrimSpace(parts[0])

	if len(parts) < 2 {
		// No credits marker — treat full output as response (degraded mode).
		result.FinalText = body
		return result
	}

	result.FinalText = body
	result.CompletionSentinel = strings.Contains(body, "TASK_COMPLETE")
	result.Events = append(result.Events, backend.Event{
		Type: "credits",
		At:   time.Now(),
	})

	return result
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
