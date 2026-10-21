package oauth

import (
	"context"
	"sync"
	"time"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// Sweeper removes requests, codes and tokens that ran out, grants left with neither, and clients nobody holds.
type Sweeper struct {
	db   *db.Cluster
	now  func() time.Time
	mu   sync.Mutex
	last time.Time
}

// NewSweeper returns the sweep the worker runs on each of its passes.
func NewSweeper(cluster *db.Cluster) *Sweeper { return &Sweeper{db: cluster, now: time.Now} }

// Sweep sweeps at most once every config.OAuthSweepEvery, as the admin role, since nobody is signed in to these rows.
func (s *Sweeper) Sweep(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.last.IsZero() && s.now().Sub(s.last) < config.OAuthSweepEvery {
		return nil
	}
	s.last = s.now()
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		sweeps := []struct {
			sql  string
			args []any
		}{
			{`DELETE FROM oauth_request WHERE expires_at <= now()`, nil},
			{`DELETE FROM oauth_code WHERE expires_at <= now()`, nil},
			{`DELETE FROM oauth_token WHERE expires_at <= now()`, nil},
			// A grant whose code was never traded, or whose last refresh token ran out, can no longer be used.
			{`DELETE FROM oauth_grant g WHERE NOT EXISTS (SELECT 1 FROM oauth_code c WHERE c.grant_id = g.id)
				AND NOT EXISTS (SELECT 1 FROM oauth_token t WHERE t.grant_id = g.id AND t.kind = 'refresh')`, nil},
			// A client that never led to a grant goes after a day, and one whose last grant ended after the refresh TTL.
			{`DELETE FROM oauth_client c WHERE NOT EXISTS (SELECT 1 FROM oauth_grant g WHERE g.client_id = c.id)
				AND NOT EXISTS (SELECT 1 FROM oauth_request r WHERE r.client_id = c.id)
				AND CASE WHEN c.idle_since IS NULL THEN c.created_at < now() - make_interval(secs => $1)
				         ELSE c.idle_since < now() - make_interval(secs => $2) END`,
				[]any{config.OAuthUnusedClientTTL.Seconds(), config.OAuthRefreshTTL.Seconds()}},
		}
		for _, sweep := range sweeps {
			if _, err := tx.Exec(ctx, sweep.sql, sweep.args...); err != nil {
				return err
			}
		}
		return nil
	})
}
