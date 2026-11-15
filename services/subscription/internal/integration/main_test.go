//go:build integration

// Package integration runs the subscription scenarios against a real PostgreSQL, from the HTTP API down to the SQL.
package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ReilEgor/GitHubRepoNotifier/shared/infrastructure/storage/postgres/pgtest"
	"github.com/ReilEgor/GitHubRepoNotifier/shared/infrastructure/storage/redistest"
)

var (
	server      *pgtest.Server
	redisServer *redistest.Server
)

func TestMain(m *testing.M) {
	srv, err := pgtest.Start(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	server = srv
	redisSrv, err := redistest.Start(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if stopErr := srv.Stop(); stopErr != nil {
			fmt.Fprintln(os.Stderr, stopErr)
		}
		os.Exit(1)
	}
	redisServer = redisSrv
	code := m.Run()
	for _, stop := range []func() error{srv.Stop, redisSrv.Stop} {
		if err := stop(); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}
	os.Exit(code)
}

func migrationsDir(t testing.TB) string {
	t.Helper()
	return pgtest.FindDir(t, "migrations")
}

func seedFile(t testing.TB) string {
	t.Helper()
	return filepath.Join(migrationsDir(t), "testdata", "seed.sql")
}
