// Package workflows keeps people's workflows: immutable, numbered versions of a spec, shared privately, with
// realm groups or with everyone. Who sees and changes what is enforced by the database's policies too.
package workflows

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/workflow/spec"
)

// Errors a caller tells apart; *Invalid lists what is wrong with a spec.
var (
	ErrNotFound = errors.New("no such workflow")
	// ErrNotOwner is a workflow the caller may see but not change.
	ErrNotOwner = errors.New("only the workflow's owner may change it")
	// ErrStale is a save based on a version that is no longer the current one.
	ErrStale = errors.New("the workflow changed since it was opened")
	// ErrArchived is a save to an archived workflow.
	ErrArchived = errors.New("the workflow is archived")
)

// Invalid is a spec with problems, which a save refuses.
type Invalid struct{ Problems []spec.Problem }

func (e *Invalid) Error() string {
	if len(e.Problems) == 0 {
		return "the workflow has problems"
	}
	return fmt.Sprintf("the workflow has %d problems, the first: %s", len(e.Problems), e.Problems[0].Message)
}

// Sharing values, as the table and the API name them.
const (
	SharingPrivate  = "private"
	SharingGroups   = "groups"
	SharingEveryone = "everyone"
)

// Workflow is a workflow as its viewers see it.
type Workflow struct {
	ID             uuid.UUID
	Name           string
	Description    string
	OwnerID        uuid.UUID
	OwnerName      string
	Sharing        string
	SharedGroups   []string
	CurrentVersion int
	UpdatedAt      time.Time
	ArchivedAt     *time.Time
}

// Version is one saved version of a workflow.
type Version struct {
	Number        int
	SpecYAML      string
	Hash          string
	Comment       string
	CreatedByID   uuid.UUID
	CreatedByName string
	CreatedAt     time.Time
}

// Diff is what changed between two versions.
type Diff struct {
	From, To int
	Hunks    []Hunk
}

// CatalogSource builds what spec.CheckAgainst asks about the tools, as the person on the context sees them.
type CatalogSource interface {
	For(ctx context.Context, s *spec.Spec) (spec.Catalog, error)
}

// Service is the workflows.
type Service struct {
	db      *db.Cluster
	catalog CatalogSource
}

// NewService returns the workflows; catalog may be nil, which checks a spec without its tools.
func NewService(cluster *db.Cluster, catalog CatalogSource) *Service {
	return &Service{db: cluster, catalog: catalog}
}

const workflowColumns = `w.id, w.name, w.description, w.owner_id, w.owner_name, w.sharing, w.shared_groups,
	w.current_version, w.updated_at, w.archived_at`

const workflowFrom = ` FROM workflow w`

func scanWorkflow(row pgx.Row) (Workflow, error) {
	var w Workflow
	err := row.Scan(&w.ID, &w.Name, &w.Description, &w.OwnerID, &w.OwnerName, &w.Sharing, &w.SharedGroups,
		&w.CurrentVersion, &w.UpdatedAt, &w.ArchivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return w, ErrNotFound
	}
	return w, err
}

// List is the workflows the person on the context may see, archived ones or the others.
func (s *Service) List(ctx context.Context, archived bool) ([]Workflow, error) {
	out := []Workflow{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT `+workflowColumns+workflowFrom+`
			WHERE (w.archived_at IS NOT NULL) = $1 ORDER BY lower(w.name), w.id`, archived)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			w, err := scanWorkflow(rows)
			if err != nil {
				return err
			}
			out = append(out, w)
		}
		return rows.Err()
	})
	return out, err
}

// Get is a workflow with its current version, as the person on the context may see it.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Workflow, Version, error) {
	var w Workflow
	var v Version
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if w, err = scanWorkflow(tx.QueryRow(ctx, `SELECT `+workflowColumns+workflowFrom+` WHERE w.id = $1`, id)); err != nil {
			return err
		}
		v, err = version(ctx, tx, id, w.CurrentVersion)
		return err
	})
	return w, v, err
}

// Versions lists a workflow's versions, newest first, without their YAML.
func (s *Service) Versions(ctx context.Context, id uuid.UUID) ([]Version, error) {
	out := []Version{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := scanWorkflow(tx.QueryRow(ctx, `SELECT `+workflowColumns+workflowFrom+` WHERE w.id = $1`, id)); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT v.number, '', v.hash, v.comment, COALESCE(v.created_by, '00000000-0000-0000-0000-000000000000'),
			v.created_by_name, v.created_at
			FROM workflow_version v WHERE v.workflow_id = $1 ORDER BY v.number DESC`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v Version
			if err := rows.Scan(&v.Number, &v.SpecYAML, &v.Hash, &v.Comment, &v.CreatedByID, &v.CreatedByName, &v.CreatedAt); err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}

// Version is one version with its YAML.
func (s *Service) Version(ctx context.Context, id uuid.UUID, number int) (Version, error) {
	var v Version
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		v, err = version(ctx, tx, id, number)
		return err
	})
	return v, err
}

// Pinned is a version's YAML for the control plane, outside any person's view: what a run executes.
func (s *Service) Pinned(ctx context.Context, id uuid.UUID, number int) (Version, error) {
	var v Version
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		v, err = version(ctx, tx, id, number)
		return err
	})
	return v, err
}

func version(ctx context.Context, tx db.DBTX, id uuid.UUID, number int) (Version, error) {
	var v Version
	err := tx.QueryRow(ctx, `SELECT v.number, v.spec_yaml, v.hash, v.comment,
			COALESCE(v.created_by, '00000000-0000-0000-0000-000000000000'), v.created_by_name, v.created_at
		FROM workflow_version v WHERE v.workflow_id = $1 AND v.number = $2`, id, number).
		Scan(&v.Number, &v.SpecYAML, &v.Hash, &v.Comment, &v.CreatedByID, &v.CreatedByName, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	return v, err
}

// Diff answers what changed between two versions of a workflow the caller may see.
func (s *Service) Diff(ctx context.Context, id uuid.UUID, from, to int) (Diff, error) {
	a, err := s.Version(ctx, id, from)
	if err != nil {
		return Diff{}, err
	}
	b, err := s.Version(ctx, id, to)
	if err != nil {
		return Diff{}, err
	}
	return Diff{From: from, To: to, Hunks: LineDiff(a.SpecYAML, b.SpecYAML)}, nil
}
