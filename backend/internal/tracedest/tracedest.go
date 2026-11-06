// Package tracedest keeps the OTLP destinations runs' traces are sent to (#246) and sends them there.
// docs/api-contract-m18.md says what is sent where.
package tracedest

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/vault"
)

// Errors a caller tells apart; an *Invalid names the fields an administrator fixes.
var (
	ErrNotFound = errors.New("no such trace destination")
	// ErrNotAcknowledged is content turned on without saying text leaves Rotor for the destination.
	ErrNotAcknowledged = errors.New("sending text needs the acknowledgement that it leaves Rotor")
	// ErrTaken is a name another destination has.
	ErrTaken = errors.New("another trace destination has the name")
	// ErrFull is an add past config.MaxTraceDestinations.
	ErrFull = errors.New("the trace destinations are full")
)

// Destination is a destination as an administrator sees it: header names, never their values.
type Destination struct {
	ID                    uuid.UUID
	Name                  string
	Endpoint              string
	HeaderNames           []string
	HeadersSetAt          *time.Time
	Enabled               bool
	Content               bool
	ContentAcknowledgedBy *string
	ContentAcknowledgedAt *time.Time
	TraceLink             *string
	CarriedOver           bool
	ChangedBy             *string
	UpdatedAt             time.Time
}

// Input adds or changes a destination; nil Headers keeps the ones held and an empty map removes them.
type Input struct {
	Name, Endpoint           string
	Headers                  *map[string]string
	Enabled, Content         bool
	AcknowledgeContentLeaves bool
	TraceLink                *string
}

// Headers is where header values are kept; *vault.Client is the real one.
type Headers interface {
	DestinationHeaders(ctx context.Context, id uuid.UUID) (vault.DestinationHeaders, error)
	PutDestinationHeaders(ctx context.Context, id uuid.UUID, h vault.DestinationHeaders) error
	DeleteDestinationHeaders(ctx context.Context, id uuid.UUID) error
}

// Service keeps the destinations and what this replica last read of them to send to.
type Service struct {
	db      *db.Cluster
	headers Headers
	log     *slog.Logger
	now     func() time.Time

	mu        sync.Mutex
	targets   []target
	read      time.Time
	exporters map[uuid.UUID]*exporter
}

// NewService returns the destinations kept in cluster, their headers in headers.
func NewService(cluster *db.Cluster, headers Headers, log *slog.Logger) *Service {
	return &Service{db: cluster, headers: headers, log: log, now: time.Now, exporters: map[uuid.UUID]*exporter{}}
}

const columns = `d.id, d.name, d.endpoint, d.header_names, d.headers_set_at, d.enabled, d.content,
	(SELECT u.name FROM app_user u WHERE u.id = d.content_acknowledged_by), d.content_acknowledged_at, d.trace_link,
	d.carried_over, (SELECT u.name FROM app_user u WHERE u.id = d.changed_by), d.updated_at`

func scan(row pgx.Row) (Destination, error) {
	var d Destination
	err := row.Scan(&d.ID, &d.Name, &d.Endpoint, &d.HeaderNames, &d.HeadersSetAt, &d.Enabled, &d.Content,
		&d.ContentAcknowledgedBy, &d.ContentAcknowledgedAt, &d.TraceLink, &d.CarriedOver, &d.ChangedBy, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrNotFound
	}
	if d.HeaderNames == nil {
		d.HeaderNames = []string{}
	}
	return d, err
}

// List is every destination by name, for an administrator; the database's policy shows them to nobody else.
func (s *Service) List(ctx context.Context) ([]Destination, error) {
	out := []Destination{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT `+columns+` FROM trace_destination d ORDER BY lower(d.name)`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			d, err := scan(rows)
			if err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	return out, err
}

// Get is one destination, for an administrator.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Destination, error) {
	var d Destination
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		d, err = scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM trace_destination d WHERE d.id = $1`, id))
		return err
	})
	return d, err
}

// Add keeps a new destination and its headers, as the administrator on the context; the database refuses anyone else.
func (s *Service) Add(ctx context.Context, in Input) (Destination, error) {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return Destination{}, auth.ErrNoSession
	}
	in, headers, err := check(in)
	if err != nil {
		return Destination{}, err
	}
	if in.Content && !in.AcknowledgeContentLeaves {
		return Destination{}, ErrNotAcknowledged
	}
	var id uuid.UUID
	err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `INSERT INTO trace_destination (name, endpoint, enabled, content, content_acknowledged_by,
			content_acknowledged_at, trace_link, changed_by) VALUES ($1, $2, $3, $4, CASE WHEN $4 THEN $5::uuid END,
			CASE WHEN $4 THEN now() END, $6, $5) RETURNING id`,
			in.Name, in.Endpoint, in.Enabled, in.Content, p.UserID, in.TraceLink).Scan(&id)
		if err != nil {
			return refusal(err)
		}
		if err := s.writeHeaders(ctx, tx, id, headers); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionTraceDestinationAdded, TargetType: "trace_destination",
			TargetID: id.String(), Actor: p.UserID, Data: map[string]any{"name": in.Name, "endpoint": in.Endpoint,
				"headers": names(headers), "enabled": in.Enabled, "content": in.Content, "acknowledged": in.AcknowledgeContentLeaves}})
	})
	if err != nil {
		return Destination{}, err
	}
	s.forget()
	return s.Get(ctx, id)
}

// Change replaces a destination's settings, and its headers when given. Text needs the acknowledgement again when
// content is turned on or its endpoint moves.
func (s *Service) Change(ctx context.Context, id uuid.UUID, in Input) (Destination, error) {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return Destination{}, auth.ErrNoSession
	}
	in, headers, err := check(in)
	if err != nil {
		return Destination{}, err
	}
	current, err := s.Get(ctx, id)
	if err != nil {
		return Destination{}, err
	}
	fresh := in.Content && (!current.Content || current.Endpoint != in.Endpoint || in.AcknowledgeContentLeaves)
	if fresh && !in.AcknowledgeContentLeaves {
		return Destination{}, ErrNotAcknowledged
	}
	err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `UPDATE trace_destination SET name = $2, endpoint = $3, enabled = $4, content = $5,
			content_acknowledged_by = CASE WHEN NOT $5 THEN NULL WHEN $6 THEN $7::uuid ELSE content_acknowledged_by END,
			content_acknowledged_at = CASE WHEN NOT $5 THEN NULL WHEN $6 THEN now() ELSE content_acknowledged_at END,
			trace_link = $8, changed_by = $7 WHERE id = $1`,
			id, in.Name, in.Endpoint, in.Enabled, in.Content, fresh, p.UserID, in.TraceLink)
		if err != nil {
			return refusal(err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		change := "kept"
		if headers != nil {
			if err := s.writeHeaders(ctx, tx, id, headers); err != nil {
				return err
			}
			change = "set"
			if len(headers) == 0 {
				change = "removed"
			}
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionTraceDestinationChanged, TargetType: "trace_destination",
			TargetID: id.String(), Actor: p.UserID, Data: map[string]any{"name": in.Name, "endpoint": in.Endpoint,
				"headers": change, "enabled": in.Enabled, "content": in.Content, "acknowledged": fresh}})
	})
	if err != nil {
		return Destination{}, err
	}
	s.forget()
	return s.Get(ctx, id)
}

// Remove forgets a destination and its headers.
func (s *Service) Remove(ctx context.Context, id uuid.UUID) error {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return auth.ErrNoSession
	}
	err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var name string
		if err := tx.QueryRow(ctx, `DELETE FROM trace_destination WHERE id = $1 RETURNING name`, id).Scan(&name); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionTraceDestinationRemoved, TargetType: "trace_destination",
			TargetID: id.String(), Actor: p.UserID, Data: map[string]any{"name": name}})
	})
	if err != nil {
		return err
	}
	s.forget()
	return s.headers.DeleteDestinationHeaders(context.WithoutCancel(ctx), id)
}

// writeHeaders puts the values in OpenBao before the row names them, so a row never names headers OpenBao lacks;
// an empty map removes them.
func (s *Service) writeHeaders(ctx context.Context, tx db.DBTX, id uuid.UUID, headers map[string]string) error {
	if headers == nil {
		return nil
	}
	if len(headers) == 0 {
		if err := s.headers.DeleteDestinationHeaders(ctx, id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE trace_destination SET header_names = '{}', headers_set_at = NULL WHERE id = $1`, id)
		return err
	}
	now := s.now().UTC()
	if err := s.headers.PutDestinationHeaders(ctx, id, vault.DestinationHeaders{Headers: headers, SetAt: now}); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE trace_destination SET header_names = $2, headers_set_at = $3 WHERE id = $1`,
		id, names(headers), now)
	return err
}

// names are the header names in order, what the row and the audit log keep of them.
func names(headers map[string]string) []string {
	out := make([]string, 0, len(headers))
	for k := range headers {
		out = append(out, k)
	}
	slices.SortFunc(out, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	return out
}

// refusal names what the database refused: a name taken, or a destination past the cap.
func refusal(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.ConstraintName {
		case "trace_destination_name_unique":
			return ErrTaken
		case "trace_destination_cap":
			return ErrFull
		}
	}
	return err
}
