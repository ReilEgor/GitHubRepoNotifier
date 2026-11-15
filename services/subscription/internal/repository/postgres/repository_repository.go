// Package postgres implements the repositories of the subscription service on PostgreSQL.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	sharedPostgres "github.com/ReilEgor/GitHubRepoNotifier/shared/infrastructure/storage/postgres"

	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/domain/model"
)

const getOrCreateRepositoryQuery = `INSERT INTO repositories (full_name, last_seen_tag)
VALUES ($1, NULLIF($2, ''))
ON CONFLICT (full_name) DO UPDATE SET full_name = EXCLUDED.full_name
RETURNING id, full_name`

// RepositoryRepository is the PostgreSQL repository.RepositoryRepository.
type RepositoryRepository struct{ pool *pgxpool.Pool }

// NewRepositoryRepository returns a RepositoryRepository on pool.
func NewRepositoryRepository(pool *pgxpool.Pool) *RepositoryRepository {
	return &RepositoryRepository{pool: pool}
}

// GetOrCreate returns the repository with the name, creating it with tag when it is new.
func (r *RepositoryRepository) GetOrCreate(ctx context.Context, fullName, tag string) (model.RepositoryRef, error) {
	const op = "RepositoryRepository.GetOrCreate"

	var ref model.RepositoryRef
	err := sharedPostgres.Executor(ctx, r.pool).
		QueryRow(ctx, getOrCreateRepositoryQuery, fullName, tag).
		Scan(&ref.ID, &ref.FullName)
	if err != nil {
		return model.RepositoryRef{}, fmt.Errorf("%s: %w", op, err)
	}
	return ref, nil
}
