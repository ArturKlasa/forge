package router

import (
	"context"
	"os"

	"github.com/arturklasa/forge/internal/backend"
)

// stubBackend is a test double that returns canned FinalText.
type stubBackend struct {
	text string
	err  error
}

func (s *stubBackend) Name() string { return "stub" }

func (s *stubBackend) Capabilities() backend.Capabilities {
	return backend.Capabilities{}
}

func (s *stubBackend) RunIteration(_ context.Context, prompt backend.Prompt, _ backend.IterationOpts) (backend.IterationResult, error) {
	if s.err != nil {
		return backend.IterationResult{}, s.err
	}
	// Clean up any temp file the router wrote.
	if prompt.Path != "" {
		os.Remove(prompt.Path)
	}
	return backend.IterationResult{FinalText: s.text}, nil
}

func (s *stubBackend) Probe(_ context.Context) error { return nil }
