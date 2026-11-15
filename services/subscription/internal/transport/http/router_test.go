package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	httptransport "github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/transport/http"
)

// keyRecorder remembers the key the rate limiter was asked about.
type keyRecorder struct {
	mu   sync.Mutex
	keys []string
}

func (k *keyRecorder) Allow(_ context.Context, key string) (bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.keys = append(k.keys, key)
	return true, nil
}

func TestNewRouter_trustedProxies(t *testing.T) {
	tests := []struct {
		name    string
		proxies []string
		wantKey string
	}{
		{"success - no trusted proxies ignores X-Forwarded-For", nil, "10.0.0.5"},
		{"success - header is believed behind a trusted proxy", []string{"10.0.0.0/24"}, "203.0.113.7"},
		{"success - header is ignored from an untrusted peer", []string{"192.168.0.0/24"}, "10.0.0.5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &keyRecorder{}
			router, err := httptransport.NewRouter(nil, rec, apiKey, tt.proxies, http.NotFoundHandler())
			require.NoError(t, err)
			req := httptest.NewRequestWithContext(
				context.Background(), http.MethodGet, "/api/v1/confirm/x", http.NoBody,
			)
			req.RemoteAddr = "10.0.0.5:4321"
			req.Header.Set("X-Forwarded-For", "203.0.113.7")

			router.ServeHTTP(httptest.NewRecorder(), req)

			require.Len(t, rec.keys, 1)
			assert.Equal(t, tt.wantKey, rec.keys[0])
		})
	}

	t.Run("error - invalid proxy address", func(t *testing.T) {
		_, err := httptransport.NewRouter(nil, &keyRecorder{}, apiKey, []string{"not-an-ip"}, http.NotFoundHandler())

		require.Error(t, err)
	})
}
