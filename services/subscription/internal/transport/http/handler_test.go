package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/domain/model"
	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/repository/memory"
	httptransport "github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/transport/http"
	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/transport/http/middleware"
	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/usecase"
)

const apiKey = "secret"

type resolver struct{ err error }

func (r resolver) GetOrCreateRepository(_ context.Context, name string) (model.TrackedRepository, error) {
	if r.err != nil {
		return model.TrackedRepository{}, r.err
	}
	return model.TrackedRepository{ID: 1, FullName: name, LastSeenTag: "v1"}, nil
}

type allowAll struct{}

func (allowAll) Allow(context.Context, string) (bool, error) { return true, nil }

type app struct {
	router http.Handler
	repos  usecase.Repositories
}

func newApp(res resolver) app {
	store := memory.NewStore()
	repos := usecase.Repositories{
		Repositories:  memory.NewRepositoryRepository(store),
		Users:         memory.NewUserRepository(store),
		Subscriptions: memory.NewSubscriptionRepository(store),
		Sagas:         memory.NewSagaRepository(store),
		Outbox:        memory.NewOutboxRepository(store),
	}
	uc := usecase.NewSubscriptionUseCase(store, repos, res)
	probe := http.NotFoundHandler()
	return app{router: httptransport.NewRouter(httptransport.NewHandler(uc), allowAll{}, apiKey, probe), repos: repos}
}

func (a app) do(method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, target, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)
	return rec
}

func (a app) token(t *testing.T, email string) string {
	t.Helper()
	subs, err := a.repos.Subscriptions.ListByEmail(context.Background(), email)
	require.NoError(t, err)
	require.NotEmpty(t, subs)
	return subs[0].Token
}

var auth = map[string]string{middleware.HeaderAPIKey: apiKey}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &m))
	return m
}

func asSlice(t *testing.T, v any) []any {
	t.Helper()
	s, ok := v.([]any)
	require.True(t, ok, "want a JSON array, got %T", v)
	return s
}

func asMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	require.True(t, ok, "want a JSON object, got %T", v)
	return m
}

func asString(t *testing.T, v any) string {
	t.Helper()
	s, ok := v.(string)
	require.True(t, ok, "want a JSON string, got %T", v)
	return s
}

func TestHandler_Subscribe(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		headers   map[string]string
		resolver  resolver
		wantCode  int
		wantError string
		wantList  []string
	}{
		{name: "success - accepted", body: `{"email":"a@b.c","repository":"golang/go"}`, headers: auth, wantCode: 202},
		{
			name:     "success - spaces around fields are trimmed",
			body:     `{"email":" a@b.c ","repository":" golang/go "}`,
			headers:  auth,
			wantCode: 202,
		},
		{
			name:      "error - body is not json",
			body:      `nope`,
			headers:   auth,
			wantCode:  400,
			wantError: "invalid request body:",
		},
		{
			name:      "error - email missing",
			body:      `{"repository":"golang/go"}`,
			headers:   auth,
			wantCode:  400,
			wantError: "invalid request body: email is required",
		},
		{
			name:      "error - repository missing",
			body:      `{"email":"a@b.c"}`,
			headers:   auth,
			wantCode:  400,
			wantError: "invalid request body: repository is required",
		},
		{
			name:     "error - both fields invalid are reported together",
			body:     `{"email":"not-an-email","repository":"nope"}`,
			headers:  auth,
			wantCode: 400,
			wantList: []string{"invalid email format", "invalid repository format (expected 'owner/repo')"},
		},
		{
			name:     "error - display name form is rejected",
			body:     `{"email":"Bob <a@b.c>","repository":"golang/go"}`,
			headers:  auth,
			wantCode: 400,
			wantList: []string{"invalid email format"},
		},
		{
			name:      "error - api key missing",
			body:      `{"email":"a@b.c","repository":"golang/go"}`,
			wantCode:  401,
			wantError: "unauthorized: invalid or missing API key",
		},
		{
			name:      "error - api key wrong",
			body:      `{"email":"a@b.c","repository":"golang/go"}`,
			headers:   map[string]string{middleware.HeaderAPIKey: "x"},
			wantCode:  401,
			wantError: "unauthorized: invalid or missing API key",
		},
		{
			name:      "error - repository not found",
			body:      `{"email":"a@b.c","repository":"golang/go"}`,
			headers:   auth,
			resolver:  resolver{model.ErrRepositoryNotFound},
			wantCode:  404,
			wantError: "repository not found",
		},
		{
			name:      "error - no releases",
			body:      `{"email":"a@b.c","repository":"golang/go"}`,
			headers:   auth,
			resolver:  resolver{model.ErrRepositoryHasNoReleases},
			wantCode:  422,
			wantError: "repository has no releases",
		},
		{
			name:      "error - tracking unavailable",
			body:      `{"email":"a@b.c","repository":"golang/go"}`,
			headers:   auth,
			resolver:  resolver{model.ErrServiceUnavailable},
			wantCode:  503,
			wantError: "GitHub API is currently unavailable, please try again later",
		},
		{
			name:      "error - unknown failure",
			body:      `{"email":"a@b.c","repository":"golang/go"}`,
			headers:   auth,
			resolver:  resolver{errors.New("boom")},
			wantCode:  500,
			wantError: "failed to subscribe",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newApp(tt.resolver)

			rec := a.do(http.MethodPost, "/api/v1/subscribe", tt.body, tt.headers)

			assert.Equal(t, tt.wantCode, rec.Code)
			body := decode(t, rec)
			if tt.wantError != "" {
				assert.Contains(t, body["error"], tt.wantError)
			}
			if tt.wantList != nil {
				got := make([]string, 0)
				for _, e := range asSlice(t, body["errors"]) {
					got = append(got, asString(t, e))
				}
				assert.Equal(t, tt.wantList, got)
			}
			if tt.wantCode == 202 {
				assert.NotContains(t, rec.Body.String(), "token")
			}
		})
	}
}

func TestHandler_ConfirmListUnsubscribe(t *testing.T) {
	a := newApp(resolver{})
	require.Equal(
		t,
		202,
		a.do(http.MethodPost, "/api/v1/subscribe", `{"email":"a@b.c","repository":"golang/go"}`, auth).Code,
	)
	token := a.token(t, "a@b.c")

	t.Run("error - confirm with unknown token", func(t *testing.T) {
		rec := a.do(http.MethodGet, "/api/v1/confirm/unknown", "", nil)
		assert.Equal(t, 404, rec.Code)
		assert.Equal(t, "invalid or expired token", decode(t, rec)["error"])
	})

	t.Run("success - list before confirmation", func(t *testing.T) {
		rec := a.do(http.MethodGet, "/api/v1/subscriptions?email=a@b.c", "", auth)
		require.Equal(t, 200, rec.Code)
		body := decode(t, rec)
		assert.InDelta(t, 1, body["total"], 0)
		item := asMap(t, asSlice(t, body["subscriptions"])[0])
		assert.Equal(t, false, item["confirmed"])
		assert.Equal(t, "golang/go", item["repository_name"])
		assert.Equal(t, "v1", item["last_seen_tag"])
		assert.NotContains(t, rec.Body.String(), token)
	})

	t.Run("success - confirm", func(t *testing.T) {
		rec := a.do(http.MethodGet, "/api/v1/confirm/"+token, "", nil)
		assert.Equal(t, 200, rec.Code)
		assert.Equal(t, "subscription confirmed successfully", decode(t, rec)["message"])
		list := decode(t, a.do(http.MethodGet, "/api/v1/subscriptions?email=a@b.c", "", auth))
		assert.Equal(t, true, asMap(t, asSlice(t, list["subscriptions"])[0])["confirmed"])
	})

	listErrors := []struct {
		name, target string
		headers      map[string]string
		wantCode     int
		wantError    string
	}{
		{"error - email missing", "/api/v1/subscriptions", auth, 400, "email is required"},
		{"error - email invalid", "/api/v1/subscriptions?email=bad", auth, 400, "invalid email format"},
		{
			"error - api key missing",
			"/api/v1/subscriptions?email=a@b.c",
			nil,
			401,
			"unauthorized: invalid or missing API key",
		},
	}
	for _, tt := range listErrors {
		t.Run(tt.name, func(t *testing.T) {
			rec := a.do(http.MethodGet, tt.target, "", tt.headers)
			assert.Equal(t, tt.wantCode, rec.Code)
			assert.Equal(t, tt.wantError, decode(t, rec)["error"])
		})
	}

	t.Run("success - unknown email gives an empty list", func(t *testing.T) {
		rec := a.do(http.MethodGet, "/api/v1/subscriptions?email=nobody@b.c", "", auth)
		assert.Equal(t, 200, rec.Code)
		assert.JSONEq(t, `{"subscriptions":[],"total":0}`, rec.Body.String())
	})

	t.Run("success - unsubscribe then error on the second try", func(t *testing.T) {
		rec := a.do(http.MethodGet, "/api/v1/unsubscribe/"+token, "", nil)
		assert.Equal(t, 200, rec.Code)
		assert.Equal(t, "You have been successfully unsubscribed", decode(t, rec)["message"])

		again := a.do(http.MethodGet, "/api/v1/unsubscribe/"+token, "", nil)
		assert.Equal(t, 404, again.Code)
	})
}

func TestHandler_Unsubscribe_parallel(t *testing.T) {
	a := newApp(resolver{})
	require.Equal(
		t,
		202,
		a.do(http.MethodPost, "/api/v1/subscribe", `{"email":"a@b.c","repository":"golang/go"}`, auth).Code,
	)
	token := a.token(t, "a@b.c")

	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- a.do(http.MethodGet, "/api/v1/unsubscribe/"+token, "", nil).Code
		}()
	}
	wg.Wait()
	close(codes)

	got := []int{}
	for c := range codes {
		got = append(got, c)
	}
	assert.ElementsMatch(t, []int{200, 404}, got)
}

func TestRequestID(t *testing.T) {
	a := newApp(resolver{})

	t.Run("success - client id is returned", func(t *testing.T) {
		rec := a.do(http.MethodGet, "/api/v1/confirm/x", "", map[string]string{middleware.HeaderRequestID: "my-id"})
		assert.Equal(t, "my-id", rec.Header().Get(middleware.HeaderRequestID))
	})

	t.Run("success - id is generated when missing", func(t *testing.T) {
		rec := a.do(http.MethodGet, "/api/v1/confirm/x", "", nil)
		assert.Len(t, rec.Header().Get(middleware.HeaderRequestID), 36)
	})

	t.Run("success - oversized id is replaced", func(t *testing.T) {
		rec := a.do(
			http.MethodGet,
			"/api/v1/confirm/x",
			"",
			map[string]string{middleware.HeaderRequestID: strings.Repeat("a", 500)},
		)
		assert.Len(t, rec.Header().Get(middleware.HeaderRequestID), 36)
	})
}
