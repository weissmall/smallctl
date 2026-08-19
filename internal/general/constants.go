package general

import (
	"log/slog"
	"time"
)

const (
	DefaultTimeout    = 30
	DefaultShell      = "bash"
	DefaultNotify     = "off"
	EnvResolveTimeout = 5 * time.Second
	ShutdownTimeout   = 5 * time.Second
	MaxRequestSize    = 16 * 1024

	LevelQuiet   = 0
	LevelError   = 1
	LevelWarn    = 2
	LevelInfo    = 3
	LevelDebug   = 4
	LevelVerbose = 5

	NotifyLevelOff   = "off"
	NotifyLevelError = "error"
	NotifyLevelAll   = "all"
)

var (
	LevelNames = map[int]slog.Level{
		LevelQuiet:   slog.Level(99),
		LevelError:   slog.LevelError,
		LevelWarn:    slog.LevelWarn,
		LevelInfo:    slog.LevelInfo,
		LevelDebug:   slog.LevelDebug,
		LevelVerbose: slog.Level(-8),
	}

	ValidNotifyValues = map[string]bool{
		NotifyLevelOff:   true,
		NotifyLevelError: true,
		NotifyLevelAll:   true,
	}
)
