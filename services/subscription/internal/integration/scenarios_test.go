//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/ReilEgor/GitHubRepoNotifier/shared/contracts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/domain/model"
)

func TestSubscribe(t *testing.T) {
	t.Run("success - new pair creates user, subscription, saga and command", func(t *testing.T) {
		e := newEnv(t, resolver{}, false)

		rec := e.subscribe("a@b.c", "golang/go")

		require.Equal(t, http.StatusAccepted, rec.Code)
		assert.Equal(t, 1, e.count(t, `SELECT count(*) FROM users WHERE email = 'a@b.c'`))
		assert.Equal(t, 1, e.count(t, `SELECT count(*) FROM subscriptions WHERE is_confirmed = FALSE`))
		assert.Equal(t, 1, e.count(t, `SELECT count(*) FROM subscription_sagas WHERE status = 'STARTED'`))
		assert.Equal(t, 1, e.count(t, `SELECT count(*) FROM outbox_messages WHERE queue = $1 AND status = 'PENDING'`,
			contracts.QueueConfirmations))
		assert.Equal(t, 1, e.count(t, `SELECT count(*) FROM repositories WHERE last_seen_tag = 'v1.0.0'`))
	})

	t.Run("success - unconfirmed pair gets a new token and a new command", func(t *testing.T) {
		e := newEnv(t, resolver{}, true)

		rec := e.subscribe(seedEmail, "golang/go")

		require.Equal(t, http.StatusAccepted, rec.Code)
		assert.NotEqual(t, seedUnconfirmedTok, e.tokenOf(t, seedEmail, "golang/go"))
		assert.Equal(t, 1, e.count(t, `SELECT count(*) FROM subscription_sagas`))
		assert.Equal(t, 1, e.count(t, `SELECT count(*) FROM outbox_messages`))
	})

	t.Run("success - confirmed pair changes nothing", func(t *testing.T) {
		e := newEnv(t, resolver{}, true)

		rec := e.subscribe(seedEmail, "rust-lang/rust")

		require.Equal(t, http.StatusAccepted, rec.Code)
		assert.Equal(t, seedConfirmedTok, e.tokenOf(t, seedEmail, "rust-lang/rust"))
		assert.Equal(
			t,
			1,
			e.count(t, `SELECT count(*) FROM subscriptions WHERE token = $1 AND is_confirmed`, seedConfirmedTok),
		)
		assert.Zero(t, e.count(t, `SELECT count(*) FROM subscription_sagas`))
		assert.Zero(t, e.count(t, `SELECT count(*) FROM outbox_messages`))
	})

	failures := []struct {
		name     string
		err      error
		wantCode int
	}{
		{"error - repository not found", model.ErrRepositoryNotFound, http.StatusNotFound},
		{"error - repository has no releases", model.ErrRepositoryHasNoReleases, http.StatusUnprocessableEntity},
		{"error - tracking unavailable", model.ErrServiceUnavailable, http.StatusServiceUnavailable},
		{"error - unexpected failure", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tt := range failures {
		t.Run(tt.name+" writes nothing", func(t *testing.T) {
			e := newEnv(t, resolver{err: tt.err}, false)

			rec := e.subscribe("a@b.c", "golang/go")

			assert.Equal(t, tt.wantCode, rec.Code)
			for _, table := range []string{"users", "repositories", "subscriptions", "subscription_sagas", "outbox_messages"} {
				assert.Zero(t, e.count(t, "SELECT count(*) FROM "+table), table)
			}
		})
	}

	t.Run("success - parallel subscriptions with the same new email share one user", func(t *testing.T) {
		e := newEnv(t, resolver{}, false)

		const n = 8
		codes := make(chan int, n)
		var wg sync.WaitGroup
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				codes <- e.subscribe("same@b.c", fmt.Sprintf("owner/repo%d", i)).Code
			}()
		}
		wg.Wait()
		close(codes)

		for c := range codes {
			assert.Equal(t, http.StatusAccepted, c)
		}
		assert.Equal(t, 1, e.count(t, `SELECT count(*) FROM users`))
		assert.Equal(t, n, e.count(t, `SELECT count(*) FROM subscriptions`))
	})

	t.Run("success - parallel subscriptions of the same pair leave one subscription", func(t *testing.T) {
		e := newEnv(t, resolver{}, false)

		const n = 8
		codes := make(chan int, n)
		var wg sync.WaitGroup
		for range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				codes <- e.subscribe("same@b.c", "golang/go").Code
			}()
		}
		wg.Wait()
		close(codes)

		for c := range codes {
			assert.Equal(t, http.StatusAccepted, c)
		}
		assert.Equal(t, 1, e.count(t, `SELECT count(*) FROM subscriptions`))
	})
}

func TestSubscribe_rollsBackEverythingOnTheLastStep(t *testing.T) {
	e := newEnv(t, resolver{}, false, func(r *usecaseRepos) { r.Outbox = failingOutbox{} })

	rec := e.subscribe("a@b.c", "golang/go")

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Zero(t, e.count(t, `SELECT count(*) FROM users`), "the user must be rolled back")
	assert.Zero(t, e.count(t, `SELECT count(*) FROM subscriptions`))
	assert.Zero(t, e.count(t, `SELECT count(*) FROM subscription_sagas`))
	assert.Equal(
		t,
		1,
		e.count(t, `SELECT count(*) FROM repositories`),
		"the repository copy is saved before the transaction",
	)
}

func TestConfirm(t *testing.T) {
	t.Run("success - subscription confirmed and event written", func(t *testing.T) {
		e := newEnv(t, resolver{}, true)

		rec := e.do(http.MethodGet, "/api/v1/confirm/"+seedUnconfirmedTok, "")

		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(
			t,
			1,
			e.count(t, `SELECT count(*) FROM subscriptions WHERE token = $1 AND is_confirmed`, seedUnconfirmedTok),
		)
		assert.Equal(
			t,
			1,
			e.count(t, `SELECT count(*) FROM outbox_messages WHERE queue = $1`, contracts.QueueSubscriptionActivated),
		)
	})

	t.Run("success - confirming again is a 200 that writes the event again", func(t *testing.T) {
		e := newEnv(t, resolver{}, true)

		rec := e.do(http.MethodGet, "/api/v1/confirm/"+seedConfirmedTok, "")

		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(
			t,
			1,
			e.count(t, `SELECT count(*) FROM outbox_messages WHERE queue = $1`, contracts.QueueSubscriptionActivated),
		)
	})

	t.Run("error - unknown token", func(t *testing.T) {
		e := newEnv(t, resolver{}, true)

		rec := e.do(http.MethodGet, "/api/v1/confirm/no-such-token", "")

		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Zero(t, e.count(t, `SELECT count(*) FROM outbox_messages`))
	})

	t.Run("error - failed event rolls the confirmation back", func(t *testing.T) {
		e := newEnv(t, resolver{}, true, func(r *usecaseRepos) { r.Outbox = failingOutbox{} })

		rec := e.do(http.MethodGet, "/api/v1/confirm/"+seedUnconfirmedTok, "")

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Equal(
			t,
			1,
			e.count(t, `SELECT count(*) FROM subscriptions WHERE token = $1 AND NOT is_confirmed`, seedUnconfirmedTok),
		)
	})
}

func TestList(t *testing.T) {
	t.Run("success - subscriptions of the user, newest first, without tokens", func(t *testing.T) {
		e := newEnv(t, resolver{}, true)

		rec := e.do(http.MethodGet, "/api/v1/subscriptions?email="+seedEmail, "")

		require.Equal(t, http.StatusOK, rec.Code)
		body := decode(t, rec)
		assert.InDelta(t, 2, body["total"], 0)
		items, ok := body["subscriptions"].([]any)
		require.True(t, ok)
		first, ok := items[0].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "rust-lang/rust", first["repository_name"])
		assert.Nil(t, first["last_seen_tag"], "a repository without a release has a null tag")
		assert.NotContains(t, rec.Body.String(), seedUnconfirmedTok)
		assert.NotContains(t, rec.Body.String(), seedConfirmedTok)
	})

	t.Run("success - unknown email gives an empty list", func(t *testing.T) {
		e := newEnv(t, resolver{}, true)

		rec := e.do(http.MethodGet, "/api/v1/subscriptions?email=nobody@b.c", "")

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"subscriptions":[],"total":0}`, rec.Body.String())
	})

	t.Run("error - database unavailable in the middle of the request", func(t *testing.T) {
		e := newEnv(t, resolver{}, true)
		e.pool.Close()

		rec := e.do(http.MethodGet, "/api/v1/subscriptions?email="+seedEmail, "")

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.JSONEq(t, `{"error":"failed to list subscriptions"}`, rec.Body.String())
	})
}

func TestUnsubscribe(t *testing.T) {
	t.Run("success - subscription deleted and event written", func(t *testing.T) {
		e := newEnv(t, resolver{}, true)

		rec := e.do(http.MethodGet, "/api/v1/unsubscribe/"+seedConfirmedTok, "")

		require.Equal(t, http.StatusOK, rec.Code)
		assert.Zero(t, e.count(t, `SELECT count(*) FROM subscriptions WHERE token = $1`, seedConfirmedTok))
		assert.Equal(
			t,
			1,
			e.count(t, `SELECT count(*) FROM outbox_messages WHERE queue = $1`, contracts.QueueUnsubscriptionActivated),
		)
	})

	t.Run("error - the token works once", func(t *testing.T) {
		e := newEnv(t, resolver{}, true)
		require.Equal(t, http.StatusOK, e.do(http.MethodGet, "/api/v1/unsubscribe/"+seedConfirmedTok, "").Code)

		rec := e.do(http.MethodGet, "/api/v1/unsubscribe/"+seedConfirmedTok, "")

		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Equal(t, 1, e.count(t, `SELECT count(*) FROM outbox_messages`))
	})

	t.Run("success - two parallel requests with one token give exactly one 200 and one 404", func(t *testing.T) {
		e := newEnv(t, resolver{}, true)

		codes := make(chan int, 2)
		var wg sync.WaitGroup
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				codes <- e.do(http.MethodGet, "/api/v1/unsubscribe/"+seedConfirmedTok, "").Code
			}()
		}
		wg.Wait()
		close(codes)

		got := []int{}
		for c := range codes {
			got = append(got, c)
		}
		assert.ElementsMatch(t, []int{http.StatusOK, http.StatusNotFound}, got)
		assert.Equal(t, 1, e.count(t, `SELECT count(*) FROM outbox_messages`), "only the winner writes the event")
	})

	t.Run(
		"success - confirm racing unsubscribe never leaves an activation event for a deleted subscription",
		func(t *testing.T) {
			for range 10 {
				e := newEnv(t, resolver{}, true)

				var confirmCode, unsubscribeCode int
				var wg sync.WaitGroup
				wg.Add(2)
				go func() {
					defer wg.Done()
					confirmCode = e.do(http.MethodGet, "/api/v1/confirm/"+seedUnconfirmedTok, "").Code
				}()
				go func() {
					defer wg.Done()
					unsubscribeCode = e.do(http.MethodGet, "/api/v1/unsubscribe/"+seedUnconfirmedTok, "").Code
				}()
				wg.Wait()

				assert.Equal(t, http.StatusOK, unsubscribeCode)
				activations := e.count(
					t,
					`SELECT count(*) FROM outbox_messages WHERE queue = $1`,
					contracts.QueueSubscriptionActivated,
				)
				if confirmCode == http.StatusNotFound {
					assert.Zero(t, activations, "a lost confirm must not write its event")
				} else {
					assert.Equal(t, http.StatusOK, confirmCode)
					assert.Equal(t, 1, activations)
				}
			}
		},
	)
}

// TestList_isOneQuery proves the subscription list does not grow with the number of subscriptions.
func TestList_isOneQuery(t *testing.T) {
	for _, n := range []int{1, 5, 25} {
		t.Run(fmt.Sprintf("success - %d subscriptions take one query", n), func(t *testing.T) {
			e := newEnv(t, resolver{}, false)
			for i := range n {
				require.Equal(t, http.StatusAccepted, e.subscribe("many@b.c", fmt.Sprintf("owner/repo%d", i)).Code)
			}
			before := e.queries.n.Load()

			subs, err := e.uc.List(context.Background(), "many@b.c")

			require.NoError(t, err)
			assert.Len(t, subs, n)
			assert.Equal(t, int64(1), e.queries.n.Load()-before)
		})
	}
}
