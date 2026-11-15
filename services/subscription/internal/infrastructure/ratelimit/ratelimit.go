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

	n, err := incrWithTTL.Run(ctx, r.client, []string{keyPrefix + key}, r.window.Milliseconds()).Int64()
	if err != nil {
		return false, fmt.Errorf("ratelimit incr: %w", err)
	}
	return n <= r.limit, nil
}

// incrWithTTL increments the counter and sets its TTL in one atomic step, so a failure
// between the two can never leave a key without expiry. A key that already has no TTL
// (left by an older version of this code) gets one on its next request.
var incrWithTTL = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
if n == 1 or redis.call('PTTL', KEYS[1]) < 0 then
	redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return n
`)
