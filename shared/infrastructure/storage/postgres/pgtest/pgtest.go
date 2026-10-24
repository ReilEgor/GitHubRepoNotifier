//go:build integration

// Package pgtest starts a PostgreSQL container for integration tests, gives every test its own migrated database
// and applies the migrations with golang-migrate, the same files the migrate container applies.
package pgtest

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"

	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // registers the pgx5 database driver.

	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	image       = "postgres:16-alpine"
	adminUser   = "postgres"
	adminSecret = "postgres"
	adminDB     = "postgres"
	opTimeout   = 30 * time.Second
)

var dbCounter atomic.Int64

// Server is a PostgreSQL container shared by all tests of one package.
type Server struct {
	container *tcpostgres.PostgresContainer
	baseDSN   string
}

// Start runs the container; call it from TestMain and Stop it afterwards.
func Start(ctx context.Context) (*Server, error) {
	c, err := tcpostgres.Run(ctx, image,
		tcpostgres.WithDatabase(adminDB),
		tcpostgres.WithUsername(adminUser),
		tcpostgres.WithPassword(adminSecret),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		return nil, fmt.Errorf("start postgres container: %w", err)
	}
	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, errors.Join(fmt.Errorf("get connection string: %w", err), testcontainers.TerminateContainer(c))
	}
	return &Server{container: c, baseDSN: dsn}, nil
}

// Stop terminates the container.
func (s *Server) Stop() error {
	if err := testcontainers.TerminateContainer(s.container); err != nil {
		return fmt.Errorf("terminate postgres container: %w", err)
	}
	return nil
}

// CreateDatabase creates an empty database with a unique name and drops it when the test ends.
func (s *Server) CreateDatabase(t testing.TB) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	admin, err := pgx.Connect(ctx, s.baseDSN)
	if err != nil {
		t.Fatalf("connect to admin database: %v", err)
	}
	defer func() {
		if closeErr := admin.Close(ctx); closeErr != nil {
			t.Logf("close admin connection: %v", closeErr)
		}
	}()

	name := fmt.Sprintf("t_%d_%d", os.Getpid(), dbCounter.Add(1))
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() { s.drop(t, name) })

	u, err := url.Parse(s.baseDSN)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	u.Path = "/" + name
	return u.String()
}

func (s *Server) drop(t testing.TB, name string) {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	admin, err := pgx.Connect(ctx, s.baseDSN)
	if err != nil {
		t.Logf("connect to admin database: %v", err)
		return
	}
	defer func() {
		if err := admin.Close(ctx); err != nil {
			t.Logf("close admin connection: %v", err)
		}
	}()
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
		t.Logf("drop database %s: %v", name, err)
	}
}

// NewMigrate returns a migrator for the database at dsn, reading the *.sql files of dir.
func NewMigrate(dsn, dir string) (*migrate.Migrate, error) {
	src, err := iofs.New(os.DirFS(dir), ".")
	if err != nil {
		return nil, fmt.Errorf("open migrations %s: %w", dir, err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, strings.Replace(dsn, "postgres://", "pgx5://", 1))
	if err != nil {
		return nil, fmt.Errorf("create migrator: %w", err)
	}
	return m, nil
}

// NewPool creates a database, applies the migrations of dir, applies the seed when seedFile is not empty,
// and returns a pool on it. A non-nil tracer sees every query.
func (s *Server) NewPool(t testing.TB, dir, seedFile string, tracer pgx.QueryTracer) *pgxpool.Pool {
	t.Helper()

	dsn := s.CreateDatabase(t)
	m, err := NewMigrate(dsn, dir)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if err = m.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	srcErr, dbErr := m.Close()
	if srcErr != nil || dbErr != nil {
		t.Fatalf("close migrator: %v, %v", srcErr, dbErr)
	}

	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	cfg.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if seedFile != "" {
		sql, err := os.ReadFile(seedFile)
		if err != nil {
			t.Fatalf("read seed: %v", err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply seed: %v", err)
		}
	}
	return pool
}

// DSN returns the connection string of a fresh, empty database, for tests that run migrations themselves.
func (s *Server) DSN(t testing.TB) string {
	t.Helper()
	return s.CreateDatabase(t)
}

// FindDir returns the path of the directory name (for example "migrations") of the module under test,
// searching from the test's working directory upwards.
func FindDir(t testing.TB, name string) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("directory %q not found above the working directory", name)
		}
		dir = parent
	}
}
