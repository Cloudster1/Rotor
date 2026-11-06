package ingestkeys

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// Errors a caller tells apart; an *Invalid names the fields an administrator fixes.
var (
	ErrNotFound = errors.New("no such ingest key")
	// ErrTaken is a name another key has, case aside.
	ErrTaken = errors.New("another ingest key has the name")
	// ErrFull is a key past config.MaxIngestKeys.
	ErrFull = errors.New("the ingest keys are full")
	// ErrExpired is a key past its expiry, refused at the endpoint and not rotated.
	ErrExpired = errors.New("the ingest key has expired")
	// ErrUnknown is a key nobody holds at the endpoint: revoked, rotated away or never made.
	ErrUnknown = errors.New("the ingest key is unknown")
	// ErrNotLetIn is a key whose account Rotor no longer lets in.
	ErrNotLetIn = errors.New("the ingest key's account is not let in")
)

// Invalid is input Rotor will not keep, with a sentence per field an administrator fixes.
type Invalid struct{ Fields map[string]string }

func (e *Invalid) Error() string { return "the ingest key needs other values" }

// Sentences an administrator reads beside the field.
var (
	nameSentence    = fmt.Sprintf("Name the key in at most %d characters.", config.MaxIngestKeyNameChars)
	expirySentence  = "Choose a time in the future, or none for a key that lasts until it is revoked."
	accountSentence = "Rotor has let in no account with this email that is switched on. Name one listed under Admin, Users, " +
		"or leave it empty to send as yourself."
)

// Account is who a key sends as.
type Account struct {
	ID          uuid.UUID
	Name, Email string
}

// Key is a key as an administrator sees it: never the key itself, only its last characters.
type Key struct {
	ID         uuid.UUID
	Name       string
	Account    Account
	Hint       string
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
	CreatedBy  *string
	CreatedAt  time.Time
	RotatedAt  *time.Time
	createdBy  *uuid.UUID
}

// Expired says whether the key is refused for its age at now.
func (k Key) Expired(now time.Time) bool { return k.ExpiresAt != nil && !now.Before(*k.ExpiresAt) }

// Input makes a key; an empty AccountEmail sends as the administrator making it.
type Input struct {
	Name, AccountEmail string
	ExpiresAt          *time.Time
}

// Sending is who a key sends spans as.
type Sending struct {
	KeyID     uuid.UUID
	Principal *auth.Principal
}

// Service keeps the keys.
type Service struct {
	db  *db.Cluster
	now func() time.Time
}

// NewService returns the keys kept in cluster.
func NewService(cluster *db.Cluster) *Service {
	return &Service{db: cluster, now: time.Now}
}

const columns = `id, name, account_id, hint, expires_at, last_used_at, created_by, created_at, rotated_at`

func scan(row pgx.Row) (Key, error) {
	var k Key
	err := row.Scan(&k.ID, &k.Name, &k.Account.ID, &k.Hint, &k.ExpiresAt, &k.LastUsedAt, &k.createdBy, &k.CreatedAt, &k.RotatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return k, ErrNotFound
	}
	return k, err
}

// List is every key by name, for an administrator; the database's policy shows them to nobody else.
func (s *Service) List(ctx context.Context) ([]Key, error) {
	out := []Key{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT `+columns+` FROM ingest_key ORDER BY lower(name)`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			k, err := scan(rows)
			if err != nil {
				return err
			}
			out = append(out, k)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, s.name(ctx, out)
}

// Get is one key, for an administrator.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Key, error) {
	var k Key
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		k, err = scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM ingest_key WHERE id = $1`, id))
		return err
	})
	if err != nil {
		return k, err
	}
	keys := []Key{k}
	err = s.name(ctx, keys)
	return keys[0], err
}

// name fills in whom each key sends as and who made it; the people's rows are read past their policy, which shows
// a person only themselves, once the keys' own policy let the administrator see the keys.
func (s *Service) name(ctx context.Context, keys []Key) error {
	ids := make([]uuid.UUID, 0, 2*len(keys))
	for _, k := range keys {
		ids = append(ids, k.Account.ID)
		if k.createdBy != nil {
			ids = append(ids, *k.createdBy)
		}
	}
	people := map[uuid.UUID]Account{}
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT id, name, email FROM app_user WHERE id = ANY($1)`, ids)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a Account
			if err := rows.Scan(&a.ID, &a.Name, &a.Email); err != nil {
				return err
			}
			people[a.ID] = a
		}
		return rows.Err()
	})
	for i, k := range keys {
		keys[i].Account = people[k.Account.ID]
		if k.createdBy != nil {
			if who, ok := people[*k.createdBy]; ok {
				keys[i].CreatedBy = &who.Name
			}
		}
	}
	return err
}

// Create makes a key as the administrator on the context and answers it with its secret, which nothing keeps.
func (s *Service) Create(ctx context.Context, in Input) (Key, string, error) {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return Key{}, "", auth.ErrNoSession
	}
	in, err := check(in, s.now())
	if err != nil {
		return Key{}, "", err
	}
	account, err := s.account(ctx, in.AccountEmail, p.UserID)
	if err != nil {
		return Key{}, "", err
	}
	secret, err := New()
	if err != nil {
		return Key{}, "", err
	}
	var id uuid.UUID
	err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `INSERT INTO ingest_key (name, account_id, secret_hash, hint, expires_at, created_by)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			in.Name, account.ID, secret.Hash, secret.Hint, in.ExpiresAt, p.UserID).Scan(&id)
		if err != nil {
			return refusal(err)
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionIngestKeyCreated, TargetType: "ingest_key",
			TargetID: id.String(), Actor: p.UserID,
			Data: map[string]any{"name": in.Name, "account": account.Email, "expiresAt": in.ExpiresAt}})
	})
	if err != nil {
		return Key{}, "", err
	}
	k, err := s.Get(ctx, id)
	return k, secret.Value, err
}

// Rotate gives a key a new secret, refusing the old one from the next request; the name, account and expiry stay.
func (s *Service) Rotate(ctx context.Context, id uuid.UUID) (Key, string, error) {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return Key{}, "", auth.ErrNoSession
	}
	current, err := s.Get(ctx, id)
	if err != nil {
		return Key{}, "", err
	}
	if current.Expired(s.now()) {
		return Key{}, "", ErrExpired
	}
	secret, err := New()
	if err != nil {
		return Key{}, "", err
	}
	err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `UPDATE ingest_key SET secret_hash = $2, hint = $3, rotated_at = now(), last_used_at = NULL
			WHERE id = $1`, id, secret.Hash, secret.Hint)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionIngestKeyRotated, TargetType: "ingest_key",
			TargetID: id.String(), Actor: p.UserID, Data: map[string]any{"name": current.Name, "account": current.Account.Email}})
	})
	if err != nil {
		return Key{}, "", err
	}
	k, err := s.Get(ctx, id)
	return k, secret.Value, err
}

// Revoke forgets a key, so it is refused from the next request; the audit log keeps its story.
func (s *Service) Revoke(ctx context.Context, id uuid.UUID) error {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return auth.ErrNoSession
	}
	current, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM ingest_key WHERE id = $1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionIngestKeyRevoked, TargetType: "ingest_key",
			TargetID: id.String(), Actor: p.UserID, Data: map[string]any{"name": current.Name, "account": current.Account.Email}})
	})
}

// Authenticate reads whom a key sends as, past the policies since nobody is signed in yet, and notes its use at most
// once in config.IngestKeyLastUsedEvery.
func (s *Service) Authenticate(ctx context.Context, credential string) (Sending, error) {
	if !WellFormed(credential) {
		return Sending{}, ErrUnknown
	}
	var (
		out      = Sending{Principal: &auth.Principal{}}
		p        = out.Principal
		expired  bool
		lastUsed *time.Time
		active   bool
		letIn    bool
	)
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT k.id, k.expires_at IS NOT NULL AND k.expires_at <= now(), k.last_used_at,
				u.id, u.email, u.name, u.groups, u.is_active, u.standing IS NOT NULL
			FROM ingest_key k JOIN app_user u ON u.id = k.account_id WHERE k.secret_hash = $1`, Hash(credential)).Scan(
			&out.KeyID, &expired, &lastUsed, &p.UserID, &p.Email, &p.Name, &p.Groups, &active, &letIn)
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Sending{}, ErrUnknown
	case err != nil:
		return Sending{}, err
	case expired:
		return Sending{}, ErrExpired
	case !letIn:
		return Sending{}, ErrNotLetIn
	case !active:
		return Sending{}, auth.ErrSwitchedOff
	}
	if lastUsed == nil || s.now().Sub(*lastUsed) > config.IngestKeyLastUsedEvery {
		// A note that fails costs a stale time on the list, never the spans.
		_ = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `UPDATE ingest_key SET last_used_at = now() WHERE id = $1`, out.KeyID)
			return err
		})
	}
	return out, nil
}

// account is whom a new key sends as: the account Rotor let in with that email and has not switched off, or the
// administrator's own when none is named.
func (s *Service) account(ctx context.Context, email string, admin uuid.UUID) (Account, error) {
	var (
		a      Account
		active bool
	)
	where, arg := `id = $1`, any(admin)
	if email != "" {
		where, arg = `email = $1`, email
	}
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT id, name, email, is_active FROM app_user WHERE standing IS NOT NULL AND `+where, arg).
			Scan(&a.ID, &a.Name, &a.Email, &active)
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !active) {
		return Account{}, &Invalid{Fields: map[string]string{"accountEmail": accountSentence}}
	}
	return a, err
}

// check trims what an administrator typed and refuses what Rotor will not keep.
func check(in Input, now time.Time) (Input, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.AccountEmail = strings.TrimSpace(in.AccountEmail)
	fields := map[string]string{}
	if in.Name == "" || utf8.RuneCountInString(in.Name) > config.MaxIngestKeyNameChars {
		fields["name"] = nameSentence
	}
	if in.ExpiresAt != nil && !in.ExpiresAt.After(now) {
		fields["expiresAt"] = expirySentence
	}
	if len(fields) > 0 {
		return in, &Invalid{Fields: fields}
	}
	return in, nil
}

// refusal names what the database refused: a name taken, or a key past the cap.
func refusal(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.ConstraintName {
		case "ingest_key_name_unique":
			return ErrTaken
		case "ingest_key_cap":
			return ErrFull
		}
	}
	return err
}
