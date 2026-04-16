package proc

import (
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

const defaultGracePeriod = 10 * time.Second

// Option is a functional option for configuring a Wrapper.
type Option func(*Wrapper)

// WithGracePeriod sets the duration Forge waits for a process to exit after
// SIGTERM before escalating to SIGKILL.
func WithGracePeriod(d time.Duration) Option {
	return func(w *Wrapper) { w.gracePeriod = d }
}

// WithStdoutWriter adds an extra io.Writer that receives a copy of stdout.
func WithStdoutWriter(wr io.Writer) Option {
	return func(w *Wrapper) { w.stdoutWriters = append(w.stdoutWriters, wr) }
}

// WithStderrWriter adds an extra io.Writer that receives a copy of stderr.
func WithStderrWriter(wr io.Writer) Option {
	return func(w *Wrapper) { w.stderrWriters = append(w.stderrWriters, wr) }
}

// WithRingBufferSize sets the ring buffer capacity in bytes.
// It must be called before Start().
func WithRingBufferSize(n int) Option {
	return func(w *Wrapper) { w.ringBuf = NewRingBuffer(n) }
}

// Wrapper manages the lifecycle of a single subprocess.
type Wrapper struct {
	cmd         *exec.Cmd
	gracePeriod time.Duration

	stdoutWriters []io.Writer
	stderrWriters []io.Writer
	ringBuf       *RingBuffer

	// forgeKilled is set to true before Forge sends any signal so that the
	// exit classifier can distinguish Forge-initiated termination from
	// external signals.
	forgeKilled atomic.Bool

	result   Result
	waitOnce sync.Once

	// wg tracks goroutines that drain stdout/stderr pipes.
	// Wait() blocks on this before calling cmd.Wait() to avoid deadlocks
	// when the pipe buffers fill up.
	wg sync.WaitGroup
}

// New creates a Wrapper around cmd and applies opts.
// The cmd must not have been started yet.
func New(cmd *exec.Cmd, opts ...Option) *Wrapper {
	w := &Wrapper{
		cmd:         cmd,
		gracePeriod: defaultGracePeriod,
		ringBuf:     NewRingBuffer(defaultRingBufferSize),
	}
	for _, o := range opts {
		o(w)
	}
	return w
}

// Start sets up stdout/stderr plumbing and starts the process.
// It calls the platform-specific platformStart() for process-group / Job Object
// setup before actually launching the child.
func (w *Wrapper) Start() error {
	// Build stdout MultiWriter: ring buffer + any extras.
	stdoutDsts := []io.Writer{w.ringBuf}
	stdoutDsts = append(stdoutDsts, w.stdoutWriters...)
	stdoutMW := io.MultiWriter(stdoutDsts...)

	// Build stderr MultiWriter: ring buffer + any extras.
	stderrDsts := []io.Writer{w.ringBuf}
	stderrDsts = append(stderrDsts, w.stderrWriters...)
	stderrMW := io.MultiWriter(stderrDsts...)

	// Obtain pipes before starting so we can drain them in goroutines.
	stdoutPipe, err := w.cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderrPipe, err := w.cmd.StderrPipe()
	if err != nil {
		return err
	}

	// Apply platform-specific SysProcAttr (process group / Job Object).
	platformStart(w)

	if err := w.cmd.Start(); err != nil {
		return err
	}

	// Platform-specific post-start work (e.g. Windows Job Object assignment).
	if err := platformPostStart(w); err != nil {
		// Best-effort: kill the process we just started to avoid orphans.
		w.cmd.Process.Kill() //nolint:errcheck
		return err
	}

	// Drain stdout and stderr concurrently so the pipe buffers never fill up
	// (which would cause the child to block on writes and deadlock cmd.Wait).
	w.wg.Add(2)
	go func() {
		defer w.wg.Done()
		io.Copy(stdoutMW, stdoutPipe) //nolint:errcheck
	}()
	go func() {
		defer w.wg.Done()
		io.Copy(stderrMW, stderrPipe) //nolint:errcheck
	}()

	return nil
}

// Wait blocks until the process exits and all pipe-draining goroutines finish,
// then classifies the exit and returns the Result.
// It is safe to call Wait multiple times; subsequent calls return the cached Result.
func (w *Wrapper) Wait() Result {
	w.waitOnce.Do(func() {
		// First drain all pipe goroutines to avoid blocking cmd.Wait on full buffers.
		w.wg.Wait()

		err := w.cmd.Wait()
		w.result = classifyExit(w, err)
	})
	return w.result
}

// RingBuffer returns the ring buffer that captures combined stdout+stderr output.
func (w *Wrapper) RingBuffer() *RingBuffer { return w.ringBuf }

// Cmd returns the underlying *exec.Cmd.
func (w *Wrapper) Cmd() *exec.Cmd { return w.cmd }
