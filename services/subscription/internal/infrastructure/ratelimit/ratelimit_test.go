package ratelimit_test

import (
	"context"
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

	t.Run("error - redis down", func(t *testing.T) {
		mr := miniredis.RunT(t)
		l := ratelimit.NewRedis(redis.NewClient(&redis.Options{Addr: mr.Addr()}), 3, time.Second)
		mr.Close()

		_, err := l.Allow(ctx, "1.2.3.4")

		require.Error(t, err)
	})
}
