package skills

import (
	"context"
	"slices"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/textdiff"
)

// What happened to a file between two versions.
const (
	FileAdded   = "added"
	FileRemoved = "removed"
	FileChanged = "changed"
)

// FileDiff is one file that differs between two versions; an added or removed one has all its lines in hunks.
type FileDiff struct {
	Path   string
	Status string
	Hunks  []textdiff.Hunk
}

// Diff is what changed between two versions, file by file, by path; files alike in both are left out.
type Diff struct {
	From, To int
	Files    []FileDiff
}

// DiffFiles compares two versions' files.
func DiffFiles(from, to []File) []FileDiff {
	before, after := map[string]string{}, map[string]string{}
	var paths []string
	for _, f := range from {
		before[f.Path] = f.Content
		paths = append(paths, f.Path)
	}
	for _, f := range to {
		after[f.Path] = f.Content
		if _, seen := before[f.Path]; !seen {
			paths = append(paths, f.Path)
		}
	}
	slices.Sort(paths)
	out := []FileDiff{}
	for _, path := range paths {
		old, hadIt := before[path]
		updated, hasIt := after[path]
		switch {
		case !hasIt:
			out = append(out, FileDiff{Path: path, Status: FileRemoved, Hunks: textdiff.LineDiff(old, "")})
		case !hadIt:
			out = append(out, FileDiff{Path: path, Status: FileAdded, Hunks: textdiff.LineDiff("", updated)})
		case old != updated:
			out = append(out, FileDiff{Path: path, Status: FileChanged, Hunks: textdiff.LineDiff(old, updated)})
		}
	}
	return out
}

// Diff answers what changed between two versions of a skill the caller reads.
func (s *Service) Diff(ctx context.Context, id uuid.UUID, from, to int) (Diff, error) {
	var a, b []File
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if a, err = files(ctx, tx, id, from); err != nil {
			return err
		}
		b, err = files(ctx, tx, id, to)
		return err
	})
	if err != nil {
		return Diff{}, err
	}
	return Diff{From: from, To: to, Files: DiffFiles(a, b)}, nil
}

// files is a version's files with their text; a version the caller cannot read is not found.
func files(ctx context.Context, tx db.DBTX, id uuid.UUID, number int) ([]File, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM skill_version WHERE skill_id = $1 AND number = $2)`, id, number).
		Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := tx.Query(ctx, `SELECT path, content FROM skill_file WHERE skill_id = $1 AND number = $2`, id, number)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []File
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.Path, &f.Content); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
