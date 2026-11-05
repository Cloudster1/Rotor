package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// The database's refusals a save reads as a sentence of its own.
const (
	nameUnique = "skill_name_unique"
	inSequence = "skill_version_in_sequence"
	versionKey = "skill_version_pkey"
)

// Validate lists everything wrong with a version's files that the files alone tell, as a save checks them.
func (s *Service) Validate(files []File) []Problem {
	_, problems := Check(files)
	return problems
}

// checked is a version's files ready to save, or *Invalid naming what is wrong with them and the comment.
func checked(files []File, comment string) (*Draft, error) {
	draft, problems := Check(files)
	if utf8.RuneCountInString(comment) > config.MaxVersionComment {
		problems = append(problems, Problem{Code: CodeCommentLength, Path: Whole,
			Message: fmt.Sprintf("Keep the comment under %d characters.", config.MaxVersionComment)})
	}
	if len(problems) > 0 {
		return nil, &Invalid{Problems: problems}
	}
	return draft, nil
}

// Create makes a skill from its files as version 1, owned by the person on the context, who administers it from then on.
func (s *Service) Create(ctx context.Context, files []File, comment string) (uuid.UUID, error) {
	return s.create(ctx, files, comment, false)
}

// create is Create; an import also audits skill.imported in the same transaction.
func (s *Service) create(ctx context.Context, files []File, comment string, imported bool) (uuid.UUID, error) {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return uuid.Nil, ErrCannotCreate
	}
	draft, err := checked(files, comment)
	if err != nil {
		return uuid.Nil, err
	}
	// The id is made here: the new row is not the creator's to read back until the database grants it to them.
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `INSERT INTO skill (id, name, description, owner_id, owner_name) VALUES ($1, $2, $3, $4, $5)`,
			id, draft.Name, draft.Description, p.UserID, p.Name); err != nil {
			return err
		}
		if err := insertVersion(ctx, tx, id, 1, draft, comment, p); err != nil {
			return err
		}
		if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionSkillCreated, TargetType: "skill", TargetID: id.String(),
			Actor: p.UserID, Data: map[string]any{"name": draft.Name, "version": 1}}); err != nil {
			return err
		}
		return auditImport(ctx, tx, imported, id, p, 1, true)
	})
	switch refused := refusal(err); {
	case errors.Is(refused, ErrForbidden):
		return uuid.Nil, ErrCannotCreate
	case refused != nil:
		return uuid.Nil, refused
	}
	return id, nil
}

// insertVersion writes a version and its files in the caller's transaction, which the files' policy insists on.
func insertVersion(ctx context.Context, tx db.DBTX, id uuid.UUID, number int, d *Draft, comment string, p *auth.Principal) error {
	frontMatter, err := json.Marshal(d.FrontMatter)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO skill_version (skill_id, number, front_matter, description, hash, bytes, comment,
			created_by, created_by_name) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		id, number, frontMatter, d.Description, d.Hash, d.Bytes, comment, p.UserID, p.Name); err != nil {
		return err
	}
	for _, f := range d.Files {
		if _, err := tx.Exec(ctx, `INSERT INTO skill_file (skill_id, number, path, content, bytes, sha256) VALUES ($1, $2, $3, $4, $5, $6)`,
			id, number, f.Path, f.Content, len(f.Content), fileHash(f.Content)); err != nil {
			return err
		}
	}
	return nil
}

// Save adds the next version; base must be the current version, so nobody overwrites a colleague unseen.
func (s *Service) Save(ctx context.Context, id uuid.UUID, base int, files []File, comment string) error {
	return s.save(ctx, id, base, files, comment, false)
}

// save is Save; an import also audits skill.imported in the same transaction.
func (s *Service) save(ctx context.Context, id uuid.UUID, base int, files []File, comment string, imported bool) error {
	p, k, err := s.allowed(ctx, id, func(k Skill) bool { return k.CanEdit })
	if err != nil {
		return err
	}
	switch {
	case k.ArchivedAt != nil:
		return ErrArchived
	case base != k.CurrentVersion:
		return ErrStale
	}
	draft, err := checked(files, comment)
	if err != nil {
		return err
	}
	if draft.Name != k.Name {
		return &Invalid{Problems: []Problem{{Code: CodeNameChanged, Path: SkillFile, Message: fmt.Sprintf("The name in SKILL.md "+
			"is %s, but this skill is %s, and a skill keeps its name. Put the name back, or make a new skill under the new name.",
			draft.Name, k.Name)}}}
	}
	return refusal(s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := insertVersion(ctx, tx, id, base+1, draft, comment, p); err != nil {
			return err
		}
		if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionSkillChanged, TargetType: "skill", TargetID: id.String(),
			Actor: p.UserID, Data: map[string]any{"version": base + 1, "comment": comment}}); err != nil {
			return err
		}
		return auditImport(ctx, tx, imported, id, p, base+1, false)
	}))
}

// Archive takes a skill out of the list; its versions stay for the workflows that pin them.
func (s *Service) Archive(ctx context.Context, id uuid.UUID) error {
	return s.setArchived(ctx, id, true)
}

// Restore takes a skill out of the archive, which is how archiving is undone; it takes what archiving takes.
func (s *Service) Restore(ctx context.Context, id uuid.UUID) error {
	return s.setArchived(ctx, id, false)
}

func (s *Service) setArchived(ctx context.Context, id uuid.UUID, archive bool) error {
	p, k, err := s.allowed(ctx, id, func(k Skill) bool { return k.CanAdminister })
	if err != nil {
		return err
	}
	if (k.ArchivedAt != nil) == archive {
		return nil
	}
	statement, action := `UPDATE skill SET archived_at = now() WHERE id = $1`, audit.ActionSkillArchived
	if !archive {
		statement, action = `UPDATE skill SET archived_at = NULL WHERE id = $1`, audit.ActionSkillRestored
	}
	return refusal(s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, statement, id); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: action, TargetType: "skill", TargetID: id.String(),
			Actor: p.UserID, Data: map[string]any{"name": k.Name}})
	}))
}

// allowed is a skill the person on the context reads and whose grants let them do what may asks.
func (s *Service) allowed(ctx context.Context, id uuid.UUID, may func(Skill) bool) (*auth.Principal, Skill, error) {
	k, err := s.Skill(ctx, id)
	if err != nil {
		return nil, k, err
	}
	p := auth.PrincipalFrom(ctx)
	switch {
	case k.Builtin:
		return nil, k, ErrBuiltin
	case p == nil || !may(k):
		return nil, k, ErrForbidden
	}
	return p, k, nil
}

// refusal names what the database refused: a name taken, a save overtaken by another, or a write the policies forbid.
func refusal(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch {
		case pg.ConstraintName == nameUnique:
			return &Invalid{Problems: []Problem{{Code: CodeNameTaken, Path: SkillFile,
				Message: "Another skill already has this name, and workflows name skills by it. Choose another name."}}}
		case pg.ConstraintName == inSequence || pg.ConstraintName == versionKey:
			return ErrStale
		case pg.Code == "42501":
			return ErrForbidden
		}
	}
	return err
}
