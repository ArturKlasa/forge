package notify

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"
)

// Message describes a notification event.
type Message struct {
	Title   string // short heading, e.g. "ESCALATION"
	Summary string // one-line for OSC / tmux / beeep
	Body    string // full detail for banner; unused by compact channels
}

// NotifyAttempt records a single channel's outcome.
type NotifyAttempt struct {
	Channel string
	OK      bool
	Err     string
}

// Channel is a single notification sink.
type Channel interface {
	Name() string
	Available() bool
	Notify(ctx context.Context, msg Message) error
}

// NotifyAll fires every channel in order, unconditionally (fail-loud policy).
// Results are logged via slog.
func NotifyAll(ctx context.Context, channels []Channel, msg Message) []NotifyAttempt {
	attempts := make([]NotifyAttempt, 0, len(channels))
	for _, ch := range channels {
		a := NotifyAttempt{Channel: ch.Name(), OK: true}
		if err := ch.Notify(ctx, msg); err != nil {
			a.OK = false
			a.Err = err.Error()
			slog.Warn("notify_channel_failed", "channel", ch.Name(), "err", err)
		}
		attempts = append(attempts, a)
	}
	return attempts
}

// DefaultChannels returns the standard cascade order for a run directory.
// Banner and FileSink are always first — they form the guaranteed floor.
func DefaultChannels(runDir string, output io.Writer) []Channel {
	if output == nil {
		output = os.Stdout
	}
	return []Channel{
		NewFileSink(runDir),
		NewBannerSink(output),
		NewOSCSink(output),
		NewTmuxSink(),
		NewBeepSink(),
	}
}

// EnvProbe inspects the current process environment to estimate which
// notification channels are likely to reach the user.
type EnvProbe struct {
	DBusSession bool // $DBUS_SESSION_BUS_ADDRESS set
	Display     bool // $DISPLAY or $WAYLAND_DISPLAY set
	SSHSession  bool // $SSH_TTY or $SSH_CONNECTION set
	TmuxSession bool // $TMUX set
	IsWSL       bool // /proc/version contains "microsoft" or "WSL"
	IsCI        bool // $CI set
}

// Probe reads the current environment.
func Probe() EnvProbe {
	p := EnvProbe{
		DBusSession: os.Getenv("DBUS_SESSION_BUS_ADDRESS") != "",
		Display:     os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "",
		SSHSession:  os.Getenv("SSH_TTY") != "" || os.Getenv("SSH_CONNECTION") != "",
		TmuxSession: os.Getenv("TMUX") != "",
		IsCI:        os.Getenv("CI") != "",
	}
	if data, err := os.ReadFile("/proc/version"); err == nil {
		v := string(data)
		for _, kw := range []string{"microsoft", "Microsoft", "WSL"} {
			if contains(v, kw) {
				p.IsWSL = true
				break
			}
		}
	}
	return p
}

// NotifyLikelyReachesUser returns true when desktop OS notifications are
// expected to be visible. This is a heuristic — it can have false positives.
func (p EnvProbe) NotifyLikelyReachesUser() bool {
	if p.IsCI {
		return false
	}
	if p.SSHSession && !p.TmuxSession {
		return false
	}
	return p.DBusSession || p.Display
}

// RecommendAutoResolve returns true when the environment suggests that the user
// is unlikely to see desktop notifications promptly and auto-resolve may help.
func (p EnvProbe) RecommendAutoResolve() bool {
	return !p.NotifyLikelyReachesUser()
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}()
}

// SendTestNotify fires all channels with a test message; returns results.
// Used by `forge doctor --test-notify`.
func SendTestNotify(ctx context.Context, channels []Channel, output io.Writer) []NotifyAttempt {
	msg := Message{
		Title:   "Forge Notification Test",
		Summary: "If you see this, OS notifications are working.",
		Body:    fmt.Sprintf("Sent at %s. If you did NOT receive a desktop notification, OS notifications may be unreliable.", time.Now().UTC().Format(time.RFC3339)),
	}
	return NotifyAll(ctx, channels, msg)
}
