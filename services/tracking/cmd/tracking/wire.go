//go:build wireinject

package main

import (
	"context"

	"github.com/google/wire"

	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/domain/repository"
	domainusecase "github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/domain/usecase"
	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/repository/postgres"
	trackinggrpc "github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/transport/grpc"
	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/usecase"
)

// repositorySet provides the repositories. Replacing it switches the storage.
var repositorySet = wire.NewSet(
	providePool,
	postgres.NewRepositoryRepository,
	wire.Bind(new(repository.RepositoryRepository), new(*postgres.RepositoryRepository)),
)

// githubSet provides the GitHub client. Replacing it switches between GitHub and the static list.
var githubSet = wire.NewSet(
	provideRedis,
	provideGitHub,
)

func initApp(ctx context.Context, cfg Config) (*App, func(), error) {
	wire.Build(
		repositorySet,
		githubSet,
		usecase.NewRepositoryUseCase,
		wire.Bind(new(domainusecase.RepositoryUseCase), new(*usecase.RepositoryUseCase)),
		trackinggrpc.NewServer,
		provideGRPCServer,
		newApp,
	)
	return nil, nil, nil
}
