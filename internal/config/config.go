package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	goyaml "github.com/goccy/go-yaml"
	koanfmaps "github.com/knadh/koanf/maps"
	kenv "github.com/knadh/koanf/providers/env"
	kfile "github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"

	forgelog "github.com/arturklasa/forge/internal/log"
)

// knownTopKeys is the set of valid top-level config keys (for unknown-key warnings).
var knownTopKeys = map[string]bool{
	"backend": true, "brain": true, "context": true, "git": true,
	"iteration": true, "notifications": true, "retention": true,
	"paths": true, "stuck_detection": true, "completion_detection": true,
	"gates": true, "chaining": true,
}

// --- Schema structs (per design §5.2) ---
// All structs carry both koanf (for koanf unmarshalling) and yaml (for goyaml marshalling) tags.

// BackendConfig holds backend selection settings.
type BackendConfig struct {
	Default string `koanf:"default" yaml:"default"`
}

// BrainConfig holds brain (meta-LLM) settings.
type BrainConfig struct {
	Mode string `koanf:"mode" yaml:"mode"`
}

// DistillationThresholds holds context distillation token thresholds.
type DistillationThresholds struct {
	StateMD int `koanf:"state_md" yaml:"state_md"`
	NotesMD int `koanf:"notes_md" yaml:"notes_md"`
	PlanMD  int `koanf:"plan_md"  yaml:"plan_md"`
}

// ContextConfig holds context management settings.
type ContextConfig struct {
	Budget                 *int                   `koanf:"budget"                  yaml:"budget"`
	Verbose                bool                   `koanf:"verbose"                 yaml:"verbose"`
	DistillationThresholds DistillationThresholds `koanf:"distillation_thresholds" yaml:"distillation_thresholds"`
}

// GitConfig holds git workflow settings.
type GitConfig struct {
	Branching         string            `koanf:"branching"          yaml:"branching"`
	AutoTag           bool              `koanf:"auto_tag"           yaml:"auto_tag"`
	ProtectedBranches ProtectedBranches `koanf:"protected_branches" yaml:"protected_branches"`
}

// ProtectedBranches defines exact and pattern-based protected branch specs.
type ProtectedBranches struct {
	Exact                      []string `koanf:"exact"                        yaml:"exact"`
	Patterns                   []string `koanf:"patterns"                     yaml:"patterns"`
	AlwaysIncludeDefaultBranch bool     `koanf:"always_include_default_branch" yaml:"always_include_default_branch"`
}

// IterationConfig holds iteration limit settings.
type IterationConfig struct {
	TimeoutSec     int `koanf:"timeout_sec"      yaml:"timeout_sec"`
	MaxIterations  int `koanf:"max_iterations"   yaml:"max_iterations"`
	MaxDurationSec int `koanf:"max_duration_sec" yaml:"max_duration_sec"`
}

// NotificationsConfig holds notification channel settings.
type NotificationsConfig struct {
	TerminalBell bool `koanf:"terminal_bell" yaml:"terminal_bell"`
	OsNotify     bool `koanf:"os_notify"     yaml:"os_notify"`
}

// RetentionConfig holds run history retention settings.
type RetentionConfig struct {
	MaxRuns int `koanf:"max_runs" yaml:"max_runs"`
}

// PathsConfig holds path-specific gate settings.
type PathsConfig struct {
	RefactorGate bool `koanf:"refactor_gate" yaml:"refactor_gate"`
}

// HardSignal defines a stuck detection hard signal configuration.
type HardSignal struct {
	Tier int `koanf:"tier" yaml:"tier"`
}

// SoftSignal defines a stuck detection soft signal configuration.
type SoftSignal struct {
	Weight int `koanf:"weight" yaml:"weight"`
}

// SoftThresholds defines the tier thresholds for soft signal sums.
type SoftThresholds struct {
	Tier1 int `koanf:"tier_1" yaml:"tier_1"`
	Tier2 int `koanf:"tier_2" yaml:"tier_2"`
}

// StuckDetectionConfig holds stuck detection configuration.
type StuckDetectionConfig struct {
	HardSignals         map[string]HardSignal `koanf:"hard_signals"           yaml:"hard_signals"`
	SoftSignals         map[string]SoftSignal `koanf:"soft_signals"           yaml:"soft_signals"`
	SoftThresholds      SoftThresholds        `koanf:"soft_thresholds"        yaml:"soft_thresholds"`
	ExternalDeathCap    int                   `koanf:"external_death_cap"     yaml:"external_death_cap"`
	RateLimitBackoffSec []int                 `koanf:"rate_limit_backoff_sec" yaml:"rate_limit_backoff_sec"`
	RateLimitMaxRetries int                   `koanf:"rate_limit_max_retries" yaml:"rate_limit_max_retries"`
}

// CompletionWeights holds completion detection signal weights.
type CompletionWeights struct {
	AgentSentinel         int `koanf:"agent_sentinel"          yaml:"agent_sentinel"`
	BuildPasses           int `koanf:"build_passes"            yaml:"build_passes"`
	TestsPass             int `koanf:"tests_pass"              yaml:"tests_pass"`
	PathSpecific          int `koanf:"path_specific"           yaml:"path_specific"`
	PlanItemsClosed       int `koanf:"plan_items_closed"       yaml:"plan_items_closed"`
	JudgeHighConfidence   int `koanf:"judge_high_confidence"   yaml:"judge_high_confidence"`
	JudgeMediumConfidence int `koanf:"judge_medium_confidence" yaml:"judge_medium_confidence"`
	JudgeIncompleteVeto   int `koanf:"judge_incomplete_veto"   yaml:"judge_incomplete_veto"`
}

// CompletionDetectionConfig holds completion detection configuration.
type CompletionDetectionConfig struct {
	Weights            CompletionWeights `koanf:"weights"             yaml:"weights"`
	ThresholdComplete  int               `koanf:"threshold_complete"  yaml:"threshold_complete"`
	ThresholdAuditIter int               `koanf:"threshold_audit_iter" yaml:"threshold_audit_iter"`
}

// GatesConfig holds policy gate extensibility settings.
type GatesConfig struct {
	AdditionalManifestPaths []string `koanf:"additional_manifest_paths" yaml:"additional_manifest_paths"`
	AdditionalCIPaths       []string `koanf:"additional_ci_paths"       yaml:"additional_ci_paths"`
	AdditionalSecretPaths   []string `koanf:"additional_secret_paths"   yaml:"additional_secret_paths"`
}

// ChainingConfig holds composite chaining settings.
type ChainingConfig struct {
	DetectNaturalLanguage bool `koanf:"detect_natural_language" yaml:"detect_natural_language"`
	MaxStagesWarn         int  `koanf:"max_stages_warn"         yaml:"max_stages_warn"`
	StageConfirmation     bool `koanf:"stage_confirmation"      yaml:"stage_confirmation"`
}

// Config is the complete Forge configuration schema (per design §5.2).
type Config struct {
	Backend             BackendConfig             `koanf:"backend"              yaml:"backend"`
	Brain               BrainConfig               `koanf:"brain"                yaml:"brain"`
	Context             ContextConfig             `koanf:"context"              yaml:"context"`
	Git                 GitConfig                 `koanf:"git"                  yaml:"git"`
	Iteration           IterationConfig           `koanf:"iteration"            yaml:"iteration"`
	Notifications       NotificationsConfig       `koanf:"notifications"        yaml:"notifications"`
	Retention           RetentionConfig           `koanf:"retention"            yaml:"retention"`
	Paths               PathsConfig               `koanf:"paths"                yaml:"paths"`
	StuckDetection      StuckDetectionConfig      `koanf:"stuck_detection"      yaml:"stuck_detection"`
	CompletionDetection CompletionDetectionConfig `koanf:"completion_detection" yaml:"completion_detection"`
	Gates               GatesConfig               `koanf:"gates"                yaml:"gates"`
	Chaining            ChainingConfig            `koanf:"chaining"             yaml:"chaining"`
}

// Manager loads and provides access to the merged Forge configuration.
type Manager struct {
	k          *koanf.Koanf
	cfg        Config
	GlobalPath string
	RepoPath   string
}

// goyamlParser implements koanf.Parser using goccy/go-yaml.
type goyamlParser struct{}

func (p *goyamlParser) Unmarshal(b []byte) (map[string]interface{}, error) {
	var out map[string]interface{}
	if err := goyaml.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *goyamlParser) Marshal(o map[string]interface{}) ([]byte, error) {
	return goyaml.Marshal(o)
}

// mapProvider is a koanf Provider backed by a flat dot-separated map.
// It implements both ReadBytes and Read to satisfy the koanf.Provider interface.
type mapProvider struct {
	data map[string]interface{}
}

func (p *mapProvider) ReadBytes() ([]byte, error) {
	b, err := goyaml.Marshal(koanfmaps.Unflatten(p.data, "."))
	return b, err
}

func (p *mapProvider) Read() (map[string]interface{}, error) {
	return koanfmaps.Unflatten(p.data, "."), nil
}

// defaultsMap returns the built-in default configuration as a flat koanf map.
func defaultsMap() map[string]interface{} {
	return map[string]interface{}{
		"backend.default":   "claude",
		"brain.mode":        "cli",
		"context.budget":    nil,
		"context.verbose":   false,
		"context.distillation_thresholds.state_md": 8000,
		"context.distillation_thresholds.notes_md": 10000,
		"context.distillation_thresholds.plan_md":  6000,
		"git.branching":    "smart",
		"git.auto_tag":     false,
		"git.protected_branches.exact": []string{
			"main", "master", "trunk", "develop", "development",
			"staging", "production", "prod", "release",
		},
		"git.protected_branches.patterns":                   []string{"release/*", "hotfix/*", "env/*"},
		"git.protected_branches.always_include_default_branch": true,
		"iteration.timeout_sec":      1800,
		"iteration.max_iterations":   100,
		"iteration.max_duration_sec": 14400,
		"notifications.terminal_bell": true,
		"notifications.os_notify":     true,
		"retention.max_runs":          50,
		"paths.refactor_gate":         true,
		"stuck_detection.external_death_cap":    3,
		"stuck_detection.rate_limit_backoff_sec": []int{30, 60, 120, 300, 600},
		"stuck_detection.rate_limit_max_retries": 4,
		"stuck_detection.soft_thresholds.tier_1": 3,
		"stuck_detection.soft_thresholds.tier_2": 6,
		"completion_detection.threshold_complete":   8,
		"completion_detection.threshold_audit_iter": 5,
		"completion_detection.weights.agent_sentinel":          3,
		"completion_detection.weights.build_passes":            2,
		"completion_detection.weights.tests_pass":              2,
		"completion_detection.weights.path_specific":           2,
		"completion_detection.weights.plan_items_closed":       2,
		"completion_detection.weights.judge_high_confidence":   3,
		"completion_detection.weights.judge_medium_confidence": 2,
		"completion_detection.weights.judge_incomplete_veto":   -4,
		"chaining.detect_natural_language": true,
		"chaining.max_stages_warn":         3,
		"chaining.stage_confirmation":      true,
	}
}

// GlobalConfigPath returns the path to the global config file.
func GlobalConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "forge", "config.yml")
}

// RepoConfigPath returns the path to the per-repo config file for the given repo dir.
func RepoConfigPath(repoDir string) string {
	return filepath.Join(repoDir, ".forge", "config.yml")
}

// Load creates a Manager by loading all config layers in precedence order:
// built-in defaults → global config → repo config → env vars → flag overrides.
func Load(repoDir string) (*Manager, error) {
	k := koanf.New(".")

	// Layer 1: built-in defaults.
	if err := k.Load(&mapProvider{data: defaultsMap()}, nil); err != nil {
		return nil, fmt.Errorf("loading defaults: %w", err)
	}

	globalPath := GlobalConfigPath()
	repoPath := RepoConfigPath(repoDir)

	// Layer 2: global config file.
	if _, err := os.Stat(globalPath); err == nil {
		if err := k.Load(kfile.Provider(globalPath), &goyamlParser{}); err != nil {
			return nil, fmt.Errorf("loading global config %s: %w", globalPath, err)
		}
	}

	// Layer 3: per-repo config file.
	if _, err := os.Stat(repoPath); err == nil {
		if err := k.Load(kfile.Provider(repoPath), &goyamlParser{}); err != nil {
			return nil, fmt.Errorf("loading repo config %s: %w", repoPath, err)
		}
	}

	// Layer 4: env vars. FORGE_BACKEND__DEFAULT → backend.default
	// Convention: strip FORGE_ prefix, lowercase, replace __ with ., keep _ within segments.
	if err := k.Load(kenv.Provider("FORGE_", ".", func(s string) string {
		s = strings.TrimPrefix(s, "FORGE_")
		s = strings.ToLower(s)
		// Double underscore is the nested path separator.
		s = strings.ReplaceAll(s, "__", ".")
		return s
	}), nil); err != nil {
		return nil, fmt.Errorf("loading env config: %w", err)
	}

	// Warn on unknown top-level keys.
	for _, key := range k.Keys() {
		top := strings.SplitN(key, ".", 2)[0]
		if !knownTopKeys[top] {
			forgelog.G().Warn("unknown config key ignored", "key", key)
		}
	}

	// Unmarshal into the typed Config struct.
	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("unmarshalling config: %w", err)
	}

	return &Manager{
		k:          k,
		cfg:        cfg,
		GlobalPath: globalPath,
		RepoPath:   repoPath,
	}, nil
}

// Effective returns the merged Config struct.
func (m *Manager) Effective() Config { return m.cfg }

// Get returns the raw koanf value for a dot-separated key.
func (m *Manager) Get(key string) interface{} { return m.k.Get(key) }

// GetString returns the string value for a dot-separated key.
func (m *Manager) GetString(key string) string { return m.k.String(key) }

// Exists reports whether a key exists in the merged config.
func (m *Manager) Exists(key string) bool { return m.k.Exists(key) }

// Override applies a single key=value override (e.g., from a CLI flag) as the topmost layer.
// This allows CLI flags to win over all file/env layers without going through koanf providers.
func (m *Manager) Override(overrides map[string]interface{}) error {
	if err := m.k.Load(&mapProvider{data: overrides}, nil); err != nil {
		return err
	}
	return m.k.Unmarshal("", &m.cfg)
}

// MarshalYAML returns the merged configuration as YAML bytes.
func (m *Manager) MarshalYAML() ([]byte, error) {
	return goyaml.Marshal(m.cfg)
}
