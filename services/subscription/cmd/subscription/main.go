// Command subscription runs the subscription service.
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
)

func main() {
	jsonHandler := slog.NewJSONHandler(os.Stdout, nil)
	logger := slog.New(ctxlog.NewHandler(jsonHandler)).With(slog.String("service", "subscription"))
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

	app, cleanup, err := initApp(ctx, cfg)
	if err != nil {
		return fmt.Errorf("assemble service: %w", err)
	}
	defer cleanup()

	addr := ":" + cfg.HTTPPort
	slog.InfoContext(ctx, "http server starting",
		slog.String("addr", addr),
		slog.String("version", buildinfo.Commit),
	)

	if err := httpserver.Run(ctx, addr, app.Handler); err != nil {
		return fmt.Errorf("run http server: %w", err)
	}
	return nil
}
