package github

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/domain/model"
	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/domain/service"
)

const (
	existsKeyPrefix  = "repo_exists:"
	releaseKeyPrefix = "release:"
	existsTTL        = time.Minute
	releaseTTL       = 5 * time.Minute
	redisTimeout     = 200 * time.Millisecond
)

// Cached remembers positive answers of another GitHubClient in Redis: that a repository exists for a minute
// and its latest release for five. A Redis failure is logged and the call goes straight to GitHub.
type Cached struct {
	next  service.GitHubClient
	redis redis.Cmdable
}

// NewCached returns a Cached in front of next.
func NewCached(next service.GitHubClient, client redis.Cmdable) *Cached {
	return &Cached{next: next, redis: client}
}

// CheckRepository answers from the cache when it can.
func (c *Cached) CheckRepository(ctx context.Context, fullName string) error {
	key := existsKeyPrefix + fullName
	if _, ok := c.get(ctx, key); ok {
		return nil
	}
	if err := c.next.CheckRepository(ctx, fullName); err != nil {
		return err //nolint:wrapcheck // the decorator is transparent: callers match the domain errors of the client.
	}
	c.set(ctx, key, "1", existsTTL)
	return nil
}

// LatestRelease answers from the cache when it can.
func (c *Cached) LatestRelease(ctx context.Context, fullName string) (model.ReleaseInfo, error) {
	key := releaseKeyPrefix + fullName
	if tag, ok := c.get(ctx, key); ok {
		return model.ReleaseInfo{TagName: tag}, nil
	}
	release, err := c.next.LatestRelease(ctx, fullName)
	if err != nil {
		return model.ReleaseInfo{}, err //nolint:wrapcheck // the decorator is transparent.
	}
	c.set(ctx, key, release.TagName, releaseTTL)
	return release, nil
}

func (c *Cached) get(ctx context.Context, key string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, redisTimeout)
	defer cancel()

	v, err := c.redis.Get(ctx, key).Result()
	if err == nil {
		return v, true
	}
	if !errors.Is(err, redis.Nil) {
		slog.WarnContext(ctx, "github cache read failed, asking GitHub", slog.String("error", err.Error()))
	}
	return "", false
}

func (c *Cached) set(ctx context.Context, key, value string, ttl time.Duration) {
	ctx, cancel := context.WithTimeout(ctx, redisTimeout)
	defer cancel()

	if err := c.redis.Set(ctx, key, value, ttl).Err(); err != nil {
		slog.WarnContext(ctx, "github cache write failed", slog.String("error", fmt.Sprint(err)))
	}
}
