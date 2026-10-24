// Package ratelimit limits how often one caller may hit the API, counting in Redis.
package ratelimit

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	keyPrefix    = "ratelimit:"
	redisTimeout = 200 * time.Millisecond
)

// ParseRate parses a rate such as "10-S" (10 per second); the unit is S, M or H.
func ParseRate(s string) (limit int64, window time.Duration, err error) {
	count, unit, ok := strings.Cut(s, "-")
	if !ok {
		return 0, 0, fmt.Errorf("parse rate %q: want <count>-<S|M|H>", s)
	}
	limit, err = strconv.ParseInt(count, 10, 64)
	if err != nil || limit <= 0 {
		return 0, 0, fmt.Errorf("parse rate %q: count must be a positive integer", s)
	}
	switch strings.ToUpper(unit) {
	case "S":
		window = time.Second
	case "M":
		window = time.Minute
	case "H":
		window = time.Hour
	default:
		return 0, 0, fmt.Errorf("parse rate %q: unit must be S, M or H", s)
	}
	return limit, window, nil
}

// Redis is a fixed-window limiter backed by Redis.
type Redis struct {
	client redis.Cmdable
	limit  int64
	window time.Duration
}

// NewRedis returns a limiter that allows limit requests per window for each key.
func NewRedis(client redis.Cmdable, limit int64, window time.Duration) *Redis {
	return &Redis{client: client, limit: limit, window: window}
}

// Allow counts one request of key and reports whether it is within the limit.
func (r *Redis) Allow(ctx context.Context, key string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, redisTimeout)
	defer cancel()

	k := keyPrefix + key
	n, err := r.client.Incr(ctx, k).Result()
	if err != nil {
		return false, fmt.Errorf("ratelimit incr: %w", err)
	}
	if n == 1 {
		if err := r.client.Expire(ctx, k, r.window).Err(); err != nil {
			return false, fmt.Errorf("ratelimit expire: %w", err)
		}
	}
	return n <= r.limit, nil
}
