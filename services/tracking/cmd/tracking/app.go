package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"github.com/ReilEgor/GitHubRepoNotifier/shared/buildinfo"
	"github.com/ReilEgor/GitHubRepoNotifier/shared/infrastructure/probe"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"

	sharedPostgres "github.com/ReilEgor/GitHubRepoNotifier/shared/infrastructure/storage/postgres"

	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/domain/service"
	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/infrastructure/clients/github"
	trackinggrpc "github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/transport/grpc"
)

// App is the assembled service.
type App struct {
	GRPC  *grpc.Server
	Probe http.Handler
}

func newApp(g *grpc.Server) *App {
	return &App{GRPC: g, Probe: probe.NewHandler(buildinfo.Commit)}
}

func provideGRPCServer(cfg Config, srv *trackinggrpc.Server) *grpc.Server {
	return trackinggrpc.NewGRPCServer(cfg.APIKey, srv)
}

// providePool opens the connection pool and checks the database; a service whose database is down does not start.
func providePool(ctx context.Context, cfg Config) (*pgxpool.Pool, func(), error) {
	pool, err := sharedPostgres.NewPool(ctx, cfg.DBURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	return pool, pool.Close, nil
}

func provideRedis(cfg Config) (redis.Cmdable, func()) {
	client := redis.NewClient(&redis.Options{
		Addr:     net.JoinHostPort(cfg.RedisHost, cfg.RedisPort),
		Password: cfg.RedisPassword,
	})
	return client, func() {
		if err := client.Close(); err != nil {
			slog.Warn("close redis client", slog.String("error", err.Error()))
		}
	}
}

// provideGitHub returns the GitHub client with the Redis cache in front of it.
func provideGitHub(cfg Config, cache redis.Cmdable) service.GitHubClient {
	client := github.NewClient(github.Config{
		BaseURL: cfg.GitHubAPIURL,
		Token:   cfg.GitHubToken,
		Timeout: cfg.GitHubHTTPTimeout,
	})
	return github.NewCached(client, cache)
}
