// Package middleware holds the Gin middleware of the subscription HTTP API.
package middleware

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/domain/model"
	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/transport/http/dto"
)

// Errors raised by the transport layer itself.
var (
	ErrUnauthorized = errors.New("unauthorized: invalid or missing API key")
	ErrRateLimited  = errors.New("limit exceeded")
)

// BodyError reports a request body that cannot be used.
type BodyError struct{ Err error }

// Error implements error.
func (e BodyError) Error() string { return "invalid request body: " + e.Err.Error() }

// Unwrap returns the cause.
func (e BodyError) Unwrap() error { return e.Err }

// ValidationError reports every field that failed validation.
type ValidationError struct{ Errs []error }

// Error implements error.
func (e ValidationError) Error() string {
	msgs := make([]string, 0, len(e.Errs))
	for _, err := range e.Errs {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// Unwrap returns the individual errors.
func (e ValidationError) Unwrap() []error { return e.Errs }

type rule struct {
	target error
	status int
	text   string
	level  slog.Level
}

const unavailableText = "GitHub API is currently unavailable, please try again later"

// catalog maps domain errors to responses; see docs/spec/errors.md.
var catalog = []rule{
	{ErrUnauthorized, http.StatusUnauthorized, ErrUnauthorized.Error(), slog.LevelWarn},
	{ErrRateLimited, http.StatusTooManyRequests, ErrRateLimited.Error(), slog.LevelWarn},
	{model.ErrEmailRequired, http.StatusBadRequest, model.ErrEmailRequired.Error(), slog.LevelWarn},
	{model.ErrInvalidEmailFormat, http.StatusBadRequest, model.ErrInvalidEmailFormat.Error(), slog.LevelWarn},
	{model.ErrInvalidRepoFormat, http.StatusBadRequest, model.ErrInvalidRepoFormat.Error(), slog.LevelWarn},
	{model.ErrRepositoryNotFound, http.StatusNotFound, "repository not found", slog.LevelWarn},
	{model.ErrRepositoryHasNoReleases, http.StatusUnprocessableEntity, "repository has no releases", slog.LevelWarn},
	{model.ErrInvalidToken, http.StatusNotFound, "invalid or expired token", slog.LevelWarn},
	{model.ErrServiceUnavailable, http.StatusServiceUnavailable, unavailableText, slog.LevelError},
}

// fallbackText is the 500 text of each route.
var fallbackText = map[string]string{
	"POST /api/v1/subscribe":         "failed to subscribe",
	"GET /api/v1/subscriptions":      "failed to list subscriptions",
	"GET /api/v1/confirm/:token":     "failed to confirm subscription",
	"GET /api/v1/unsubscribe/:token": "failed to unsubscribe",
}

const defaultFallbackText = "internal error"

// Fail attaches err to the request; HandleErrors turns it into the response.
func Fail(c *gin.Context, err error) {
	c.Errors = append(c.Errors, &gin.Error{Err: err, Type: gin.ErrorTypePrivate})
}

// HandleErrors is the single place that turns an error into a response and a log record.
// Handlers only attach the error with middleware.Fail.
func HandleErrors() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if len(c.Errors) == 0 || c.Writer.Written() {
			return
		}
		err := c.Errors.Last().Err
		statusCode, body, level := resolve(c, err)

		slog.Log(c.Request.Context(), level, "request failed",
			slog.String("route", c.Request.Method+" "+c.FullPath()),
			slog.Int("status", statusCode),
			slog.String("error", err.Error()),
		)
		c.AbortWithStatusJSON(statusCode, body)
	}
}

func resolve(c *gin.Context, err error) (int, any, slog.Level) {
	var validation ValidationError
	if errors.As(err, &validation) {
		texts := make([]string, 0, len(validation.Errs))
		for _, e := range validation.Errs {
			texts = append(texts, e.Error())
		}
		return http.StatusBadRequest, dto.ErrorsResponse{Errors: texts}, slog.LevelWarn
	}
	var bodyErr BodyError
	if errors.As(err, &bodyErr) {
		return http.StatusBadRequest, dto.ErrorResponse{Error: bodyErr.Error()}, slog.LevelWarn
	}
	for _, r := range catalog {
		if errors.Is(err, r.target) {
			return r.status, dto.ErrorResponse{Error: r.text}, r.level
		}
	}
	text, ok := fallbackText[c.Request.Method+" "+c.FullPath()]
	if !ok {
		text = defaultFallbackText
	}
	return http.StatusInternalServerError, dto.ErrorResponse{Error: text}, slog.LevelError
}
