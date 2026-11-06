package tracedest

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/vault"
)

// LangSmithName names the destination the deprecated LangSmith routes make where none was carried over.
const LangSmithName = "LangSmith"

var (
	// ErrLangSmithNoKey is turning LangSmith on with no key given now or before.
	ErrLangSmithNoKey = errors.New("LangSmith needs a key before it is turned on")
	// ErrLangSmithInput is a key or project of a shape LangSmith never issues.
	ErrLangSmithInput = errors.New("the key or the project is not of a shape LangSmith takes")
)

// LangSmithSetting is the carried-over destination as the deprecated /settings/langsmith routes answer it.
type LangSmithSetting struct {
	Enabled   bool
	Project   string
	KeySetAt  *time.Time
	ChangedBy *string
	UpdatedAt time.Time
	Endpoint  string
}

// LangSmithInput changes it as those routes did; a nil Key keeps the one held.
type LangSmithInput struct {
	Enabled               bool
	Project               string
	Key                   *string
	AcknowledgeDataLeaves bool
}

// carried is the carried-over destination with its headers, or found false where there is none.
type carried struct {
	found   bool
	d       Destination
	headers map[string]string
}

func (s *Service) carried(ctx context.Context) (carried, error) {
	var c carried
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		c.d, err = scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM trace_destination d WHERE d.carried_over`))
		return err
	})
	switch {
	case errors.Is(err, ErrNotFound):
		return c, nil
	case err != nil:
		return c, err
	}
	c.found, c.headers = true, map[string]string{}
	if c.d.HeadersSetAt != nil {
		h, err := s.headers.DestinationHeaders(ctx, c.d.ID)
		if err != nil && !errors.Is(err, vault.ErrNotFound) {
			return c, err
		}
		for k, v := range h.Headers {
			c.headers[k] = v
		}
	}
	return c, nil
}

// LangSmith is the carried-over destination in the old setting's shape, for an administrator.
func (s *Service) LangSmith(ctx context.Context) (LangSmithSetting, error) {
	c, err := s.carried(ctx)
	if err != nil {
		return LangSmithSetting{}, err
	}
	if !c.found {
		out := LangSmithSetting{Project: config.DefaultLangSmithProject, Endpoint: s.langSmithEndpoint}
		err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `SELECT (SELECT u.name FROM app_user u WHERE u.id = l.changed_by), l.updated_at
				FROM langsmith_setting l`).Scan(&out.ChangedBy, &out.UpdatedAt)
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return LangSmithSetting{}, auth.ErrNoSession
		}
		return out, err
	}
	out := LangSmithSetting{Enabled: c.d.Enabled, Project: config.DefaultLangSmithProject, ChangedBy: c.d.ChangedBy,
		UpdatedAt: c.d.UpdatedAt, Endpoint: c.d.Endpoint}
	if project := c.headers[LangSmithProjectHeader]; project != "" {
		out.Project = project
	}
	if c.headers[LangSmithKeyHeader] != "" {
		out.KeySetAt = c.d.HeadersSetAt
	}
	return out, nil
}

// SetLangSmith changes the carried-over destination as PUT /settings/langsmith did, making it where none was: on
// means sending text, which needs the acknowledgement and a key.
func (s *Service) SetLangSmith(ctx context.Context, in LangSmithInput) (LangSmithSetting, error) {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return LangSmithSetting{}, auth.ErrNoSession
	}
	in.Project = strings.TrimSpace(in.Project)
	if in.Project == "" {
		in.Project = config.DefaultLangSmithProject
	}
	if len(in.Project) > config.MaxLangSmithProjectChars || strings.IndexFunc(in.Project, unicode.IsControl) >= 0 {
		return LangSmithSetting{}, ErrLangSmithInput
	}
	if in.Enabled && !in.AcknowledgeDataLeaves {
		return LangSmithSetting{}, ErrNotAcknowledged
	}
	c, err := s.carried(ctx)
	if err != nil {
		return LangSmithSetting{}, err
	}
	headers := c.headers
	if headers == nil {
		headers = map[string]string{}
	}
	change := "kept"
	if in.Key != nil {
		key := strings.TrimSpace(*in.Key)
		if key == "" || len(key) > config.MaxLangSmithKeyChars || strings.ContainsAny(key, " \t\r\n") {
			return LangSmithSetting{}, ErrLangSmithInput
		}
		headers[LangSmithKeyHeader], change = key, "set"
	}
	if in.Enabled && headers[LangSmithKeyHeader] == "" {
		return LangSmithSetting{}, ErrLangSmithNoKey
	}
	if headers[LangSmithProjectHeader] != in.Project {
		headers[LangSmithProjectHeader], change = in.Project, "set"
	}
	dest := Input{Name: LangSmithName, Endpoint: s.langSmithEndpoint, Enabled: in.Enabled, Content: in.Enabled,
		AcknowledgeContentLeaves: in.AcknowledgeDataLeaves}
	if c.found {
		dest.Name, dest.Endpoint, dest.TraceLink = c.d.Name, c.d.Endpoint, c.d.TraceLink
	}
	if err := s.putLangSmith(ctx, p.UserID, c, dest, headers, change); err != nil {
		return LangSmithSetting{}, err
	}
	return s.LangSmith(ctx)
}

// putLangSmith writes the carried-over destination, audited as any destination's change is.
func (s *Service) putLangSmith(ctx context.Context, actor uuid.UUID, c carried, in Input, headers map[string]string, change string) error {
	err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		id, action := c.d.ID, audit.ActionTraceDestinationChanged
		if !c.found {
			action = audit.ActionTraceDestinationAdded
			if err := tx.QueryRow(ctx, `INSERT INTO trace_destination (name, endpoint, carried_over, changed_by)
				VALUES ($1, $2, true, $3) RETURNING id`, in.Name, in.Endpoint, actor).Scan(&id); err != nil {
				return refusal(err)
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE trace_destination SET enabled = $2, content = $3,
			content_acknowledged_by = CASE WHEN $3 THEN $4::uuid END, content_acknowledged_at = CASE WHEN $3 THEN now() END,
			changed_by = $4 WHERE id = $1`, id, in.Enabled, in.Content, actor); err != nil {
			return err
		}
		if change != "kept" {
			if err := s.writeHeaders(ctx, tx, id, headers); err != nil {
				return err
			}
		}
		data := map[string]any{"name": in.Name, "endpoint": in.Endpoint, "headers": change, "enabled": in.Enabled,
			"content": in.Content, "acknowledged": in.AcknowledgeContentLeaves}
		if !c.found {
			data["headers"] = names(headers)
		}
		return audit.Write(ctx, tx, audit.Entry{Action: action, TargetType: "trace_destination", TargetID: id.String(),
			Actor: actor, Data: data})
	})
	if err == nil {
		s.forget()
	}
	return err
}

// RemoveLangSmithKey forgets the key and turns the carried-over destination off, keeping its project.
func (s *Service) RemoveLangSmithKey(ctx context.Context) (LangSmithSetting, error) {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return LangSmithSetting{}, auth.ErrNoSession
	}
	c, err := s.carried(ctx)
	if err != nil {
		return LangSmithSetting{}, err
	}
	if c.found {
		delete(c.headers, LangSmithKeyHeader)
		in := Input{Name: c.d.Name, Endpoint: c.d.Endpoint}
		if err := s.putLangSmith(ctx, p.UserID, c, in, c.headers, "removed"); err != nil {
			return LangSmithSetting{}, err
		}
	}
	return s.LangSmith(ctx)
}
