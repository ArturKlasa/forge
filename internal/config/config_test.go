package config_test

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	goyaml "github.com/goccy/go-yaml"

	forgelog "github.com/arturklasa/forge/internal/log"
	"github.com/arturklasa/forge/internal/config"
)

// initTestLogger initialises the global logger so that config package warnings
// are captured and don't panic during tests.
func initTestLogger(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	forgelog.Init(forgelog.Config{})
	_ = buf
	return &buf
}

func writeYAML(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
}

// TestLayerPrecedence verifies that each successive layer overwrites the previous one.
func TestLayerPrecedence(t *testing.T) {
	initTestLogger(t)

	tmp := t.TempDir()

	// Global config sets backend.default = "global".
	globalPath := filepath.Join(tmp, "global", "config.yml")
	writeYAML(t, globalPath, "backend:\n  default: global\n")

	// Repo config sets backend.default = "repo".
	repoDir := filepath.Join(tmp, "repo")
	repoConfigPath := filepath.Join(repoDir, ".forge", "config.yml")
	writeYAML(t, repoConfigPath, "backend:\n  default: repo\n")

	// Override GlobalConfigPath and RepoConfigPath by using our custom paths.
	// We monkey-patch via env to point global home path; instead, test via the
	// Manager directly after construction.

	// Build a manager by calling Load with a custom global path via the internal
	// override mechanism: set HOME to our tmp dir so GlobalConfigPath resolves to
	// globalPath.
	t.Setenv("HOME", filepath.Join(tmp, "global", ".."))
	// Actually we need to write to the exact path returned by GlobalConfigPath.
	// Let's use $HOME approach: set HOME=tmp, then globalPath = tmp/.config/forge/config.yml
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Layer 2: global
	globalCfg := filepath.Join(home, ".config", "forge", "config.yml")
	writeYAML(t, globalCfg, "backend:\n  default: global\n")

	// Layer 3: repo
	repoDir2 := t.TempDir()
	repoCfg := filepath.Join(repoDir2, ".forge", "config.yml")
	writeYAML(t, repoCfg, "backend:\n  default: repo\n")

	// Layer 4: env (FORGE_BACKEND__DEFAULT=env)
	t.Setenv("FORGE_BACKEND__DEFAULT", "env")

	m, err := config.Load(repoDir2)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Env should win over repo config.
	if got := m.GetString("backend.default"); got != "env" {
		t.Errorf("expected env to win, got %q", got)
	}

	// Clear env and verify repo wins over global.
	t.Setenv("FORGE_BACKEND__DEFAULT", "")
	os.Unsetenv("FORGE_BACKEND__DEFAULT")

	m2, err := config.Load(repoDir2)
	if err != nil {
		t.Fatalf("Load2: %v", err)
	}
	if got := m2.GetString("backend.default"); got != "repo" {
		t.Errorf("expected repo to win over global, got %q", got)
	}

	// Layer 5: flag override wins over all.
	if err := m2.Override(map[string]interface{}{"backend.default": "flag"}); err != nil {
		t.Fatalf("Override: %v", err)
	}
	if got := m2.Effective().Backend.Default; got != "flag" {
		t.Errorf("expected flag override to win, got %q", got)
	}
}

// TestDefaultValues verifies built-in defaults are applied when no files exist.
func TestDefaultValues(t *testing.T) {
	initTestLogger(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	m, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg := m.Effective()
	if cfg.Backend.Default != "claude" {
		t.Errorf("default backend: want claude, got %q", cfg.Backend.Default)
	}
	if cfg.Iteration.MaxIterations != 100 {
		t.Errorf("default max_iterations: want 100, got %d", cfg.Iteration.MaxIterations)
	}
	if cfg.Retention.MaxRuns != 50 {
		t.Errorf("default max_runs: want 50, got %d", cfg.Retention.MaxRuns)
	}
}

// TestRoundTrip verifies that MarshalYAML → Unmarshal produces the same Config.
func TestRoundTrip(t *testing.T) {
	initTestLogger(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	m, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	yamlBytes, err := m.MarshalYAML()
	if err != nil {
		t.Fatalf("MarshalYAML: %v", err)
	}

	// Write to a temp file and reload.
	repoDir := t.TempDir()
	repoCfg := filepath.Join(repoDir, ".forge", "config.yml")
	writeYAML(t, repoCfg, string(yamlBytes))

	m2, err := config.Load(repoDir)
	if err != nil {
		t.Fatalf("Load after round-trip: %v", err)
	}

	orig := m.Effective()
	reloaded := m2.Effective()

	if orig.Backend.Default != reloaded.Backend.Default {
		t.Errorf("round-trip Backend.Default: %q != %q", orig.Backend.Default, reloaded.Backend.Default)
	}
	if orig.Iteration.MaxIterations != reloaded.Iteration.MaxIterations {
		t.Errorf("round-trip Iteration.MaxIterations: %d != %d",
			orig.Iteration.MaxIterations, reloaded.Iteration.MaxIterations)
	}
}

// TestUnknownKey verifies that an unknown key in the config file logs a warning and does not crash.
func TestUnknownKey(t *testing.T) {
	// Capture log output.
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	forgelog.InitWithHandler(handler)

	home := t.TempDir()
	t.Setenv("HOME", home)

	repoDir := t.TempDir()
	repoCfg := filepath.Join(repoDir, ".forge", "config.yml")
	writeYAML(t, repoCfg, "backend:\n  default: claude\nfoo: bar\n")

	m, err := config.Load(repoDir)
	if err != nil {
		t.Fatalf("Load with unknown key: %v", err)
	}
	// Must not crash and backend must still be loaded.
	if m.Effective().Backend.Default != "claude" {
		t.Errorf("expected backend=claude, got %q", m.Effective().Backend.Default)
	}
	// Warning should be logged.
	if !strings.Contains(buf.String(), "unknown config key") {
		t.Errorf("expected unknown key warning, log output: %s", buf.String())
	}
}

// TestSetKey verifies that SetKey writes a value to the repo config file.
func TestSetKey(t *testing.T) {
	initTestLogger(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	repoDir := t.TempDir()
	m, err := config.Load(repoDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := m.SetKey("backend.default", "gemini", false); err != nil {
		t.Fatalf("SetKey: %v", err)
	}

	// Reload and verify.
	m2, err := config.Load(repoDir)
	if err != nil {
		t.Fatalf("Load after set: %v", err)
	}
	if got := m2.Effective().Backend.Default; got != "gemini" {
		t.Errorf("after set: want gemini, got %q", got)
	}
}

// TestUnsetKey verifies that UnsetKey removes a key from the config file.
func TestUnsetKey(t *testing.T) {
	initTestLogger(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	repoDir := t.TempDir()
	repoCfg := filepath.Join(repoDir, ".forge", "config.yml")
	writeYAML(t, repoCfg, "backend:\n  default: kiro\n")

	m, err := config.Load(repoDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Effective().Backend.Default != "kiro" {
		t.Fatalf("precondition: expected kiro")
	}

	if err := m.UnsetKey("backend.default", false); err != nil {
		t.Fatalf("UnsetKey: %v", err)
	}

	// Reload: key removed from repo, default should win.
	m2, err := config.Load(repoDir)
	if err != nil {
		t.Fatalf("Load after unset: %v", err)
	}
	if got := m2.Effective().Backend.Default; got != "claude" {
		t.Errorf("after unset: want default (claude), got %q", got)
	}
}

// TestSetKeyGlobal verifies that --global flag writes to the global config file.
func TestSetKeyGlobal(t *testing.T) {
	initTestLogger(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	repoDir := t.TempDir()
	m, err := config.Load(repoDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := m.SetKey("backend.default", "kiro", true); err != nil {
		t.Fatalf("SetKey global: %v", err)
	}

	// Verify file was written to global path.
	b, err := os.ReadFile(m.GlobalPath)
	if err != nil {
		t.Fatalf("read global config: %v", err)
	}
	var data map[string]interface{}
	if err := goyaml.Unmarshal(b, &data); err != nil {
		t.Fatalf("parse global config: %v", err)
	}
	backend, _ := data["backend"].(map[string]interface{})
	if backend["default"] != "kiro" {
		t.Errorf("global config: expected kiro, got %v", backend["default"])
	}
}
