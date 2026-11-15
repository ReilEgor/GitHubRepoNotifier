package middleware

import (
	"context"
	"log/slog"

	"github.com/gin-gonic/gin"
)

// Limiter decides whether a caller may make another request.
type Limiter interface {
	// Allow counts one request of key and reports whether it is within the limit.
	Allow(ctx context.Context, key string) (bool, error)
}

// RateLimit rejects callers over the limit. It fails open: if the limiter is broken, the request passes.
func RateLimit(l Limiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		allowed, err := l.Allow(ctx, c.ClientIP())
		if err != nil {
			slog.WarnContext(ctx, "rate limiter unavailable, request allowed", slog.String("error", err.Error()))
			c.Next()
			return
		}
		if !allowed {
			Fail(c, ErrRateLimited)
			c.Abort()
			return
		}
		c.Next()
	}
}
