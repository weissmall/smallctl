package logging

import "log/slog"

// levelHandler is a slog.Handler wrapper that enforces a minimum log level.
type levelHandler struct {
	minLevel slog.Level
	handler  slog.Handler
}
