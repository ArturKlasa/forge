package notify

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

// BannerSink writes a loud ASCII banner to the configured writer AND to
// /dev/tty so it bypasses --quiet / stdout redirection. Always available.
type BannerSink struct {
	w io.Writer
}

func NewBannerSink(w io.Writer) *BannerSink {
	if w == nil {
		w = os.Stdout
	}
	return &BannerSink{w: w}
}

func (b *BannerSink) Name() string    { return "banner" }
func (b *BannerSink) Available() bool { return true }

func (b *BannerSink) Notify(_ context.Context, msg Message) error {
	line := strings.Repeat("=", 72)
	banner := fmt.Sprintf("\n%s\n%s\n%s\n%s\n\n", line, msg.Title, msg.Body, line)

	// Write to configured writer (stdout / test buffer).
	if _, err := fmt.Fprint(b.w, banner); err != nil {
		return err
	}

	// Also write to /dev/tty when it differs from our writer (bypasses redirection).
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err == nil {
		defer tty.Close()
		_, _ = fmt.Fprint(tty, banner)
	}
	return nil
}
