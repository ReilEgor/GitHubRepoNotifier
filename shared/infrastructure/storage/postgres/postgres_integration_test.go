//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ReilEgor/GitHubRepoNotifier/shared/infrastructure/storage/postgres"
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

func TestNewPool(t *testing.T) {
	t.Run("success - reachable database", func(t *testing.T) {
		pool, err := postgres.NewPool(context.Background(), server.DSN(t), nil)

		require.NoError(t, err)
		pool.Close()
	})

	t.Run("error - unreachable database", func(t *testing.T) {
		_, err := postgres.NewPool(context.Background(), "postgres://u:p@127.0.0.1:1/db?sslmode=disable", nil)

		require.Error(t, err)
	})

	t.Run("error - malformed dsn", func(t *testing.T) {
		_, err := postgres.NewPool(context.Background(), "://", nil)

		require.Error(t, err)
	})
}

func newTable(t *testing.T) (*postgres.Transactor, func(ctx context.Context, v int) error, func() int) {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, server.DSN(t), nil)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, "CREATE TABLE items (v INT NOT NULL)")
	require.NoError(t, err)

	insert := func(ctx context.Context, v int) error {
		_, err := postgres.Executor(ctx, pool).Exec(ctx, "INSERT INTO items (v) VALUES ($1)", v)
		return err //nolint:wrapcheck // test helper.
	}
	count := func() int {
		var n int
		require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM items").Scan(&n))
		return n
	}
	return postgres.NewTransactor(pool), insert, count
}

func TestTransactor_WithinTransaction(t *testing.T) {
	ctx := context.Background()

	t.Run("success - commit keeps every write", func(t *testing.T) {
		tx, insert, count := newTable(t)

		err := tx.WithinTransaction(ctx, func(txCtx context.Context) error {
			require.NoError(t, insert(txCtx, 1))
			return insert(txCtx, 2)
		})

		require.NoError(t, err)
		assert.Equal(t, 2, count())
	})

	t.Run("error - failure rolls every write back", func(t *testing.T) {
		tx, insert, count := newTable(t)
		boom := errors.New("boom")

		err := tx.WithinTransaction(ctx, func(txCtx context.Context) error {
			require.NoError(t, insert(txCtx, 1))
			return boom
		})

		require.ErrorIs(t, err, boom)
		assert.Zero(t, count())
	})

	t.Run("error - panic rolls back and propagates", func(t *testing.T) {
		tx, insert, count := newTable(t)

		assert.PanicsWithValue(t, "boom", func() {
			err := tx.WithinTransaction(ctx, func(txCtx context.Context) error {
				require.NoError(t, insert(txCtx, 1))
				panic("boom")
			})
			t.Errorf("WithinTransaction returned %v instead of panicking", err)
		})
		assert.Zero(t, count())
	})

	t.Run("success - write with the outer context is not part of the transaction", func(t *testing.T) {
		tx, insert, count := newTable(t)

		err := tx.WithinTransaction(ctx, func(txCtx context.Context) error {
			require.NoError(t, insert(txCtx, 1))
			require.NoError(t, insert(ctx, 2)) // the mistake the Transactor cannot prevent.
			return errors.New("rollback")
		})

		require.Error(t, err)
		assert.Equal(t, 1, count(), "only the write made with the outer context survives")
	})

	t.Run("success - nested call joins the outer transaction", func(t *testing.T) {
		tx, insert, count := newTable(t)

		err := tx.WithinTransaction(ctx, func(txCtx context.Context) error {
			require.NoError(
				t,
				tx.WithinTransaction(txCtx, func(inner context.Context) error { return insert(inner, 1) }),
			)
			return errors.New("rollback")
		})

		require.Error(t, err)
		assert.Zero(t, count(), "the inner block was rolled back with the outer one")
	})
}
