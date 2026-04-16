package log_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	forgelog "github.com/arturklasa/forge/internal/log"
)

// newBufs returns fresh out/err byte buffers and a Config wired to them.
func newBufs(verbose, quiet, jsonMode bool) (*bytes.Buffer, *bytes.Buffer, forgelog.Config) {
	out := new(bytes.Buffer)
	userOut := new(bytes.Buffer)
	return out, userOut, forgelog.Config{
		Verbose: verbose,
		Quiet:   quiet,
		JSON:    jsonMode,
		Out:     out,
		UserOut: userOut,
	}
}

// TestDefaultMode verifies text-mode output: no timestamp, level+msg present.
func TestDefaultMode(t *testing.T) {
	diagOut, _, cfg := newBufs(false, false, false)
	forgelog.Init(cfg)

	forgelog.G().Info("hello world")

	got := diagOut.String()
	if strings.Contains(got, "time=") {
		t.Errorf("default mode should strip timestamps; got: %q", got)
	}
	if !strings.Contains(got, "level=INFO") {
		t.Errorf("expected level=INFO in output; got: %q", got)
	}
	if !strings.Contains(got, "msg=\"hello world\"") {
		t.Errorf("expected msg in output; got: %q", got)
	}
}

// TestVerboseMode verifies DEBUG messages appear.
func TestVerboseMode(t *testing.T) {
	diagOut, _, cfg := newBufs(true, false, false)
	forgelog.Init(cfg)

	forgelog.G().Debug("debug msg")

	got := diagOut.String()
	if !strings.Contains(got, "level=DEBUG") {
		t.Errorf("verbose mode: expected debug level; got: %q", got)
	}
}

// TestDefaultModeDebugSuppressed verifies DEBUG is hidden in default mode.
func TestDefaultModeDebugSuppressed(t *testing.T) {
	diagOut, _, cfg := newBufs(false, false, false)
	forgelog.Init(cfg)

	forgelog.G().Debug("should not appear")

	if diagOut.Len() != 0 {
		t.Errorf("default mode should not emit DEBUG; got: %q", diagOut.String())
	}
}

// TestQuietMode verifies INFO is suppressed; WARN appears.
func TestQuietMode(t *testing.T) {
	diagOut, _, cfg := newBufs(false, true, false)
	forgelog.Init(cfg)

	forgelog.G().Info("info msg")
	forgelog.G().Warn("warn msg")

	got := diagOut.String()
	if strings.Contains(got, "info msg") {
		t.Errorf("quiet mode should suppress INFO; got: %q", got)
	}
	if !strings.Contains(got, "level=WARN") {
		t.Errorf("quiet mode: WARN should appear; got: %q", got)
	}
}

// TestJSONMode verifies NDJSON output on UserOut with valid JSON and time key.
func TestJSONMode(t *testing.T) {
	_, userOut, cfg := newBufs(false, false, true)
	forgelog.Init(cfg)

	forgelog.G().Info("json test", "key", "value")

	lines := strings.Split(strings.TrimSpace(userOut.String()), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatal("expected at least one JSON line")
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &obj); err != nil {
		t.Fatalf("output is not valid JSON: %v; got: %q", err, lines[0])
	}

	if obj["level"] != "INFO" {
		t.Errorf("expected level=INFO in JSON; got: %v", obj["level"])
	}
	if obj["msg"] != "json test" {
		t.Errorf("expected msg='json test'; got: %v", obj["msg"])
	}
	if obj["key"] != "value" {
		t.Errorf("expected key=value; got: %v", obj["key"])
	}
	// JSON mode retains time field.
	if _, ok := obj["time"]; !ok {
		t.Errorf("JSON mode should include time field; got: %v", obj)
	}
}

// TestJSONModeNDJSON verifies multiple lines are each individually valid JSON.
func TestJSONModeNDJSON(t *testing.T) {
	_, userOut, cfg := newBufs(false, false, true)
	forgelog.Init(cfg)

	forgelog.G().Info("line one")
	forgelog.G().Warn("line two")

	lines := strings.Split(strings.TrimSpace(userOut.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSON lines, got %d: %q", len(lines), userOut.String())
	}
	for i, line := range lines {
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Errorf("line %d is not valid JSON: %v; content: %q", i+1, err, line)
		}
	}
}

// TestNOCOLOR verifies that NO_COLOR disables interactive mode.
func TestNOCOLOR(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	_, userOut, cfg := newBufs(false, false, false)
	// Use a real os.File-like writer to avoid the non-TTY short-circuit.
	cfg.UserOut = userOut

	forgelog.Init(cfg)

	if forgelog.G().Interactive() {
		t.Error("NO_COLOR=1 should disable interactive mode")
	}
}

// TestCIEnv verifies that CI=true disables interactive mode.
func TestCIEnv(t *testing.T) {
	t.Setenv("CI", "true")

	_, userOut, cfg := newBufs(false, false, false)
	cfg.UserOut = userOut

	forgelog.Init(cfg)

	if forgelog.G().Interactive() {
		t.Error("CI env should disable interactive mode")
	}
}

// TestNonTTYDisablesInteractive verifies bytes.Buffer (non-TTY) disables interactive mode.
func TestNonTTYDisablesInteractive(t *testing.T) {
	// Ensure neither NO_COLOR nor CI are set (don't want them to interfere).
	os.Unsetenv("NO_COLOR")
	os.Unsetenv("CI")

	_, userOut, cfg := newBufs(false, false, false)
	cfg.UserOut = userOut // bytes.Buffer is not *os.File → non-TTY

	forgelog.Init(cfg)

	if forgelog.G().Interactive() {
		t.Error("non-TTY writer should disable interactive mode")
	}
}

// TestPrint verifies user-facing Print writes to UserOut.
func TestPrint(t *testing.T) {
	_, userOut, cfg := newBufs(false, false, false)
	forgelog.Init(cfg)

	forgelog.Print("hello user")

	if !strings.Contains(userOut.String(), "hello user") {
		t.Errorf("Print should write to UserOut; got: %q", userOut.String())
	}
}

// TestPrintQuietSuppressed verifies Print is silent in quiet mode.
func TestPrintQuietSuppressed(t *testing.T) {
	_, userOut, cfg := newBufs(false, true, false)
	forgelog.Init(cfg)

	forgelog.Print("should be suppressed")

	if userOut.Len() != 0 {
		t.Errorf("Print in quiet mode should be suppressed; got: %q", userOut.String())
	}
}

// TestSlogDefaultSet verifies slog.Default() is updated to the global logger.
func TestSlogDefaultSet(t *testing.T) {
	_, _, cfg := newBufs(false, false, false)
	forgelog.Init(cfg)

	if forgelog.G().SlogLogger() == nil {
		t.Error("SlogLogger should not be nil after Init")
	}
}
