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

func migrationsDir(t testing.TB) string {
	t.Helper()
	return pgtest.FindDir(t, "migrations")
}

func seedFile(t testing.TB) string {
	t.Helper()
	return filepath.Join(migrationsDir(t), "testdata", "seed.sql")
}
