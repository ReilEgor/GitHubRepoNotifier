package usecase_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/ReilEgor/GitHubRepoNotifier/shared/contracts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/domain/model"
	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/repository/memory"
	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/usecase"
)

type fakeResolver struct {
	repo model.TrackedRepository
	err  error
}

func (f fakeResolver) GetOrCreateRepository(_ context.Context, name string) (model.TrackedRepository, error) {
	if f.err != nil {
		return model.TrackedRepository{}, f.err
	}
	r := f.repo
	r.FullName = name
	return r, nil
}

// failingOutbox fails every insert, to test that the transaction rolls back.
type failingOutbox struct{}

func (failingOutbox) Insert(context.Context, string, []byte) error { return errors.New("outbox down") }

type env struct {
	store *memory.Store
	repos usecase.Repositories
	uc    *usecase.SubscriptionUseCase
}

func newEnv(resolver usecase.RepositoryResolver, mutate func(*usecase.Repositories)) env {
	store := memory.NewStore()
	repos := usecase.Repositories{
		Repositories:  memory.NewRepositoryRepository(store),
		Users:         memory.NewUserRepository(store),
		Subscriptions: memory.NewSubscriptionRepository(store),
		Sagas:         memory.NewSagaRepository(store),
		Outbox:        memory.NewOutboxRepository(store),
	}
	if mutate != nil {
		mutate(&repos)
	}
	return env{store: store, repos: repos, uc: usecase.NewSubscriptionUseCase(store, repos, resolver)}
}

var okResolver = fakeResolver{repo: model.TrackedRepository{ID: 7, LastSeenTag: "v1.0.0"}}

func tokenOf(t *testing.T, e env, email string) string {
	t.Helper()
	subs, err := e.repos.Subscriptions.ListByEmail(context.Background(), email)
	require.NoError(t, err)
	require.NotEmpty(t, subs)
	return subs[0].Token
}

func TestSubscriptionUseCase_Subscribe(t *testing.T) {
	ctx := context.Background()

	t.Run("success - new subscription creates saga and confirmation command", func(t *testing.T) {
		e := newEnv(okResolver, nil)

		require.NoError(t, e.uc.Subscribe(ctx, "a@b.c", "golang/go"))

		subs, err := e.uc.List(ctx, "a@b.c")
		require.NoError(t, err)
		require.Len(t, subs, 1)
		assert.False(t, subs[0].Confirmed)
		assert.Equal(t, "golang/go", subs[0].RepositoryName)
		require.NotNil(t, subs[0].LastSeenTag)
		assert.Equal(t, "v1.0.0", *subs[0].LastSeenTag)
		assert.Len(t, e.store.Sagas(), 1)

		out := e.store.Outbox()
		require.Len(t, out, 1)
		assert.Equal(t, contracts.QueueConfirmations, out[0].Queue)
		var cmd contracts.SendConfirmationCommand
		require.NoError(t, json.Unmarshal(out[0].Payload, &cmd))
		assert.Equal(t, subs[0].Token, cmd.Token)
		assert.Equal(t, "a@b.c", cmd.Email)
	})

	t.Run("success - unconfirmed pair gets a new token and a new command", func(t *testing.T) {
		e := newEnv(okResolver, nil)
		require.NoError(t, e.uc.Subscribe(ctx, "a@b.c", "golang/go"))
		first := tokenOf(t, e, "a@b.c")

		require.NoError(t, e.uc.Subscribe(ctx, "a@b.c", "golang/go"))

		assert.NotEqual(t, first, tokenOf(t, e, "a@b.c"))
		assert.Len(t, e.store.Outbox(), 2)
		assert.Len(t, e.store.Sagas(), 2)
	})

	t.Run("success - confirmed pair changes nothing", func(t *testing.T) {
		e := newEnv(okResolver, nil)
		require.NoError(t, e.uc.Subscribe(ctx, "a@b.c", "golang/go"))
		token := tokenOf(t, e, "a@b.c")
		require.NoError(t, e.uc.Confirm(ctx, token))
		outboxBefore := len(e.store.Outbox())

		require.NoError(t, e.uc.Subscribe(ctx, "a@b.c", "golang/go"))

		assert.Equal(t, token, tokenOf(t, e, "a@b.c"))
		assert.Len(t, e.store.Outbox(), outboxBefore)
		assert.Len(t, e.store.Sagas(), 1)
		subs, err := e.uc.List(ctx, "a@b.c")
		require.NoError(t, err)
		assert.True(t, subs[0].Confirmed)
	})

	errCases := []struct {
		name string
		err  error
	}{
		{"error - repository not found", model.ErrRepositoryNotFound},
		{"error - repository has no releases", model.ErrRepositoryHasNoReleases},
		{"error - service unavailable", model.ErrServiceUnavailable},
	}
	for _, tt := range errCases {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(fakeResolver{err: tt.err}, nil)

			err := e.uc.Subscribe(ctx, "a@b.c", "golang/go")

			require.ErrorIs(t, err, tt.err)
			assert.Empty(t, e.store.Outbox())
			subs, listErr := e.uc.List(ctx, "a@b.c")
			require.NoError(t, listErr)
			assert.Empty(t, subs)
		})
	}

	t.Run("error - failure on the last step leaves no user, subscription or saga", func(t *testing.T) {
		e := newEnv(okResolver, func(r *usecase.Repositories) { r.Outbox = failingOutbox{} })

		err := e.uc.Subscribe(ctx, "a@b.c", "golang/go")

		require.Error(t, err)
		subs, listErr := e.uc.List(ctx, "a@b.c")
		require.NoError(t, listErr)
		assert.Empty(t, subs)
		assert.Empty(t, e.store.Sagas())
		user, userErr := e.repos.Users.GetOrCreate(ctx, "a@b.c")
		require.NoError(t, userErr)
		assert.Equal(t, int64(1), user.ID, "the rolled back user must not have consumed an id")
	})
}

func TestSubscriptionUseCase_Subscribe_usesTransactionContext(t *testing.T) {
	spy := &ctxSpy{calls: map[string]bool{}}
	e := newEnv(okResolver, func(r *usecase.Repositories) {
		r.Users = spyUsers{r.Users, spy}
		r.Subscriptions = spySubscriptions{r.Subscriptions, spy}
		r.Sagas = spySagas{r.Sagas, spy}
		r.Outbox = spyOutbox{r.Outbox, spy}
	})

	require.NoError(t, e.uc.Subscribe(context.Background(), "a@b.c", "golang/go"))

	for _, name := range []string{"users", "subscriptions", "sagas", "outbox"} {
		assert.True(t, spy.calls[name], "%s must be called with the transaction context", name)
	}
}

func TestSubscriptionUseCase_Confirm(t *testing.T) {
	ctx := context.Background()

	t.Run("success - subscription confirmed and event queued", func(t *testing.T) {
		e := newEnv(okResolver, nil)
		require.NoError(t, e.uc.Subscribe(ctx, "a@b.c", "golang/go"))
		token := tokenOf(t, e, "a@b.c")

		require.NoError(t, e.uc.Confirm(ctx, token))

		subs, err := e.uc.List(ctx, "a@b.c")
		require.NoError(t, err)
		assert.True(t, subs[0].Confirmed)
		out := e.store.Outbox()
		require.Len(t, out, 2)
		assert.Equal(t, contracts.QueueSubscriptionActivated, out[1].Queue)
	})

	t.Run("success - confirming twice writes the event again", func(t *testing.T) {
		e := newEnv(okResolver, nil)
		require.NoError(t, e.uc.Subscribe(ctx, "a@b.c", "golang/go"))
		token := tokenOf(t, e, "a@b.c")

		require.NoError(t, e.uc.Confirm(ctx, token))
		require.NoError(t, e.uc.Confirm(ctx, token))

		assert.Len(t, e.store.Outbox(), 3)
	})

	t.Run("error - unknown token", func(t *testing.T) {
		e := newEnv(okResolver, nil)

		err := e.uc.Confirm(ctx, "nope")

		require.ErrorIs(t, err, model.ErrInvalidToken)
		assert.Empty(t, e.store.Outbox())
	})

	t.Run("error - failed event rolls the confirmation back", func(t *testing.T) {
		e := newEnv(okResolver, nil)
		require.NoError(t, e.uc.Subscribe(ctx, "a@b.c", "golang/go"))
		token := tokenOf(t, e, "a@b.c")
		broken := usecase.NewSubscriptionUseCase(e.store, withOutbox(e.repos, failingOutbox{}), okResolver)

		require.Error(t, broken.Confirm(ctx, token))

		subs, err := e.uc.List(ctx, "a@b.c")
		require.NoError(t, err)
		assert.False(t, subs[0].Confirmed)
	})
}

func TestSubscriptionUseCase_List(t *testing.T) {
	ctx := context.Background()

	t.Run("success - newest first", func(t *testing.T) {
		e := newEnv(okResolver, nil)
		require.NoError(t, e.uc.Subscribe(ctx, "a@b.c", "golang/go"))
		require.NoError(t, e.uc.Subscribe(ctx, "a@b.c", "rust-lang/rust"))

		subs, err := e.uc.List(ctx, "a@b.c")

		require.NoError(t, err)
		require.Len(t, subs, 2)
		assert.Equal(t, "rust-lang/rust", subs[0].RepositoryName)
	})

	t.Run("success - unknown email gives an empty list", func(t *testing.T) {
		e := newEnv(okResolver, nil)

		subs, err := e.uc.List(ctx, "nobody@b.c")

		require.NoError(t, err)
		assert.NotNil(t, subs)
		assert.Empty(t, subs)
	})
}

func TestSubscriptionUseCase_Unsubscribe(t *testing.T) {
	ctx := context.Background()

	t.Run("success - subscription deleted and event queued", func(t *testing.T) {
		e := newEnv(okResolver, nil)
		require.NoError(t, e.uc.Subscribe(ctx, "a@b.c", "golang/go"))
		token := tokenOf(t, e, "a@b.c")

		require.NoError(t, e.uc.Unsubscribe(ctx, token))

		subs, err := e.uc.List(ctx, "a@b.c")
		require.NoError(t, err)
		assert.Empty(t, subs)
		out := e.store.Outbox()
		assert.Equal(t, contracts.QueueUnsubscriptionActivated, out[len(out)-1].Queue)
	})

	t.Run("error - second unsubscribe with the same token", func(t *testing.T) {
		e := newEnv(okResolver, nil)
		require.NoError(t, e.uc.Subscribe(ctx, "a@b.c", "golang/go"))
		token := tokenOf(t, e, "a@b.c")
		require.NoError(t, e.uc.Unsubscribe(ctx, token))

		err := e.uc.Unsubscribe(ctx, token)

		require.ErrorIs(t, err, model.ErrInvalidToken)
	})

	t.Run("success - two parallel requests give exactly one winner", func(t *testing.T) {
		e := newEnv(okResolver, nil)
		require.NoError(t, e.uc.Subscribe(ctx, "a@b.c", "golang/go"))
		token := tokenOf(t, e, "a@b.c")
		before := len(e.store.Outbox())

		const n = 8
		errs := make(chan error, n)
		var wg sync.WaitGroup
		for range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs <- e.uc.Unsubscribe(ctx, token)
			}()
		}
		wg.Wait()
		close(errs)

		var ok, invalid int
		for err := range errs {
			switch {
			case err == nil:
				ok++
			case errors.Is(err, model.ErrInvalidToken):
				invalid++
			default:
				t.Fatalf("unexpected error: %v", err)
			}
		}
		assert.Equal(t, 1, ok)
		assert.Equal(t, n-1, invalid)
		assert.Len(t, e.store.Outbox(), before+1, "only the winner writes the event")
	})
}
