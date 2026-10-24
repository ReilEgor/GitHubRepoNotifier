package main

import (
	"net/http"

	"github.com/ReilEgor/GitHubRepoNotifier/shared/buildinfo"
	"github.com/ReilEgor/GitHubRepoNotifier/shared/infrastructure/probe"
	"google.golang.org/grpc"

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
