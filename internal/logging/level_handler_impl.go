package logging

import (
	"context"
	"log/slog"
)

func (h *levelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.minLevel
}

func (h *levelHandler) Handle(ctx context.Context, r slog.Record) error {
	return h.handler.Handle(ctx, r)
}

func (h *levelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &levelHandler{
		minLevel: h.minLevel,
		handler:  h.handler.WithAttrs(attrs),
	}
}

func (h *levelHandler) WithGroup(name string) slog.Handler {
	return &levelHandler{
		minLevel: h.minLevel,
		handler:  h.handler.WithGroup(name),
	}
}
