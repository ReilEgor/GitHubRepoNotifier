//go:build wireinject

package main

import (
	"github.com/google/wire"

	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/domain/repository"
	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/domain/service"
	domainusecase "github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/domain/usecase"
	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/infrastructure/clients/github"
	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/repository/memory"
	trackinggrpc "github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/transport/grpc"
	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/usecase"
)

// repositorySet provides the repositories. Replacing it switches the storage.
var repositorySet = wire.NewSet(
	memory.NewRepositoryRepository,
	wire.Bind(new(repository.RepositoryRepository), new(*memory.RepositoryRepository)),
)

// githubSet provides the GitHub client. Replacing it switches from the static list to GitHub.
var githubSet = wire.NewSet(
	github.NewStaticClient,
	wire.Bind(new(service.GitHubClient), new(*github.StaticClient)),
)

func initApp(cfg Config) *App {
	wire.Build(
		repositorySet,
		githubSet,
		usecase.NewRepositoryUseCase,
		wire.Bind(new(domainusecase.RepositoryUseCase), new(*usecase.RepositoryUseCase)),
		trackinggrpc.NewServer,
		provideGRPCServer,
		newApp,
	)
	return nil
}
