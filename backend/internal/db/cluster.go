// Package db owns the Postgres connections: a pool as rotor_app, which row level
// security binds, and one as rotor_admin, exempt by policy for sign-in and jobs.
package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// DBTX is the query surface shared by pgxpool.Pool and pgx.Tx, so the same
// repository code runs in a transaction or not.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Cluster is the app pool and the admin pool beside it.
type Cluster struct {
	app   *pgxpool.Pool
	admin *pgxpool.Pool
	log   *slog.Logger
}

// Open connects both pools. The caller must Close the returned cluster.
func Open(ctx context.Context, cfg config.DB, log *slog.Logger) (*Cluster, error) {
	app, err := openPool(ctx, cfg, cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("connect to the database: %w", err)
	}
	admin := app
	if cfg.AdminURL != "" && cfg.AdminURL != cfg.URL {
		admin, err = openPool(ctx, cfg, cfg.AdminURL)
		if err != nil {
			app.Close()
			return nil, fmt.Errorf("connect to the database as the admin role: %w", err)
		}
	}
	log.Info("database ready")
	return &Cluster{app: app, admin: admin, log: log}, nil
}

func openPool(ctx context.Context, cfg config.DB, url string) (*pgxpool.Pool, error) {
	pcfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	pcfg.MaxConns = cfg.MaxConns
	pcfg.MinConns = min(cfg.MinConns, cfg.MaxConns)
	pcfg.MaxConnLifetime = cfg.ConnMaxLifetime

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, config.DBPingTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// Close shuts both pools.
func (c *Cluster) Close() {
	if c.admin != c.app {
		c.admin.Close()
	}
	c.app.Close()
}

// ErrPrivilegedRole is returned when the application connects as a role that
// row level security does not apply to.
var ErrPrivilegedRole = errors.New("the database connection uses a role that bypasses row level security; connect as rotor_app instead")

// RefuseSuperuser fails when either pool connects as a role that ignores row
// level security, which would make every policy in the schema decoration.
func (c *Cluster) RefuseSuperuser(ctx context.Context) error {
	for _, pool := range []*pgxpool.Pool{c.app, c.admin} {
		var privileged bool
		err := pool.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = current_user`).Scan(&privileged)
		if err != nil {
			return fmt.Errorf("ask which role this connects as: %w", err)
		}
		if privileged {
			return ErrPrivilegedRole
		}
	}
	return nil
}

// Ping verifies the database is reachable.
func (c *Cluster) Ping(ctx context.Context) error {
	return c.app.Ping(ctx)
}
