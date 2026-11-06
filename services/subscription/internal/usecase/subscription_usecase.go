// Package usecase implements the business operations of the subscription service.
package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/ReilEgor/GitHubRepoNotifier/shared/contracts"
	"github.com/ReilEgor/GitHubRepoNotifier/shared/ctxlog"
	"github.com/google/uuid"

	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/domain/model"
	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/domain/repository"
)

// RepositoryResolver asks the tracking service for a repository.
type RepositoryResolver interface {
	// GetOrCreateRepository returns the repository, verifying it on GitHub if tracking does not know it yet.
	GetOrCreateRepository(ctx context.Context, fullName string) (model.TrackedRepository, error)
}

// Repositories groups the data access dependencies of SubscriptionUseCase.
type Repositories struct {
	Repositories  repository.RepositoryRepository
	Users         repository.UserRepository
	Subscriptions repository.SubscriptionRepository
	Sagas         repository.SagaRepository
	Outbox        repository.OutboxRepository
}

// SubscriptionUseCase implements usecase.SubscriptionUseCase.
type SubscriptionUseCase struct {
	tx       repository.Transactor
	repos    Repositories
	resolver RepositoryResolver
}

// NewSubscriptionUseCase returns a SubscriptionUseCase.
func NewSubscriptionUseCase(
	tx repository.Transactor,
	repos Repositories,
	resolver RepositoryResolver,
) *SubscriptionUseCase {
	return &SubscriptionUseCase{tx: tx, repos: repos, resolver: resolver}
}

// Subscribe asks tracking for the repository first and only then opens the transaction,
// so no call leaves the service while a database connection is held.
func (u *SubscriptionUseCase) Subscribe(ctx context.Context, email, repositoryName string) error {
	const op = "SubscriptionUseCase.Subscribe"

	ctx = ctxlog.WithInitiator(ctx, email)
	slog.DebugContext(ctx, "called", slog.String("op", op), slog.String("repository", repositoryName))

	tracked, err := u.resolver.GetOrCreateRepository(ctx, repositoryName)
	if err != nil {
		return fmt.Errorf("%s: resolve repository: %w", op, err)
	}

	ref, err := u.repos.Repositories.GetOrCreate(ctx, tracked.FullName, tracked.LastSeenTag)
	if err != nil {
		return fmt.Errorf("%s: save repository: %w", op, err)
	}

	err = u.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		return u.createSubscription(txCtx, email, ref)
	})
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	slog.InfoContext(ctx, "subscription initiated", slog.String("op", op), slog.String("repository", ref.FullName))
	return nil
}

func (u *SubscriptionUseCase) createSubscription(txCtx context.Context, email string, ref model.RepositoryRef) error {
	user, err := u.repos.Users.GetOrCreate(txCtx, email)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	sub, err := u.repos.Subscriptions.Upsert(txCtx, user.ID, ref.ID, uuid.NewString())
	if err != nil {
		return fmt.Errorf("upsert subscription: %w", err)
	}
	if sub.Confirmed {
		return nil
	}
	sagaID, err := u.repos.Sagas.Create(txCtx, sub.ID)
	if err != nil {
		return fmt.Errorf("create saga: %w", err)
	}
	return u.enqueue(txCtx, contracts.QueueConfirmations, contracts.SendConfirmationCommand{
		Email:          email,
		RepoName:       ref.FullName,
		Token:          sub.Token,
		SagaID:         sagaID,
		SubscriptionID: sub.ID,
		RequestID:      ctxlog.RequestID(txCtx),
	})
}

// Confirm confirms the subscription and records the activation event in the same transaction.
func (u *SubscriptionUseCase) Confirm(ctx context.Context, token string) error {
	const op = "SubscriptionUseCase.Confirm"

	sub, ctx, err := u.findByToken(ctx, token)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	err = u.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		if confirmErr := u.repos.Subscriptions.Confirm(txCtx, sub.ID); confirmErr != nil {
			return fmt.Errorf("confirm subscription: %w", confirmErr)
		}
		return u.enqueue(txCtx, contracts.QueueSubscriptionActivated, contracts.SubscriptionActivatedEvent{
			FullName:  sub.RepositoryName,
			Email:     sub.Email,
			Token:     sub.Token,
			RequestID: ctxlog.RequestID(txCtx),
		})
	})
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	slog.InfoContext(
		ctx,
		"subscription confirmed",
		slog.String("op", op),
		slog.String("repository", sub.RepositoryName),
	)
	return nil
}

// List returns all subscriptions of the user with one query.
func (u *SubscriptionUseCase) List(ctx context.Context, email string) ([]model.Subscription, error) {
	const op = "SubscriptionUseCase.List"

	ctx = ctxlog.WithInitiator(ctx, email)
	slog.DebugContext(ctx, "called", slog.String("op", op))

	subs, err := u.repos.Subscriptions.ListByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("%s: list subscriptions: %w", op, err)
	}
	return subs, nil
}

// Unsubscribe deletes the subscription and records the event in the same transaction.
func (u *SubscriptionUseCase) Unsubscribe(ctx context.Context, token string) error {
	const op = "SubscriptionUseCase.Unsubscribe"

	sub, ctx, err := u.findByToken(ctx, token)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	err = u.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		if deleteErr := u.repos.Subscriptions.Delete(txCtx, sub.ID); deleteErr != nil {
			return fmt.Errorf("delete subscription: %w", deleteErr)
		}
		return u.enqueue(txCtx, contracts.QueueUnsubscriptionActivated, contracts.UnsubscriptionActivatedEvent{
			Email:     sub.Email,
			RepoName:  sub.RepositoryName,
			RequestID: ctxlog.RequestID(txCtx),
		})
	})
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	slog.InfoContext(ctx, "unsubscribed", slog.String("op", op), slog.String("repository", sub.RepositoryName))
	return nil
}

// findByToken returns the subscription and a context whose initiator is the subscription owner.
func (u *SubscriptionUseCase) findByToken(
	ctx context.Context,
	token string,
) (model.Subscription, context.Context, error) {
	sub, err := u.repos.Subscriptions.GetByToken(ctx, token)
	if err != nil {
		return model.Subscription{}, ctxlog.WithInitiator(ctx, ctxlog.InitiatorUnknown),
			fmt.Errorf("find subscription: %w", err)
	}
	return sub, ctxlog.WithInitiator(ctx, sub.Email), nil
}

func (u *SubscriptionUseCase) enqueue(txCtx context.Context, queue string, message any) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshal %s message: %w", queue, err)
	}
	if err := u.repos.Outbox.Insert(txCtx, queue, payload); err != nil {
		return fmt.Errorf("insert %s message: %w", queue, err)
	}
	return nil
}
