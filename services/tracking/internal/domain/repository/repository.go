// Package repository declares the data access contracts of the tracking service.
package repository

import (
	"context"

	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/domain/model"
)

// RepositoryRepository stores the repositories tracking knows about.
type RepositoryRepository interface {
	// GetByName returns the repository with the name, or model.ErrRepositoryNotFound when it is not stored.
	GetByName(ctx context.Context, fullName string) (model.Repository, error)
	// Create stores the repository with its current tag; an already stored repository is returned unchanged.
	Create(ctx context.Context, fullName, tag string) (model.Repository, error)
}
