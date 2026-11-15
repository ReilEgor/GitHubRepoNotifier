package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	sharedPostgres "github.com/ReilEgor/GitHubRepoNotifier/shared/infrastructure/storage/postgres"
)

const insertOutboxQuery = `INSERT INTO outbox_messages (queue, payload) VALUES ($1, $2)`

// OutboxRepository is the PostgreSQL repository.OutboxRepository.
type OutboxRepository struct{ pool *pgxpool.Pool }

// NewOutboxRepository returns an OutboxRepository on pool.
func NewOutboxRepository(pool *pgxpool.Pool) *OutboxRepository {
	return &OutboxRepository{pool: pool}
}

// Insert queues a message for publication. Nothing publishes it yet: the relay belongs to a later task.
func (r *OutboxRepository) Insert(ctx context.Context, queue string, payload []byte) error {
	const op = "OutboxRepository.Insert"

	if _, err := sharedPostgres.Executor(ctx, r.pool).Exec(ctx, insertOutboxQuery, queue, payload); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}
