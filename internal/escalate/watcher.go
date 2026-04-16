package escalate

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	debounceInterval  = 250 * time.Millisecond
	stabilityInterval = 20 * time.Millisecond
	pollInterval      = 2 * time.Second
	answerFile        = "answer.md"
)

// Watch monitors runDir for answer.md events. onContent is called with file
// content when a stable read is available; return true to stop watching.
// Uses fsnotify by default; falls back to polling when netFS is true.
func Watch(ctx context.Context, runDir string, onContent func([]byte) bool, netFS bool) error {
	if netFS {
		return pollLoop(ctx, runDir, onContent)
	}
	return fsnotifyLoop(ctx, runDir, onContent)
}

// fsnotifyLoop uses fsnotify directory watch with 250ms debounce.
func fsnotifyLoop(ctx context.Context, runDir string, onContent func([]byte) bool) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()

	if err := w.Add(runDir); err != nil {
		return err
	}

	var debounceTimer *time.Timer
	resetDebounce := func() {
		if debounceTimer != nil {
			debounceTimer.Stop()
		}
		debounceTimer = time.AfterFunc(debounceInterval, func() {
			if tryReadAnswer(runDir, onContent) {
				w.Close() // signal the loop to exit
			}
		})
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-w.Events:
			if !ok {
				return nil // watcher closed (answer consumed)
			}
			base := filepath.Base(event.Name)
			if base != answerFile {
				continue
			}
			if isSidecar(event.Name) {
				continue
			}
			if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) != 0 {
				resetDebounce()
			}
		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			_ = err // log in a real implementation; don't abort
		}
	}
}

// pollLoop polls for answer.md at 2-second intervals (network-FS fallback).
func pollLoop(ctx context.Context, runDir string, onContent func([]byte) bool) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if tryReadAnswer(runDir, onContent) {
				return nil
			}
		}
	}
}

// tryReadAnswer performs the stability check and calls onContent if stable.
// Returns true when onContent returned true (answer consumed).
func tryReadAnswer(runDir string, onContent func([]byte) bool) bool {
	answerPath := filepath.Join(runDir, answerFile)

	// Size-stability check: stat twice 20ms apart.
	stat1, err := os.Stat(answerPath)
	if err != nil {
		return false
	}
	time.Sleep(stabilityInterval)
	stat2, err := os.Stat(answerPath)
	if err != nil {
		return false
	}
	if stat1.Size() != stat2.Size() || !stat1.ModTime().Equal(stat2.ModTime()) {
		return false // still being written
	}

	data, err := os.ReadFile(answerPath)
	if err != nil || len(data) == 0 {
		return false
	}
	return onContent(data)
}
