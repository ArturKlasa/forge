package notify

import (
	"context"

	"github.com/gen2brain/beeep"
)

// BeepSink fires an OS-native desktop notification via gen2brain/beeep.
// Failure is expected in headless/SSH/WSL environments; the cascade continues.
type BeepSink struct{}

func NewBeepSink() *BeepSink { return &BeepSink{} }

func (b *BeepSink) Name() string    { return "beeep" }
func (b *BeepSink) Available() bool { return true }

func (b *BeepSink) Notify(_ context.Context, msg Message) error {
	return beeep.Notify(msg.Title, msg.Summary, "")
}
