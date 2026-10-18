package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// setLocalSQL binds a row level security variable for the current transaction.
// SET LOCAL takes no bind parameter; set_config does, and its true makes it local.
const setLocalSQL = `SELECT set_config($1, $2, true)`

// readOnly keeps one snapshot for a whole read.
var readOnly = pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}

var readWrite = pgx.TxOptions{AccessMode: pgx.ReadWrite}

// Write runs fn in a read-write transaction as rotor_app, acting for the person ctx names.
func (c *Cluster) Write(ctx context.Context, fn func(context.Context, DBTX) error) error {
	return inTx(ctx, c.app, readWrite, fn)
}

// Read runs fn in a read-only transaction as rotor_app, acting for the person ctx names.
func (c *Cluster) Read(ctx context.Context, fn func(context.Context, DBTX) error) error {
	return inTx(ctx, c.app, readOnly, fn)
}

// WriteAdmin runs fn as the role exempt from row level security, for work done
// before anyone is signed in, such as sign-in itself, and for background jobs.
func (c *Cluster) WriteAdmin(ctx context.Context, fn func(context.Context, DBTX) error) error {
	return inTx(ctx, c.admin, readWrite, fn)
}

// ReadAdmin is WriteAdmin read-only.
func (c *Cluster) ReadAdmin(ctx context.Context, fn func(context.Context, DBTX) error) error {
	return inTx(ctx, c.admin, readOnly, fn)
}

// inTx opens a transaction, names the acting person for the policies, runs fn and commits.
func inTx(ctx context.Context, pool *pgxpool.Pool, opts pgx.TxOptions, fn func(context.Context, DBTX) error) error {
	tx, err := pool.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	// Rollback is a no-op once the transaction has committed, so this is safe on every path.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if user, ok := UserFrom(ctx); ok {
		if _, err := tx.Exec(ctx, setLocalSQL, UserVar, user.String()); err != nil {
			return fmt.Errorf("apply the acting person: %w", err)
		}
	}
	if err := fn(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
