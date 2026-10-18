// Package providers keeps the language model providers workflows may use: which models each offers, the one the
// installation defaults to, and each key, which lives in OpenBao and reaches a run's sandbox alone.
package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/vault"
)

// Kinds of provider, as init_chat_model builds them in the runner.
const (
	KindAnthropic        = "anthropic"
	KindOpenAI           = "openai"
	KindAzureOpenAI      = "azure_openai"
	KindOpenAICompatible = "openai_compatible"
)

// Errors a caller tells apart; a *Refusal is input an administrator fixes.
var (
	ErrNotFound = errors.New("no such provider")
	ErrNoModel  = errors.New("no model is allowed for this workflow")
)

// Refusal is a provider Rotor will not keep, with the sentence an administrator reads.
type Refusal struct{ Message string }

func (r *Refusal) Error() string { return r.Message }

// Provider is a provider as administrators see it; HasKey says whether OpenBao holds its key.
type Provider struct {
	ID           uuid.UUID
	Name         string
	Kind         string
	BaseURL      string
	APIVersion   string
	Models       []string
	DefaultModel string
	IsDefault    bool
	Enabled      bool
	HasKey       bool
}

// Input adds or changes a provider; an empty APIKey keeps the one held.
type Input struct {
	Name, Kind, BaseURL, APIVersion, DefaultModel, APIKey string
	Models                                                []string
	IsDefault, Enabled                                    bool
}

// Keys is what providers keep in OpenBao; *vault.Client is the real one.
type Keys interface {
	ProviderKey(ctx context.Context, id uuid.UUID) (vault.ProviderKey, int, error)
	PutProviderKey(ctx context.Context, id uuid.UUID, k vault.ProviderKey, version int) (int, error)
	DeleteProviderKey(ctx context.Context, id uuid.UUID) error
}

// Service is the providers.
type Service struct {
	db   *db.Cluster
	keys Keys
	http *http.Client
}

// NewService returns the providers; client reaches them for the connection test.
func NewService(cluster *db.Cluster, keys Keys, client *http.Client) *Service {
	return &Service{db: cluster, keys: keys, http: client}
}

const columns = `id, name, kind, base_url, api_version, models, default_model, is_default, enabled`

func scan(row pgx.Row) (Provider, error) {
	var p Provider
	err := row.Scan(&p.ID, &p.Name, &p.Kind, &p.BaseURL, &p.APIVersion, &p.Models, &p.DefaultModel, &p.IsDefault, &p.Enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// List is every provider, with whether OpenBao holds its key; for administrators.
func (s *Service) List(ctx context.Context) ([]Provider, error) {
	out, err := s.query(ctx, s.db.Read, `SELECT `+columns+` FROM provider ORDER BY name`)
	if err != nil {
		return nil, err
	}
	for i := range out {
		_, _, err := s.keys.ProviderKey(ctx, out[i].ID)
		out[i].HasKey = err == nil
	}
	return out, nil
}

func (s *Service) query(ctx context.Context, read func(context.Context, func(context.Context, db.DBTX) error) error, sql string, args ...any) ([]Provider, error) {
	out := []Provider{}
	err := read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, sql, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			p, err := scan(rows)
			if err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

func check(in Input) error {
	switch in.Kind {
	case KindAnthropic, KindOpenAI:
	case KindAzureOpenAI:
		if in.BaseURL == "" || in.APIVersion == "" {
			return &Refusal{"An Azure OpenAI provider needs its endpoint as the base URL and an API version, such as 2024-10-21."}
		}
	case KindOpenAICompatible:
		if in.BaseURL == "" {
			return &Refusal{"An OpenAI-compatible provider needs its base URL, such as http://ollama:11434/v1."}
		}
	default:
		return &Refusal{fmt.Sprintf("%q is not a kind Rotor knows; choose anthropic, openai, azure_openai or openai_compatible.", in.Kind)}
	}
	if strings.TrimSpace(in.Name) == "" || len(in.Models) == 0 {
		return &Refusal{"Name the provider and list at least one model workflows may use."}
	}
	if in.DefaultModel != "" && !slices.Contains(in.Models, in.DefaultModel) {
		return &Refusal{fmt.Sprintf("The default model %s is not in the provider's models.", in.DefaultModel)}
	}
	if in.IsDefault && in.DefaultModel == "" {
		return &Refusal{"Name the default model of the installation's default provider."}
	}
	return nil
}

// Add keeps a new provider and its key, as the administrator on the context; the database refuses anyone else.
func (s *Service) Add(ctx context.Context, in Input) (Provider, error) {
	if err := check(in); err != nil {
		return Provider{}, err
	}
	var p Provider
	err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if in.IsDefault {
			if _, err := tx.Exec(ctx, `UPDATE provider SET is_default = false WHERE is_default`); err != nil {
				return err
			}
		}
		var err error
		p, err = scan(tx.QueryRow(ctx, `INSERT INTO provider (name, kind, base_url, api_version, models, default_model, is_default, enabled)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING `+columns,
			in.Name, in.Kind, strings.TrimSuffix(in.BaseURL, "/"), in.APIVersion, in.Models, in.DefaultModel, in.IsDefault, in.Enabled))
		if err != nil {
			return err
		}
		return s.changed(ctx, tx, p, "added")
	})
	if err != nil {
		return Provider{}, err
	}
	return p, s.keep(ctx, p.ID, in.APIKey, &p)
}

// Change replaces a provider's settings, and its key when one is given.
func (s *Service) Change(ctx context.Context, id uuid.UUID, in Input) (Provider, error) {
	if err := check(in); err != nil {
		return Provider{}, err
	}
	var p Provider
	err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if in.IsDefault {
			if _, err := tx.Exec(ctx, `UPDATE provider SET is_default = false WHERE is_default AND id <> $1`, id); err != nil {
				return err
			}
		}
		var err error
		p, err = scan(tx.QueryRow(ctx, `UPDATE provider SET name = $2, kind = $3, base_url = $4, api_version = $5, models = $6,
			default_model = $7, is_default = $8, enabled = $9 WHERE id = $1 RETURNING `+columns,
			id, in.Name, in.Kind, strings.TrimSuffix(in.BaseURL, "/"), in.APIVersion, in.Models, in.DefaultModel, in.IsDefault, in.Enabled))
		if err != nil {
			return err
		}
		return s.changed(ctx, tx, p, "changed")
	})
	if err != nil {
		return Provider{}, err
	}
	return p, s.keep(ctx, p.ID, in.APIKey, &p)
}

// keep writes a new key over the one held, and reads whether one is held.
func (s *Service) keep(ctx context.Context, id uuid.UUID, key string, p *Provider) error {
	if key != "" {
		_, version, err := s.keys.ProviderKey(ctx, id)
		if err != nil && !errors.Is(err, vault.ErrNotFound) {
			return err
		}
		if _, err := s.keys.PutProviderKey(ctx, id, vault.ProviderKey{Key: key}, version); err != nil {
			return err
		}
	}
	_, _, err := s.keys.ProviderKey(ctx, id)
	p.HasKey = err == nil
	return nil
}

// Remove forgets a provider and its key.
func (s *Service) Remove(ctx context.Context, id uuid.UUID) error {
	err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := scan(tx.QueryRow(ctx, `DELETE FROM provider WHERE id = $1 RETURNING `+columns, id))
		if err != nil {
			return err
		}
		return s.changed(ctx, tx, p, "removed")
	})
	if err != nil {
		return err
	}
	return s.keys.DeleteProviderKey(context.WithoutCancel(ctx), id)
}

func (s *Service) changed(ctx context.Context, tx db.DBTX, p Provider, what string) error {
	actor := uuid.Nil
	if principal := auth.PrincipalFrom(ctx); principal != nil {
		actor = principal.UserID
	}
	return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionProviderChanged, TargetType: "provider", TargetID: p.ID.String(), Actor: actor,
		Data: map[string]any{"what": what, "name": p.Name, "kind": p.Kind, "models": p.Models, "default": p.IsDefault}})
}
