package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"github.com/ReilEgor/GitHubRepoNotifier/shared/buildinfo"
	"github.com/ReilEgor/GitHubRepoNotifier/shared/infrastructure/probe"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	sharedPostgres "github.com/ReilEgor/GitHubRepoNotifier/shared/infrastructure/storage/postgres"

	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/infrastructure/adapter"
	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/infrastructure/ratelimit"
	httptransport "github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/transport/http"
	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/transport/http/middleware"
)

// App is the assembled service.
type App struct {
	Handler http.Handler
}

func newApp(router *gin.Engine) *App {
	return &App{Handler: router}
}

func provideTrackingConn(cfg Config) (*grpc.ClientConn, func(), error) {
	conn, err := grpc.NewClient(cfg.TrackingGRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, fmt.Errorf("dial tracking: %w", err)
	}
	return conn, func() {
		if err := conn.Close(); err != nil {
			slog.Warn("close tracking connection", slog.String("error", err.Error()))
		}
	}, nil
}

func provideTrackingClient(conn *grpc.ClientConn, cfg Config) *adapter.TrackingClient {
	return adapter.NewTrackingClient(conn, cfg.APIKey)
}

// providePool opens the connection pool and checks the database; a service whose database is down does not start.
func providePool(ctx context.Context, cfg Config) (*pgxpool.Pool, func(), error) {
	pool, err := sharedPostgres.NewPool(ctx, cfg.DBURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	return pool, pool.Close, nil
}

// provideLimiter returns the limiter that counts requests in Redis.
func provideLimiter(cfg Config) (middleware.Limiter, func(), error) {
	limit, window, err := ratelimit.ParseRate(cfg.RateLimit)
	if err != nil {
		return nil, nil, fmt.Errorf("rate limit: %w", err)
	}
	client := redis.NewClient(&redis.Options{
		Addr:     net.JoinHostPort(cfg.RedisHost, cfg.RedisPort),
		Password: cfg.RedisPassword,
	})
	return ratelimit.NewRedis(client, limit, window), func() {
		if err := client.Close(); err != nil {
			slog.Warn("close redis client", slog.String("error", err.Error()))
		}
	}, nil
}

func provideRouter(h *httptransport.Handler, limiter middleware.Limiter, cfg Config) *gin.Engine {
	return httptransport.NewRouter(h, limiter, cfg.APIKey, probe.NewHandler(buildinfo.Commit))
}
