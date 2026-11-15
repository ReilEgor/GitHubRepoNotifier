package ratelimit_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/infrastructure/ratelimit"
)

func TestParseRate(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		wantLimit  int64
		wantWindow time.Duration
		wantErr    bool
	}{
		{name: "success - per second", in: "10-S", wantLimit: 10, wantWindow: time.Second},
		{name: "success - per minute lowercase", in: "5-m", wantLimit: 5, wantWindow: time.Minute},
		{name: "success - per hour", in: "100-H", wantLimit: 100, wantWindow: time.Hour},
		{name: "error - negative", in: "-1-S", wantErr: true},
		{name: "error - empty", in: "", wantErr: true},
		{name: "error - no unit", in: "10", wantErr: true},
		{name: "error - bad unit", in: "10-D", wantErr: true},
		{name: "error - zero", in: "0-S", wantErr: true},
		{name: "error - not a number", in: "x-S", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limit, window, err := ratelimit.ParseRate(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantLimit, limit)
			assert.Equal(t, tt.wantWindow, window)
		})
	}
}

func TestRedis_Allow(t *testing.T) {
	// step is one call of Allow; advance moves the Redis clock first.
	type step struct {
		advance time.Duration
		key     string
		want    bool
	}
	tests := []struct {
		name    string
		limit   int64
		window  time.Duration
		preset  map[string]string // keys that exist before the first step.
		steps   []step
		wantTTL map[string]time.Duration // checked after the last step.
	}{
		{
			name:   "success - requests over the limit are denied until the window passes",
			limit:  3,
			window: time.Second,
			steps: []step{
				{key: "1.2.3.4", want: true},
				{key: "1.2.3.4", want: true},
				{key: "1.2.3.4", want: true},
				{key: "1.2.3.4", want: false},
				{key: "5.6.7.8", want: true}, // the limit is per caller.
				{advance: 2 * time.Second, key: "1.2.3.4", want: true},
			},
		},
		{
			name:   "success - the exact limit is allowed and one more is not",
			limit:  1,
			window: time.Minute,
			steps:  []step{{key: "k", want: true}, {key: "k", want: false}},
		},
		{
			name:    "success - the first request sets the TTL together with the counter",
			limit:   3,
			window:  time.Minute,
			steps:   []step{{key: "1.2.3.4", want: true}},
			wantTTL: map[string]time.Duration{"ratelimit:1.2.3.4": time.Minute},
		},
		{
			name:   "success - a repeated request does not extend the window",
			limit:  2,
			window: time.Minute,
			steps: []step{
				{key: "k", want: true},
				{advance: 40 * time.Second, key: "k", want: true},
			},
			wantTTL: map[string]time.Duration{"ratelimit:k": 20 * time.Second},
		},
		{
			// The old INCR-then-EXPIRE failure left a counter without a TTL.
			name:   "success - a key left without TTL is healed and expires",
			limit:  3,
			window: time.Second,
			preset: map[string]string{"ratelimit:1.2.3.4": "3"},
			steps: []step{
				{key: "1.2.3.4", want: false},
				{advance: 2 * time.Second, key: "1.2.3.4", want: true}, // the address is not blocked forever.
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr := miniredis.RunT(t)
			for k, v := range tt.preset {
				require.NoError(t, mr.Set(k, v))
			}
			limiter := ratelimit.NewRedis(redis.NewClient(&redis.Options{Addr: mr.Addr()}), tt.limit, tt.window)

			for i, s := range tt.steps {
				mr.FastForward(s.advance)
				ok, err := limiter.Allow(context.Background(), s.key)
				require.NoError(t, err, "step %d", i+1)
				assert.Equal(t, s.want, ok, "step %d", i+1)
			}

			for key, ttl := range tt.wantTTL {
				assert.Equal(t, ttl, mr.TTL(key), key)
			}
		})
	}
}

func TestRedis_Allow_failures(t *testing.T) {
	tests := []struct {
		name string
		// start returns the address of the broken Redis and the number of connections it has accepted.
		start        func(t *testing.T) (addr string, accepted *atomic.Int32)
		wantWithin   time.Duration // the call must fail in less than this; zero means no limit.
		wantAccepted int32         // the connections the limiter may open; zero means no limit.
	}{
		{name: "error - redis down", start: startClosed},
		{
			name:       "error - unresponsive redis fails fast instead of holding the request",
			start:      startSilent,
			wantWithin: 2 * time.Second,
		},
		{name: "error - a failed call is not retried", start: startDropping, wantAccepted: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, accepted := tt.start(t)
			client := ratelimit.NewClient(addr, "")
			t.Cleanup(func() { closeQuietly(client) })
			limiter := ratelimit.NewRedis(client, 3, time.Second)

			start := time.Now()
			_, err := limiter.Allow(context.Background(), "k")

			require.Error(t, err)
			if tt.wantWithin > 0 {
				assert.Less(t, time.Since(start), tt.wantWithin)
			}
			if tt.wantAccepted > 0 {
				assert.Equal(t, tt.wantAccepted, accepted.Load(), "the limiter must fail open at once, not retry")
			}
		})
	}
}

// listen opens a loopback listener and calls serve for every accepted connection.
func listen(t *testing.T, serve func(t *testing.T, conn net.Conn)) (string, *atomic.Int32) {
	t.Helper()

	lis, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { closeQuietly(lis) })

	accepted := &atomic.Int32{}
	go func() {
		for {
			conn, acceptErr := lis.Accept()
			if acceptErr != nil {
				return
			}
			accepted.Add(1)
			serve(t, conn)
		}
	}()
	return lis.Addr().String(), accepted
}

// startClosed returns the address of a Redis that has been shut down.
func startClosed(t *testing.T) (string, *atomic.Int32) {
	t.Helper()

	mr := miniredis.RunT(t)
	addr := mr.Addr()
	mr.Close()
	return addr, &atomic.Int32{}
}

// startSilent returns a server that accepts connections and never answers.
func startSilent(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	return listen(t, func(t *testing.T, conn net.Conn) { t.Cleanup(func() { closeQuietly(conn) }) })
}

// startDropping returns a server that closes every connection at once: a retryable network error.
func startDropping(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	return listen(t, func(_ *testing.T, conn net.Conn) { closeQuietly(conn) })
}

// closeQuietly closes c and only logs a failure: a cleanup error must not fail a test that already has its result.
func closeQuietly(c io.Closer) {
	if err := c.Close(); err != nil {
		slog.Debug("close", slog.String("error", err.Error()))
	}
}
