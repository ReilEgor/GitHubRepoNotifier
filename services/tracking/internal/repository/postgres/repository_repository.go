// Package postgres implements the repositories of the tracking service on PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	sharedPostgres "github.com/ReilEgor/GitHubRepoNotifier/shared/infrastructure/storage/postgres"

	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/domain/model"
)

const getRepositoryByNameQuery = `SELECT id, full_name, last_seen_tag, updated_at
FROM repositories
WHERE full_name = $1`

const createRepositoryQuery = `INSERT INTO repositories (full_name, last_seen_tag)
VALUES ($1, $2)
ON CONFLICT (full_name) DO UPDATE SET full_name = EXCLUDED.full_name
RETURNING id, full_name, last_seen_tag, updated_at`

// RepositoryRepository is the PostgreSQL repository.RepositoryRepository.
type RepositoryRepository struct{ pool *pgxpool.Pool }

// NewRepositoryRepository returns a RepositoryRepository on pool.
func NewRepositoryRepository(pool *pgxpool.Pool) *RepositoryRepository {
	return &RepositoryRepository{pool: pool}
}

// GetByName returns the stored repository, or model.ErrRepositoryNotFound.
func (r *RepositoryRepository) GetByName(ctx context.Context, fullName string) (model.Repository, error) {
	const op = "RepositoryRepository.GetByName"

	repo, err := scan(sharedPostgres.Executor(ctx, r.pool).QueryRow(ctx, getRepositoryByNameQuery, fullName))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Repository{}, model.ErrRepositoryNotFound
	}
	if err != nil {
		return model.Repository{}, fmt.Errorf("%s: %w", op, err)
	}
	return repo, nil
}

// Create stores the repository; an already stored one is returned unchanged.
func (r *RepositoryRepository) Create(ctx context.Context, fullName, tag string) (model.Repository, error) {
	const op = "RepositoryRepository.Create"

	repo, err := scan(sharedPostgres.Executor(ctx, r.pool).QueryRow(ctx, createRepositoryQuery, fullName, tag))
	if err != nil {
		return model.Repository{}, fmt.Errorf("%s: %w", op, err)
	}
	return repo, nil
}

func scan(row pgx.Row) (model.Repository, error) {
	var (
		repo      model.Repository
		tag       *string
		updatedAt time.Time
	)
	if err := row.Scan(&repo.ID, &repo.FullName, &tag, &updatedAt); err != nil {
		return model.Repository{}, err //nolint:wrapcheck // the callers wrap it with their operation name.
	}
	if tag != nil {
		repo.LastSeenTag = *tag
	}
	repo.UpdatedAt = updatedAt
	return repo, nil
}
