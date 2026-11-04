package perm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// Assignment is one role held by one subject over one scope, as a listing shows it: with the names filled in, because
// a page of identifiers explains nothing.
type Assignment struct {
	ID       uuid.UUID
	Role     Role
	RoleName string
	// WorkflowID is nil for a role held over the whole installation.
	WorkflowID   *uuid.UUID
	WorkflowName string
	// Exactly one of these is set.
	UserID    *uuid.UUID
	UserName  string
	UserEmail string
	GroupID   *uuid.UUID
	GroupName string
	// GroupBuiltin is the group's mark when the installation keeps it, "everyone".
	GroupBuiltin string
	CreatedAt    time.Time
}

const assignmentFrom = `
FROM role_assignment a
JOIN role r ON r.key = a.role
LEFT JOIN workflow w ON w.id = a.workflow_id
LEFT JOIN app_user u ON u.id = a.user_id
LEFT JOIN user_group g ON g.id = a.group_id`

const selectAssignment = `
SELECT a.id, a.role, r.name, a.workflow_id, COALESCE(w.name, ''), a.user_id, COALESCE(u.name, ''), COALESCE(u.email::text, ''),
       a.group_id, COALESCE(g.name, ''), COALESCE(g.builtin, ''), a.created_at` + assignmentFrom

// assignmentOrder reads the way somebody looks for a grant: the installation-wide ones first, then by workflow, and the
// most powerful role at the top of each.
const assignmentOrder = `
ORDER BY (a.workflow_id IS NOT NULL), lower(w.name), r.sort_order, lower(r.name), lower(COALESCE(g.name, u.name)), a.id`

func scanAssignment(row pgx.Row) (*Assignment, error) {
	var a Assignment
	err := row.Scan(&a.ID, &a.Role, &a.RoleName, &a.WorkflowID, &a.WorkflowName, &a.UserID, &a.UserName, &a.UserEmail,
		&a.GroupID, &a.GroupName, &a.GroupBuiltin, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// AssignmentFilter narrows and pages a listing of grants.
type AssignmentFilter struct {
	// Workflow keeps the grants that answer there, the installation-wide ones included because they answer everywhere.
	Workflow uuid.UUID
	// Text matches the person, the group, or the workflow's name.
	Text   string
	Role   Role
	Limit  int
	Offset int
}

// AssignmentPage is one page of grants plus how many matched in all.
type AssignmentPage struct {
	Assignments []Assignment
	Total       int
	Limit       int
	Offset      int
}

func (f AssignmentFilter) where() (string, []any) {
	var (
		clauses []string
		args    []any
	)
	if f.Workflow != uuid.Nil {
		args = append(args, f.Workflow)
		clauses = append(clauses, fmt.Sprintf("(a.workflow_id IS NULL OR a.workflow_id = $%d)", len(args)))
	}
	if f.Role != "" {
		args = append(args, string(f.Role))
		clauses = append(clauses, fmt.Sprintf("a.role = $%d", len(args)))
	}
	if text := strings.TrimSpace(f.Text); text != "" {
		args = append(args, text)
		clauses = append(clauses, fmt.Sprintf("(strpos(lower(COALESCE(u.name, '')), lower($%[1]d)) > 0"+
			" OR strpos(lower(COALESCE(u.email::text, '')), lower($%[1]d)) > 0 OR strpos(lower(COALESCE(g.name, '')), lower($%[1]d)) > 0"+
			" OR strpos(lower(COALESCE(w.name, '')), lower($%[1]d)) > 0)", len(args)))
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// ListAssignments returns one page of every grant the filter allows, for an administrator of Rotor. The names of
// people are not rotor_app's to read, so it reads as rotor_admin once the acting person is shown to administer.
func (s *Store) ListAssignments(ctx context.Context, f AssignmentFilter) (AssignmentPage, error) {
	if f.Limit <= 0 || f.Limit > config.AccessMaxPage {
		f.Limit = config.AccessPageSize
	}
	f.Offset = max(f.Offset, 0)
	page := AssignmentPage{Limit: f.Limit, Offset: f.Offset, Assignments: []Assignment{}}
	where, args := f.where()
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := require(ctx, tx, actorAdministers); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*)`+assignmentFrom+where, args...).Scan(&page.Total); err != nil {
			return fmt.Errorf("count grants: %w", err)
		}
		n := len(args)
		var err error
		page.Assignments, err = readAssignments(ctx, tx,
			selectAssignment+where+assignmentOrder+fmt.Sprintf(" LIMIT $%d OFFSET $%d", n+1, n+2), append(args, f.Limit, f.Offset)...)
		return err
	})
	if err != nil {
		return AssignmentPage{}, err
	}
	return page, nil
}

// WorkflowAssignments lists the grants made on one workflow alone, for anybody who may see it.
func (s *Store) WorkflowAssignments(ctx context.Context, workflow uuid.UUID) ([]Assignment, error) {
	var out []Assignment
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := requireOn(ctx, tx, WorkflowRead, workflow); err != nil {
			return err
		}
		var err error
		out, err = readAssignments(ctx, tx, selectAssignment+` WHERE a.workflow_id = $1`+assignmentOrder, workflow)
		return err
	})
	return out, err
}

// Assignment reads one grant, for anybody who administers Rotor or may see the workflow it is made on.
func (s *Store) Assignment(ctx context.Context, id uuid.UUID) (*Assignment, error) {
	var out *Assignment
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if out, err = scanAssignment(tx.QueryRow(ctx, selectAssignment+` WHERE a.id = $1`, id)); err != nil {
			return err
		}
		if out.WorkflowID == nil {
			err = require(ctx, tx, actorAdministers)
		} else {
			err = requireOn(ctx, tx, WorkflowRead, *out.WorkflowID)
		}
		if errors.Is(err, ErrForbidden) {
			return ErrNotFound
		}
		return err
	})
	return out, err
}

// requireOn refuses the transaction's person unless they hold the permission on the workflow; one they may not see
// reads as not there, as the workflow routes answer.
func requireOn(ctx context.Context, tx db.DBTX, p Permission, workflow uuid.UUID) error {
	var allowed bool
	if err := tx.QueryRow(ctx, `SELECT actor_can($1, $2)`, string(p), workflow).Scan(&allowed); err != nil {
		return fmt.Errorf("check the acting person: %w", err)
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

func readAssignments(ctx context.Context, tx db.DBTX, query string, args ...any) ([]Assignment, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list grants: %w", err)
	}
	defer rows.Close()
	out := []Assignment{}
	for rows.Next() {
		a, err := scanAssignment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}
