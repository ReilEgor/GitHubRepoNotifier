package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

const unmatchedRoute = "unmatched"

// AccessLog writes one record when a request completes: method, route, status and duration.
// With the request_id that the logging handler adds, the records of one business process read as a path
// from the request through its calls to the response. The route is the template (/confirm/:token),
// never the URL, so tokens stay out of the log.
func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		route := c.FullPath()
		if route == "" {
			route = unmatchedRoute
		}
		level := slog.LevelInfo
		if route == "/health" || route == "/version" {
			level = slog.LevelDebug
		}
		slog.LogAttrs(c.Request.Context(), level, "request completed",
			slog.String("method", c.Request.Method),
			slog.String("route", route),
			slog.Int("status", c.Writer.Status()),
			slog.Duration("duration", time.Since(start)),
		)
	}
}
