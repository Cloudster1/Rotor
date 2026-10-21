package audit

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/keyset"
)

// The read side of the log, for administrators (#36); adapted from Exciter's audit service (c0d072d).

// ErrExportTooLarge is a filter that matches more than one export may hold.
var ErrExportTooLarge = errors.New("the export would hold more entries than one file may")

// Record is one entry as an administrator reads it; ActorName is empty for Rotor itself.
type Record struct {
	ID         uuid.UUID       `json:"id"`
	Action     string          `json:"action"`
	TargetType string          `json:"targetType"`
	TargetID   string          `json:"targetId"`
	ActorID    *uuid.UUID      `json:"actorId"`
	ActorName  string          `json:"actorName"`
	Data       json.RawMessage `json:"data"`
	IP         string          `json:"ip"`
	RequestID  string          `json:"requestId"`
	CreatedAt  time.Time       `json:"createdAt"`
	// Via names the assistant the actor acted through, empty when they acted in the browser (#47).
	Via string `json:"via"`
}

// Actor is somebody who acted, by id and name.
type Actor struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// Facets are what the log holds to narrow it by.
type Facets struct {
	Actions     []string `json:"actions"`
	Actors      []Actor  `json:"actors"`
	TargetTypes []string `json:"targetTypes"`
}

// Filter narrows the log; every part is optional. From is inclusive, To is not.
type Filter struct {
	Action     string
	ActorID    *uuid.UUID
	TargetType string
	TargetID   string
	From       *time.Time
	To         *time.Time
}

// Describe is the filter as an export's own entry keeps it.
func (f Filter) Describe() map[string]any {
	out := map[string]any{}
	if f.Action != "" {
		out["action"] = f.Action
	}
	if f.ActorID != nil {
		out["actorId"] = f.ActorID.String()
	}
	if f.TargetType != "" {
		out["targetType"] = f.TargetType
	}
	if f.TargetID != "" {
		out["targetId"] = f.TargetID
	}
	if f.From != nil {
		out["from"] = f.From.UTC().Format(time.RFC3339)
	}
	if f.To != nil {
		out["to"] = f.To.UTC().Format(time.RFC3339)
	}
	return out
}

// where is the filter as SQL over audit_log a, its arguments numbered from 1.
func (f Filter) where() (string, []any) {
	var clauses []string
	var args []any
	add := func(clause string, v any) {
		args = append(args, v)
		clauses = append(clauses, fmt.Sprintf(clause, len(args)))
	}
	if f.Action != "" {
		add("a.action = $%d", f.Action)
	}
	if f.ActorID != nil {
		add("a.actor_user_id = $%d", *f.ActorID)
	}
	if f.TargetType != "" {
		add("a.target_type = $%d", f.TargetType)
	}
	if f.TargetID != "" {
		add("a.target_id = $%d", f.TargetID)
	}
	if f.From != nil {
		add("a.created_at >= $%d", *f.From)
	}
	if f.To != nil {
		add("a.created_at < $%d", *f.To)
	}
	if len(clauses) == 0 {
		return "true", nil
	}
	return strings.Join(clauses, " AND "), args
}

// Service reads the log as the person on the context; the database shows it to administrators alone.
type Service struct {
	db *db.Cluster
}

// NewService returns the log's read side.
func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

const selectRow = `SELECT a.id, a.action, a.target_type, COALESCE(a.target_id, ''), a.actor_user_id, COALESCE(u.name, ''),
	COALESCE(a.data, '{}'::jsonb), COALESCE(host(a.ip), ''), COALESCE(a.request_id, ''), a.created_at,
	COALESCE(a.via_client, '')
FROM audit_log a LEFT JOIN app_user u ON u.id = a.actor_user_id`

func scanRow(row pgx.Row) (Record, error) {
	var r Record
	err := row.Scan(&r.ID, &r.Action, &r.TargetType, &r.TargetID, &r.ActorID, &r.ActorName, &r.Data, &r.IP, &r.RequestID, &r.CreatedAt, &r.Via)
	return r, err
}

// List reads one window of the log, newest first, after the cursor, and the cursor of the next window, nil at the end.
func (s *Service) List(ctx context.Context, f Filter, after *keyset.Cursor, limit int) ([]Record, *string, error) {
	if limit <= 0 || limit > config.AuditMaxPage {
		limit = config.AuditPageSize
	}
	where, args := f.where()
	if after != nil {
		args = append(args, after.At, after.ID)
		where += fmt.Sprintf(" AND (a.created_at, a.id) < ($%d, $%d)", len(args)-1, len(args))
	}
	args = append(args, limit+1)
	out, err := s.read(ctx, where, args)
	if err != nil {
		return nil, nil, err
	}
	var next *string
	if len(out) > limit {
		last := out[limit-1]
		next = keyset.Next(limit, len(out), keyset.Cursor{At: last.CreatedAt, ID: last.ID})
		out = out[:limit]
	}
	return out, next, nil
}

func (s *Service) read(ctx context.Context, where string, args []any) ([]Record, error) {
	out := []Record{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectRow+` WHERE `+where+fmt.Sprintf(` ORDER BY a.created_at DESC, a.id DESC LIMIT $%d`, len(args)), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanRow(rows)
			if err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// Facets reads which actions, people and kinds of target the log holds.
func (s *Service) Facets(ctx context.Context) (Facets, error) {
	out := Facets{Actions: []string{}, Actors: []Actor{}, TargetTypes: []string{}}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		strs := func(sql string, into *[]string) error {
			rows, err := tx.Query(ctx, sql)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var v string
				if err := rows.Scan(&v); err != nil {
					return err
				}
				*into = append(*into, v)
			}
			return rows.Err()
		}
		if err := strs(`SELECT DISTINCT action FROM audit_log ORDER BY action`, &out.Actions); err != nil {
			return err
		}
		if err := strs(`SELECT DISTINCT target_type FROM audit_log ORDER BY target_type`, &out.TargetTypes); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT a.actor_user_id, COALESCE(u.name, '')
			FROM (SELECT DISTINCT actor_user_id FROM audit_log WHERE actor_user_id IS NOT NULL) a
			LEFT JOIN app_user u ON u.id = a.actor_user_id ORDER BY lower(COALESCE(u.name, '')), a.actor_user_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p Actor
			if err := rows.Scan(&p.ID, &p.Name); err != nil {
				return err
			}
			out.Actors = append(out.Actors, p)
		}
		return rows.Err()
	})
	return out, err
}

// Export formats.
const (
	FormatCSV  = "csv"
	FormatJSON = "json"
)

// CSVHeader is the first line of a CSV export.
var CSVHeader = []string{"time", "action", "actor_id", "actor", "target_type", "target_id", "ip", "request_id", "data", "via"}

// Export writes the filtered log, newest first, as CSV or as a JSON array, and refuses more than
// config.AuditMaxExport entries: a record cut short reads as complete.
func (s *Service) Export(ctx context.Context, f Filter, format string) ([]byte, int, error) {
	where, args := f.where()
	args = append(args, config.AuditMaxExport+1)
	records, err := s.read(ctx, where, args)
	if err != nil {
		return nil, 0, err
	}
	if len(records) > config.AuditMaxExport {
		return nil, 0, ErrExportTooLarge
	}
	if format == FormatJSON {
		body, err := json.Marshal(records)
		return body, len(records), err
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write(CSVHeader)
	for _, r := range records {
		_ = w.Write(CSVLine(r))
	}
	w.Flush()
	return buf.Bytes(), len(records), w.Error()
}

// CSVLine is one entry as its line of the CSV export.
func CSVLine(r Record) []string {
	actor := ""
	if r.ActorID != nil {
		actor = r.ActorID.String()
	}
	return []string{r.CreatedAt.UTC().Format(time.RFC3339Nano), r.Action, actor, Cell(r.ActorName), Cell(r.TargetType),
		Cell(r.TargetID), r.IP, Cell(r.RequestID), Cell(string(r.Data)), Cell(r.Via)}
}

// Cell keeps a spreadsheet from reading a value as a formula, as Exciter's and Armature's exports do.
func Cell(s string) string {
	if s == "" || !strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return s
	}
	return "'" + s
}

// Note records an act that changes nothing, such as an export leaving, in a transaction of its own; the caller
// hands the file over only once it succeeds.
func (s *Service) Note(ctx context.Context, e Entry) error {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		return Write(ctx, tx, e)
	})
}
