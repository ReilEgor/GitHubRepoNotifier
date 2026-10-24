// Command tracking runs the tracking service.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ReilEgor/GitHubRepoNotifier/shared/buildinfo"
	"github.com/ReilEgor/GitHubRepoNotifier/shared/ctxlog"
	"github.com/ReilEgor/GitHubRepoNotifier/shared/infrastructure/httpserver"
	"github.com/caarlos0/env/v11"

	trackinggrpc "github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/transport/grpc"
)

func main() {
	jsonHandler := slog.NewJSONHandler(os.Stdout, nil)
	logger := slog.New(ctxlog.NewHandler(jsonHandler)).With(slog.String("service", "tracking"))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		logger.Error("service stopped", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	app := initApp(cfg)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 2)
	go func() { errCh <- trackinggrpc.Run(ctx, ":"+cfg.GRPCPort, app.GRPC) }()
	go func() { errCh <- httpserver.Run(ctx, ":"+cfg.HealthPort, app.Probe) }()

	slog.InfoContext(ctx, "tracking starting",
		slog.String("grpc_port", cfg.GRPCPort),
		slog.String("health_port", cfg.HealthPort),
		slog.String("version", buildinfo.Commit),
	)

	err = <-errCh
	cancel()
	<-errCh
	if err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}
