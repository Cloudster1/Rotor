package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/triggers"
	"github.com/cloudster1/rotor/backend/internal/vault"
)

// What the service refuses; the HTTP layer says each in a sentence.
var (
	// ErrNotFound is a tool Rotor does not know, or one whose deliveries it takes no secret for.
	ErrNotFound = errors.New("no such hook")
	// ErrNotProduct is a tool that is not Armature, Exciter or Stator, whose deliveries Rotor cannot read.
	ErrNotProduct = errors.New("the tool sends no events Rotor reads")
	// ErrBadSecret is a secret that is not the tool's shape.
	ErrBadSecret = errors.New("that is not the tool's webhook secret")
	// ErrBadSignature is a delivery whose signature is missing, wrong or outside the replay window.
	ErrBadSignature = errors.New("the delivery's signature does not hold")
)

// Secrets is where the tools' webhook secrets live; *vault.Client is the real one.
type Secrets interface {
	WebhookSecret(ctx context.Context, toolID uuid.UUID) (vault.WebhookSecret, int, error)
	PutWebhookSecret(ctx context.Context, toolID uuid.UUID, s vault.WebhookSecret, version int) (int, error)
	DeleteWebhookSecret(ctx context.Context, toolID uuid.UUID) error
}

// Hook is a tool's webhook as its administrator sees it, never with the secret.
type Hook struct {
	ToolID         uuid.UUID
	Product        string
	SecretSetAt    *time.Time
	LastReceivedAt *time.Time
}

// Outcome is what became of a delivery that was let in.
type Outcome int

// Outcomes of a delivery.
const (
	Accepted Outcome = iota
	Duplicate
	Ping
)

// Service keeps the tools' webhook secrets and takes their deliveries.
type Service struct {
	db    *db.Cluster
	vault Secrets
	// Now is the clock the replay window is measured on.
	Now func() time.Time

	mu    sync.Mutex
	cache map[uuid.UUID]cached
}

type cached struct {
	product, secret string
	at              time.Time
}

// NewService returns the hooks.
func NewService(cluster *db.Cluster, secrets Secrets) *Service {
	return &Service{db: cluster, vault: secrets, Now: time.Now, cache: map[uuid.UUID]cached{}}
}

// Get is a tool's hook; a tool that is not one of the three products answers ErrNotProduct.
func (s *Service) Get(ctx context.Context, toolID uuid.UUID) (Hook, error) {
	h := Hook{ToolID: toolID}
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT name, hook_secret_set_at, hook_last_received_at FROM tool WHERE id = $1`, toolID).
			Scan(&h.Product, &h.SecretSetAt, &h.LastReceivedAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Hook{}, ErrNotFound
	}
	if err != nil {
		return Hook{}, err
	}
	if _, ok := config.HookProducts[h.Product]; !ok {
		return h, ErrNotProduct
	}
	return h, nil
}

// SetSecret keeps the secret the tool made for Rotor's webhook in OpenBao, after checking it is the tool's shape.
func (s *Service) SetSecret(ctx context.Context, toolID uuid.UUID, secret string) (Hook, error) {
	h, err := s.Get(ctx, toolID)
	if err != nil {
		return Hook{}, err
	}
	secret = strings.TrimSpace(secret)
	if !strings.HasPrefix(secret, h.Product+config.HookSecretSuffix) || len(secret) <= len(h.Product+config.HookSecretSuffix) ||
		len(secret) > config.MaxHookSecretChars {
		return Hook{}, ErrBadSecret
	}
	_, version, err := s.vault.WebhookSecret(ctx, toolID)
	if err != nil && !errors.Is(err, vault.ErrNotFound) {
		return Hook{}, err
	}
	now := time.Now().UTC()
	if _, err := s.vault.PutWebhookSecret(ctx, toolID, vault.WebhookSecret{Secret: secret, SetAt: now}, version); err != nil {
		return Hook{}, err
	}
	if err := s.record(ctx, toolID, &now, "set"); err != nil {
		return Hook{}, err
	}
	return s.Get(ctx, toolID)
}

// RemoveSecret forgets the tool's webhook secret; its deliveries are refused from then on.
func (s *Service) RemoveSecret(ctx context.Context, toolID uuid.UUID) (Hook, error) {
	if _, err := s.Get(ctx, toolID); err != nil && !errors.Is(err, ErrNotProduct) {
		return Hook{}, err
	}
	if err := s.vault.DeleteWebhookSecret(ctx, toolID); err != nil {
		return Hook{}, err
	}
	if err := s.record(ctx, toolID, nil, "removed"); err != nil {
		return Hook{}, err
	}
	return s.Get(ctx, toolID)
}

// record notes when the secret was set, or that it is gone, with an audit entry that never holds it.
func (s *Service) record(ctx context.Context, toolID uuid.UUID, at *time.Time, change string) error {
	s.forget(toolID)
	var actor uuid.UUID
	if p := auth.PrincipalFrom(ctx); p != nil {
		actor = p.UserID
	}
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `UPDATE tool SET hook_secret_set_at = $2 WHERE id = $1`, toolID, at); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionHookSecretChanged, TargetType: "tool", TargetID: toolID.String(),
			Actor: actor, Data: map[string]any{"change": change}})
	})
}

// Receive takes one delivery: it checks the signature within the replay window, reads the envelope, keeps the event's
// id against repeats and queues a firing for each enabled event trigger of the tool it matches.
func (s *Service) Receive(ctx context.Context, toolID uuid.UUID, header http.Header, body []byte) (Outcome, error) {
	product, secret, err := s.secret(ctx, toolID)
	if err != nil {
		return 0, err
	}
	word := config.HookProducts[product]
	if !VerifyTimestamped(body, secret, header.Get("X-"+word+"-Timestamp"), header.Get("X-"+word+"-Signature-Timestamped-256"),
		config.HookReplayWindow, s.Now()) {
		return 0, ErrBadSignature
	}
	event, err := Decode(body, header.Get("X-"+word+"-Webhook-Version"), product)
	if err != nil {
		return 0, err
	}
	if event.Topic == config.HookPingTopic {
		return Ping, nil
	}
	outcome := Accepted
	err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `INSERT INTO hook_event (tool_id, event_id, topic) VALUES ($1, $2, $3)
			ON CONFLICT (tool_id, event_id) DO NOTHING`, toolID, event.ID, event.Topic)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE tool SET hook_last_received_at = now() WHERE id = $1`, toolID); err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			outcome = Duplicate
			return nil
		}
		return queue(ctx, tx, toolID, event, s.Now())
	})
	return outcome, err
}

// queue adds a firing for each enabled event trigger of the tool whose filter the event matches.
func queue(ctx context.Context, tx db.DBTX, toolID uuid.UUID, event Event, now time.Time) error {
	rows, err := tx.Query(ctx, `SELECT id, topics, projects, conditions FROM trigger
		WHERE kind = 'event' AND enabled AND tool_id = $1`, toolID)
	if err != nil {
		return err
	}
	var matched []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		var f Filter
		var conditions []byte
		if err := rows.Scan(&id, &f.Topics, &f.Projects, &conditions); err != nil {
			rows.Close()
			return err
		}
		if json.Unmarshal(conditions, &f.Conditions) == nil && f.Matches(event) {
			matched = append(matched, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range matched {
		if _, err := triggers.Enqueue(ctx, tx, id, triggers.EventKey(event.ID), map[string]any{"event": event}, now); err != nil {
			return err
		}
	}
	return nil
}

// secret is a known tool's product and webhook secret, kept in memory for config.HookSecretCacheTTL, a missing one too.
func (s *Service) secret(ctx context.Context, toolID uuid.UUID) (string, string, error) {
	s.mu.Lock()
	c, ok := s.cache[toolID]
	s.mu.Unlock()
	if !ok || time.Since(c.at) > config.HookSecretCacheTTL {
		c = cached{at: time.Now()}
		h, err := s.Get(ctx, toolID)
		switch {
		case errors.Is(err, ErrNotFound):
			// An unknown id is not kept, so made-up ids cannot grow the cache.
			return "", "", err
		case errors.Is(err, ErrNotProduct):
		case err != nil:
			return "", "", err
		default:
			stored, _, err := s.vault.WebhookSecret(ctx, toolID)
			if err != nil && !errors.Is(err, vault.ErrNotFound) {
				return "", "", err
			}
			c.product, c.secret = h.Product, stored.Secret
		}
		s.mu.Lock()
		s.cache[toolID] = c
		s.mu.Unlock()
	}
	if c.secret == "" {
		return "", "", ErrNotFound
	}
	return c.product, c.secret, nil
}

func (s *Service) forget(toolID uuid.UUID) {
	s.mu.Lock()
	delete(s.cache, toolID)
	s.mu.Unlock()
}

// Purge forgets event ids older than config.HookEventRetention; the worker calls it now and then.
func (s *Service) Purge(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM hook_event WHERE received_at < now() - make_interval(secs => $1)`,
			config.HookEventRetention.Seconds())
		n = tag.RowsAffected()
		return err
	})
	return n, err
}
