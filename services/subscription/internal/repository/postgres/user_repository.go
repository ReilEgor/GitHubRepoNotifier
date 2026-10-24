package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	sharedPostgres "github.com/ReilEgor/GitHubRepoNotifier/shared/infrastructure/storage/postgres"

	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/domain/model"
)

const getOrCreateUserQuery = `INSERT INTO users (email)
VALUES ($1)
ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email
RETURNING id, email, created_at`

// UserRepository is the PostgreSQL repository.UserRepository.
type UserRepository struct{ pool *pgxpool.Pool }

// NewUserRepository returns a UserRepository on pool.
func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

// GetOrCreate returns the user with the email, creating it when it is new.
func (r *UserRepository) GetOrCreate(ctx context.Context, email string) (model.User, error) {
	const op = "UserRepository.GetOrCreate"

	var u model.User
	err := sharedPostgres.Executor(ctx, r.pool).
		QueryRow(ctx, getOrCreateUserQuery, email).
		Scan(&u.ID, &u.Email, &u.CreatedAt)
	if err != nil {
		return model.User{}, fmt.Errorf("%s: %w", op, err)
	}
	return u, nil
}
