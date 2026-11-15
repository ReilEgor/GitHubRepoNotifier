// Package usecase declares the business operation contracts of the subscription service.
package usecase

import (
	"context"

	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/domain/model"
)

// SubscriptionUseCase is the set of operations a user can perform on subscriptions.
type SubscriptionUseCase interface {
	// Subscribe starts a subscription of email to the repository.
	Subscribe(ctx context.Context, email, repository string) error
	// Confirm confirms the subscription with the token.
	Confirm(ctx context.Context, token string) error
	// List returns all subscriptions of email.
	List(ctx context.Context, email string) ([]model.Subscription, error)
	// Unsubscribe removes the subscription with the token.
	Unsubscribe(ctx context.Context, token string) error
}
