package skills

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// auditImport records that a zip made the version, when the save was an import.
func auditImport(ctx context.Context, tx db.DBTX, imported bool, id uuid.UUID, p *auth.Principal, version int, created bool) error {
	if !imported {
		return nil
	}
	return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionSkillImported, TargetType: "skill", TargetID: id.String(),
		Actor: p.UserID, Data: map[string]any{"version": version, "created": created}})
}

// Import reads a zip as the checks read files and makes the skill it names, or saves the next version of the one with
// that name; created says which.
func (s *Service) Import(ctx context.Context, zipped []byte, comment string) (id uuid.UUID, created bool, err error) {
	files, err := ReadZip(zipped)
	if err != nil {
		return uuid.Nil, false, err
	}
	draft, problems := Check(files)
	if len(problems) > 0 {
		return uuid.Nil, false, &Invalid{Problems: problems}
	}
	existing, err := s.byName(ctx, draft.Name)
	switch {
	case err != nil:
		return uuid.Nil, false, err
	case existing != nil:
		return existing.ID, false, s.save(ctx, existing.ID, existing.CurrentVersion, files, comment, true)
	}
	id, err = s.create(ctx, files, comment, true)
	var invalid *Invalid
	if errors.As(err, &invalid) && len(invalid.Problems) > 0 && invalid.Problems[0].Code == CodeNameTaken {
		// The name is held by a skill the caller cannot read, which they are not told exists.
		return uuid.Nil, false, ErrNotFound
	}
	return id, err == nil, err
}

// byName is the skill with the name that the caller reads, or nil.
func (s *Service) byName(ctx context.Context, name string) (*Skill, error) {
	var found *Skill
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		k, err := scanSkill(tx.QueryRow(ctx, `SELECT `+skillColumns+` FROM skill s WHERE s.name = $1`, name))
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		found = &k
		return err
	})
	return found, err
}

// Export is a version of a skill the caller reads, as a zip of its files under a folder named after the skill.
func (s *Service) Export(ctx context.Context, id uuid.UUID, number int) (name string, zipped []byte, err error) {
	var files []File
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		k, err := readSkill(ctx, tx, id)
		if err != nil {
			return err
		}
		name = k.Name
		if _, err := version(ctx, tx, id, number); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT path, content FROM skill_file WHERE skill_id = $1 AND number = $2`, id, number)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var f File
			if err := rows.Scan(&f.Path, &f.Content); err != nil {
				return err
			}
			files = append(files, f)
		}
		return rows.Err()
	})
	if err != nil {
		return "", nil, err
	}
	zipped, err = WriteZip(name, files)
	return name, zipped, err
}
