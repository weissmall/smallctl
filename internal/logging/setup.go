package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"smallctl/internal/general"
)

func Setup(level int, logFile string) (*slog.Logger, func() error) {
	if level < general.LevelQuiet || level > general.LevelVerbose {
		level = general.LevelInfo
	}
	minLevel := general.LevelNames[level]

	var writers []io.Writer
	writers = append(writers, os.Stdout)
	var fileCloser io.Closer

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

	writer := io.MultiWriter(writers...)
	baseHandler := slog.NewTextHandler(writer, &slog.HandlerOptions{
		Level: minLevel,
	})

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
