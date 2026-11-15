//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ReilEgor/GitHubRepoNotifier/shared/contracts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/infrastructure/ratelimit"
)

var callerSeq atomic.Int64

// newCaller returns a caller address nobody used before, so tests sharing one Redis never share a rate limit bucket.
func newCaller() string {
	n := callerSeq.Add(1)
	return fmt.Sprintf("10.%d.%d.%d:4000", n>>16&0xff, n>>8&0xff, n&0xff)
}

// newLimitedEnv puts the real Redis limiter (limit requests per window for each caller) in front of the API.
func newLimitedEnv(t *testing.T, limit int64, window time.Duration) env {
	t.Helper()
	client := ratelimit.NewClient(redisServer.Addr(), "")
	t.Cleanup(func() { closeQuietly(client) })
	return newEnvLimited(t, resolver{}, false, ratelimit.NewRedis(client, limit, window))
}

// call sends a request that carries the API key.
func (e env) call(method, target, body, caller string) *httptest.ResponseRecorder {
	return e.request(method, target, body, caller, map[string]string{"X-API-Key": apiKey})
}

// firstSubscription returns the first entry of a list response.
func firstSubscription(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	items, ok := decode(t, rec)["subscriptions"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, items)
	first, ok := items[0].(map[string]any)
	require.True(t, ok)
	return first
}

// TestJourney walks one subscriber through the whole life of a subscription over HTTP, checking after every step
// what the user would see and what the system stored for the other services.
func TestJourney(t *testing.T) {
	t.Run("success - subscribe, confirm, list, unsubscribe", func(t *testing.T) {
		e := newLimitedEnv(t, 100, time.Minute)
		caller := newCaller()
		const email, repo = "reader@example.com", "golang/go"

		subscribed := e.call(
			http.MethodPost,
			"/api/v1/subscribe",
			`{"email":"`+email+`","repository":"`+repo+`"}`,
			caller,
		)
		require.Equal(t, http.StatusAccepted, subscribed.Code)
		token := e.tokenOf(t, email, repo)

		listed := e.request(
			http.MethodGet,
			"/api/v1/subscriptions?email="+email,
			"",
			caller,
			map[string]string{"X-API-Key": apiKey},
		)
		require.Equal(t, http.StatusOK, listed.Code)
		first := firstSubscription(t, listed)
		assert.Equal(t, false, first["confirmed"], "before the email link is followed")
		assert.Equal(
			t,
			1,
			e.count(t, `SELECT count(*) FROM outbox_messages WHERE queue = $1`, contracts.QueueConfirmations),
		)

		confirmed := e.call(http.MethodGet, "/api/v1/confirm/"+token, "", caller)
		require.Equal(t, http.StatusOK, confirmed.Code)

		listed = e.request(
			http.MethodGet,
			"/api/v1/subscriptions?email="+email,
			"",
			caller,
			map[string]string{"X-API-Key": apiKey},
		)
		first = firstSubscription(t, listed)
		assert.Equal(t, true, first["confirmed"])
		assert.Equal(
			t,
			1,
			e.count(t, `SELECT count(*) FROM outbox_messages WHERE queue = $1`, contracts.QueueSubscriptionActivated),
		)

		unsubscribed := e.call(http.MethodGet, "/api/v1/unsubscribe/"+token, "", caller)
		require.Equal(t, http.StatusOK, unsubscribed.Code)

		listed = e.request(
			http.MethodGet,
			"/api/v1/subscriptions?email="+email,
			"",
			caller,
			map[string]string{"X-API-Key": apiKey},
		)
		assert.InDelta(t, 0, decode(t, listed)["total"], 0)
		assert.Equal(
			t,
			1,
			e.count(t, `SELECT count(*) FROM outbox_messages WHERE queue = $1`, contracts.QueueUnsubscriptionActivated),
		)
		assert.Equal(t, http.StatusNotFound, e.call(http.MethodGet, "/api/v1/confirm/"+token, "", caller).Code,
			"a used-up token stays dead")
	})

	t.Run("success - subscribing again before confirmation kills the old link", func(t *testing.T) {
		e := newLimitedEnv(t, 100, time.Minute)
		caller := newCaller()
		const body = `{"email":"twice@example.com","repository":"golang/go"}`

		require.Equal(t, http.StatusAccepted, e.call(http.MethodPost, "/api/v1/subscribe", body, caller).Code)
		oldToken := e.tokenOf(t, "twice@example.com", "golang/go")
		require.Equal(t, http.StatusAccepted, e.call(http.MethodPost, "/api/v1/subscribe", body, caller).Code)
		newToken := e.tokenOf(t, "twice@example.com", "golang/go")

		require.NotEqual(t, oldToken, newToken)
		assert.Equal(t, http.StatusNotFound, e.call(http.MethodGet, "/api/v1/confirm/"+oldToken, "", caller).Code)
		assert.Equal(t, http.StatusOK, e.call(http.MethodGet, "/api/v1/confirm/"+newToken, "", caller).Code)
		assert.Equal(t, 1, e.count(t, `SELECT count(*) FROM subscriptions`), "one pair is one subscription")
	})
}

func TestAPI_authentication(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		target  string
		body    string
		headers map[string]string
		want    int
	}{
		{
			"error - subscribe without a key",
			http.MethodPost,
			"/api/v1/subscribe",
			`{"email":"a@b.c","repository":"o/r"}`,
			nil,
			http.StatusUnauthorized,
		},
		{
			"error - subscribe with a wrong key",
			http.MethodPost,
			"/api/v1/subscribe",
			`{"email":"a@b.c","repository":"o/r"}`,
			map[string]string{"X-API-Key": "nope"},
			http.StatusUnauthorized,
		},
		{
			"error - list without a key",
			http.MethodGet,
			"/api/v1/subscriptions?email=a@b.c",
			"",
			nil,
			http.StatusUnauthorized,
		},
		{
			"success - confirm needs no key, the token is the credential",
			http.MethodGet,
			"/api/v1/confirm/" + seedUnconfirmedTok,
			"",
			nil,
			http.StatusOK,
		},
		{
			"success - unsubscribe needs no key",
			http.MethodGet,
			"/api/v1/unsubscribe/" + seedConfirmedTok,
			"",
			nil,
			http.StatusOK,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newLimitedEnvSeeded(t, 100, time.Minute)

			rec := e.request(tt.method, tt.target, tt.body, newCaller(), tt.headers)

			assert.Equal(t, tt.want, rec.Code)
		})
	}

	t.Run("error - a rejected subscribe stores nothing", func(t *testing.T) {
		e := newLimitedEnv(t, 100, time.Minute)

		e.request(http.MethodPost, "/api/v1/subscribe", `{"email":"a@b.c","repository":"o/r"}`, newCaller(), nil)

		assert.Zero(t, e.count(t, `SELECT count(*) FROM users`))
	})
}

func newLimitedEnvSeeded(t *testing.T, limit int64, window time.Duration) env {
	t.Helper()
	client := ratelimit.NewClient(redisServer.Addr(), "")
	t.Cleanup(func() { closeQuietly(client) })
	return newEnvLimited(t, resolver{}, true, ratelimit.NewRedis(client, limit, window))
}

func TestAPI_rateLimit(t *testing.T) {
	t.Run("error - the caller over the limit gets 429 on every endpoint, others are not affected", func(t *testing.T) {
		e := newLimitedEnvSeeded(t, 3, time.Minute)
		busy, other := newCaller(), newCaller()
		for i := range 3 {
			rec := e.request(
				http.MethodGet,
				"/api/v1/subscriptions?email="+seedEmail,
				"",
				busy,
				map[string]string{"X-API-Key": apiKey},
			)
			require.Equal(t, http.StatusOK, rec.Code, "request %d", i+1)
		}

		over := e.request(
			http.MethodGet,
			"/api/v1/subscriptions?email="+seedEmail,
			"",
			busy,
			map[string]string{"X-API-Key": apiKey},
		)
		overConfirm := e.request(http.MethodGet, "/api/v1/confirm/"+seedUnconfirmedTok, "", busy, nil)
		fine := e.request(
			http.MethodGet,
			"/api/v1/subscriptions?email="+seedEmail,
			"",
			other,
			map[string]string{"X-API-Key": apiKey},
		)

		assert.Equal(t, http.StatusTooManyRequests, over.Code)
		assert.Equal(t, http.StatusTooManyRequests, overConfirm.Code)
		assert.Equal(
			t,
			0,
			e.count(t, `SELECT count(*) FROM subscriptions WHERE token = $1 AND is_confirmed`, seedUnconfirmedTok),
			"a limited request must not reach the use case",
		)
		assert.Equal(t, http.StatusOK, fine.Code)
	})

	t.Run("success - the caller is served again when the window passes", func(t *testing.T) {
		e := newLimitedEnvSeeded(t, 1, time.Second)
		caller := newCaller()
		list := func() int {
			return e.request(
				http.MethodGet,
				"/api/v1/subscriptions?email="+seedEmail,
				"",
				caller,
				map[string]string{"X-API-Key": apiKey},
			).Code
		}
		require.Equal(t, http.StatusOK, list())
		require.Equal(t, http.StatusTooManyRequests, list())

		time.Sleep(1200 * time.Millisecond)

		assert.Equal(t, http.StatusOK, list())
	})

	t.Run("success - an unreachable limiter lets the request through", func(t *testing.T) {
		client := ratelimit.NewClient("127.0.0.1:1", "")
		t.Cleanup(func() { closeQuietly(client) })
		e := newEnvLimited(t, resolver{}, true, ratelimit.NewRedis(client, 1, time.Minute))

		rec := e.request(
			http.MethodGet,
			"/api/v1/subscriptions?email="+seedEmail,
			"",
			newCaller(),
			map[string]string{"X-API-Key": apiKey},
		)

		assert.Equal(t, http.StatusOK, rec.Code)
	})
}
