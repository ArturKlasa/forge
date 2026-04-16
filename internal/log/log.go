// Package log provides structured logging and user-facing styled output for forge.
//
// Two output streams:
//   - Diagnostic stream (slog): machine-readable text/JSON via Out (default: os.Stderr).
//     In --json mode, slog writes to UserOut (stdout) so callers can pipe JSON.
//   - User-facing stream: styled prose via Print methods using lipgloss, written to UserOut.
//
// Color/styling is disabled when NO_COLOR or CI env vars are set, or when stdout is not a TTY.
package log

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// Config holds initialization parameters for the global logger.
type Config struct {
	Verbose bool
	Quiet   bool
	JSON    bool

	// Out is the diagnostic slog stream (default: os.Stderr).
	Out io.Writer
	// UserOut is the user-facing output stream (default: os.Stdout).
	UserOut io.Writer
}

// Logger wraps slog and provides styled user-facing output.
type Logger struct {
	sl          *slog.Logger
	cfg         Config
	renderer    *lipgloss.Renderer
	interactive bool
}

var global *Logger

// Init initialises the global logger from cfg. Must be called before G().
func Init(cfg Config) {
	if cfg.Out == nil {
		cfg.Out = os.Stderr
	}
	if cfg.UserOut == nil {
		cfg.UserOut = os.Stdout
	}

	interactive := checkInteractive(cfg.UserOut)

	level := slog.LevelInfo
	switch {
	case cfg.Verbose:
		level = slog.LevelDebug
	case cfg.Quiet:
		level = slog.LevelWarn
	}

	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if cfg.JSON {
		// JSON mode: emit structured NDJSON to UserOut so callers can parse stdout.
		handler = slog.NewJSONHandler(cfg.UserOut, opts)
	} else {
		// Text mode: strip timestamp for clean CLI output, write to Out (stderr).
		opts.ReplaceAttr = stripTimestamp
		handler = slog.NewTextHandler(cfg.Out, opts)
	}

	renderer := lipgloss.NewRenderer(cfg.UserOut)
	if !interactive {
		renderer.SetColorProfile(0) // disable color
	}

	global = &Logger{
		sl:          slog.New(handler),
		cfg:         cfg,
		renderer:    renderer,
		interactive: interactive,
	}

	slog.SetDefault(global.sl)
}

// InitWithHandler initialises the global logger with a custom slog.Handler.
// Intended for tests that need to capture log output.
func InitWithHandler(h slog.Handler) {
	global = &Logger{
		sl:          slog.New(h),
		cfg:         Config{Out: os.Stderr, UserOut: os.Stdout},
		renderer:    lipgloss.NewRenderer(os.Stdout),
		interactive: false,
	}
	slog.SetDefault(global.sl)
}

// G returns the global Logger. Panics if Init has not been called.
func G() *Logger {
	if global == nil {
		panic("log.Init must be called before log.G()")
	}
	return global
}

// -- slog passthrough methods -------------------------------------------------

func (l *Logger) Info(msg string, args ...any)  { l.sl.Info(msg, args...) }
func (l *Logger) Warn(msg string, args ...any)  { l.sl.Warn(msg, args...) }
func (l *Logger) Error(msg string, args ...any) { l.sl.Error(msg, args...) }
func (l *Logger) Debug(msg string, args ...any) { l.sl.Debug(msg, args...) }

// SlogLogger returns the underlying *slog.Logger for callers that need it directly.
func (l *Logger) SlogLogger() *slog.Logger { return l.sl }

// -- user-facing output -------------------------------------------------------

// Print writes msg to UserOut. Suppressed in --quiet mode.
func (l *Logger) Print(msg string) {
	if l.cfg.Quiet {
		return
	}
	fmt.Fprintln(l.cfg.UserOut, msg)
}

// Println is Print with a trailing newline already included (alias for Print).
func (l *Logger) Println(msg string) { l.Print(msg) }

// Style returns a new lipgloss Style from this logger's renderer.
// Callers can use it to style text respecting the TTY/NO_COLOR state.
func (l *Logger) Style() lipgloss.Style { return l.renderer.NewStyle() }

// Renderer returns the lipgloss Renderer (useful for complex layouts).
func (l *Logger) Renderer() *lipgloss.Renderer { return l.renderer }

// Interactive reports whether color and styling are active.
func (l *Logger) Interactive() bool { return l.interactive }

// -- package-level convenience wrappers ---------------------------------------

// Info logs at INFO level on the global logger.
func Info(msg string, args ...any) { G().Info(msg, args...) }

// Warn logs at WARN level on the global logger.
func Warn(msg string, args ...any) { G().Warn(msg, args...) }

// Error logs at ERROR level on the global logger.
func Error(msg string, args ...any) { G().Error(msg, args...) }

// Debug logs at DEBUG level on the global logger.
func Debug(msg string, args ...any) { G().Debug(msg, args...) }

// Print writes a user-facing message via the global logger.
func Print(msg string) { G().Print(msg) }

// -- helpers ------------------------------------------------------------------

// checkInteractive returns true when color/styling should be active.
// Disabled by NO_COLOR, CI, or a non-TTY writer.
func checkInteractive(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("CI") != "" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// stripTimestamp removes the time key from slog text-handler output.
func stripTimestamp(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 && a.Key == slog.TimeKey {
		return slog.Attr{}
	}
	return a
}
