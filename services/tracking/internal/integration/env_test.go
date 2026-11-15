//go:build integration

package integration

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ReilEgor/GitHubRepoNotifier/shared/infrastructure/storage/postgres/pgtest"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	pb "github.com/ReilEgor/GitHubRepoNotifier/shared/infrastructure/grpc/trackingv1"

	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/infrastructure/clients/github"
	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/repository/postgres"
	trackinggrpc "github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/transport/grpc"
	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/usecase"
)

const apiKey = "test-key"

// githubRepo describes how the fake GitHub answers for one repository.
type githubRepo struct {
	exists     bool
	tag        string // empty: the repository has no release.
	failStatus int    // when set, every request to the repository answers with it.
	remaining  string // the X-RateLimit-Remaining header of the failing answer.
}

// fakeGitHub is a GitHub that counts the requests it receives.
type fakeGitHub struct {
	srv   *httptest.Server
	mu    sync.Mutex
	repos map[string]githubRepo
	calls map[string]int // "METHOD /path" -> number of requests.
}

func newFakeGitHub(t *testing.T, repos map[string]githubRepo) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{repos: repos, calls: map[string]int{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.calls[r.Method+" "+r.URL.Path]++
	f.mu.Unlock()

	const prefix = "/repos/"
	name, isRelease := r.URL.Path[len(prefix):], false
	if suffix := "/releases/latest"; len(name) > len(suffix) && name[len(name)-len(suffix):] == suffix {
		name, isRelease = name[:len(name)-len(suffix)], true
	}
	repo, ok := f.repos[name]
	switch {
	case ok && repo.failStatus != 0:
		if repo.remaining != "" {
			w.Header().Set("X-RateLimit-Remaining", repo.remaining)
		}
		w.WriteHeader(repo.failStatus)
	case !ok || !repo.exists:
		w.WriteHeader(http.StatusNotFound)
	case isRelease && repo.tag == "":
		w.WriteHeader(http.StatusNotFound)
	case isRelease:
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"tag_name":"` + repo.tag + `"}`)); err != nil {
			slog.Debug("write fake response", slog.String("error", err.Error()))
		}
	default:
		w.WriteHeader(http.StatusOK)
	}
}

// total is the number of requests the fake received.
func (f *fakeGitHub) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		n += c
	}
	return n
}

type env struct {
	client pb.TrackingServiceClient
	pool   *pgxpool.Pool
	github *fakeGitHub
}

func (e env) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, e.pool.QueryRow(context.Background(), sql, args...).Scan(&n))
	return n
}

// newEnv wires the real stack: gRPC server, use case, PostgreSQL repository, GitHub client with the Redis cache,
// all but GitHub itself, which is the fake.
func newEnv(t *testing.T, repos map[string]githubRepo, seed bool) env {
	t.Helper()

	dir := pgtest.FindDir(t, "migrations")
	seedPath := ""
	if seed {
		seedPath = filepath.Join(dir, "testdata", "seed.sql")
	}
	pool := server.NewPool(t, dir, seedPath, nil)

	fake := newFakeGitHub(t, repos)
	cache := redis.NewClient(&redis.Options{Addr: redisServer.Addr(), ContextTimeoutEnabled: true})
	t.Cleanup(func() { closeQuietly(cache) })
	redisServer.Flush(t)
	gh := github.NewCached(github.NewClient(github.Config{
		BaseURL:  fake.srv.URL,
		Timeout:  time.Second,
		Attempts: 2,
		Backoff:  time.Millisecond,
	}), cache)

	uc := usecase.NewRepositoryUseCase(postgres.NewRepositoryRepository(pool), gh)
	g := trackinggrpc.NewGRPCServer(apiKey, trackinggrpc.NewServer(uc))
	lis, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() {
		if serveErr := g.Serve(lis); serveErr != nil {
			slog.Debug("test server stopped", slog.String("error", serveErr.Error()))
		}
	}()
	t.Cleanup(g.Stop)

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { closeQuietly(conn) })

	return env{client: pb.NewTrackingServiceClient(conn), pool: pool, github: fake}
}

// withKey returns a context that carries the API key.
func withKey(key string) context.Context {
	return metadata.AppendToOutgoingContext(context.Background(), "x-api-key", key)
}

// closeQuietly closes c and only logs a failure: a cleanup error must not fail a test that already has its result.
func closeQuietly(c io.Closer) {
	if err := c.Close(); err != nil {
		slog.Debug("close", slog.String("error", err.Error()))
	}
}
