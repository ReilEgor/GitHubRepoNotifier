// Package ctxlog carries the request ID and the initiator through context and adds them to every log record.
package ctxlog

import (
	"context"
	"log/slog"
)

type (
	logKey       struct{}
	reqIDKey     struct{}
	initiatorKey struct{}
)

// InitiatorSystem is the initiator of background work.
const InitiatorSystem = "system"

// InitiatorUnknown is the initiator when the subscription behind a token could not be found.
const InitiatorUnknown = "unknown"

// WithLogger returns a context that carries l.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, logKey{}, l)
}

// FromCtx returns the logger stored in ctx, or the default logger.
func FromCtx(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(logKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}

// WithRequestID returns a context that carries the request ID.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, reqIDKey{}, id)
}

// RequestID returns the request ID stored in ctx, or an empty string.
func RequestID(ctx context.Context) string {
	if id, ok := ctx.Value(reqIDKey{}).(string); ok {
		return id
	}
	return ""
}

// WithInitiator returns a context that carries the initiator of the request.
func WithInitiator(ctx context.Context, initiator string) context.Context {
	return context.WithValue(ctx, initiatorKey{}, initiator)
}

// Initiator returns the initiator stored in ctx, or an empty string.
func Initiator(ctx context.Context) string {
	if v, ok := ctx.Value(initiatorKey{}).(string); ok {
		return v
	}
	return ""
}

// Handler wraps a slog.Handler and adds request_id and initiator from the record context.
type Handler struct {
	slog.Handler
}

// NewHandler returns a Handler that delegates to next.
func NewHandler(next slog.Handler) *Handler {
	return &Handler{Handler: next}
}

// Handle adds the request ID and the initiator found in ctx to the record.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	if id := RequestID(ctx); id != "" {
		r.AddAttrs(slog.String("request_id", id))
	}
	if who := Initiator(ctx); who != "" {
		r.AddAttrs(slog.String("initiator", who))
	}
	return h.Handler.Handle(ctx, r) //nolint:wrapcheck // the wrapped handler's error is passed through unchanged.
}

// WithAttrs returns a Handler whose inner handler carries attrs.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{Handler: h.Handler.WithAttrs(attrs)}
}

// WithGroup returns a Handler whose inner handler opens the group name.
func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{Handler: h.Handler.WithGroup(name)}
}
