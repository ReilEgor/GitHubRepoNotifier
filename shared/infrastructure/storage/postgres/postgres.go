// Package postgres opens the connection pool and runs transactions for the services that store data in PostgreSQL.
package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const startupTimeout = 5 * time.Second

// Querier is what repositories run their queries on: the pool, or the transaction in the context.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// NewPool opens a pool and proves the database answers with SELECT now(); an unreachable database is an error,
// so the service refuses to start. A nil tracer disables query tracing.
func NewPool(ctx context.Context, dsn string, tracer pgx.QueryTracer) (*pgxpool.Pool, error) {
	const op = "postgres.NewPool"

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("%s: parse config: %w", op, err)
	}
	cfg.ConnConfig.Tracer = tracer

	ctx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("%s: create pool: %w", op, err)
	}
	var now time.Time
	if err := pool.QueryRow(ctx, "SELECT now()").Scan(&now); err != nil {
		pool.Close()
		return nil, fmt.Errorf("%s: check connection: %w", op, err)
	}
	return pool, nil
}

type txKey struct{}

// Executor returns the transaction carried by ctx, or the pool when there is none.
func Executor(ctx context.Context, pool *pgxpool.Pool) Querier {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return pool
}

// Transactor runs functions in database transactions.
type Transactor struct {
	pool *pgxpool.Pool
}

// NewTransactor returns a Transactor on pool.
func NewTransactor(pool *pgxpool.Pool) *Transactor {
	return &Transactor{pool: pool}
}

// WithinTransaction commits when fn returns nil and rolls back on an error or a panic.
// A nested call joins the transaction already carried by ctx.
func (t *Transactor) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	const op = "Transactor.WithinTransaction"

	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}

	tx, err := t.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%s: begin: %w", op, err)
	}
	defer func() {
		if r := recover(); r != nil {
			t.rollback(ctx, tx)
			panic(r)
		}
	}()

	if err := fn(context.WithValue(ctx, txKey{}, tx)); err != nil {
		t.rollback(ctx, tx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%s: commit: %w", op, err)
	}
	return nil
}

// rollback uses a context that survives cancellation of ctx, so a canceled request still releases its transaction.
func (t *Transactor) rollback(ctx context.Context, tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), startupTimeout)
	defer cancel()
	if err := tx.Rollback(ctx); err != nil {
		slog.WarnContext(ctx, "transaction rollback failed", slog.String("error", err.Error()))
	}
}
