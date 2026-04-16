package notify

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// FileSink writes the ESCALATION sentinel file to the run directory.
// Always available; forms part of the guaranteed floor.
type FileSink struct {
	runDir string
}

func NewFileSink(runDir string) *FileSink { return &FileSink{runDir: runDir} }

func (f *FileSink) Name() string      { return "file" }
func (f *FileSink) Available() bool   { return f.runDir != "" }
func (f *FileSink) Notify(_ context.Context, msg Message) error {
	if f.runDir == "" {
		return nil
	}
	content := fmt.Sprintf("%s — %s\n", msg.Title, msg.Summary)
	path := filepath.Join(f.runDir, "ESCALATION")
	return os.WriteFile(path, []byte(content), 0o644)
}
