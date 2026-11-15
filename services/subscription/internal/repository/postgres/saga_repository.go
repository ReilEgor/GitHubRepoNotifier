package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	sharedPostgres "github.com/ReilEgor/GitHubRepoNotifier/shared/infrastructure/storage/postgres"
)

const createSagaQuery = `INSERT INTO subscription_sagas (subscription_id, status, current_step)
VALUES ($1, 'STARTED', 'SEND_CONFIRMATION')
RETURNING id`

// SagaRepository is the PostgreSQL repository.SagaRepository.
type SagaRepository struct{ pool *pgxpool.Pool }

// NewSagaRepository returns a SagaRepository on pool.
func NewSagaRepository(pool *pgxpool.Pool) *SagaRepository {
	return &SagaRepository{pool: pool}
}

// Create starts a saga for the subscription and returns its ID.
func (r *SagaRepository) Create(ctx context.Context, subscriptionID int64) (int64, error) {
	const op = "SagaRepository.Create"

	var id int64
	if err := sharedPostgres.Executor(ctx, r.pool).
		QueryRow(ctx, createSagaQuery, subscriptionID).
		Scan(&id); err != nil {
		return 0, fmt.Errorf("%s: %w", op, err)
	}
	return id, nil
}
