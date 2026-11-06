//go:build wireinject

package main

import (
	"context"

	"github.com/google/wire"

	sharedPostgres "github.com/ReilEgor/GitHubRepoNotifier/shared/infrastructure/storage/postgres"

	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/domain/repository"
	domainusecase "github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/domain/usecase"
	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/infrastructure/adapter"
	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/repository/postgres"
	httptransport "github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/transport/http"
	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/usecase"
)

// repositorySet provides the repositories and the Transactor. Replacing it switches the storage.
var repositorySet = wire.NewSet(
	providePool,
	sharedPostgres.NewTransactor,
	postgres.NewRepositoryRepository,
	postgres.NewUserRepository,
	postgres.NewSubscriptionRepository,
	postgres.NewSagaRepository,
	postgres.NewOutboxRepository,
	wire.Bind(new(repository.Transactor), new(*sharedPostgres.Transactor)),
	wire.Bind(new(repository.RepositoryRepository), new(*postgres.RepositoryRepository)),
	wire.Bind(new(repository.UserRepository), new(*postgres.UserRepository)),
	wire.Bind(new(repository.SubscriptionRepository), new(*postgres.SubscriptionRepository)),
	wire.Bind(new(repository.SagaRepository), new(*postgres.SagaRepository)),
	wire.Bind(new(repository.OutboxRepository), new(*postgres.OutboxRepository)),
	wire.Struct(new(usecase.Repositories), "*"),
)

func initApp(ctx context.Context, cfg Config) (*App, func(), error) {
	wire.Build(
		repositorySet,
		provideTrackingConn,
		provideTrackingClient,
		wire.Bind(new(usecase.RepositoryResolver), new(*adapter.TrackingClient)),
		usecase.NewSubscriptionUseCase,
		wire.Bind(new(domainusecase.SubscriptionUseCase), new(*usecase.SubscriptionUseCase)),
		provideLimiter,
		httptransport.NewHandler,
		provideRouter,
		newApp,
	)
	return nil, nil, nil
}
