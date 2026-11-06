package tools

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// nameUniqueConstraint is what migration 01530's trigger names a refused duplicate by.
const nameUniqueConstraint = "tool_name_unique"

// namePattern is a name an administrator gives: one word a workflow's YAML writes plainly, lower case so that two
// names never differ by case alone.
var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

// ErrBadName is a name an administrator gave that a binding cannot write plainly.
var ErrBadName = errors.New("Name the tool with 1 to 63 lower case letters, digits, dots, dashes or underscores, " +
	"starting with a letter or digit.")

// NameTaken is a name another tool has, case aside; Taken is that name as the other tool writes it.
type NameTaken struct{ Name, Taken string }

func (e *NameTaken) Error() string {
	return fmt.Sprintf("A tool called %s exists already, and workflows bind tools by name. Give this one another name, such as %s-2.",
		e.Taken, e.Name)
}

// checkName refuses a name that is not one word, or that another tool than except has.
func (s *Service) checkName(ctx context.Context, name string, except uuid.UUID) error {
	if !namePattern.MatchString(name) {
		return ErrBadName
	}
	return s.nameFree(ctx, name, except)
}

// nameFree refuses a name another tool than except has, case aside; the database refuses it too.
func (s *Service) nameFree(ctx context.Context, name string, except uuid.UUID) error {
	var taken string
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT name FROM tool WHERE lower(name) = lower($1) AND id <> $2 LIMIT 1`, name, except).Scan(&taken)
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return err
	}
	return &NameTaken{Name: name, Taken: taken}
}

// nameRefused turns the database's refusal of a duplicate, from a writer that passed nameFree at the same moment.
func nameRefused(err error, name string) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.ConstraintName == nameUniqueConstraint {
		return &NameTaken{Name: name, Taken: name}
	}
	return err
}

// Rename changes the name workflows bind the tool by. Workflows that bind the old name stop finding the tool, so
// the caller lists them first; the product and every connection stay.
func (s *Service) Rename(ctx context.Context, id uuid.UUID, name string) (Tool, error) {
	name = strings.TrimSpace(name)
	tool, err := s.Get(ctx, id)
	if err != nil {
		return Tool{}, err
	}
	if name == tool.Name {
		return tool, nil
	}
	if err := s.checkName(ctx, name, id); err != nil {
		return Tool{}, err
	}
	actor, from := actorOf(ctx), tool.Name
	err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `UPDATE tool SET name = $2 WHERE id = $1`, id, name)
		if err != nil {
			return nameRefused(err, name)
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionToolRenamed, TargetType: "tool", TargetID: id.String(), Actor: actor,
			Data: map[string]any{"from": from, "to": name, "product": tool.Product, "base_url": tool.BaseURL}})
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Tool{}, ErrNotFound
	}
	if err != nil {
		return Tool{}, err
	}
	tool.Name = name
	return tool, nil
}

// Duplicates are the names several tools share from before names were unique (#267), case aside, each with how
// many share it, for an administrator to rename all but one.
func (s *Service) Duplicates(ctx context.Context) (map[string]int, error) {
	out := map[string]int{}
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT min(name), count(*) FROM tool GROUP BY lower(name) HAVING count(*) > 1`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			var n int
			if err := rows.Scan(&name, &n); err != nil {
				return err
			}
			out[name] = n
		}
		return rows.Err()
	})
	return out, err
}
