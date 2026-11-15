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
	ctx := context.Background()

	t.Run("success - requests over the limit are denied until the window passes", func(t *testing.T) {
		mr := miniredis.RunT(t)
		l := ratelimit.NewRedis(redis.NewClient(&redis.Options{Addr: mr.Addr()}), 3, time.Second)

		for i := range 3 {
			ok, err := l.Allow(ctx, "1.2.3.4")
			require.NoError(t, err)
			assert.True(t, ok, "request %d", i+1)
		}
		ok, err := l.Allow(ctx, "1.2.3.4")
		require.NoError(t, err)
		assert.False(t, ok)

		other, err := l.Allow(ctx, "5.6.7.8")
		require.NoError(t, err)
		assert.True(t, other, "the limit is per caller")

		mr.FastForward(2 * time.Second)
		ok, err = l.Allow(ctx, "1.2.3.4")
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("success - the first request sets the TTL together with the counter", func(t *testing.T) {
		mr := miniredis.RunT(t)
		l := ratelimit.NewRedis(redis.NewClient(&redis.Options{Addr: mr.Addr()}), 3, time.Minute)

		_, err := l.Allow(ctx, "1.2.3.4")

		require.NoError(t, err)
		assert.Equal(t, time.Minute, mr.TTL("ratelimit:1.2.3.4"))
	})

	t.Run("success - a key left without TTL is healed and expires", func(t *testing.T) {
		mr := miniredis.RunT(t)
		l := ratelimit.NewRedis(redis.NewClient(&redis.Options{Addr: mr.Addr()}), 3, time.Second)
		require.NoError(t, mr.Set("ratelimit:1.2.3.4", "3")) // the old INCR-then-EXPIRE failure: counter, no TTL
		require.Zero(t, mr.TTL("ratelimit:1.2.3.4"))

		ok, err := l.Allow(ctx, "1.2.3.4")
		require.NoError(t, err)
		assert.False(t, ok)
		assert.Equal(t, time.Second, mr.TTL("ratelimit:1.2.3.4"))

		mr.FastForward(2 * time.Second)
		ok, err = l.Allow(ctx, "1.2.3.4")
		require.NoError(t, err)
		assert.True(t, ok, "the address is not blocked forever")
	})

	t.Run("error - redis down", func(t *testing.T) {
		mr := miniredis.RunT(t)
		l := ratelimit.NewRedis(redis.NewClient(&redis.Options{Addr: mr.Addr()}), 3, time.Second)
		mr.Close()

		_, err := l.Allow(ctx, "1.2.3.4")

		require.Error(t, err)
	})

	t.Run("success - the exact limit is allowed and one more is not", func(t *testing.T) {
		mr := miniredis.RunT(t)
		l := ratelimit.NewRedis(redis.NewClient(&redis.Options{Addr: mr.Addr()}), 1, time.Minute)

		first, err := l.Allow(ctx, "k")
		require.NoError(t, err)
		second, err := l.Allow(ctx, "k")
		require.NoError(t, err)

		assert.True(t, first)
		assert.False(t, second)
	})

	t.Run("success - the window restarts only after the TTL, not on every request", func(t *testing.T) {
		mr := miniredis.RunT(t)
		l := ratelimit.NewRedis(redis.NewClient(&redis.Options{Addr: mr.Addr()}), 2, time.Minute)

		_, err := l.Allow(ctx, "k")
		require.NoError(t, err)
		mr.FastForward(40 * time.Second)
		_, err = l.Allow(ctx, "k")
		require.NoError(t, err)

		assert.Equal(t, 20*time.Second, mr.TTL("ratelimit:k"), "a repeated request must not extend the window")
	})

	t.Run("error - unresponsive redis fails fast instead of holding the request", func(t *testing.T) {
		lis, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
		require.NoError(t, err)
		t.Cleanup(func() { closeQuietly(lis) })
		go func() { // accepts the connection and never answers.
			for {
				conn, acceptErr := lis.Accept()
				if acceptErr != nil {
					return
				}
				t.Cleanup(func() { closeQuietly(conn) })
			}
		}()
		client := ratelimit.NewClient(lis.Addr().String(), "")
		t.Cleanup(func() { closeQuietly(client) })
		l := ratelimit.NewRedis(client, 3, time.Second)

		start := time.Now()
		_, err = l.Allow(ctx, "k")

		require.Error(t, err)
		assert.Less(t, time.Since(start), 2*time.Second)
	})

	t.Run("error - a failed call is not retried", func(t *testing.T) {
		lis, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
		require.NoError(t, err)
		t.Cleanup(func() { closeQuietly(lis) })
		var accepted atomic.Int32
		go func() { // drops every connection at once: a retryable network error.
			for {
				conn, acceptErr := lis.Accept()
				if acceptErr != nil {
					return
				}
				accepted.Add(1)
				closeQuietly(conn)
			}
		}()
		client := ratelimit.NewClient(lis.Addr().String(), "")
		t.Cleanup(func() { closeQuietly(client) })
		l := ratelimit.NewRedis(client, 3, time.Second)

		_, err = l.Allow(ctx, "k")

		require.Error(t, err)
		assert.EqualValues(t, 1, accepted.Load(), "the limiter must fail open at once, not retry")
	})
}

// closeQuietly closes c and only logs a failure: a cleanup error must not fail a test that already has its result.
func closeQuietly(c io.Closer) {
	if err := c.Close(); err != nil {
		slog.Debug("close", slog.String("error", err.Error()))
	}
}
