package logs

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// StreamHandler wraps another slog.Handler (the real stdout sink, text or JSON)
// and additionally publishes a human-readable rendering of every record to the
// Hub, so the live log stream mirrors the terminal output.
type StreamHandler struct {
	inner slog.Handler
	hub   *Hub
}

// NewStreamHandler wraps inner so handled records are also published to hub.
func NewStreamHandler(inner slog.Handler, hub *Hub) slog.Handler {
	return &StreamHandler{inner: inner, hub: hub}
}

func (h *StreamHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *StreamHandler) Handle(ctx context.Context, r slog.Record) error {
	h.hub.Publish(formatRecord(&r))
	return h.inner.Handle(ctx, r)
}

func (h *StreamHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &StreamHandler{inner: h.inner.WithAttrs(attrs), hub: h.hub}
}

func (h *StreamHandler) WithGroup(name string) slog.Handler {
	return &StreamHandler{inner: h.inner.WithGroup(name), hub: h.hub}
}

// formatRecord renders a record as a plain, terminal-friendly line:
//
//	2026-08-18 01:36:00.000 INFO  database ready key=value
func formatRecord(r *slog.Record) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %-7s %s", r.Time.Format("2006-01-02 15:04:05.000"), r.Level.String(), r.Message)
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "" {
			return true
		}
		b.WriteByte(' ')
		b.WriteString(a.String())
		return true
	})
	return b.String()
}
