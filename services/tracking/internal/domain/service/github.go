// Package service declares the contracts of external services used by tracking.
package service

import (
	"context"

	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/domain/model"
)

// GitHubClient reads repository data from GitHub.
type GitHubClient interface {
	// CheckRepository returns model.ErrRepositoryNotFound when the repository does not exist.
	CheckRepository(ctx context.Context, fullName string) error
	// LatestRelease returns model.ErrReleaseNotFound when the repository has no releases.
	LatestRelease(ctx context.Context, fullName string) (model.ReleaseInfo, error)
}
