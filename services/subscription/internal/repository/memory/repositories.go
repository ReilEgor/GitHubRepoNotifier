package memory

import (
	"context"
	"sort"
	"time"

	sharedModel "github.com/ReilEgor/GitHubRepoNotifier/shared/domain/model"

	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/domain/model"
)

// RepositoryRepository is the in-memory repository.RepositoryRepository.
type RepositoryRepository struct{ store *Store }

// NewRepositoryRepository returns a RepositoryRepository on store.
func NewRepositoryRepository(store *Store) *RepositoryRepository {
	return &RepositoryRepository{store: store}
}

// GetOrCreate returns the repository with the name, creating it with tag when it is new.
func (r *RepositoryRepository) GetOrCreate(ctx context.Context, fullName, tag string) (model.RepositoryRef, error) {
	var ref model.RepositoryRef
	err := r.store.run(ctx, func(st *state) error {
		for _, row := range st.repositories {
			if row.fullName == fullName {
				ref = model.RepositoryRef{ID: row.id, FullName: row.fullName}
				return nil
			}
		}
		row := repositoryRow{id: st.nextID("repositories"), fullName: fullName, tag: tag}
		st.repositories = append(st.repositories, row)
		ref = model.RepositoryRef{ID: row.id, FullName: row.fullName}
		return nil
	})
	return ref, err
}

// UserRepository is the in-memory repository.UserRepository.
type UserRepository struct{ store *Store }

// NewUserRepository returns a UserRepository on store.
func NewUserRepository(store *Store) *UserRepository {
	return &UserRepository{store: store}
}

// GetOrCreate returns the user with the email, creating it when it is new.
func (r *UserRepository) GetOrCreate(ctx context.Context, email string) (model.User, error) {
	var user model.User
	err := r.store.run(ctx, func(st *state) error {
		for _, u := range st.users {
			if u.Email == email {
				user = u
				return nil
			}
		}
		user = model.User{ID: st.nextID("users"), Email: email, CreatedAt: time.Now()}
		st.users = append(st.users, user)
		return nil
	})
	return user, err
}

// SagaRepository is the in-memory repository.SagaRepository.
type SagaRepository struct{ store *Store }

// NewSagaRepository returns a SagaRepository on store.
func NewSagaRepository(store *Store) *SagaRepository {
	return &SagaRepository{store: store}
}

// Create starts a saga for the subscription.
func (r *SagaRepository) Create(ctx context.Context, subscriptionID int64) (int64, error) {
	var id int64
	err := r.store.run(ctx, func(st *state) error {
		now := time.Now()
		id = st.nextID("sagas")
		st.sagas = append(st.sagas, model.SubscriptionSaga{
			ID:             id,
			SubscriptionID: subscriptionID,
			Status:         model.SagaStatusStarted,
			CurrentStep:    model.SagaStepSendConfirmation,
			CreatedAt:      now,
			UpdatedAt:      now,
		})
		return nil
	})
	return id, err
}

// OutboxRepository is the in-memory repository.OutboxRepository.
type OutboxRepository struct{ store *Store }

// NewOutboxRepository returns an OutboxRepository on store.
func NewOutboxRepository(store *Store) *OutboxRepository {
	return &OutboxRepository{store: store}
}

// Insert queues a message for publication.
func (r *OutboxRepository) Insert(ctx context.Context, queue string, payload []byte) error {
	return r.store.run(ctx, func(st *state) error {
		st.outbox = append(st.outbox, sharedModel.OutboxMessage{
			ID:      st.nextID("outbox"),
			Queue:   queue,
			Payload: payload,
		})
		return nil
	})
}

// SubscriptionRepository is the in-memory repository.SubscriptionRepository.
type SubscriptionRepository struct{ store *Store }

// NewSubscriptionRepository returns a SubscriptionRepository on store.
func NewSubscriptionRepository(store *Store) *SubscriptionRepository {
	return &SubscriptionRepository{store: store}
}

// Upsert creates the subscription or, for an existing unconfirmed pair, replaces its token.
func (r *SubscriptionRepository) Upsert(
	ctx context.Context,
	userID, repositoryID int64,
	token string,
) (model.Subscription, error) {
	var sub model.Subscription
	err := r.store.run(ctx, func(st *state) error {
		for i, row := range st.subscriptions {
			if row.userID != userID || row.repositoryID != repositoryID {
				continue
			}
			if !row.confirmed {
				st.subscriptions[i].token = token
			}
			sub = view(st, st.subscriptions[i])
			return nil
		}
		row := subscriptionRow{
			id:           st.nextID("subscriptions"),
			userID:       userID,
			repositoryID: repositoryID,
			token:        token,
			createdAt:    time.Now(),
		}
		st.subscriptions = append(st.subscriptions, row)
		sub = view(st, row)
		return nil
	})
	return sub, err
}

// GetByToken returns the subscription with the token.
func (r *SubscriptionRepository) GetByToken(ctx context.Context, token string) (model.Subscription, error) {
	var sub model.Subscription
	err := r.store.run(ctx, func(st *state) error {
		for _, row := range st.subscriptions {
			if row.token == token {
				sub = view(st, row)
				return nil
			}
		}
		return model.ErrInvalidToken
	})
	return sub, err
}

// ListByEmail returns the subscriptions of the user, newest first.
func (r *SubscriptionRepository) ListByEmail(ctx context.Context, email string) ([]model.Subscription, error) {
	subs := []model.Subscription{}
	err := r.store.run(ctx, func(st *state) error {
		for _, row := range st.subscriptions {
			if v := view(st, row); v.Email == email {
				subs = append(subs, v)
			}
		}
		sort.Slice(subs, func(i, j int) bool { return subs[i].ID > subs[j].ID })
		return nil
	})
	return subs, err
}

// Confirm marks the subscription confirmed.
func (r *SubscriptionRepository) Confirm(ctx context.Context, id int64) error {
	return r.store.run(ctx, func(st *state) error {
		for i, row := range st.subscriptions {
			if row.id == id {
				st.subscriptions[i].confirmed = true
				return nil
			}
		}
		return model.ErrInvalidToken
	})
}

// Delete removes the subscription.
func (r *SubscriptionRepository) Delete(ctx context.Context, id int64) error {
	return r.store.run(ctx, func(st *state) error {
		for i, row := range st.subscriptions {
			if row.id == id {
				st.subscriptions = append(st.subscriptions[:i:i], st.subscriptions[i+1:]...)
				return nil
			}
		}
		return model.ErrInvalidToken
	})
}

// view joins a subscription row with its user and repository, like the SQL queries do.
func view(st *state, row subscriptionRow) model.Subscription {
	sub := model.Subscription{
		ID:           row.id,
		UserID:       row.userID,
		RepositoryID: row.repositoryID,
		Token:        row.token,
		Confirmed:    row.confirmed,
		CreatedAt:    row.createdAt,
	}
	for _, u := range st.users {
		if u.ID == row.userID {
			sub.Email = u.Email
		}
	}
	for _, repo := range st.repositories {
		if repo.id != row.repositoryID {
			continue
		}
		sub.RepositoryName = repo.fullName
		if repo.tag != "" {
			tag := repo.tag
			sub.LastSeenTag = &tag
		}
	}
	return sub
}
