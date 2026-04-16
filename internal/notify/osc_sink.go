package notify

import (
	"context"
	"fmt"
	"io"
	"os"
)

// OSCSink emits OSC 9 escape + terminal bell to stdout and /dev/tty.
// Supported by iTerm2, WezTerm, Kitty, ConEmu. Safe to emit on unsupporting
// terminals (unknown OSC sequences are silently discarded).
type OSCSink struct {
	w io.Writer
}

func NewOSCSink(w io.Writer) *OSCSink {
	if w == nil {
		w = os.Stdout
	}
	return &OSCSink{w: w}
}

func (o *OSCSink) Name() string    { return "osc9" }
func (o *OSCSink) Available() bool { return true }

func (o *OSCSink) Notify(_ context.Context, msg Message) error {
	// OSC 9 notification + terminal bell.
	seq := fmt.Sprintf("\033]9;%s\007\a", msg.Summary)

	if _, err := fmt.Fprint(o.w, seq); err != nil {
		return err
	}

	// Best-effort write to /dev/tty.
	if tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
		defer tty.Close()
		_, _ = fmt.Fprint(tty, seq)
	}
	return nil
}
