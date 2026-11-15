package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	sharedPostgres "github.com/ReilEgor/GitHubRepoNotifier/shared/infrastructure/storage/postgres"

	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/domain/model"
)

// A confirmed pair keeps its token and confirmation; an unconfirmed one gets the new token.
const upsertSubscriptionQuery = `INSERT INTO subscriptions (user_id, repository_id, token, is_confirmed)
VALUES ($1, $2, $3, FALSE)
ON CONFLICT (user_id, repository_id) DO UPDATE
SET token = CASE WHEN subscriptions.is_confirmed THEN subscriptions.token ELSE EXCLUDED.token END
RETURNING id, token, is_confirmed, created_at`

const getSubscriptionByTokenQuery = `SELECT s.id, s.user_id, u.email, s.repository_id, r.full_name,
       s.token, s.is_confirmed, s.created_at
FROM subscriptions s
JOIN users u ON u.id = s.user_id
JOIN repositories r ON r.id = s.repository_id
WHERE s.token = $1`

const confirmSubscriptionQuery = `UPDATE subscriptions SET is_confirmed = TRUE WHERE id = $1`

const listSubscriptionsByEmailQuery = `SELECT s.id, u.email, r.full_name, s.is_confirmed, r.last_seen_tag, s.created_at
FROM subscriptions s
JOIN users u ON u.id = s.user_id
JOIN repositories r ON r.id = s.repository_id
WHERE u.email = $1
ORDER BY s.created_at DESC, s.id DESC`

const deleteSubscriptionQuery = `DELETE FROM subscriptions WHERE id = $1`

// SubscriptionRepository is the PostgreSQL repository.SubscriptionRepository.
type SubscriptionRepository struct{ pool *pgxpool.Pool }

// NewSubscriptionRepository returns a SubscriptionRepository on pool.
func NewSubscriptionRepository(pool *pgxpool.Pool) *SubscriptionRepository {
	return &SubscriptionRepository{pool: pool}
}

// Upsert creates the subscription or, for an existing unconfirmed pair, replaces its token.
func (r *SubscriptionRepository) Upsert(
	ctx context.Context,
	userID, repositoryID int64,
	token string,
) (model.Subscription, error) {
	const op = "SubscriptionRepository.Upsert"

	sub := model.Subscription{UserID: userID, RepositoryID: repositoryID}
	err := sharedPostgres.Executor(ctx, r.pool).
		QueryRow(ctx, upsertSubscriptionQuery, userID, repositoryID, token).
		Scan(&sub.ID, &sub.Token, &sub.Confirmed, &sub.CreatedAt)
	if err != nil {
		return model.Subscription{}, fmt.Errorf("%s: %w", op, err)
	}
	return sub, nil
}

// GetByToken returns the subscription with the token, or model.ErrInvalidToken.
func (r *SubscriptionRepository) GetByToken(ctx context.Context, token string) (model.Subscription, error) {
	const op = "SubscriptionRepository.GetByToken"

	var sub model.Subscription
	err := sharedPostgres.Executor(ctx, r.pool).
		QueryRow(ctx, getSubscriptionByTokenQuery, token).
		Scan(&sub.ID, &sub.UserID, &sub.Email, &sub.RepositoryID, &sub.RepositoryName,
			&sub.Token, &sub.Confirmed, &sub.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Subscription{}, model.ErrInvalidToken
	}
	if err != nil {
		return model.Subscription{}, fmt.Errorf("%s: %w", op, err)
	}
	return sub, nil
}

// ListByEmail returns all subscriptions of the user, newest first, with one query.
func (r *SubscriptionRepository) ListByEmail(ctx context.Context, email string) ([]model.Subscription, error) {
	const op = "SubscriptionRepository.ListByEmail"

	rows, err := sharedPostgres.Executor(ctx, r.pool).Query(ctx, listSubscriptionsByEmailQuery, email)
	if err != nil {
		return nil, fmt.Errorf("%s: query: %w", op, err)
	}
	defer rows.Close()

	subs := []model.Subscription{}
	for rows.Next() {
		var s model.Subscription
		if scanErr := rows.Scan(
			&s.ID, &s.Email, &s.RepositoryName, &s.Confirmed, &s.LastSeenTag, &s.CreatedAt,
		); scanErr != nil {
			return nil, fmt.Errorf("%s: scan: %w", op, scanErr)
		}
		subs = append(subs, s)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: rows: %w", op, err)
	}
	return subs, nil
}

// Confirm marks the subscription confirmed; it returns model.ErrInvalidToken when no row was changed.
func (r *SubscriptionRepository) Confirm(ctx context.Context, id int64) error {
	const op = "SubscriptionRepository.Confirm"

	tag, err := sharedPostgres.Executor(ctx, r.pool).Exec(ctx, confirmSubscriptionQuery, id)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if tag.RowsAffected() == 0 {
		return model.ErrInvalidToken
	}
	return nil
}

// Delete removes the subscription; it returns model.ErrInvalidToken when no row was deleted.
func (r *SubscriptionRepository) Delete(ctx context.Context, id int64) error {
	const op = "SubscriptionRepository.Delete"

	tag, err := sharedPostgres.Executor(ctx, r.pool).Exec(ctx, deleteSubscriptionQuery, id)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if tag.RowsAffected() == 0 {
		return model.ErrInvalidToken
	}
	return nil
}
