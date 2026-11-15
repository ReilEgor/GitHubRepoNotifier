//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	sharedPostgres "github.com/ReilEgor/GitHubRepoNotifier/shared/infrastructure/storage/postgres"

	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/domain/model"
	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/repository/postgres"
	httptransport "github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/transport/http"
	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/transport/http/middleware"
	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/usecase"
)

const apiKey = "test-key"

// Tokens and names created by migrations/testdata/seed.sql.
const (
	seedEmail          = "seed@example.com"
	seedUnconfirmedTok = "11111111-1111-4111-8111-111111111111" // golang/go, not confirmed.
	seedConfirmedTok   = "22222222-2222-4222-8222-222222222222" // rust-lang/rust, confirmed.
)

type resolver struct{ err error }

func (r resolver) GetOrCreateRepository(_ context.Context, name string) (model.TrackedRepository, error) {
	if r.err != nil {
		return model.TrackedRepository{}, r.err
	}
	return model.TrackedRepository{ID: 1, FullName: name, LastSeenTag: "v1.0.0"}, nil
}

type allowAll struct{}

func (allowAll) Allow(context.Context, string) (bool, error) { return true, nil }

// queryCounter counts the queries sent to the database.
type queryCounter struct{ n atomic.Int64 }

func (c *queryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.n.Add(1)
	return ctx
}

func (*queryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

type env struct {
	pool    *pgxpool.Pool
	uc      *usecase.SubscriptionUseCase
	router  http.Handler
	queries *queryCounter
}

type usecaseRepos = usecase.Repositories

// failingOutbox fails every insert, to prove the surrounding transaction rolls back.
type failingOutbox struct{}

func (failingOutbox) Insert(context.Context, string, []byte) error { return errors.New("outbox down") }

type option func(*usecaseRepos)

func newEnv(t *testing.T, res resolver, seed bool, opts ...option) env {
	t.Helper()
	return newEnvLimited(t, res, seed, allowAll{}, opts...)
}

// newEnvLimited is newEnv with the given rate limiter in front of the API.
func newEnvLimited(t *testing.T, res resolver, seed bool, limiter middleware.Limiter, opts ...option) env {
	t.Helper()

	seedPath := ""
	if seed {
		seedPath = seedFile(t)
	}
	counter := &queryCounter{}
	pool := server.NewPool(t, migrationsDir(t), seedPath, counter)

	repos := usecase.Repositories{
		Repositories:  postgres.NewRepositoryRepository(pool),
		Users:         postgres.NewUserRepository(pool),
		Subscriptions: postgres.NewSubscriptionRepository(pool),
		Sagas:         postgres.NewSagaRepository(pool),
		Outbox:        postgres.NewOutboxRepository(pool),
	}
	for _, o := range opts {
		o(&repos)
	}
	uc := usecase.NewSubscriptionUseCase(sharedPostgres.NewTransactor(pool), repos, res)
	router, err := httptransport.NewRouter(
		httptransport.NewHandler(uc),
		limiter,
		apiKey,
		nil,
		http.NotFoundHandler(),
	)
	require.NoError(t, err)
	return env{pool: pool, uc: uc, router: router, queries: counter}
}

func (e env) do(method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, target, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", apiKey)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

// request sends a request with full control of the headers and of the caller address.
func (e env) request(method, target, body, remoteAddr string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, target, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = remoteAddr
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func (e env) subscribe(email, repo string) *httptest.ResponseRecorder {
	body, err := json.Marshal(map[string]string{"email": email, "repository": repo})
	if err != nil {
		panic(err)
	}
	return e.do(http.MethodPost, "/api/v1/subscribe", string(body))
}

func (e env) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, e.pool.QueryRow(context.Background(), sql, args...).Scan(&n))
	return n
}

func (e env) tokenOf(t *testing.T, email, repo string) string {
	t.Helper()
	var token string
	err := e.pool.QueryRow(
		context.Background(),
		`SELECT s.token FROM subscriptions s JOIN users u ON u.id = s.user_id JOIN repositories r ON r.id = s.repository_id
		 WHERE u.email = $1 AND r.full_name = $2`,
		email,
		repo,
	).Scan(&token)
	require.NoError(t, err)
	return token
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &m))
	return m
}

// closeQuietly closes c and only logs a failure: a cleanup error must not fail a test that already has its result.
func closeQuietly(c io.Closer) {
	if err := c.Close(); err != nil {
		slog.Debug("close", slog.String("error", err.Error()))
	}
}
