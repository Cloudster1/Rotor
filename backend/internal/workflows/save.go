package workflows

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/providers"
	"github.com/cloudster1/rotor/backend/internal/workflow/spec"
)

// Validate lists everything wrong with a spec: its YAML, schema and sense, then what the caller's tools tell.
func (s *Service) Validate(ctx context.Context, source string) []spec.Problem {
	problems, _ := s.Check(ctx, source)
	return problems
}

// Check is Validate with the warnings too: what is allowed but may be unwise, such as a write that text read from
// a tool could steer (#40). Warnings never stop a save.
func (s *Service) Check(ctx context.Context, source string) (problems, warnings []spec.Problem) {
	parsed, problems := spec.Parse([]byte(source))
	if parsed == nil {
		return problems, nil
	}
	var catalog spec.Catalog
	if len(problems) == 0 && s.catalog != nil {
		if found, err := s.catalog.For(ctx, parsed); err == nil {
			catalog = found
			problems = append(problems, parsed.CheckAgainst(found)...)
		}
	}
	if len(problems) == 0 && s.Models != nil {
		if listed, err := s.Models.Models(ctx); err == nil {
			problems = append(problems, parsed.CheckModels(func(m spec.Model) bool { return providers.Allows(listed, m) })...)
		}
	}
	if len(problems) == 0 && s.Images != nil && len(parsed.WorkspaceImages()) > 0 {
		if names, err := s.Images.ImageNames(ctx); err == nil {
			problems = append(problems, parsed.CheckImages(func(name string) bool { return slices.Contains(names, name) })...)
		}
	}
	return problems, parsed.Warnings(catalog)
}

// checked parses a spec for saving, refusing one with any problem.
func (s *Service) checked(ctx context.Context, source, comment string) (*spec.Spec, []byte, error) {
	if len(comment) > config.MaxVersionComment {
		return nil, nil, &Invalid{Problems: []spec.Problem{{Code: "comment_too_long",
			Message: fmt.Sprintf("Keep the comment under %d characters.", config.MaxVersionComment)}}}
	}
	if problems := s.Validate(ctx, source); len(problems) > 0 {
		return nil, nil, &Invalid{Problems: problems}
	}
	parsed, _ := spec.Parse([]byte(source))
	document, err := json.Marshal(parsed)
	return parsed, document, err
}

func hashOf(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}

// Create saves a new workflow as version 1, owned by the person on the context, who administers it from then on.
func (s *Service) Create(ctx context.Context, source, comment string) (uuid.UUID, error) {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return uuid.Nil, ErrCannotCreate
	}
	parsed, document, err := s.checked(ctx, source, comment)
	if err != nil {
		return uuid.Nil, err
	}
	// The id is made here: the new row is not the creator's to read back until the database grants it to them.
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `INSERT INTO workflow (id, name, description, owner_id, owner_name) VALUES ($1, $2, $3, $4, $5)`,
			id, parsed.Metadata.Name, parsed.Metadata.Description, p.UserID, p.Name); err != nil {
			return err
		}
		if err := insertVersion(ctx, tx, id, 1, source, document, comment, p); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionWorkflowCreated, TargetType: "workflow", TargetID: id.String(),
			Actor: p.UserID, Data: map[string]any{"name": parsed.Metadata.Name}})
	})
	switch refused := refusal(err); {
	case errors.Is(refused, ErrNotOwner):
		return uuid.Nil, ErrCannotCreate
	case refused != nil:
		return uuid.Nil, refused
	}
	return id, nil
}

func insertVersion(ctx context.Context, tx db.DBTX, id uuid.UUID, number int, source string, document []byte, comment string, p *auth.Principal) error {
	_, err := tx.Exec(ctx, `INSERT INTO workflow_version (workflow_id, number, spec_yaml, spec, hash, comment, created_by, created_by_name)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, id, number, source, document, hashOf(source), comment, p.UserID, p.Name)
	return err
}

// Save adds the next version; base must be the current version, so nobody overwrites a colleague unseen.
func (s *Service) Save(ctx context.Context, id uuid.UUID, source, comment string, base int) error {
	p, w, err := s.allowed(ctx, id, func(w Workflow) bool { return w.CanEdit })
	if err != nil {
		return err
	}
	if w.ArchivedAt != nil {
		return ErrArchived
	}
	if base != w.CurrentVersion {
		return ErrStale
	}
	parsed, document, err := s.checked(ctx, source, comment)
	if err != nil {
		return err
	}
	err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := insertVersion(ctx, tx, id, base+1, source, document, comment, p); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE workflow SET name = $2, description = $3 WHERE id = $1`,
			id, parsed.Metadata.Name, parsed.Metadata.Description); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionWorkflowChanged, TargetType: "workflow", TargetID: id.String(),
			Actor: p.UserID, Data: map[string]any{"version": base + 1, "comment": comment}})
	})
	return refusal(err)
}

// Archive takes a workflow out of the list; its versions stay for the runs that used them. Whoever administers it may,
// administrators of Rotor among them.
func (s *Service) Archive(ctx context.Context, id uuid.UUID) error {
	p, w, err := s.allowed(ctx, id, func(w Workflow) bool { return w.CanAdminister })
	if err != nil {
		return err
	}
	if w.ArchivedAt != nil {
		return nil
	}
	return refusal(s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `UPDATE workflow SET archived_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionWorkflowArchived, TargetType: "workflow", TargetID: id.String(),
			Actor: p.UserID, Data: map[string]any{"name": w.Name}})
	}))
}

// Restore takes a workflow out of the archive, which is how archiving is undone; it takes what archiving takes.
func (s *Service) Restore(ctx context.Context, id uuid.UUID) error {
	p, w, err := s.allowed(ctx, id, func(w Workflow) bool { return w.CanAdminister })
	if err != nil {
		return err
	}
	if w.ArchivedAt == nil {
		return nil
	}
	return refusal(s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `UPDATE workflow SET archived_at = NULL WHERE id = $1`, id); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionWorkflowRestored, TargetType: "workflow", TargetID: id.String(),
			Actor: p.UserID, Data: map[string]any{"name": w.Name}})
	}))
}

// read is a workflow as the person on the context sees it.
func (s *Service) read(ctx context.Context, id uuid.UUID) (*auth.Principal, Workflow, error) {
	var w Workflow
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		w, err = scanWorkflow(tx.QueryRow(ctx, `SELECT `+workflowColumns+workflowFrom+` WHERE w.id = $1`, id))
		return err
	})
	return auth.PrincipalFrom(ctx), w, err
}

// allowed is a workflow the person on the context sees and whose grants let them do what may asks.
func (s *Service) allowed(ctx context.Context, id uuid.UUID, may func(Workflow) bool) (*auth.Principal, Workflow, error) {
	p, w, err := s.read(ctx, id)
	if err != nil {
		return nil, w, err
	}
	if p == nil || !may(w) {
		return nil, w, ErrNotOwner
	}
	return p, w, nil
}

// refusal names what the database refused: a save overtaken by another, or a write the policies forbid.
func refusal(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch {
		case pg.ConstraintName == "workflow_version_in_sequence" || pg.Code == "23505":
			return ErrStale
		case pg.Code == "42501":
			return ErrNotOwner
		}
	}
	return err
}
