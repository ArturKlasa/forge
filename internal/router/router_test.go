package router

import (
	"context"
	"testing"
)

func TestKeywordFastPath(t *testing.T) {
	tests := []struct {
		task string
		want Path
	}{
		{"Fix the login redirect bug", PathFix},
		{"fix the login redirect bug", PathFix},
		{"Create a hello-world Go CLI", PathCreate},
		{"add a payment endpoint", PathAdd},
		{"refactor the auth module", PathRefactor},
		{"upgrade all dependencies", PathUpgrade},
		{"test the login flow", PathTest},
		{"review the security model", PathReview},
		{"document the API", PathDocument},
		{"explain how the auth works", PathExplain},
		{"research the best logging library", PathResearch},
		{"build a new CLI", PathCreate},
		{"generate scaffolding", PathCreate},
		{"debug the crash", PathFix},
		{"resolve the CORS issue", PathFix},
		{"restructure the package layout", PathRefactor},
		{"migrate to Postgres", PathUpgrade},
		{"cover the uncovered functions", PathTest},
		{"audit the codebase", PathReview},
		{"describe the data model", PathDocument},
		{"investigate memory leaks", PathResearch},
	}
	r := New()
	ctx := context.Background()
	for _, tt := range tests {
		res, err := r.Route(ctx, tt.task)
		if err != nil {
			t.Errorf("%q: unexpected error: %v", tt.task, err)
			continue
		}
		if res.Path != tt.want {
			t.Errorf("%q: got path %q, want %q", tt.task, res.Path, tt.want)
		}
		if res.Confidence != ConfidenceHigh {
			t.Errorf("%q: got confidence %q, want high", tt.task, res.Confidence)
		}
		if res.Method != "keyword" {
			t.Errorf("%q: got method %q, want keyword", tt.task, res.Method)
		}
	}
}

func TestPathOverride(t *testing.T) {
	r := New(WithPathOverride(PathCreate))
	ctx := context.Background()

	// Even an unambiguous "fix" verb should be overridden.
	res, err := r.Route(ctx, "fix the login bug")
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != PathCreate {
		t.Errorf("got %q, want create", res.Path)
	}
	if res.Method != "override" {
		t.Errorf("got method %q, want override", res.Method)
	}
}

func TestUnknownVerbNoBackend(t *testing.T) {
	r := New()
	ctx := context.Background()

	res, err := r.Route(ctx, "obliterate the monolith")
	if err != nil {
		t.Fatal(err)
	}
	if !res.NeedsHumanEscalation {
		t.Error("expected NeedsHumanEscalation=true for unknown verb without backend")
	}
}

func TestChainDetection(t *testing.T) {
	tests := []struct {
		task       string
		wantChain  string
		predefined bool
	}{
		{"Review and fix the auth module", "review:fix", true},
		{"review AND fix the login bug", "review:fix", true},
		{"upgrade and test the dependencies", "upgrade:test", true},
		{"refactor and test the parser", "refactor:test", true},
	}
	r := New()
	ctx := context.Background()
	for _, tt := range tests {
		res, err := r.Route(ctx, tt.task)
		if err != nil {
			t.Errorf("%q: unexpected error: %v", tt.task, err)
			continue
		}
		if !res.IsChain {
			t.Errorf("%q: expected IsChain=true, got false (path=%q)", tt.task, res.Path)
			continue
		}
		if res.ChainKey != tt.wantChain {
			t.Errorf("%q: got chain %q, want %q", tt.task, res.ChainKey, tt.wantChain)
		}
		if res.Predefined != tt.predefined {
			t.Errorf("%q: got predefined=%v, want %v", tt.task, res.Predefined, tt.predefined)
		}
	}
}

func TestChainNotPredefined(t *testing.T) {
	r := New()
	ctx := context.Background()

	// "create and document" is not in predefined chains but both verbs are known.
	res, err := r.Route(ctx, "create and document the new API")
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsChain {
		t.Fatalf("expected IsChain=true")
	}
	if res.Predefined {
		t.Error("expected Predefined=false for create:document")
	}
}

func TestLLMClassifierLowConfidence(t *testing.T) {
	r := New(WithBackend(&stubBackend{text: "path=fix\nconfidence=low"}))
	ctx := context.Background()

	res, err := r.Route(ctx, "obliterate the monolith")
	if err != nil {
		t.Fatal(err)
	}
	if !res.NeedsHumanEscalation {
		t.Error("expected NeedsHumanEscalation=true for low confidence")
	}
	if res.Recommendation != "fix" {
		t.Errorf("got recommendation %q, want fix", res.Recommendation)
	}
}

func TestLLMClassifierHighConfidence(t *testing.T) {
	r := New(WithBackend(&stubBackend{text: "path=fix\nconfidence=high"}))
	ctx := context.Background()

	res, err := r.Route(ctx, "obliterate the monolith")
	if err != nil {
		t.Fatal(err)
	}
	if res.NeedsHumanEscalation {
		t.Error("expected NeedsHumanEscalation=false for high confidence")
	}
	if res.Path != PathFix {
		t.Errorf("got path %q, want fix", res.Path)
	}
}

func TestClassifyLLMResponseParsing(t *testing.T) {
	tests := []struct {
		input    string
		wantPath Path
		wantConf Confidence
		wantOK   bool
	}{
		{"path=fix\nconfidence=high", PathFix, ConfidenceHigh, true},
		{"path=create\nconfidence=medium", PathCreate, ConfidenceMedium, true},
		{"path=research\nconfidence=low", PathResearch, ConfidenceLow, true},
		{"no fields here", "", "", false},
		{"path=review", "", "", false}, // missing confidence
	}
	for _, tt := range tests {
		p, c, ok := classifyLLMResponse(tt.input)
		if ok != tt.wantOK {
			t.Errorf("input %q: got ok=%v want %v", tt.input, ok, tt.wantOK)
			continue
		}
		if ok && p != tt.wantPath {
			t.Errorf("input %q: got path %q want %q", tt.input, p, tt.wantPath)
		}
		if ok && c != tt.wantConf {
			t.Errorf("input %q: got conf %q want %q", tt.input, c, tt.wantConf)
		}
	}
}
