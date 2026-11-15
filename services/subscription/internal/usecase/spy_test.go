//nolint:wrapcheck // test spies pass errors through unchanged.
package usecase_test

import (
	"context"
	"sync"

	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/domain/model"
	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/domain/repository"
	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/repository/memory"
	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/usecase"
)

// ctxSpy records, per repository, whether the last write got a transaction context.
type ctxSpy struct {
	mu    sync.Mutex
	calls map[string]bool
}

func (s *ctxSpy) record(ctx context.Context, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[name] = memory.InTransaction(ctx)
}

type spyUsers struct {
	repository.UserRepository
	*ctxSpy
}

func (r spyUsers) GetOrCreate(ctx context.Context, email string) (model.User, error) {
	r.record(ctx, "users")
	return r.UserRepository.GetOrCreate(ctx, email)
}

type spySubscriptions struct {
	repository.SubscriptionRepository
	*ctxSpy
}

func (r spySubscriptions) Upsert(ctx context.Context, userID, repoID int64, token string) (model.Subscription, error) {
	r.record(ctx, "subscriptions")
	return r.SubscriptionRepository.Upsert(ctx, userID, repoID, token)
}

type spySagas struct {
	repository.SagaRepository
	*ctxSpy
}

func (r spySagas) Create(ctx context.Context, subscriptionID int64) (int64, error) {
	r.record(ctx, "sagas")
	return r.SagaRepository.Create(ctx, subscriptionID)
}

type spyOutbox struct {
	repository.OutboxRepository
	*ctxSpy
}

func (r spyOutbox) Insert(ctx context.Context, queue string, payload []byte) error {
	r.record(ctx, "outbox")
	return r.OutboxRepository.Insert(ctx, queue, payload)
}

func withOutbox(r usecase.Repositories, o repository.OutboxRepository) usecase.Repositories {
	r.Outbox = o
	return r
}
