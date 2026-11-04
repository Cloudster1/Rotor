package perm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// Group is a named set of people, and how much it has been granted.
type Group struct {
	ID          uuid.UUID
	Name        string
	Description string
	// Source says who owns the membership: "oidc" mirrors the realm's groups claim, "local" is kept by hand.
	Source string
	// ExternalRef is the value the realm's groups claim is matched against.
	ExternalRef string
	// Builtin marks a group the installation keeps itself; "everyone" holds every person let in.
	Builtin     string
	Members     []Member
	MemberCount int
	RoleCount   int
}

// FromProvider reports whether the identity provider owns this group's members.
func (g Group) FromProvider() bool { return g.Source == SourceOIDC }

// IsBuiltin reports whether the installation keeps this group itself.
func (g Group) IsBuiltin() bool { return g.Builtin != "" }

// Member is a person in a group.
type Member struct {
	UserID uuid.UUID
	Name   string
	Email  string
}

// GroupInput describes a group being created.
type GroupInput struct {
	Name        string
	Description string
	// ExternalRef ties the group to a value in the realm's groups claim; setting it makes the realm own the membership.
	ExternalRef string
}

// Where a group's members come from, as user_group.source holds it.
const (
	SourceLocal = "local"
	SourceOIDC  = "oidc"
)

var (
	// ErrGroupNotFound is returned for a group that does not exist.
	ErrGroupNotFound = errors.New("there is no such group")
	// ErrGroupName is returned for a group without a name.
	ErrGroupName = errors.New("give the group a name")
	// ErrGroupNameTaken is returned when another group already has the name or the claim value.
	ErrGroupNameTaken = errors.New("there is already a group with that name or claim value, so choose another")
	// ErrBuiltinGroup is returned for changing Everyone.
	ErrBuiltinGroup = errors.New("everyone holds every person let in to Rotor, so it is neither deleted nor given members by hand")
	// ErrManagedElsewhere is returned for changing the members of a group the identity provider keeps.
	ErrManagedElsewhere = errors.New("the identity provider keeps the members of this group, so change them there")
	// ErrNoSuchPerson is returned for adding somebody Rotor does not know.
	ErrNoSuchPerson = errors.New("there is no such person; they appear here once they have signed in")
)

const selectGroup = `
SELECT g.id, g.name, g.description, g.source, COALESCE(g.external_ref, ''), COALESCE(g.builtin, ''),
       (SELECT count(*) FROM group_member m WHERE m.group_id = g.id),
       (SELECT count(*) FROM role_assignment a WHERE a.group_id = g.id)
FROM user_group g`

// Membership is invisible to rotor_app beyond one's own, so groups are read and their members written as rotor_admin;
// these ask, in that same transaction, whether the person it acts for may.
const (
	actorAdministers = `SELECT current_actor_via() IS NULL AND actor_can('rotor.administer', NULL)`
	actorMayGrant    = `
SELECT current_actor_via() IS NULL
   AND EXISTS (SELECT 1 FROM app_user u WHERE u.id = current_actor_id() AND u.is_active AND u.standing IS NOT NULL)
   AND EXISTS (SELECT 1 FROM role_assignment a JOIN role r ON r.key = a.role
               WHERE r.permissions && ARRAY['workflow.administer', 'rotor.administer']
                 AND (a.user_id = current_actor_id()
                      OR a.group_id IN (SELECT m.group_id FROM group_member m WHERE m.user_id = current_actor_id())))`
)

// require refuses the transaction's person unless the check, one of the above, holds for them.
func require(ctx context.Context, tx db.DBTX, check string) error {
	var allowed bool
	if err := tx.QueryRow(ctx, check).Scan(&allowed); err != nil {
		return fmt.Errorf("check the acting person: %w", err)
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

func scanGroup(row pgx.Row) (*Group, error) {
	var g Group
	err := row.Scan(&g.ID, &g.Name, &g.Description, &g.Source, &g.ExternalRef, &g.Builtin, &g.MemberCount, &g.RoleCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrGroupNotFound
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// Groups lists the installation's groups, Everyone first, then the realm's, then those made by hand, for anybody who
// may grant a role somewhere.
func (s *Store) Groups(ctx context.Context) ([]Group, error) {
	out := []Group{}
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := require(ctx, tx, actorMayGrant); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, selectGroup+` ORDER BY (g.builtin IS NULL), (g.source <> 'oidc'), lower(g.name)`)
		if err != nil {
			return fmt.Errorf("list groups: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			g, err := scanGroup(rows)
			if err != nil {
				return err
			}
			out = append(out, *g)
		}
		return rows.Err()
	})
	return out, err
}

// GroupByID reads one group with the people in it, for an administrator of Rotor.
func (s *Store) GroupByID(ctx context.Context, id uuid.UUID) (*Group, error) {
	var out *Group
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := require(ctx, tx, actorAdministers); err != nil {
			return err
		}
		var err error
		out, err = reloadGroup(ctx, tx, id)
		return err
	})
	return out, err
}

func reloadGroup(ctx context.Context, tx db.DBTX, id uuid.UUID) (*Group, error) {
	found, err := scanGroup(tx.QueryRow(ctx, selectGroup+` WHERE g.id = $1`, id))
	if err != nil {
		return nil, err
	}
	found.Members, err = groupMembers(ctx, tx, id)
	return found, err
}

func groupMembers(ctx context.Context, tx db.DBTX, groupID uuid.UUID) ([]Member, error) {
	rows, err := tx.Query(ctx, `
		SELECT m.user_id, u.name, u.email::text FROM group_member m JOIN app_user u ON u.id = m.user_id
		WHERE m.group_id = $1 ORDER BY lower(u.name), u.email`, groupID)
	if err != nil {
		return nil, fmt.Errorf("read group members: %w", err)
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Name, &m.Email); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CreateGroup adds a group as the person on the context, whom the policies on user_group hold to administering Rotor.
func (s *Store) CreateGroup(ctx context.Context, in GroupInput, actor uuid.UUID) (*Group, error) {
	name, ref := strings.TrimSpace(in.Name), strings.TrimSpace(in.ExternalRef)
	if name == "" {
		return nil, ErrGroupName
	}
	out := &Group{Name: name, Description: strings.TrimSpace(in.Description), Source: SourceLocal, ExternalRef: ref, Members: []Member{}}
	var external any
	if ref != "" {
		out.Source, external = SourceOIDC, ref
	}
	err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO user_group (name, description, source, external_ref) VALUES ($1, $2, $3, $4) RETURNING id`,
			out.Name, out.Description, out.Source, external).Scan(&out.ID)
		switch {
		case isUniqueViolation(err):
			return ErrGroupNameTaken
		case isPermissionDenied(err):
			return ErrForbidden
		case err != nil:
			return fmt.Errorf("create group: %w", err)
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionGroupCreated, TargetType: "group", TargetID: out.ID.String(),
			Actor: actor, Data: map[string]any{"name": out.Name, "externalRef": ref}})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteGroup removes a group along with everything granted to it; Everyone stays, as the database insists too.
func (s *Store) DeleteGroup(ctx context.Context, id uuid.UUID, actor uuid.UUID) error {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var name, builtin string
		err := tx.QueryRow(ctx, `SELECT name, COALESCE(builtin, '') FROM user_group WHERE id = $1`, id).Scan(&name, &builtin)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrGroupNotFound
		}
		if err != nil {
			return err
		}
		if builtin != "" {
			return ErrBuiltinGroup
		}
		tag, err := tx.Exec(ctx, `DELETE FROM user_group WHERE id = $1`, id)
		switch {
		case violates(err, KeepsAnAdministrator):
			return ErrLastAdministrator
		case isCheckViolation(err):
			return ErrBuiltinGroup
		case err != nil:
			return fmt.Errorf("delete group: %w", err)
		case tag.RowsAffected() == 0:
			return ErrForbidden
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionGroupDeleted, TargetType: "group", TargetID: id.String(),
			Actor: actor, Data: map[string]any{"name": name}})
	})
}

// AddToGroup puts somebody in a group kept by hand.
func (s *Store) AddToGroup(ctx context.Context, groupID, userID uuid.UUID, actor uuid.UUID) (*Group, error) {
	return s.changeMembers(ctx, groupID, userID, actor, audit.ActionGroupMemberAdded,
		`INSERT INTO group_member (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`)
}

// RemoveFromGroup takes somebody out of a group kept by hand.
func (s *Store) RemoveFromGroup(ctx context.Context, groupID, userID uuid.UUID, actor uuid.UUID) (*Group, error) {
	return s.changeMembers(ctx, groupID, userID, actor, audit.ActionGroupMemberRemoved,
		`DELETE FROM group_member WHERE group_id = $1 AND user_id = $2`)
}

// changeMembers writes one membership as rotor_admin, since rotor_app writes none, once the person acting is shown to
// administer Rotor; Everyone's and the realm's groups follow sign-in alone.
func (s *Store) changeMembers(ctx context.Context, groupID, userID, actor uuid.UUID, action, statement string) (*Group, error) {
	var out *Group
	err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := require(ctx, tx, actorAdministers); err != nil {
			return err
		}
		found, err := scanGroup(tx.QueryRow(ctx, selectGroup+` WHERE g.id = $1 FOR UPDATE OF g`, groupID))
		if err != nil {
			return err
		}
		if found.IsBuiltin() {
			return ErrBuiltinGroup
		}
		if found.FromProvider() {
			return ErrManagedElsewhere
		}
		var known bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM app_user WHERE id = $1)`, userID).Scan(&known); err != nil {
			return err
		}
		if !known {
			return ErrNoSuchPerson
		}
		if _, err := tx.Exec(ctx, statement, groupID, userID); err != nil {
			return fmt.Errorf("change the group's members: %w", err)
		}
		if out, err = reloadGroup(ctx, tx, groupID); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: action, TargetType: "group", TargetID: groupID.String(),
			Actor: actor, Data: map[string]any{"user": userID}})
	})
	return out, err
}
