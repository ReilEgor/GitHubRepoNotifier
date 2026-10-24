package middleware_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ReilEgor/GitHubRepoNotifier/shared/ctxlog"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/domain/model"
	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/transport/http/middleware"
)

func engine(handler gin.HandlerFunc, extra ...gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RequestID(), middleware.Recovery(), middleware.HandleErrors())
	r.Use(extra...)
	r.POST("/api/v1/subscribe", handler)
	return r
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(ctxlog.NewHandler(slog.NewJSONHandler(&buf, nil))))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestHandleErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantCode   int
		wantError  string
		wantErrors []string
	}{
		{
			"error - invalid email",
			fmt.Errorf("wrapped: %w", model.ErrInvalidEmailFormat),
			400,
			"invalid email format",
			nil,
		},
		{"error - email required", model.ErrEmailRequired, 400, "email is required", nil},
		{
			"error - invalid repo",
			model.ErrInvalidRepoFormat,
			400,
			"invalid repository format (expected 'owner/repo')",
			nil,
		},
		{"error - body", middleware.BodyError{Err: errors.New("eof")}, 400, "invalid request body: eof", nil},
		{
			"error - validation list",
			middleware.ValidationError{Errs: []error{model.ErrInvalidEmailFormat, model.ErrInvalidRepoFormat}},
			400,
			"",
			[]string{"invalid email format", "invalid repository format (expected 'owner/repo')"},
		},
		{"error - unauthorized", middleware.ErrUnauthorized, 401, "unauthorized: invalid or missing API key", nil},
		{
			"error - repository not found",
			fmt.Errorf("a: %w", model.ErrRepositoryNotFound),
			404,
			"repository not found",
			nil,
		},
		{"error - invalid token", fmt.Errorf("a: %w", model.ErrInvalidToken), 404, "invalid or expired token", nil},
		{"error - no releases", model.ErrRepositoryHasNoReleases, 422, "repository has no releases", nil},
		{"error - rate limited", middleware.ErrRateLimited, 429, "limit exceeded", nil},
		{
			"error - service unavailable",
			fmt.Errorf("a: %w", model.ErrServiceUnavailable),
			503,
			"GitHub API is currently unavailable, please try again later",
			nil,
		},
		{"error - unknown error hides details", errors.New("pq: password=hunter2"), 500, "failed to subscribe", nil},
		{"error - database timeout", context.DeadlineExceeded, 500, "failed to subscribe", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := captureLogs(t)
			r := engine(func(c *gin.Context) { middleware.Fail(c, tt.err) })
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/subscribe", nil)
			req.Header.Set(middleware.HeaderRequestID, "req-42")
			rec := httptest.NewRecorder()

			r.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantCode, rec.Code)
			var body struct {
				Error  string   `json:"error"`
				Errors []string `json:"errors"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, tt.wantError, body.Error)
			assert.Equal(t, tt.wantErrors, body.Errors)

			assert.Equal(t, 1, bytes.Count(logs.Bytes(), []byte("\n")), "an error is logged exactly once")
			assert.Contains(t, logs.String(), `"request_id":"req-42"`)
			assert.NotContains(t, logs.String(), `"initiator"`)
		})
	}
}

func TestRecovery(t *testing.T) {
	logs := captureLogs(t)
	r := engine(func(*gin.Context) { panic("boom") })
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/subscribe", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	assert.Equal(t, 500, rec.Code)
	assert.JSONEq(t, `{"error":"internal error"}`, rec.Body.String())
	assert.Contains(t, logs.String(), `"request_id"`)
	assert.Contains(t, logs.String(), `"stack"`)
}

type fakeLimiter struct {
	allowed bool
	err     error
}

func (f fakeLimiter) Allow(context.Context, string) (bool, error) { return f.allowed, f.err }

func TestRateLimit(t *testing.T) {
	tests := []struct {
		name     string
		limiter  fakeLimiter
		wantCode int
	}{
		{"success - under the limit", fakeLimiter{allowed: true}, 200},
		{"error - over the limit", fakeLimiter{allowed: false}, 429},
		{"success - limiter failure lets the request through", fakeLimiter{err: errors.New("redis down")}, 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := captureLogs(t)
			r := engine(func(c *gin.Context) { c.Status(http.StatusOK) }, middleware.RateLimit(tt.limiter))
			rec := httptest.NewRecorder()

			r.ServeHTTP(
				rec,
				httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/subscribe", nil),
			)

			assert.Equal(t, tt.wantCode, rec.Code)
			if tt.limiter.err != nil {
				assert.Contains(t, logs.String(), `"level":"WARN"`)
			}
		})
	}
}
