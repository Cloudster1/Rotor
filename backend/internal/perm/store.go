package perm

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/rotor/backend/internal/db"
)

var (
	// ErrNotFound is returned for a group or a grant that does not exist.
	ErrNotFound = errors.New("there is no such grant")
	// ErrScope is returned for a role granted over the wrong kind of scope.
	ErrScope = errors.New("that role answers for the whole installation, so grant it without naming a workflow or a skill")
	// ErrSubject is returned for a grant naming both a person and a group, or neither.
	ErrSubject = errors.New("grant a role to one person or to one group, not both")
	// ErrNotASkillRole is returned for a role granted on a skill that gives nothing there.
	ErrNotASkillRole = errors.New("that role gives nothing on a skill, so grant Skill reader, Skill editor or Skill administrator instead")
	// ErrOneScope is returned for a grant naming a workflow and a skill at once.
	ErrOneScope = errors.New("grant a role on one workflow or on one skill, not both")
	// ErrEveryoneAdministers is returned for granting Everyone a role that administers Rotor, which would make every
	// person an administrator.
	ErrEveryoneAdministers = errors.New("everyone cannot be given a role that administers Rotor; grant it to a group of administrators instead")
	// ErrForbidden is a grant the caller's own grants do not let them give or take away.
	ErrForbidden = errors.New("only an administrator of Rotor, or of the workflow or skill, may change who holds its roles, so ask one to do it")
	// ErrLastAdministrator refuses taking away the last Administrator grant.
	ErrLastAdministrator = errors.New("this is the last administrator grant, so grant Administrator to someone else first")
)

// Everyone is the built-in group's mark: it holds every person who signs in to Rotor.
const Everyone = "everyone"

// The database's refusals, by the constraint name its guards raise, so a write they stop reads as the error above.
const (
	EveryoneAdministers  = "role_assignment_everyone_administers"
	KeepsAnAdministrator = "role_assignment_keeps_an_administrator"
)

// Store reads and writes roles and grants.
type Store struct {
	db *db.Cluster
}

// NewStore returns the store over the cluster.
func NewStore(cluster *db.Cluster) *Store { return &Store{db: cluster} }

// resolveFor gathers everything granted to one person: the roles given to them directly, and the roles given to any
// group they belong to. A grant naming neither a workflow nor a skill is installation-wide.
const resolveFor = `
SELECT a.role, COALESCE(a.workflow_id, '00000000-0000-0000-0000-000000000000'::uuid),
       COALESCE(a.skill_id, '00000000-0000-0000-0000-000000000000'::uuid)
FROM role_assignment a
LEFT JOIN group_member m ON m.group_id = a.group_id AND m.user_id = $1
WHERE a.user_id = $1 OR m.user_id IS NOT NULL`

// Resolve returns what one person may do, read in the caller's transaction.
func Resolve(ctx context.Context, tx db.DBTX, userID uuid.UUID) (Set, error) {
	defs, err := definitionsFor(ctx, tx)
	if err != nil {
		return Set{}, err
	}
	rows, err := tx.Query(ctx, resolveFor, userID)
	if err != nil {
		return Set{}, fmt.Errorf("resolve permissions: %w", err)
	}
	defer rows.Close()
	var grants []Grant
	for rows.Next() {
		var g Grant
		if err := rows.Scan(&g.Role, &g.Workflow, &g.Skill); err != nil {
			return Set{}, err
		}
		grants = append(grants, g)
	}
	if err := rows.Err(); err != nil {
		return Set{}, err
	}
	return NewSetWith(grants, defs), nil
}

// ResolveFor is Resolve on its own connection, for a request that has not opened a transaction yet; the request's
// own policies are not yet bound, so it reads as the admin role, as sign-in does.
func (s *Store) ResolveFor(ctx context.Context, userID uuid.UUID) (Set, error) {
	var set Set
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		set, err = Resolve(ctx, tx, userID)
		return err
	})
	return set, err
}

func isCheckViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514"
}

func isPermissionDenied(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42501"
}

func violates(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.ConstraintName == constraint
}
