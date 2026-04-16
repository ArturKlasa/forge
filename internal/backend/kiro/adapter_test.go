package kiro

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/arturklasa/forge/internal/backend"
	"github.com/arturklasa/forge/internal/proc"
)

var fakeBackendBin string
var canExec bool

func TestMain(m *testing.M) {
	probe := exec.Command("/bin/sh", "-c", "exit 0")
	probe.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Setsid: true}
	canExec = probe.Run() == nil

	if canExec {
		tmp, err := os.MkdirTemp("", "forge-kiro-test-*")
		if err != nil {
			panic("cannot create temp dir: " + err.Error())
		}
		defer os.RemoveAll(tmp)

		bin := filepath.Join(tmp, "fake-backend")
		out, err := exec.Command("go", "build", "-o", bin, "github.com/arturklasa/forge/cmd/fake-backend").CombinedOutput()
		if err != nil {
			canExec = false
			_ = out
		} else {
			fakeBackendBin = bin
		}
	}

	os.Exit(m.Run())
}

func skipIfNoExec(t *testing.T) {
	t.Helper()
	if !canExec {
		t.Skip("sandbox blocks subprocess execution")
	}
}

func scriptPath(t *testing.T) string {
	t.Helper()
	root := findProjectRoot(t)
	return filepath.Join(root, "testdata", "trivial.yaml")
}

func findProjectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find project root (go.mod)")
		}
		dir = parent
	}
}

// newACPAdapter returns a Kiro adapter in ACP mode backed by fake-backend acp.
func newACPAdapter(t *testing.T, scriptFile string) *Adapter {
	t.Helper()
	tmp := t.TempDir()
	wrapper := filepath.Join(tmp, "kiro-cli")

	// Wrapper: for 'acp' sub-arg, run fake-backend in acp mode; ignore all flags.
	wrapperContent := "#!/bin/sh\n" +
		"exec " + fakeBackendBin + " --mode acp --script " + scriptFile + "\n"

	if err := os.WriteFile(wrapper, []byte(wrapperContent), 0o755); err != nil {
		t.Fatalf("write wrapper: %v", err)
	}

	return New(WithExecutable(wrapper), WithGracePeriod(2*time.Second), WithMode(ModeACP))
}

// newTextAdapter returns a Kiro adapter in text mode backed by fake-backend kiro-text.
// The wrapper reads the last arg (prompt) and pipes it to fake-backend stdin.
func newTextAdapter(t *testing.T, scriptFile string) *Adapter {
	t.Helper()
	tmp := t.TempDir()
	wrapper := filepath.Join(tmp, "kiro-cli")

	// Kiro text mode: kiro-cli chat --no-interactive --trust-all-tools <prompt>
	// Prompt is the last argument. Extract it and pipe via stdin.
	wrapperContent := "#!/bin/sh\n" +
		"# last arg is the prompt\n" +
		"PROMPT=\"${@: -1}\"\n" +
		"printf '%s' \"$PROMPT\" | exec " + fakeBackendBin + " --mode kiro-text --script " + scriptFile + "\n"

	if err := os.WriteFile(wrapper, []byte(wrapperContent), 0o755); err != nil {
		t.Fatalf("write wrapper: %v", err)
	}

	return New(WithExecutable(wrapper), WithGracePeriod(2*time.Second), WithMode(ModeText))
}

// --- ACP tests ---

// TestKiroACP_FullHandshake verifies initialize → session/new → session/prompt round-trip.
func TestKiroACP_FullHandshake(t *testing.T) {
	skipIfNoExec(t)
	adapter := newACPAdapter(t, scriptPath(t))

	result, err := adapter.RunIteration(context.Background(), backend.Prompt{Body: "create a hello-world"}, backend.IterationOpts{})
	if err != nil {
		t.Fatalf("RunIteration: %v", err)
	}
	if result.Error != nil {
		t.Errorf("unexpected result.Error: %v", result.Error)
	}
	if !strings.Contains(result.FinalText, "Hello World") {
		t.Errorf("FinalText = %q, want to contain 'Hello World'", result.FinalText)
	}
	if result.TokensUsage.Input == 0 && result.TokensUsage.Output == 0 {
		t.Error("expected non-zero token usage")
	}
}

// TestKiroACP_Event verifies a session/prompt event is recorded.
func TestKiroACP_Event(t *testing.T) {
	skipIfNoExec(t)
	adapter := newACPAdapter(t, scriptPath(t))

	result, err := adapter.RunIteration(context.Background(), backend.Prompt{Body: "create a hello-world"}, backend.IterationOpts{})
	if err != nil {
		t.Fatalf("RunIteration: %v", err)
	}
	if len(result.Events) == 0 {
		t.Fatal("expected at least one event")
	}
	found := false
	for _, ev := range result.Events {
		if ev.Type == "session/prompt" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("events %v: no session/prompt event", result.Events)
	}
}

// --- Text mode tests ---

// TestKiroText_Success verifies text mode detects ▸ Credits: footer and strips it.
func TestKiroText_Success(t *testing.T) {
	skipIfNoExec(t)
	adapter := newTextAdapter(t, scriptPath(t))

	result, err := adapter.RunIteration(context.Background(), backend.Prompt{Body: "create a hello-world"}, backend.IterationOpts{})
	if err != nil {
		t.Fatalf("RunIteration: %v", err)
	}
	if result.Error != nil {
		t.Errorf("unexpected result.Error: %v", result.Error)
	}
	if !strings.Contains(result.FinalText, "Hello World") {
		t.Errorf("FinalText = %q, want to contain 'Hello World'", result.FinalText)
	}
	if strings.Contains(result.FinalText, "Credits:") {
		t.Error("FinalText should not contain the Credits: footer")
	}
}

// TestKiroText_CreditsEvent verifies a credits event is recorded.
func TestKiroText_CreditsEvent(t *testing.T) {
	skipIfNoExec(t)
	adapter := newTextAdapter(t, scriptPath(t))

	result, err := adapter.RunIteration(context.Background(), backend.Prompt{Body: "create a hello-world"}, backend.IterationOpts{})
	if err != nil {
		t.Fatalf("RunIteration: %v", err)
	}
	found := false
	for _, ev := range result.Events {
		if ev.Type == "credits" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("events %v: no credits event", result.Events)
	}
}

// TestKiroText_ParseCreditsMarker unit-tests parseKiroText directly.
func TestKiroText_ParseCreditsMarker(t *testing.T) {
	stdout := "Hello World\n\u25b8 Credits: 0.39 \u2022 Time: 1s\n"
	result := parseKiroText(stdout, "", proc.Result{ExitCode: 0})

	if result.FinalText != "Hello World" {
		t.Errorf("FinalText = %q, want 'Hello World'", result.FinalText)
	}
	if strings.Contains(result.FinalText, "Credits") {
		t.Error("FinalText should not contain Credits: footer")
	}
	found := false
	for _, ev := range result.Events {
		if ev.Type == "credits" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected credits event")
	}
}

// TestKiroText_NoMarker verifies degraded mode when no ▸ Credits: marker is present.
func TestKiroText_NoMarker(t *testing.T) {
	stdout := "Some output without footer\n"
	result := parseKiroText(stdout, "", proc.Result{ExitCode: 0})

	if result.FinalText != "Some output without footer" {
		t.Errorf("FinalText = %q", result.FinalText)
	}
}

// --- Common tests ---

// TestKiro_Name verifies adapter name.
func TestKiro_Name(t *testing.T) {
	a := New()
	if a.Name() != "kiro" {
		t.Errorf("Name() = %q, want 'kiro'", a.Name())
	}
}

// TestKiro_Capabilities_ACP verifies ACP mode capabilities.
func TestKiro_Capabilities_ACP(t *testing.T) {
	a := New(WithMode(ModeACP))
	caps := a.Capabilities()
	if !caps.StructuredOutput {
		t.Error("ACP mode: expected StructuredOutput=true")
	}
	if caps.SkipPermissionsFlag != "--trust-all-tools" {
		t.Errorf("SkipPermissionsFlag = %q, want '--trust-all-tools'", caps.SkipPermissionsFlag)
	}
}

// TestKiro_Capabilities_Text verifies text mode capabilities.
func TestKiro_Capabilities_Text(t *testing.T) {
	a := New(WithMode(ModeText))
	caps := a.Capabilities()
	if caps.StructuredOutput {
		t.Error("text mode: expected StructuredOutput=false")
	}
}
