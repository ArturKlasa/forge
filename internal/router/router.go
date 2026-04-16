package router

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/arturklasa/forge/internal/backend"
)

// Path names all 10 supported modes.
type Path string

const (
	PathCreate   Path = "create"
	PathAdd      Path = "add"
	PathFix      Path = "fix"
	PathRefactor Path = "refactor"
	PathUpgrade  Path = "upgrade"
	PathTest     Path = "test"
	PathReview   Path = "review"
	PathDocument Path = "document"
	PathExplain  Path = "explain"
	PathResearch Path = "research"
)

// Confidence describes how certain the router is about the detected path.
type Confidence string

const (
	ConfidenceLow    Confidence = "low"
	ConfidenceMedium Confidence = "medium"
	ConfidenceHigh   Confidence = "high"
)

// Result holds the outcome of routing a task description.
type Result struct {
	// Single mode — set when IsChain is false.
	Path       Path
	Confidence Confidence
	Method     string // "keyword", "llm", "override", "chain"

	// Chain mode — set when IsChain is true.
	IsChain    bool
	Chain      []Path
	ChainKey   string // e.g. "review:fix"
	Predefined bool   // whether the chain has a known inter-stage contract

	// NeedsHumanEscalation is true when confidence is too low to proceed.
	NeedsHumanEscalation bool
	// Recommendation is the best candidate path/chain when escalation is needed.
	Recommendation string
}

// PredefinedChains lists v1 chains that have documented inter-stage contracts.
var PredefinedChains = map[string][]Path{
	"review:fix":       {PathReview, PathFix},
	"review:refactor":  {PathReview, PathRefactor},
	"refactor:test":    {PathRefactor, PathTest},
	"upgrade:fix":      {PathUpgrade, PathFix},
	"upgrade:test":     {PathUpgrade, PathTest},
	"upgrade:fix:test": {PathUpgrade, PathFix, PathTest},
	"fix:test":         {PathFix, PathTest},
	"document:review":  {PathDocument, PathReview},
	"create:test":      {PathCreate, PathTest},
	"add:test":         {PathAdd, PathTest},
}

// keywordTable maps trigger verbs to paths per §2.1.2.
var keywordTable = map[string]Path{
	// Create
	"create": PathCreate, "build": PathCreate, "generate": PathCreate,
	"make": PathCreate, "scaffold": PathCreate, "start": PathCreate,
	"initialize": PathCreate, "bootstrap": PathCreate, "new": PathCreate,

	// Add
	"add": PathAdd, "implement": PathAdd, "extend": PathAdd,
	"introduce": PathAdd, "support": PathAdd, "enable": PathAdd,
	"integrate": PathAdd,

	// Fix
	"fix": PathFix, "debug": PathFix, "repair": PathFix,
	"resolve": PathFix, "patch": PathFix, "address": PathFix,
	"troubleshoot": PathFix,

	// Refactor
	"refactor": PathRefactor, "restructure": PathRefactor, "rename": PathRefactor,
	"reorganize": PathRefactor, "simplify": PathRefactor, "cleanup": PathRefactor,
	"modernize": PathRefactor, "tidy": PathRefactor, "rewrite": PathRefactor,

	// Upgrade
	"upgrade": PathUpgrade, "migrate": PathUpgrade, "bump": PathUpgrade,
	"update": PathUpgrade,

	// Test
	"test": PathTest, "cover": PathTest,

	// Review
	"review": PathReview, "audit": PathReview, "analyze": PathReview,
	"inspect": PathReview, "check": PathReview, "critique": PathReview,
	"examine": PathReview, "assess": PathReview,

	// Document
	"document": PathDocument, "describe": PathDocument,

	// Explain
	"explain": PathExplain,

	// Research
	"research": PathResearch, "investigate": PathResearch,
}

// multiVerbPattern detects "X and Y" or "X AND Y" or "X, Y" between two known verbs.
var multiVerbPattern = regexp.MustCompile(`(?i)\b(\w+)\s+(?:and|,)\s+(\w+)\b`)

// Router classifies a task description into a path or chain.
type Router struct {
	backend    backend.Backend
	pathOverride Path // empty = no override
}

// Option configures a Router.
type Option func(*Router)

// WithBackend sets the backend used for LLM classification.
func WithBackend(b backend.Backend) Option {
	return func(r *Router) { r.backend = b }
}

// WithPathOverride forces a specific path (equivalent to --path flag).
func WithPathOverride(p Path) Option {
	return func(r *Router) { r.pathOverride = p }
}

// New creates a Router with the given options.
func New(opts ...Option) *Router {
	r := &Router{}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Route classifies task into a Result. ctx is used for LLM calls.
func (r *Router) Route(ctx context.Context, task string) (Result, error) {
	// --path override short-circuits everything.
	if r.pathOverride != "" {
		return Result{
			Path:       r.pathOverride,
			Confidence: ConfidenceHigh,
			Method:     "override",
		}, nil
	}

	// Check for multi-verb chain pattern first.
	if chain, key, predefined := detectChain(task); chain != nil {
		return Result{
			IsChain:    true,
			Chain:      chain,
			ChainKey:   key,
			Predefined: predefined,
			Method:     "chain",
			Confidence: ConfidenceHigh,
		}, nil
	}

	// Keyword fast-path: first token.
	first := firstToken(task)
	if p, ok := keywordTable[first]; ok {
		return Result{
			Path:       p,
			Confidence: ConfidenceHigh,
			Method:     "keyword",
		}, nil
	}

	// LLM classifier.
	if r.backend != nil {
		res, err := r.classifyWithLLM(ctx, task)
		if err != nil {
			return Result{}, fmt.Errorf("llm classify: %w", err)
		}
		if res.NeedsHumanEscalation {
			return res, nil
		}
		return res, nil
	}

	// No backend: escalate to human.
	return Result{
		NeedsHumanEscalation: true,
		Recommendation:       "",
		Method:               "keyword-miss",
	}, nil
}

// firstToken returns the lowercase first whitespace-delimited token of s.
func firstToken(s string) string {
	s = strings.TrimSpace(s)
	i := strings.IndexAny(s, " \t\n\r")
	if i < 0 {
		return strings.ToLower(s)
	}
	return strings.ToLower(s[:i])
}

// detectChain looks for a multi-verb "X and Y" pattern and returns a chain if
// both verbs are recognized. Returns nil when no chain is found.
func detectChain(task string) (chain []Path, key string, predefined bool) {
	matches := multiVerbPattern.FindAllStringSubmatch(strings.ToLower(task), -1)
	for _, m := range matches {
		v1, v2 := m[1], m[2]
		p1, ok1 := keywordTable[v1]
		p2, ok2 := keywordTable[v2]
		if !ok1 || !ok2 {
			continue
		}
		chainKey := string(p1) + ":" + string(p2)
		_, pre := PredefinedChains[chainKey]
		return []Path{p1, p2}, chainKey, pre
	}
	return nil, "", false
}

// classifyLLMResponse parses the backend's text response for path= and confidence= fields.
func classifyLLMResponse(text string) (Path, Confidence, bool) {
	var path Path
	var conf Confidence
	for _, line := range strings.Split(strings.ToLower(text), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "path=") {
			raw := strings.TrimPrefix(line, "path=")
			raw = strings.Trim(raw, " \t\r`\"'")
			path = Path(raw)
		}
		if strings.HasPrefix(line, "confidence=") {
			raw := strings.TrimPrefix(line, "confidence=")
			raw = strings.Trim(raw, " \t\r`\"'")
			conf = Confidence(raw)
		}
	}
	if path == "" || conf == "" {
		return "", "", false
	}
	return path, conf, true
}

func (r *Router) classifyWithLLM(ctx context.Context, task string) (Result, error) {
	classifyPrompt := fmt.Sprintf(`Classify the following task description into exactly one of these paths:
create, add, fix, refactor, upgrade, test, review, document, explain, research

Task: %s

Respond with exactly two lines:
path=<name>
confidence=<low|medium|high>

Use low confidence when the task could reasonably belong to multiple paths.`, task)

	tmpFile, err := writeTempPrompt(classifyPrompt)
	if err != nil {
		return Result{}, err
	}

	res, err := r.backend.RunIteration(ctx, backend.Prompt{Path: tmpFile}, backend.IterationOpts{})
	if err != nil {
		return Result{}, fmt.Errorf("backend iteration: %w", err)
	}

	path, conf, ok := classifyLLMResponse(res.FinalText)
	if !ok {
		return Result{
			NeedsHumanEscalation: true,
			Recommendation:       "",
			Method:               "llm",
		}, nil
	}

	if conf == ConfidenceLow {
		return Result{
			NeedsHumanEscalation: true,
			Recommendation:       string(path),
			Method:               "llm",
			Confidence:           conf,
		}, nil
	}

	return Result{
		Path:       path,
		Confidence: conf,
		Method:     "llm",
	}, nil
}
