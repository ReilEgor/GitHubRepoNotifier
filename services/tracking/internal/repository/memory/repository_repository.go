// Package memory implements the repositories of the tracking service in process memory.
package memory

import (
	"context"
	"sync"
	"time"

	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/domain/model"
)

// RepositoryRepository is the in-memory repository.RepositoryRepository.
type RepositoryRepository struct {
	mu     sync.Mutex
	nextID int64
	byName map[string]model.Repository
}

// NewRepositoryRepository returns an empty RepositoryRepository.
func NewRepositoryRepository() *RepositoryRepository {
	return &RepositoryRepository{byName: map[string]model.Repository{}}
}

// GetByName returns the stored repository, or model.ErrRepositoryNotFound.
func (r *RepositoryRepository) GetByName(_ context.Context, fullName string) (model.Repository, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	repo, ok := r.byName[fullName]
	if !ok {
		return model.Repository{}, model.ErrRepositoryNotFound
	}
	return repo, nil
}

// Create stores the repository; an already stored one is returned unchanged.
func (r *RepositoryRepository) Create(_ context.Context, fullName, tag string) (model.Repository, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if repo, ok := r.byName[fullName]; ok {
		return repo, nil
	}
	r.nextID++
	repo := model.Repository{ID: r.nextID, FullName: fullName, LastSeenTag: tag, UpdatedAt: time.Now()}
	r.byName[fullName] = repo
	return repo, nil
}
