package logging

import "log/slog"

const (
	LevelQuiet   = 0
	LevelError   = 1
	LevelWarn    = 2
	LevelInfo    = 3
	LevelDebug   = 4
	LevelVerbose = 5
)

var LevelNames = map[int]slog.Level{
	LevelQuiet:   slog.Level(99),
	LevelError:   slog.LevelError,
	LevelWarn:    slog.LevelWarn,
	LevelInfo:    slog.LevelInfo,
	LevelDebug:   slog.LevelDebug,
	LevelVerbose: slog.Level(-8),
}
