// Package runnerctl serves the runner protocol: one gRPC stream per run between the control plane and the runner
// in the run's sandbox (docs/api-contract-m2.md). The runner dials out with a single-use bootstrap token.
package runnerctl

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// Credential kinds.
const (
	kindBootstrap = "bootstrap"
	kindSession   = "session"
)

// errBadCredential is a token that is unknown, used, expired or of another kind.
var errBadCredential = errors.New("unknown, used or expired runner credential")

func newToken() (string, error) {
	b := make([]byte, config.StateBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Bootstrap makes the single-use token a run's sandbox starts with; it expires after config.BootstrapTTL.
func Bootstrap(ctx context.Context, cluster *db.Cluster, runID uuid.UUID) (string, error) {
	return issue(ctx, cluster, runID, kindBootstrap, time.Now().Add(config.BootstrapTTL))
}

func issue(ctx context.Context, cluster *db.Cluster, runID uuid.UUID, kind string, expires time.Time) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	err = cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `INSERT INTO runner_credential (token_hash, run_id, kind, expires_at) VALUES ($1, $2, $3, $4)`,
			auth.Hash(token), runID, kind, expires)
		return err
	})
	return token, err
}

// redeem uses a credential up, answering the run it is bound to; a session reopens the stream at most once.
func redeem(ctx context.Context, cluster *db.Cluster, token, kind string) (uuid.UUID, error) {
	var runID uuid.UUID
	err := cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `DELETE FROM runner_credential WHERE token_hash = $1 AND kind = $2 AND expires_at > now()
			RETURNING run_id`, auth.Hash(token), kind).Scan(&runID)
	})
	if errors.Is(err, pgx.ErrNoRows) || token == "" {
		return uuid.Nil, errBadCredential
	}
	return runID, err
}

// Resume hands a waiting run the answer to its interrupt: the worker starts a sandbox for it, whose StartRun
// resumes from the last checkpoint. Approvals in the browser (M5) call it; until then tests do.
func Resume(ctx context.Context, cluster *db.Cluster, runID uuid.UUID, answer []byte) error {
	return cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `UPDATE run SET resume = $2, status = 'starting', attempts = 0, sandbox_id = NULL
			WHERE id = $1 AND status = 'waiting'`, runID, answer)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("run %s is not waiting for an answer", runID)
		}
		return nil
	})
}
