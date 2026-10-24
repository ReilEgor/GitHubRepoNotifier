// Package github calls the GitHub REST API and caches its responses.
package github

import (
	"context"
	"fmt"

	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/domain/model"
)

// Names understood by StaticClient. Each one produces a fixed outcome, so every branch of the API
// can be exercised without GitHub.
const (
	StaticExisting    = "static/existing"
	StaticNotFound    = "static/not-found"
	StaticNoReleases  = "static/no-releases"
	StaticUnavailable = "static/unavailable"
	StaticRateLimited = "static/rate-limited"
	StaticTimeout     = "static/timeout"

	staticTag = "v1.0.0"
)

// StaticClient answers from a fixed list instead of calling GitHub.
type StaticClient struct{}

// NewStaticClient returns a StaticClient.
func NewStaticClient() *StaticClient {
	return &StaticClient{}
}

// CheckRepository reports whether the repository exists; unknown names do not.
func (StaticClient) CheckRepository(_ context.Context, fullName string) error {
	switch fullName {
	case StaticExisting, StaticNoReleases:
		return nil
	case StaticUnavailable:
		return model.ErrGitHubUnavailable
	case StaticRateLimited:
		return model.ErrRateLimitExceeded
	case StaticTimeout:
		return fmt.Errorf("static client: %w", context.DeadlineExceeded)
	default:
		return model.ErrRepositoryNotFound
	}
}

// LatestRelease returns the fixed tag, or model.ErrReleaseNotFound for the repository without releases.
func (StaticClient) LatestRelease(_ context.Context, fullName string) (model.ReleaseInfo, error) {
	if fullName == StaticNoReleases {
		return model.ReleaseInfo{}, model.ErrReleaseNotFound
	}
	return model.ReleaseInfo{TagName: staticTag}, nil
}
