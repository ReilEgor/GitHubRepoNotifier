//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ReilEgor/GitHubRepoNotifier/shared/infrastructure/storage/postgres/pgtest"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/domain/model"
	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/infrastructure/clients/github"
	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/repository/postgres"
	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/usecase"
)

var server *pgtest.Server

func TestMain(m *testing.M) {
	srv, err := pgtest.Start(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	server = srv
	code := m.Run()
	if err := srv.Stop(); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(code)
}

func newRepo(t *testing.T, seed bool) *postgres.RepositoryRepository {
	t.Helper()
	dir := pgtest.FindDir(t, "migrations")
	seedPath := ""
	if seed {
		seedPath = filepath.Join(dir, "testdata", "seed.sql")
	}
	return postgres.NewRepositoryRepository(server.NewPool(t, dir, seedPath, nil))
}

func TestRepositoryRepository_GetByName(t *testing.T) {
	ctx := context.Background()

	t.Run("success - seeded repository", func(t *testing.T) {
		repo, err := newRepo(t, true).GetByName(ctx, "golang/go")

		require.NoError(t, err)
		assert.Equal(t, "golang/go", repo.FullName)
		assert.Equal(t, "go1.25.3", repo.LastSeenTag)
		assert.False(t, repo.UpdatedAt.IsZero())
	})

	t.Run("error - unknown repository", func(t *testing.T) {
		_, err := newRepo(t, true).GetByName(ctx, "no/such")

		require.ErrorIs(t, err, model.ErrRepositoryNotFound)
	})
}

func TestRepositoryRepository_Create(t *testing.T) {
	ctx := context.Background()

	t.Run("success - new repository is stored with its tag", func(t *testing.T) {
		r := newRepo(t, false)

		created, err := r.Create(ctx, "rust-lang/rust", "1.90.0")
		require.NoError(t, err)
		got, err := r.GetByName(ctx, "rust-lang/rust")

		require.NoError(t, err)
		assert.Equal(t, created, got)
		assert.Equal(t, "1.90.0", got.LastSeenTag)
	})

	t.Run("success - creating an existing repository returns it unchanged", func(t *testing.T) {
		r := newRepo(t, true)

		got, err := r.Create(ctx, "golang/go", "other-tag")

		require.NoError(t, err)
		assert.Equal(t, "go1.25.3", got.LastSeenTag)
	})
}

// TestRepositoryUseCase_onPostgres runs the use case on the real repository with the static GitHub client.
func TestRepositoryUseCase_onPostgres(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		repoName string
		wantErr  error
	}{
		{name: "success - unknown repository is verified and stored", repoName: github.StaticExisting},
		{name: "error - repository not found", repoName: github.StaticNotFound, wantErr: model.ErrRepositoryNotFound},
		{name: "error - no releases", repoName: github.StaticNoReleases, wantErr: model.ErrReleaseNotFound},
		{name: "error - github unavailable", repoName: github.StaticUnavailable, wantErr: model.ErrGitHubUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repos := newRepo(t, false)
			uc := usecase.NewRepositoryUseCase(repos, github.NewStaticClient())

			got, err := uc.GetOrCreate(ctx, tt.repoName)

			_, storedErr := repos.GetByName(ctx, tt.repoName)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.ErrorIs(t, storedErr, model.ErrRepositoryNotFound, "a failed lookup must store nothing")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "v1.0.0", got.LastSeenTag)
			assert.NoError(t, storedErr)
		})
	}

	t.Run("success - a seeded repository never asks GitHub", func(t *testing.T) {
		uc := usecase.NewRepositoryUseCase(newRepo(t, true), github.NewStaticClient())

		got, err := uc.GetOrCreate(ctx, "golang/go")

		require.NoError(t, err)
		assert.Equal(t, "go1.25.3", got.LastSeenTag)
	})
}

func TestMigrations_upDownUp(t *testing.T) {
	dsn := server.DSN(t)
	m, err := pgtest.NewMigrate(dsn, pgtest.FindDir(t, "migrations"))
	require.NoError(t, err)
	t.Cleanup(func() {
		srcErr, dbErr := m.Close()
		assert.NoError(t, srcErr)
		assert.NoError(t, dbErr)
	})

	tables := func() int {
		ctx := context.Background()
		conn, err := pgx.Connect(ctx, dsn)
		require.NoError(t, err)
		defer func() { require.NoError(t, conn.Close(ctx)) }()
		var n int
		require.NoError(t, conn.QueryRow(ctx,
			`SELECT count(*) FROM information_schema.tables
			 WHERE table_schema = 'public' AND table_name <> 'schema_migrations'`).Scan(&n))
		return n
	}

	require.NoError(t, m.Up())
	assert.Equal(t, 3, tables(), "repositories, subscriptions, outbox_messages")
	require.NoError(t, m.Down())
	assert.Zero(t, tables(), "down leaves no table behind")
	require.NoError(t, m.Up())
	assert.Equal(t, 3, tables())
}
