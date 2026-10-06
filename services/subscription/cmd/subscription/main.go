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
	"github.com/ReilEgor/GitHubRepoNotifier/shared/config"
	"github.com/ReilEgor/GitHubRepoNotifier/shared/infrastructure/httpserver"
	"github.com/ReilEgor/GitHubRepoNotifier/shared/infrastructure/probe"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With(slog.String("service", "subscription"))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		logger.Error("service stopped", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	addr := ":" + config.Env("APP_HTTP_PORT", "8080")
	slog.InfoContext(ctx, "http server starting",
		slog.String("addr", addr),
		slog.String("version", buildinfo.Commit),
	)

	if err := httpserver.Run(ctx, addr, probe.NewHandler(buildinfo.Commit)); err != nil {
		return fmt.Errorf("run http server: %w", err)
	}
	return nil
}
