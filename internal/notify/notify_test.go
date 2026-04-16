package notify_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arturklasa/forge/internal/notify"
)

// errorChannel always fails Notify.
type errorChannel struct{ name string }

func (e *errorChannel) Name() string                                { return e.name }
func (e *errorChannel) Available() bool                             { return true }
func (e *errorChannel) Notify(_ context.Context, _ notify.Message) error {
	return errors.New("simulated failure")
}

// recordChannel records calls without side effects.
type recordChannel struct {
	name  string
	calls int
	last  notify.Message
}

func (r *recordChannel) Name() string    { return r.name }
func (r *recordChannel) Available() bool { return true }
func (r *recordChannel) Notify(_ context.Context, msg notify.Message) error {
	r.calls++
	r.last = msg
	return nil
}

func TestNotifyAll_FailLoud(t *testing.T) {
	a := &recordChannel{name: "a"}
	bad := &errorChannel{name: "bad"}
	b := &recordChannel{name: "b"}

	channels := []notify.Channel{a, bad, b}
	attempts := notify.NotifyAll(context.Background(), channels, notify.Message{
		Title:   "Test",
		Summary: "summary",
	})

	if len(attempts) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(attempts))
	}
	if !attempts[0].OK {
		t.Error("channel a should succeed")
	}
	if attempts[1].OK {
		t.Error("channel bad should fail")
	}
	if !attempts[2].OK {
		t.Error("channel b should succeed even after bad channel failed")
	}
	if a.calls != 1 || b.calls != 1 {
		t.Error("both channels should have been called")
	}
}

func TestFileSink(t *testing.T) {
	dir := t.TempDir()
	sink := notify.NewFileSink(dir)

	if sink.Name() != "file" {
		t.Errorf("unexpected name: %s", sink.Name())
	}
	if !sink.Available() {
		t.Error("FileSink should be available when runDir is set")
	}

	msg := notify.Message{Title: "ESCALATION", Summary: "something happened"}
	if err := sink.Notify(context.Background(), msg); err != nil {
		t.Fatalf("FileSink.Notify: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "ESCALATION"))
	if err != nil {
		t.Fatalf("ESCALATION file not written: %v", err)
	}
	if !strings.Contains(string(data), "ESCALATION") {
		t.Errorf("sentinel file missing title: %s", data)
	}
}

func TestFileSink_EmptyRunDir(t *testing.T) {
	sink := notify.NewFileSink("")
	if sink.Available() {
		t.Error("FileSink should not be available without runDir")
	}
	if err := sink.Notify(context.Background(), notify.Message{Title: "x"}); err != nil {
		t.Errorf("empty runDir should not error: %v", err)
	}
}

func TestBannerSink(t *testing.T) {
	var buf bytes.Buffer
	sink := notify.NewBannerSink(&buf)

	if sink.Name() != "banner" {
		t.Errorf("unexpected name: %s", sink.Name())
	}
	if !sink.Available() {
		t.Error("BannerSink should always be available")
	}

	msg := notify.Message{Title: "ESCALATION", Body: "details here"}
	if err := sink.Notify(context.Background(), msg); err != nil {
		t.Fatalf("BannerSink.Notify: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "ESCALATION") {
		t.Errorf("banner missing title: %q", out)
	}
	if !strings.Contains(out, "details here") {
		t.Errorf("banner missing body: %q", out)
	}
	if !strings.Contains(out, "====") {
		t.Errorf("banner missing separator: %q", out)
	}
}

func TestOSCSink(t *testing.T) {
	var buf bytes.Buffer
	sink := notify.NewOSCSink(&buf)

	if sink.Name() != "osc9" {
		t.Errorf("unexpected name: %s", sink.Name())
	}

	msg := notify.Message{Summary: "escalation triggered"}
	if err := sink.Notify(context.Background(), msg); err != nil {
		t.Fatalf("OSCSink.Notify: %v", err)
	}

	out := buf.String()
	// OSC 9 sequence starts with \033]9;
	if !strings.Contains(out, "\033]9;") {
		t.Errorf("OSCSink missing OSC 9 escape: %q", out)
	}
	if !strings.Contains(out, "escalation triggered") {
		t.Errorf("OSCSink missing summary in sequence: %q", out)
	}
}

func TestTmuxSink_NotAvailableWithoutTmux(t *testing.T) {
	// Unset TMUX so TmuxSink is not available.
	old := os.Getenv("TMUX")
	os.Unsetenv("TMUX")
	defer os.Setenv("TMUX", old)

	sink := notify.NewTmuxSink()
	if sink.Name() != "tmux" {
		t.Errorf("unexpected name: %s", sink.Name())
	}
	if sink.Available() {
		t.Error("TmuxSink should not be available when $TMUX is unset")
	}

	// Notify should be a no-op (no error).
	if err := sink.Notify(context.Background(), notify.Message{Title: "t"}); err != nil {
		t.Errorf("TmuxSink.Notify without TMUX: %v", err)
	}
}

func TestBeepSink(t *testing.T) {
	sink := notify.NewBeepSink()
	if sink.Name() != "beeep" {
		t.Errorf("unexpected name: %s", sink.Name())
	}
	// Available always returns true (channel probes at Notify time).
	if !sink.Available() {
		t.Error("BeepSink.Available should return true")
	}
	// In CI / headless, beeep may fail — we just verify no panic.
	_ = sink.Notify(context.Background(), notify.Message{
		Title:   "Forge Test",
		Summary: "unit test notification",
	})
}

func TestEnvProbe(t *testing.T) {
	// CI environment — notifications unlikely.
	os.Setenv("CI", "true")
	os.Unsetenv("DBUS_SESSION_BUS_ADDRESS")
	os.Unsetenv("DISPLAY")
	defer os.Unsetenv("CI")

	p := notify.Probe()
	if p.NotifyLikelyReachesUser() {
		t.Error("CI env should mark notifications as unlikely")
	}
	if !p.RecommendAutoResolve() {
		t.Error("CI env should recommend auto-resolve")
	}
}

func TestEnvProbe_Desktop(t *testing.T) {
	os.Unsetenv("CI")
	os.Unsetenv("SSH_TTY")
	os.Unsetenv("SSH_CONNECTION")
	os.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/run/user/1000/bus")
	defer os.Unsetenv("DBUS_SESSION_BUS_ADDRESS")

	p := notify.Probe()
	if !p.DBusSession {
		t.Error("expected DBusSession to be true")
	}
	if !p.NotifyLikelyReachesUser() {
		t.Error("desktop env should predict notifications reachable")
	}
}

func TestDefaultChannels(t *testing.T) {
	dir := t.TempDir()
	channels := notify.DefaultChannels(dir, nil)
	if len(channels) == 0 {
		t.Fatal("DefaultChannels returned empty list")
	}
	// First two must be file + banner (guaranteed floor).
	if channels[0].Name() != "file" {
		t.Errorf("first channel should be file, got %s", channels[0].Name())
	}
	if channels[1].Name() != "banner" {
		t.Errorf("second channel should be banner, got %s", channels[1].Name())
	}
}
