package middleware

import (
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/ReilEgor/GitHubRepoNotifier/shared/ctxlog"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/transport/http/dto"
)

const (
	// HeaderRequestID carries the ID of the business process.
	HeaderRequestID = "X-Request-ID"
	// HeaderAPIKey carries the API key.
	HeaderAPIKey = "X-API-Key"

	maxRequestIDLen = 128
)

// RequestID puts the request ID into the request context and the response header.
// The client's X-Request-ID is used when present and sane, otherwise a new UUID is created.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(HeaderRequestID)
		if id == "" || len(id) > maxRequestIDLen {
			id = uuid.NewString()
		}
		c.Header(HeaderRequestID, id)
		c.Request = c.Request.WithContext(ctxlog.WithRequestID(c.Request.Context(), id))
		c.Next()
	}
}

// Recovery turns a panic into a 500 response and logs it with the stack.
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				slog.ErrorContext(c.Request.Context(), "panic recovered",
					slog.String("panic", fmt.Sprint(r)),
					slog.String("stack", string(debug.Stack())),
				)
				c.AbortWithStatusJSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "internal error"})
			}
		}()
		c.Next()
	}
}

// APIKey rejects requests whose X-API-Key header differs from key.
func APIKey(key string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if subtle.ConstantTimeCompare([]byte(c.GetHeader(HeaderAPIKey)), []byte(key)) != 1 {
			Fail(c, ErrUnauthorized)
			c.Abort()
			return
		}
		c.Next()
	}
}
