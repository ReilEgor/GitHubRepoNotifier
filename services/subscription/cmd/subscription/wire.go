//go:build wireinject

package main

import (
	"github.com/google/wire"

	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/domain/repository"
	domainusecase "github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/domain/usecase"
	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/infrastructure/adapter"
	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/repository/memory"
	httptransport "github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/transport/http"
	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/usecase"
)

// repositorySet provides the repositories and the Transactor. Replacing it switches the storage.
var repositorySet = wire.NewSet(
	memory.NewStore,
	memory.NewRepositoryRepository,
	memory.NewUserRepository,
	memory.NewSubscriptionRepository,
	memory.NewSagaRepository,
	memory.NewOutboxRepository,
	wire.Bind(new(repository.Transactor), new(*memory.Store)),
	wire.Bind(new(repository.RepositoryRepository), new(*memory.RepositoryRepository)),
	wire.Bind(new(repository.UserRepository), new(*memory.UserRepository)),
	wire.Bind(new(repository.SubscriptionRepository), new(*memory.SubscriptionRepository)),
	wire.Bind(new(repository.SagaRepository), new(*memory.SagaRepository)),
	wire.Bind(new(repository.OutboxRepository), new(*memory.OutboxRepository)),
	wire.Struct(new(usecase.Repositories), "*"),
)

func initApp(cfg Config) (*App, func(), error) {
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
