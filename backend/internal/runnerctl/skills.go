package runnerctl

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/runnerpb"
	"github.com/cloudster1/rotor/backend/internal/skills"
	"github.com/cloudster1/rotor/backend/internal/workflow/spec"
)

// SkillLibrary is what a run's skills hold (#222); dbSkills is the real one. Readable asks as the run's person, the
// other two read as rotor_admin, so they are asked only after Readable said yes.
type SkillLibrary interface {
	Readable(ctx context.Context, user uuid.UUID, names []string) (map[string]bool, error)
	Card(ctx context.Context, ref spec.SkillRef) (*runnerpb.SkillCard, error)
	File(ctx context.Context, ref spec.SkillRef, path string) (string, bool, error)
}

// agentSkills are the agents' skills for StartRun, without content. One the person may no longer read, as on a
// resume after a grant was revoked, is left out: the run still starts, without it.
func (s *Server) agentSkills(ctx context.Context, r *run) (map[string]*runnerpb.AgentSkills, error) {
	names := r.spec.SkillNames()
	if s.Skills == nil || len(names) == 0 {
		return nil, nil
	}
	readable, err := s.Skills.Readable(ctx, r.UserID, names)
	if err != nil {
		return nil, err
	}
	out := map[string]*runnerpb.AgentSkills{}
	for _, n := range r.spec.Nodes {
		if n.Type != spec.TypeAgent {
			continue
		}
		for _, ref := range n.Skills {
			if !readable[ref.Name] {
				s.Log.Warn("an agent starts without a skill its run's person may not read", "run", r.ID, "node", n.ID, "skill", ref.Name)
				continue
			}
			card, err := s.Skills.Card(ctx, ref)
			if err != nil {
				return nil, fmt.Errorf("read skill %s version %d: %w", ref.Name, ref.Version, err)
			}
			if out[n.ID] == nil {
				out[n.ID] = &runnerpb.AgentSkills{}
			}
			out[n.ID].Skills = append(out[n.ID].Skills, card)
		}
	}
	return out, nil
}

// pins says whether an agent of the run's pinned spec names that version of the skill.
func (r *run) pins(name string, version int) bool {
	for _, n := range r.spec.Nodes {
		for _, ref := range n.Skills {
			if n.Type == spec.TypeAgent && ref.Name == name && ref.Version == version {
				return true
			}
		}
	}
	return false
}

// skillFile answers a SkillFileRequest: part of a file of a skill version the run pins and its person may still
// read, or a refusal the agent reads as a sentence. It carries no seq: the runner asks again after a reconnect.
func (s *Server) skillFile(ctx context.Context, r *run, req *runnerpb.SkillFileRequest) *runnerpb.ConnectResponse {
	answer := &runnerpb.SkillFileResponse{RequestId: req.RequestId}
	answer.Refusal = s.readSkill(ctx, r, req, answer)
	return &runnerpb.ConnectResponse{Message: &runnerpb.ConnectResponse_SkillFile{SkillFile: answer}}
}

// readSkill fills answer with the file's part, or answers why it may not.
func (s *Server) readSkill(ctx context.Context, r *run, req *runnerpb.SkillFileRequest, answer *runnerpb.SkillFileResponse) string {
	ref := spec.SkillRef{Name: req.Name, Version: int(req.Version)}
	if !r.pins(ref.Name, ref.Version) {
		return fmt.Sprintf("This workflow does not give its agents version %d of the skill %s; read only the skills your instructions list.", ref.Version, ref.Name)
	}
	if s.Skills == nil {
		return "This control plane hands out no skills, so the skill cannot be read; carry on without it."
	}
	readable, err := s.Skills.Readable(ctx, r.UserID, []string{ref.Name})
	if err != nil {
		s.Log.Error("cannot check a run's skill", "run", r.ID, "skill", ref.Name, "error", err)
		return fmt.Sprintf("Rotor could not read the skill %s just now; try again.", ref.Name)
	}
	if !readable[ref.Name] {
		return fmt.Sprintf("The person this run acts for may no longer read the skill %s, so it cannot be read; carry on without it.", ref.Name)
	}
	content, found, err := s.Skills.File(ctx, ref, req.Path)
	if err != nil {
		s.Log.Error("cannot read a run's skill file", "run", r.ID, "skill", ref.Name, "error", err)
		return fmt.Sprintf("Rotor could not read the skill %s just now; try again.", ref.Name)
	}
	if !found {
		return fmt.Sprintf("Version %d of the skill %s has no file %s; read one of the files it lists.", ref.Version, ref.Name, req.Path)
	}
	total := int64(len(content))
	if req.Offset < 0 || req.Offset > total {
		return fmt.Sprintf("%s of the skill %s has %d bytes, so offset %d is outside it; read from an offset the file has.", req.Path, ref.Name, total, req.Offset)
	}
	answer.Content, answer.Offset = slice(content, req.Offset, config.MaxSkillReadBytes)
	answer.Total = total
	return ""
}

// slice is at most limit bytes of text from offset on, both ends moved onto a character's first byte so no
// character is split; it answers the part and where it starts.
func slice(text string, offset int64, limit int) (string, int64) {
	start := int(offset)
	for start < len(text) && !utf8.RuneStart(text[start]) {
		start++
	}
	end := min(start+limit, len(text))
	for end > start && end < len(text) && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[start:end], int64(start)
}

// dbSkills reads the library: whether the person reads a skill through the policies, as rotor_app acting for them,
// and the pinned version's card and files as rotor_admin.
type dbSkills struct{ db *db.Cluster }

func (d dbSkills) Readable(ctx context.Context, user uuid.UUID, names []string) (map[string]bool, error) {
	out := map[string]bool{}
	err := d.db.Read(db.WithUser(ctx, user), func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT name FROM skill WHERE name = ANY($1)`, names)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			out[name] = true
		}
		return rows.Err()
	})
	return out, err
}

func (d dbSkills) Card(ctx context.Context, ref spec.SkillRef) (*runnerpb.SkillCard, error) {
	card := &runnerpb.SkillCard{Name: ref.Name, Version: int32(ref.Version)}
	err := d.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `SELECT v.description FROM skill_version v JOIN skill s ON s.id = v.skill_id
			WHERE s.name = $1 AND v.number = $2`, ref.Name, ref.Version).Scan(&card.Description)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT f.path, f.bytes FROM skill_file f JOIN skill s ON s.id = f.skill_id
			WHERE s.name = $1 AND f.number = $2 ORDER BY (f.path <> $3), f.path`, ref.Name, ref.Version, skills.SkillFile)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			f := &runnerpb.SkillFileInfo{}
			if err := rows.Scan(&f.Path, &f.Bytes); err != nil {
				return err
			}
			card.Files = append(card.Files, f)
		}
		return rows.Err()
	})
	return card, err
}

func (d dbSkills) File(ctx context.Context, ref spec.SkillRef, path string) (string, bool, error) {
	var content string
	err := d.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT f.content FROM skill_file f JOIN skill s ON s.id = f.skill_id
			WHERE s.name = $1 AND f.number = $2 AND f.path = $3`, ref.Name, ref.Version, path).Scan(&content)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return content, err == nil, err
}
