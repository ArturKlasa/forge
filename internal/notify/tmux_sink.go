package notify

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// TmuxSink sends a display-message to the current tmux session.
// Available only when $TMUX is set in the environment.
type TmuxSink struct {
	tmuxEnv string // value of $TMUX at construction time
}

func NewTmuxSink() *TmuxSink {
	return &TmuxSink{tmuxEnv: os.Getenv("TMUX")}
}

func (t *TmuxSink) Name() string    { return "tmux" }
func (t *TmuxSink) Available() bool { return t.tmuxEnv != "" }

func (t *TmuxSink) Notify(ctx context.Context, msg Message) error {
	if !t.Available() {
		return nil
	}
	text := fmt.Sprintf("[FORGE] %s: %s", msg.Title, msg.Summary)
	cmd := exec.CommandContext(ctx, "tmux", "display-message", text)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux display-message: %w: %s", err, out)
	}
	return nil
}
