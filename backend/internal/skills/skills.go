// Package skills keeps the skill library (#219): skills in the Agent Skills format, saved as immutable, numbered
// versions of their files and shared through grants, which the database's policies ask too.
package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// Errors a caller tells apart; *Invalid lists what is wrong with a version's files.
var (
	ErrNotFound = errors.New("no such skill")
	// ErrForbidden is a skill the caller reads but whose grants do not let them change it this way.
	ErrForbidden = errors.New("the caller's grants do not let them change the skill this way")
	// ErrCannotCreate is a caller whose grants do not let them create skills.
	ErrCannotCreate = errors.New("the caller's grants do not let them create skills")
	// ErrStale is a save based on a version that is no longer the current one.
	ErrStale = errors.New("the skill changed since it was opened")
	// ErrArchived is a save to an archived skill.
	ErrArchived = errors.New("the skill is archived")
	// ErrBuiltin is a change to a skill that ships with Rotor, which nobody makes through the API.
	ErrBuiltin = errors.New("the skill ships with Rotor")
)

// Invalid is a version with problems, which a save refuses.
type Invalid struct{ Problems []Problem }

func (e *Invalid) Error() string {
	if len(e.Problems) == 0 {
		return "the skill has problems"
	}
	return fmt.Sprintf("the skill has %d problems, the first: %s", len(e.Problems), e.Problems[0].Message)
}

// Skill is a skill as its readers see it.
type Skill struct {
	ID          uuid.UUID
	Name        string
	Description string
	// OwnerID is nil for a built-in skill, which nobody owns.
	OwnerID        *uuid.UUID
	OwnerName      string
	CurrentVersion int
	UpdatedAt      time.Time
	ArchivedAt     *time.Time
	Builtin        bool
	// What the caller's grants let them do with it, as the database's policies answer.
	CanEdit, CanAdminister bool
}

// FileInfo is a file as a version lists it.
type FileInfo struct {
	Path   string
	Bytes  int
	SHA256 string
}

// FileContent is one file with its text.
type FileContent struct {
	FileInfo
	Content string
}

// Version is one saved version of a skill; Files is filled only where one version is read.
type Version struct {
	Number        int
	Description   string
	FrontMatter   map[string]any
	Hash          string
	Bytes         int
	Comment       string
	CreatedByID   *uuid.UUID
	CreatedByName string
	CreatedAt     time.Time
	Files         []FileInfo
}

// Filter narrows and pages the listing.
type Filter struct {
	Archived bool
	// Text matches a name or a description.
	Text   string
	Limit  int
	Offset int
}

// Page is one page of skills plus how many matched in all.
type Page struct {
	Skills []Skill
	Total  int
	Limit  int
	Offset int
}

// Service is the skill library.
type Service struct {
	db *db.Cluster
}

// NewService returns the library over the cluster.
func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// A built-in skill is edited by nobody, whatever grants say, so the flags ask that too.
const skillColumns = `s.id, s.name, s.description, s.owner_id, s.owner_name, s.current_version, s.updated_at, s.archived_at,
	s.builtin, NOT s.builtin AND actor_can_skill('skill.edit', s.id), NOT s.builtin AND actor_can_skill('skill.administer', s.id)`

func scanSkill(row pgx.Row) (Skill, error) {
	var k Skill
	err := row.Scan(&k.ID, &k.Name, &k.Description, &k.OwnerID, &k.OwnerName, &k.CurrentVersion, &k.UpdatedAt, &k.ArchivedAt,
		&k.Builtin, &k.CanEdit, &k.CanAdminister)
	if errors.Is(err, pgx.ErrNoRows) {
		return k, ErrNotFound
	}
	return k, err
}

func readSkill(ctx context.Context, tx db.DBTX, id uuid.UUID) (Skill, error) {
	return scanSkill(tx.QueryRow(ctx, `SELECT `+skillColumns+` FROM skill s WHERE s.id = $1`, id))
}

// List is one page of the skills the person on the context reads, built-in ones included, by name.
func (s *Service) List(ctx context.Context, f Filter) (Page, error) {
	if f.Limit <= 0 || f.Limit > config.SkillMaxPage {
		f.Limit = config.SkillPageSize
	}
	f.Offset = max(f.Offset, 0)
	page := Page{Skills: []Skill{}, Limit: f.Limit, Offset: f.Offset}
	const where = ` FROM skill s WHERE (s.archived_at IS NOT NULL) = $1
		AND ($2 = '' OR strpos(s.name, lower($2)) > 0 OR strpos(lower(s.description), lower($2)) > 0)`
	text := strings.TrimSpace(f.Text)
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := tx.QueryRow(ctx, `SELECT count(*)`+where, f.Archived, text).Scan(&page.Total); err != nil {
			return fmt.Errorf("count skills: %w", err)
		}
		rows, err := tx.Query(ctx, `SELECT `+skillColumns+where+` ORDER BY s.name LIMIT $3 OFFSET $4`, f.Archived, text, f.Limit, f.Offset)
		if err != nil {
			return fmt.Errorf("list skills: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			k, err := scanSkill(rows)
			if err != nil {
				return err
			}
			page.Skills = append(page.Skills, k)
		}
		return rows.Err()
	})
	return page, err
}

// Skill is one skill as the person on the context reads it, without its files.
func (s *Service) Skill(ctx context.Context, id uuid.UUID) (Skill, error) {
	var k Skill
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		k, err = readSkill(ctx, tx, id)
		return err
	})
	return k, err
}

// Get is a skill with its current version and that version's files, as the person on the context reads it.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Skill, Version, error) {
	var (
		k Skill
		v Version
	)
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if k, err = readSkill(ctx, tx, id); err != nil {
			return err
		}
		v, err = version(ctx, tx, id, k.CurrentVersion)
		return err
	})
	return k, v, err
}

// Versions lists a skill's versions, newest first, without their files.
func (s *Service) Versions(ctx context.Context, id uuid.UUID) ([]Version, error) {
	out := []Version{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := readSkill(ctx, tx, id); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT `+versionColumns+` FROM skill_version v WHERE v.skill_id = $1 ORDER BY v.number DESC`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v, err := scanVersion(rows)
			if err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}

// Version is one version with its file list.
func (s *Service) Version(ctx context.Context, id uuid.UUID, number int) (Version, error) {
	var v Version
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		v, err = version(ctx, tx, id, number)
		return err
	})
	return v, err
}

// File is one file of a version with its text.
func (s *Service) File(ctx context.Context, id uuid.UUID, number int, path string) (FileContent, error) {
	var f FileContent
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `SELECT path, bytes, sha256, content FROM skill_file WHERE skill_id = $1 AND number = $2 AND path = $3`,
			id, number, path).Scan(&f.Path, &f.Bytes, &f.SHA256, &f.Content)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	return f, err
}

const versionColumns = `v.number, v.description, v.front_matter, v.hash, v.bytes, v.comment, v.created_by, v.created_by_name,
	v.created_at`

func scanVersion(row pgx.Row) (Version, error) {
	var (
		v           Version
		frontMatter []byte
	)
	err := row.Scan(&v.Number, &v.Description, &frontMatter, &v.Hash, &v.Bytes, &v.Comment, &v.CreatedByID, &v.CreatedByName, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	return v, json.Unmarshal(frontMatter, &v.FrontMatter)
}

// version reads one version and lists its files, SKILL.md first.
func version(ctx context.Context, tx db.DBTX, id uuid.UUID, number int) (Version, error) {
	v, err := scanVersion(tx.QueryRow(ctx, `SELECT `+versionColumns+` FROM skill_version v WHERE v.skill_id = $1 AND v.number = $2`,
		id, number))
	if err != nil {
		return v, err
	}
	rows, err := tx.Query(ctx, `SELECT path, bytes, sha256 FROM skill_file WHERE skill_id = $1 AND number = $2
		ORDER BY (path <> $3), path`, id, number, SkillFile)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	v.Files = []FileInfo{}
	for rows.Next() {
		var f FileInfo
		if err := rows.Scan(&f.Path, &f.Bytes, &f.SHA256); err != nil {
			return v, err
		}
		v.Files = append(v.Files, f)
	}
	return v, rows.Err()
}
