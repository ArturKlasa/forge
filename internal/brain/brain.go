// Package brain implements Forge's internal LLM primitives for meta-tasks.
// All calls go through the configured backend CLI.
package brain

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/arturklasa/forge/internal/backend"
)

// Brain holds a backend reference for all primitive calls.
type Brain struct {
	Backend backend.Backend
	// Timeout is the per-call deadline (default 120s).
	Timeout time.Duration
}

// New creates a Brain backed by b.
func New(b backend.Backend) *Brain {
	return &Brain{Backend: b, Timeout: 120 * time.Second}
}

func (br *Brain) timeout() time.Duration {
	if br.Timeout <= 0 {
		return 120 * time.Second
	}
	return br.Timeout
}

// run calls the backend with the given body, retrying once on parse failure.
// parse is called with the raw FinalText; on error, a correction prompt is sent.
func (br *Brain) run(ctx context.Context, body string, parse func(string) error) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, br.timeout())
	defer cancel()

	res, err := br.Backend.RunIteration(ctx, backend.Prompt{Body: body}, backend.IterationOpts{})
	if err != nil {
		return "", fmt.Errorf("brain call: %w", err)
	}
	text := res.FinalText

	if parseErr := parse(text); parseErr == nil {
		return text, nil
	}

	// Retry with correction.
	correction := body + "\n\nYour previous response could not be parsed. Please respond ONLY with the exact format described, nothing else."
	res2, err2 := br.Backend.RunIteration(ctx, backend.Prompt{Body: correction}, backend.IterationOpts{})
	if err2 != nil {
		return text, nil // return original text; caller handles
	}
	return res2.FinalText, nil
}

// ClassifyResult is the output of Classify.
type ClassifyResult struct {
	Category   string
	Confidence string // "low", "medium", "high"
}

// Classify maps input to one of the provided categories with a confidence level.
func (br *Brain) Classify(ctx context.Context, input string, categories []string) (ClassifyResult, error) {
	body := fmt.Sprintf(`You are a classification assistant.
Task: classify the following input into exactly one of these categories: %s

Input: %s

Respond with exactly two lines:
category=<one of the listed categories>
confidence=<low|medium|high>`, strings.Join(categories, ", "), input)

	var out ClassifyResult
	parse := func(text string) error {
		cat, ok1 := extractField(text, "category")
		conf, ok2 := extractField(text, "confidence")
		if !ok1 || !ok2 {
			return fmt.Errorf("missing fields")
		}
		out.Category = cat
		out.Confidence = conf
		return nil
	}

	text, err := br.run(ctx, body, parse)
	if err != nil {
		return ClassifyResult{}, err
	}
	_ = parse(text)
	return out, nil
}

// JudgeVerdict describes the completion assessment.
type JudgeVerdict string

const (
	VerdictComplete   JudgeVerdict = "complete"
	VerdictIncomplete JudgeVerdict = "incomplete"
	VerdictAudit      JudgeVerdict = "audit"
)

// JudgeResult is the output of Judge.
type JudgeResult struct {
	Verdict    JudgeVerdict
	Confidence string // "low", "medium", "high"
	Rationale  string
}

// Judge assesses whether a task is complete given the current state and diff.
func (br *Brain) Judge(ctx context.Context, task, state, diff string) (JudgeResult, error) {
	body := fmt.Sprintf(`You are a code review assistant assessing task completion.

Task description:
%s

Current state summary:
%s

Recent diff:
%s

Respond with exactly three lines:
verdict=<complete|incomplete|audit>
confidence=<low|medium|high>
rationale=<one sentence>`, task, state, diff)

	var out JudgeResult
	parse := func(text string) error {
		v, ok1 := extractField(text, "verdict")
		c, ok2 := extractField(text, "confidence")
		r, ok3 := extractField(text, "rationale")
		if !ok1 || !ok2 || !ok3 {
			return fmt.Errorf("missing fields")
		}
		out.Verdict = JudgeVerdict(v)
		out.Confidence = c
		out.Rationale = r
		return nil
	}

	text, err := br.run(ctx, body, parse)
	if err != nil {
		return JudgeResult{}, err
	}
	_ = parse(text)
	return out, nil
}

// Distill compresses source to approximately targetTokens tokens.
func (br *Brain) Distill(ctx context.Context, source string, targetTokens int) (string, error) {
	body := fmt.Sprintf(`You are a technical summarizer. Compress the following content to approximately %d tokens.
Keep all factual details, decisions, and current state. Remove redundancy and verbose prose.
Respond with ONLY the compressed content — no preamble, no explanation.

Content to compress:
%s`, targetTokens, source)

	var out string
	parse := func(text string) error {
		if strings.TrimSpace(text) == "" {
			return fmt.Errorf("empty response")
		}
		out = strings.TrimSpace(text)
		return nil
	}

	text, err := br.run(ctx, body, parse)
	if err != nil {
		return "", err
	}
	_ = parse(text)
	return out, nil
}

// DiagnoseResult is the output of Diagnose.
type DiagnoseResult struct {
	Diagnosis  string
	Suggestion string
}

// Diagnose analyses a ledger window and state to explain why the agent is stuck.
func (br *Brain) Diagnose(ctx context.Context, ledgerWindow, state string) (DiagnoseResult, error) {
	body := fmt.Sprintf(`You are an AI orchestration debugger. Analyse why an AI agent appears to be stuck.

Recent iteration ledger (last few entries):
%s

Current state:
%s

Respond with exactly two lines:
diagnosis=<one sentence describing what's wrong>
suggestion=<one concrete action to unblock the agent>`, ledgerWindow, state)

	var out DiagnoseResult
	parse := func(text string) error {
		d, ok1 := extractField(text, "diagnosis")
		s, ok2 := extractField(text, "suggestion")
		if !ok1 || !ok2 {
			return fmt.Errorf("missing fields")
		}
		out.Diagnosis = d
		out.Suggestion = s
		return nil
	}

	text, err := br.run(ctx, body, parse)
	if err != nil {
		return DiagnoseResult{}, err
	}
	_ = parse(text)
	return out, nil
}

// Draft generates text for the given purpose with the provided context.
func (br *Brain) Draft(ctx context.Context, purpose, draftCtx string) (string, error) {
	body := fmt.Sprintf(`You are a technical writer. Generate %s.

Context:
%s

Respond with ONLY the requested content — no preamble, no explanation.`, purpose, draftCtx)

	var out string
	parse := func(text string) error {
		if strings.TrimSpace(text) == "" {
			return fmt.Errorf("empty response")
		}
		out = strings.TrimSpace(text)
		return nil
	}

	text, err := br.run(ctx, body, parse)
	if err != nil {
		return "", err
	}
	_ = parse(text)
	return out, nil
}

// SpawnResult is the output of a Spawn call.
type SpawnResult struct {
	Output string
}

// Spawn runs a scoped subagent prompt and returns its output.
func (br *Brain) Spawn(ctx context.Context, prompt, scope string) (SpawnResult, error) {
	body := fmt.Sprintf(`You are a subagent working within the following scope: %s

%s

Respond with your findings. Be concise and focused on the scope.`, scope, prompt)

	var out SpawnResult
	parse := func(text string) error {
		if strings.TrimSpace(text) == "" {
			return fmt.Errorf("empty response")
		}
		out.Output = strings.TrimSpace(text)
		return nil
	}

	text, err := br.run(ctx, body, parse)
	if err != nil {
		return SpawnResult{}, err
	}
	_ = parse(text)
	return out, nil
}

var fieldRe = regexp.MustCompile(`(?m)^(\w+)=(.+)$`)

// extractField extracts the value of key=value from text.
func extractField(text, key string) (string, bool) {
	for _, m := range fieldRe.FindAllStringSubmatch(text, -1) {
		if m[1] == key {
			return strings.TrimSpace(m[2]), true
		}
	}
	return "", false
}
