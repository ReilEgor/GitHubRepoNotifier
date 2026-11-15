//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/ReilEgor/GitHubRepoNotifier/shared/infrastructure/storage/postgres/pgtest"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tableCount(t *testing.T, dsn string) int {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(ctx)) }()

	var n int
	err = conn.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.tables
		 WHERE table_schema = 'public' AND table_name <> 'schema_migrations'`).Scan(&n)
	require.NoError(t, err)
	return n
}

// TestMigrations_upDownUp proves every migration can be reverted and applied again on an empty database.
func TestMigrations_upDownUp(t *testing.T) {
	dsn := server.DSN(t)
	m, err := pgtest.NewMigrate(dsn, migrationsDir(t))
	require.NoError(t, err)
	t.Cleanup(func() {
		srcErr, dbErr := m.Close()
		assert.NoError(t, srcErr)
		assert.NoError(t, dbErr)
	})

	require.NoError(t, m.Up())
	assert.Equal(t, 5, tableCount(t, dsn), "users, repositories, subscriptions, subscription_sagas, outbox_messages")

	require.NoError(t, m.Down())
	assert.Zero(t, tableCount(t, dsn), "down leaves no table behind")

	require.NoError(t, m.Up())
	assert.Equal(t, 5, tableCount(t, dsn))
}
