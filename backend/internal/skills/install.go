package skills

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// installLock is held while the shipped skills are installed, so api replicas starting together take turns.
const installLock = "rotor.skills.builtin"

// InstalledComment is the comment of a version the api installed, which the skill's Versions show.
const InstalledComment = "Shipped with Rotor."

// ErrShippedChanged is a shipped skill whose declared version is installed with other files.
var ErrShippedChanged = errors.New("a shipped skill changed without a new metadata.version")

// Outcome is what installing did with one shipped skill.
type Outcome string

// What installing a shipped skill can come to.
const (
	// Added is a version the api installed, making the skill when it was missing.
	Added Outcome = "added"
	// Present is a version installed before with the same files.
	Present Outcome = "present"
	// Superseded is a version older than the current one and never installed, as after a newer Rotor ran first.
	Superseded Outcome = "superseded"
	// NameTaken is a name a skill somebody made holds, which no built-in may take over.
	NameTaken Outcome = "name_taken"
)

// Installed is one shipped skill and what installing it came to.
type Installed struct {
	Name    string
	Version int
	Outcome Outcome
}

// InstallBuiltins installs the skills Rotor ships, as the api does at every start, and logs what changed.
func (s *Service) InstallBuiltins(ctx context.Context, log *slog.Logger) error {
	shipped, err := Builtins()
	if err != nil {
		return err
	}
	installed, err := s.Install(ctx, shipped)
	if err != nil {
		return err
	}
	for _, one := range installed {
		switch one.Outcome {
		case Added:
			log.Info("installed a built-in skill", "skill", one.Name, "version", one.Version)
		case Superseded:
			log.Info("a newer version of a built-in skill is installed, so the older one this Rotor ships is left out",
				"skill", one.Name, "version", one.Version)
		case NameTaken:
			log.Warn("a skill somebody made has the name of a skill Rotor ships, so the shipped one is not installed; "+
				"a workflow that pins the name gets that skill instead", "skill", one.Name, "version", one.Version)
		}
	}
	return nil
}

// Install adds each shipped skill's declared version as a built-in, as rotor_admin, in one transaction; a version
// installed with other files refuses the whole start rather than meaning two things.
func (s *Service) Install(ctx context.Context, shipped []Shipped) ([]Installed, error) {
	var out []Installed
	err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		out = out[:0]
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, installLock); err != nil {
			return err
		}
		for _, one := range shipped {
			outcome, err := installOne(ctx, tx, one)
			if err != nil {
				return err
			}
			out = append(out, Installed{Name: one.Draft.Name, Version: one.Version, Outcome: outcome})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// installOne makes the built-in skill when it is missing and adds its declared version when that is.
func installOne(ctx context.Context, tx db.DBTX, one Shipped) (Outcome, error) {
	d := one.Draft
	var (
		id      uuid.UUID
		builtin bool
		current int
	)
	err := tx.QueryRow(ctx, `SELECT id, builtin, current_version FROM skill WHERE name = $1`, d.Name).Scan(&id, &builtin, &current)
	if errors.Is(err, pgx.ErrNoRows) {
		// Somebody may save a skill of the same name meanwhile, which the lock does not hold back; theirs then stands.
		err = tx.QueryRow(ctx, `INSERT INTO skill (name, description, builtin) VALUES ($1, $2, true)
			ON CONFLICT ON CONSTRAINT skill_name_unique DO NOTHING RETURNING id`, d.Name, d.Description).Scan(&id)
		builtin = err == nil
		if errors.Is(err, pgx.ErrNoRows) {
			err = nil
		}
	}
	switch {
	case err != nil:
		return "", err
	case !builtin:
		return NameTaken, nil
	}
	var hash string
	err = tx.QueryRow(ctx, `SELECT hash FROM skill_version WHERE skill_id = $1 AND number = $2`, id, one.Version).Scan(&hash)
	switch {
	case err == nil && hash == d.Hash:
		return Present, nil
	case err == nil:
		return "", fmt.Errorf("%w: version %d of %s is installed with other files; give the shipped skill %s a new "+
			"metadata.version, or put version %d back as it shipped", ErrShippedChanged, one.Version, d.Name, d.Name, one.Version)
	case !errors.Is(err, pgx.ErrNoRows):
		return "", err
	case one.Version < current:
		return Superseded, nil
	}
	// A built-in skill has no author, so the version names nobody.
	if err := insertVersion(ctx, tx, id, one.Version, d, InstalledComment, nil); err != nil {
		return "", err
	}
	return Added, audit.Write(ctx, tx, audit.Entry{Action: audit.ActionSkillInstalled, TargetType: "skill", TargetID: id.String(),
		Data: map[string]any{"name": d.Name, "version": one.Version}})
}
