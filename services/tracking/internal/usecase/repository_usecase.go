// Package usecase implements repository registration for the tracking service.
package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/domain/model"
	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/domain/repository"
	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/domain/service"
)

// RepositoryUseCase implements usecase.RepositoryUseCase.
type RepositoryUseCase struct {
	repos  repository.RepositoryRepository
	github service.GitHubClient
}

// NewRepositoryUseCase returns a RepositoryUseCase.
func NewRepositoryUseCase(repos repository.RepositoryRepository, github service.GitHubClient) *RepositoryUseCase {
	return &RepositoryUseCase{repos: repos, github: github}
}

// GetOrCreate returns the stored repository or, for an unknown one, verifies it on GitHub and stores it.
// Nothing is stored when the repository does not exist or has no release.
func (u *RepositoryUseCase) GetOrCreate(ctx context.Context, fullName string) (model.Repository, error) {
	const op = "RepositoryUseCase.GetOrCreate"

	slog.DebugContext(ctx, "called", slog.String("op", op), slog.String("repository", fullName))

	repo, err := u.repos.GetByName(ctx, fullName)
	if err == nil {
		return repo, nil
	}
	if !errors.Is(err, model.ErrRepositoryNotFound) {
		return model.Repository{}, fmt.Errorf("%s: get repository: %w", op, err)
	}

	if checkErr := u.github.CheckRepository(ctx, fullName); checkErr != nil {
		return model.Repository{}, fmt.Errorf("%s: check repository: %w", op, checkErr)
	}
	release, err := u.github.LatestRelease(ctx, fullName)
	if err != nil {
		return model.Repository{}, fmt.Errorf("%s: latest release: %w", op, err)
	}

	repo, err = u.repos.Create(ctx, fullName, release.TagName)
	if err != nil {
		return model.Repository{}, fmt.Errorf("%s: create repository: %w", op, err)
	}
	slog.InfoContext(ctx, "repository registered", slog.String("op", op), slog.String("repository", fullName))
	return repo, nil
}
