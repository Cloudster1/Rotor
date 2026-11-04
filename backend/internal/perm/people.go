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

// Person is somebody Rotor knows, as a picker offers them.
type Person struct {
	ID    uuid.UUID
	Name  string
	Email string
}

// People matches the people let in by name or address, for anybody who may grant a role somewhere. rotor_app reads only
// its own app_user row, so this reads as rotor_admin once the acting person is shown to grant.
func (s *Store) People(ctx context.Context, text string, limit int) ([]Person, error) {
	if limit <= 0 || limit > config.PeopleLookupMax {
		limit = config.PeopleLookupSize
	}
	out := []Person{}
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := require(ctx, tx, actorMayGrant); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT id, name, email::text FROM app_user
			WHERE is_active AND ($1 = '' OR strpos(lower(name), lower($1)) > 0 OR strpos(lower(email::text), lower($1)) > 0)
			ORDER BY lower(name), email LIMIT $2`, strings.TrimSpace(text), limit)
		if err != nil {
			return fmt.Errorf("look people up: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var p Person
			if err := rows.Scan(&p.ID, &p.Name, &p.Email); err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

// User is a person as Admin's Users page shows them: whether they may sign in, their groups and what they hold.
type User struct {
	Person
	Active    bool
	CreatedAt time.Time
	Groups    []UserGroup
	Grants    []UserGrant
}

// UserGroup is one group a person is in.
type UserGroup struct {
	ID      uuid.UUID
	Name    string
	Source  string
	Builtin string
}

// UserGrant is one role a person holds, directly or through a group, which Group then names.
type UserGrant struct {
	Role         Role
	RoleName     string
	WorkflowID   *uuid.UUID
	WorkflowName string
	Group        string
}

// UserFilter narrows and pages the listing of people.
type UserFilter struct {
	// Text matches the name or the address.
	Text   string
	Limit  int
	Offset int
}

// UserPage is one page of people plus how many matched in all.
type UserPage struct {
	Users  []User
	Total  int
	Limit  int
	Offset int
}

// Users lists every person Rotor knows, a page at a time, for an administrator of Rotor.
func (s *Store) Users(ctx context.Context, f UserFilter) (UserPage, error) {
	if f.Limit <= 0 || f.Limit > config.AccessMaxPage {
		f.Limit = config.AccessPageSize
	}
	f.Offset = max(f.Offset, 0)
	page := UserPage{Limit: f.Limit, Offset: f.Offset, Users: []User{}}
	text := strings.TrimSpace(f.Text)
	const match = ` WHERE ($1 = '' OR strpos(lower(u.name), lower($1)) > 0 OR strpos(lower(u.email::text), lower($1)) > 0)`
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := require(ctx, tx, actorAdministers); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM app_user u`+match, text).Scan(&page.Total); err != nil {
			return fmt.Errorf("count people: %w", err)
		}
		rows, err := tx.Query(ctx, `
			SELECT u.id, u.name, u.email::text, u.is_active, u.created_at FROM app_user u`+match+` ORDER BY lower(u.name), u.email LIMIT $2 OFFSET $3`, text, f.Limit, f.Offset)
		if err != nil {
			return fmt.Errorf("list people: %w", err)
		}
		defer rows.Close()
		index := map[uuid.UUID]int{}
		for rows.Next() {
			u := User{Groups: []UserGroup{}, Grants: []UserGrant{}}
			if err := rows.Scan(&u.ID, &u.Name, &u.Email, &u.Active, &u.CreatedAt); err != nil {
				return err
			}
			index[u.ID] = len(page.Users)
			page.Users = append(page.Users, u)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return fillUsers(ctx, tx, page.Users, index)
	})
	if err != nil {
		return UserPage{}, err
	}
	return page, nil
}

// User reads one person as Users lists them, for an administrator of Rotor.
func (s *Store) User(ctx context.Context, id uuid.UUID) (*User, error) {
	var out *User
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := require(ctx, tx, actorAdministers); err != nil {
			return err
		}
		u := User{Groups: []UserGroup{}, Grants: []UserGrant{}}
		err := tx.QueryRow(ctx, `SELECT id, name, email::text, is_active, created_at FROM app_user WHERE id = $1`, id).
			Scan(&u.ID, &u.Name, &u.Email, &u.Active, &u.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchPerson
		}
		if err != nil {
			return err
		}
		users := []User{u}
		if err := fillUsers(ctx, tx, users, map[uuid.UUID]int{id: 0}); err != nil {
			return err
		}
		out = &users[0]
		return nil
	})
	return out, err
}

// fillUsers adds each listed person's groups and grants, two queries for the page rather than two for each person.
func fillUsers(ctx context.Context, tx db.DBTX, users []User, index map[uuid.UUID]int) error {
	ids := make([]uuid.UUID, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.ID)
	}
	groups, err := tx.Query(ctx, `
		SELECT m.user_id, g.id, g.name, g.source, COALESCE(g.builtin, '')
		FROM group_member m JOIN user_group g ON g.id = m.group_id
		WHERE m.user_id = ANY ($1) ORDER BY (g.builtin IS NULL), lower(g.name)`, ids)
	if err != nil {
		return fmt.Errorf("read people's groups: %w", err)
	}
	defer groups.Close()
	for groups.Next() {
		var (
			user uuid.UUID
			g    UserGroup
		)
		if err := groups.Scan(&user, &g.ID, &g.Name, &g.Source, &g.Builtin); err != nil {
			return err
		}
		users[index[user]].Groups = append(users[index[user]].Groups, g)
	}
	if err := groups.Err(); err != nil {
		return err
	}
	grants, err := tx.Query(ctx, `
		SELECT COALESCE(a.user_id, m.user_id), a.role, r.name, a.workflow_id, COALESCE(w.name, ''), COALESCE(g.name, '')
		FROM role_assignment a
		JOIN role r ON r.key = a.role
		LEFT JOIN workflow w ON w.id = a.workflow_id
		LEFT JOIN user_group g ON g.id = a.group_id
		LEFT JOIN group_member m ON m.group_id = a.group_id
		WHERE a.user_id = ANY ($1) OR m.user_id = ANY ($1)
		ORDER BY (a.workflow_id IS NOT NULL), lower(w.name), r.sort_order, lower(r.name), lower(g.name)`, ids)
	if err != nil {
		return fmt.Errorf("read people's grants: %w", err)
	}
	defer grants.Close()
	for grants.Next() {
		var (
			user uuid.UUID
			g    UserGrant
		)
		if err := grants.Scan(&user, &g.Role, &g.RoleName, &g.WorkflowID, &g.WorkflowName, &g.Group); err != nil {
			return err
		}
		if i, ok := index[user]; ok {
			users[i].Grants = append(users[i].Grants, g)
		}
	}
	return grants.Err()
}
