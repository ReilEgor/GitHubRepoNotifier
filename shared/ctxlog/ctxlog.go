// Package ctxlog carries the logger and the request ID through context.
package ctxlog

import (
	"context"
	"log/slog"
)

type (
	logKey   struct{}
	reqIDKey struct{}
)

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
