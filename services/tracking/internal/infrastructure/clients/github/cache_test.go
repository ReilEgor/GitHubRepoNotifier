package github_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/domain/model"
	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/infrastructure/clients/github"
)

type countingClient struct {
	checks, releases int
	checkErr         error
	tag              string
}

func (c *countingClient) CheckRepository(context.Context, string) error {
	c.checks++
	return c.checkErr
}

func (c *countingClient) LatestRelease(context.Context, string) (model.ReleaseInfo, error) {
	c.releases++
	return model.ReleaseInfo{TagName: c.tag}, nil
}

func TestCached(t *testing.T) {
	ctx := context.Background()

	t.Run("success - the second call is served from the cache", func(t *testing.T) {
		mr := miniredis.RunT(t)
		next := &countingClient{tag: "v1"}
		c := github.NewCached(next, redis.NewClient(&redis.Options{Addr: mr.Addr()}))

		for range 3 {
			require.NoError(t, c.CheckRepository(ctx, "golang/go"))
			got, err := c.LatestRelease(ctx, "golang/go")
			require.NoError(t, err)
			assert.Equal(t, "v1", got.TagName)
		}

		assert.Equal(t, 1, next.checks)
		assert.Equal(t, 1, next.releases)
	})

	t.Run("success - existence expires after a minute and the release after five", func(t *testing.T) {
		mr := miniredis.RunT(t)
		next := &countingClient{tag: "v1"}
		c := github.NewCached(next, redis.NewClient(&redis.Options{Addr: mr.Addr()}))
		require.NoError(t, c.CheckRepository(ctx, "golang/go"))
		_, err := c.LatestRelease(ctx, "golang/go")
		require.NoError(t, err)

		mr.FastForward(61 * time.Second)
		require.NoError(t, c.CheckRepository(ctx, "golang/go"))
		_, err = c.LatestRelease(ctx, "golang/go")
		require.NoError(t, err)
		assert.Equal(t, 2, next.checks)
		assert.Equal(t, 1, next.releases)

		mr.FastForward(5 * time.Minute)
		_, err = c.LatestRelease(ctx, "golang/go")
		require.NoError(t, err)
		assert.Equal(t, 2, next.releases)
	})

	t.Run("error - a failed lookup is not cached", func(t *testing.T) {
		mr := miniredis.RunT(t)
		next := &countingClient{checkErr: model.ErrRepositoryNotFound}
		c := github.NewCached(next, redis.NewClient(&redis.Options{Addr: mr.Addr()}))

		for range 2 {
			require.ErrorIs(t, c.CheckRepository(ctx, "golang/go"), model.ErrRepositoryNotFound)
		}

		assert.Equal(t, 2, next.checks)
	})

	t.Run("success - Redis down falls through to GitHub", func(t *testing.T) {
		mr := miniredis.RunT(t)
		next := &countingClient{tag: "v1"}
		c := github.NewCached(next, redis.NewClient(&redis.Options{Addr: mr.Addr()}))
		mr.Close()

		for range 2 {
			require.NoError(t, c.CheckRepository(ctx, "golang/go"))
		}

		assert.Equal(t, 2, next.checks)
	})
}
