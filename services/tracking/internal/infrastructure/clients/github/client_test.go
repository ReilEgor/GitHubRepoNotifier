package github_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/domain/model"
	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/infrastructure/clients/github"
)

// fakeGitHub answers every request with the next scripted handler and counts the requests it received.
type fakeGitHub struct {
	srv   *httptest.Server
	calls atomic.Int64
}

func newFake(t *testing.T, handler http.HandlerFunc) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		handler(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitHub) client(token string) *github.Client {
	return github.NewClient(github.Config{
		BaseURL:  f.srv.URL,
		Token:    token,
		Timeout:  100 * time.Millisecond,
		Attempts: 3,
		Backoff:  time.Millisecond,
	})
}

func body(s string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if _, err := io.WriteString(w, s); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
}

func status(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
}

func TestClient_CheckRepository(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		handler   http.HandlerFunc
		wantErr   error
		wantOther bool
		wantCalls int64
	}{
		{name: "success - exists", handler: status(200), wantCalls: 1},
		{name: "error - not found", handler: status(404), wantErr: model.ErrRepositoryNotFound, wantCalls: 1},
		{
			name:      "error - 429 is a rate limit and is not retried",
			handler:   status(429),
			wantErr:   model.ErrRateLimitExceeded,
			wantCalls: 1,
		},
		{
			name: "error - 403 without requests left is a rate limit and is not retried",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.WriteHeader(403)
			},
			wantErr: model.ErrRateLimitExceeded, wantCalls: 1,
		},
		{
			name:      "error - other 403 is a refusal, not a rate limit, and is not retried",
			handler:   status(403),
			wantOther: true,
			wantCalls: 1,
		},
		{name: "error - 401 is not retried", handler: status(401), wantOther: true, wantCalls: 1},
		{
			name:      "error - 500 is retried and then unavailable",
			handler:   status(500),
			wantErr:   model.ErrGitHubUnavailable,
			wantCalls: 3,
		},
		{
			name: "error - slow answer times out on every attempt",
			handler: func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-time.After(2 * time.Second):
				case <-r.Context().Done():
				}
				w.WriteHeader(200)
			},
			wantErr: model.ErrGitHubUnavailable, wantCalls: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake(t, tt.handler)

			err := f.client("").CheckRepository(ctx, "golang/go")

			switch {
			case tt.wantErr != nil:
				require.ErrorIs(t, err, tt.wantErr)
			case tt.wantOther:
				require.Error(t, err)
				for _, domain := range []error{model.ErrRateLimitExceeded, model.ErrGitHubUnavailable, model.ErrRepositoryNotFound} {
					require.NotErrorIs(t, err, domain)
				}
			default:
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantCalls, f.calls.Load())
		})
	}

	t.Run("success - recovers when a retry succeeds", func(t *testing.T) {
		var n atomic.Int64
		f := newFake(t, func(w http.ResponseWriter, _ *http.Request) {
			if n.Add(1) < 3 {
				w.WriteHeader(502)
				return
			}
			w.WriteHeader(200)
		})

		require.NoError(t, f.client("").CheckRepository(ctx, "golang/go"))
		assert.Equal(t, int64(3), f.calls.Load())
	})

	t.Run("success - uses HEAD and sends the token", func(t *testing.T) {
		var method, auth string
		f := newFake(t, func(w http.ResponseWriter, r *http.Request) {
			method, auth = r.Method, r.Header.Get("Authorization")
			w.WriteHeader(200)
		})

		require.NoError(t, f.client("secret").CheckRepository(ctx, "golang/go"))
		assert.Equal(t, http.MethodHead, method)
		assert.Equal(t, "Bearer secret", auth)
	})

	t.Run("error - canceled context stops at once without retries", func(t *testing.T) {
		f := newFake(t, status(500))
		canceled, cancel := context.WithCancel(ctx)
		cancel()

		err := f.client("").CheckRepository(canceled, "golang/go")

		require.ErrorIs(t, err, context.Canceled)
		assert.NotErrorIs(t, err, model.ErrGitHubUnavailable)
	})
}

func TestClient_LatestRelease(t *testing.T) {
	ctx := context.Background()

	t.Run("success - tag returned with GET", func(t *testing.T) {
		var method, path string
		f := newFake(t, func(w http.ResponseWriter, r *http.Request) {
			method, path = r.Method, r.URL.Path
			body(`{"tag_name":"go1.25.3"}`)(w, r)
		})

		got, err := f.client("").LatestRelease(ctx, "golang/go")

		require.NoError(t, err)
		assert.Equal(t, "go1.25.3", got.TagName)
		assert.Equal(t, http.MethodGet, method)
		assert.Equal(t, "/repos/golang/go/releases/latest", path)
	})

	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr error
	}{
		{name: "error - no releases", handler: status(404), wantErr: model.ErrReleaseNotFound},
		{name: "error - rate limit", handler: status(429), wantErr: model.ErrRateLimitExceeded},
		{name: "error - unavailable", handler: status(503), wantErr: model.ErrGitHubUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake(t, tt.handler)

			_, err := f.client("").LatestRelease(ctx, "golang/go")

			require.ErrorIs(t, err, tt.wantErr)
		})
	}

	t.Run("error - answer without a tag counts as no release", func(t *testing.T) {
		f := newFake(t, body(`{}`))

		_, err := f.client("").LatestRelease(ctx, "golang/go")

		require.ErrorIs(t, err, model.ErrReleaseNotFound)
	})

	t.Run("error - malformed json", func(t *testing.T) {
		f := newFake(t, body(`{`))

		_, err := f.client("").LatestRelease(ctx, "golang/go")

		require.Error(t, err)
	})
}

func TestClient_circuitBreaker(t *testing.T) {
	ctx := context.Background()

	t.Run("success - a failed series is one failure, so two failed series keep it closed", func(t *testing.T) {
		f := newFake(t, status(500))
		c := f.client("")

		for range 2 {
			require.ErrorIs(t, c.CheckRepository(ctx, "golang/go"), model.ErrGitHubUnavailable)
		}
		assert.Equal(t, int64(6), f.calls.Load(), "two series of three attempts")

		require.ErrorIs(t, c.CheckRepository(ctx, "golang/go"), model.ErrGitHubUnavailable)
		assert.Equal(t, int64(9), f.calls.Load(), "the third series still reaches GitHub")
	})

	t.Run("error - three failed series open it and the next call never reaches GitHub", func(t *testing.T) {
		f := newFake(t, status(500))
		c := f.client("")
		for range 3 {
			require.ErrorIs(t, c.CheckRepository(ctx, "golang/go"), model.ErrGitHubUnavailable)
		}
		before := f.calls.Load()

		err := c.CheckRepository(ctx, "golang/go")

		require.ErrorIs(t, err, model.ErrGitHubUnavailable)
		assert.Equal(t, before, f.calls.Load())
	})

	t.Run("success - answers that prove GitHub is up never open it", func(t *testing.T) {
		f := newFake(t, status(404))
		c := f.client("")

		for range 10 {
			require.ErrorIs(t, c.CheckRepository(ctx, "golang/go"), model.ErrRepositoryNotFound)
		}
		assert.Equal(t, int64(10), f.calls.Load())
	})
}
