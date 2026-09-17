package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type UnitOfWork[R any] struct {
	pool  *pgxpool.Pool
	build func(tx pgx.Tx) R
	opts  pgx.TxOptions
}

func NewUnitOfWork[R any](pool *pgxpool.Pool, build func(tx pgx.Tx) R) *UnitOfWork[R] {
	return &UnitOfWork[R]{
		pool:  pool,
		build: build,
		opts:  pgx.TxOptions{IsoLevel: pgx.ReadCommitted},
	}
}

func (u *UnitOfWork[R]) Do(ctx context.Context, fn func(ctx context.Context, repos R) error) error {
	return InTx(ctx, u.pool, u.opts, func(tx pgx.Tx) error {
		return fn(ctx, u.build(tx))
	})
}

func InTx(ctx context.Context, pool *pgxpool.Pool, opts pgx.TxOptions, fn func(tx pgx.Tx) error) (err error) {
	tx, err := pool.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err == nil {
			return
		}
		if rbErr := tx.Rollback(context.WithoutCancel(ctx)); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			err = errors.Join(err, fmt.Errorf("rollback tx: %w", rbErr))
		}
	}()

	if err = fn(tx); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}
