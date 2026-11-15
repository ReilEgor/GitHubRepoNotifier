//go:build integration

// Package integration runs the tracking scenarios from the gRPC API down to PostgreSQL and out to a fake GitHub.
package integration

import (
	"context"
	"fmt"
	"os"
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
