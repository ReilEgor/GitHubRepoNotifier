// Package repository declares the data access contracts of the subscription service.
package repository

import (
	"context"

	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/domain/model"
)

// Transactor runs fn in one transaction. Repositories called with the context passed to fn take part in it.
type Transactor interface {
	WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// RepositoryRepository stores the local copies of repositories.
type RepositoryRepository interface {
	// GetOrCreate returns the repository with the given name, creating it with tag when it is new.
	GetOrCreate(ctx context.Context, fullName, tag string) (model.RepositoryRef, error)
}

// UserRepository stores users.
type UserRepository interface {
	// GetOrCreate returns the user with the given email, creating it when it is new.
	GetOrCreate(ctx context.Context, email string) (model.User, error)
}

// SubscriptionReader reads subscriptions.
type SubscriptionReader interface {
	// GetByToken returns the subscription with the token, or model.ErrInvalidToken.
	GetByToken(ctx context.Context, token string) (model.Subscription, error)
	// ListByEmail returns all subscriptions of the user, newest first, in one query.
	ListByEmail(ctx context.Context, email string) ([]model.Subscription, error)
}

// SubscriptionWriter changes subscriptions.
type SubscriptionWriter interface {
	// Upsert creates the subscription, or, for an existing pair, replaces the token unless the pair is confirmed.
	// The returned subscription carries the stored token and the confirmation flag.
	Upsert(ctx context.Context, userID, repositoryID int64, token string) (model.Subscription, error)
	// Confirm marks the subscription confirmed; it returns model.ErrInvalidToken when no row was changed.
	Confirm(ctx context.Context, id int64) error
	// Delete removes the subscription; it returns model.ErrInvalidToken when no row was deleted.
	Delete(ctx context.Context, id int64) error
}

// SubscriptionRepository combines the reading and writing sides.
type SubscriptionRepository interface {
	SubscriptionReader
	SubscriptionWriter
}

// SagaRepository stores the confirmation email sagas.
type SagaRepository interface {
	// Create starts a saga for the subscription and returns its ID.
	Create(ctx context.Context, subscriptionID int64) (int64, error)
}

// OutboxRepository stores messages waiting to be published.
type OutboxRepository interface {
	Insert(ctx context.Context, queue string, payload []byte) error
}
