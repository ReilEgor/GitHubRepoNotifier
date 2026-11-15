package usecase

import (
	"context"

	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/domain/model"
)

// RepositoryUseCase registers repositories for tracking.
type RepositoryUseCase interface {
	// GetOrCreate returns the stored repository, or verifies it on GitHub and stores it with its latest tag.
	GetOrCreate(ctx context.Context, fullName string) (model.Repository, error)
}
