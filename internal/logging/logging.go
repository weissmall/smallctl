// Package logging configures the structured logger for smallctl.
// It wraps log/slog with a custom handler that filters by numeric level
// and writes to both stdout and an optional log file.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// Valid log levels.
const (
	LevelQuiet   = 0
	LevelError   = 1
	LevelWarn    = 2
	LevelInfo    = 3
	LevelDebug   = 4
	LevelVerbose = 5
)

// LevelNames maps numeric levels to slog.Level for the handler.
var LevelNames = map[int]slog.Level{
	LevelQuiet:   slog.Level(99), // above all standard levels — filters everything
	LevelError:   slog.LevelError,
	LevelWarn:    slog.LevelWarn,
	LevelInfo:    slog.LevelInfo,
	LevelDebug:   slog.LevelDebug,
	LevelVerbose: slog.Level(-8), // below debug — shows everything
}

// Setup creates a new slog.Logger that writes to stdout and optionally to a log file.
//
// Parameters:
//   - level: 0=quiet .. 5=verbose. Defaults to 3 (Info) if out of range.
//   - logFile: path to a log file. If empty, only stdout is used.
//     If the file cannot be opened, a warning is logged to stderr and
//     stdout-only logging continues.
//
// Returns a configured *slog.Logger. The caller is responsible for closing
// the log file when the server shuts down.
func Setup(level int, logFile string) (*slog.Logger, func() error) {
	// Validate and clamp level to 0..5 range.
	if level < 0 || level > 5 {
		level = LevelInfo
	}
	minLevel := LevelNames[level]

	var writers []io.Writer
	writers = append(writers, os.Stdout)
	var fileCloser io.Closer

	// If a log file is specified, try to open it.
	if logFile != "" {
		dir := filepath.Dir(logFile)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "smallctl: warning: cannot create log directory %s: %v\n", dir, err)
		} else {
			f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err != nil {
				fmt.Fprintf(os.Stderr, "smallctl: warning: cannot open log file %s: %v, logging to stdout only\n", logFile, err)
			} else {
				writers = append(writers, f)
				fileCloser = f
			}
		}
	}

	// Build the multi-writer and underlying text handler.
	writer := io.MultiWriter(writers...)
	baseHandler := slog.NewTextHandler(writer, &slog.HandlerOptions{
		Level: minLevel,
	})

	// Wrap in our level-filtering handler.
	handler := &levelHandler{
		minLevel: minLevel,
		handler:  baseHandler,
	}

	logger := slog.New(handler)

	cleanup := func() error {
		if fileCloser != nil {
			return fileCloser.Close()
		}
		return nil
	}

	return logger, cleanup
}

// levelHandler is a slog.Handler wrapper that enforces a minimum log level.
type levelHandler struct {
	minLevel slog.Level
	handler  slog.Handler
}

// Ensure levelHandler implements slog.Handler.
var _ slog.Handler = (*levelHandler)(nil)

// Enabled reports whether the given level is at or above the minimum.
func (h *levelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.minLevel
}

// Handle processes a log record. Delegates to the underlying handler.
func (h *levelHandler) Handle(ctx context.Context, r slog.Record) error {
	return h.handler.Handle(ctx, r)
}

// WithAttrs returns a new levelHandler with additional attributes.
func (h *levelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &levelHandler{
		minLevel: h.minLevel,
		handler:  h.handler.WithAttrs(attrs),
	}
}

// WithGroup returns a new levelHandler with a group name.
func (h *levelHandler) WithGroup(name string) slog.Handler {
	return &levelHandler{
		minLevel: h.minLevel,
		handler:  h.handler.WithGroup(name),
	}
}

// writerCloser wraps an io.Writer with a Close method for cleanup.
type writerCloser struct {
	io.Writer
	closeFn func() error
}

func (w *writerCloser) Close() error {
	if w.closeFn != nil {
		return w.closeFn()
	}
	return nil
}
