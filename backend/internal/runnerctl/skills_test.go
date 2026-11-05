package runnerctl

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/runnerpb"
	"github.com/cloudster1/rotor/backend/internal/workflow/spec"
)

const skillsSpec = `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: skilled}
limits: {maxSteps: 20, timeout: 5m}
entry: analyse
nodes:
  - id: analyse
    type: agent
    prompt: Analyse.
    skills:
      - {name: requirements-engineering, version: 2}
      - {name: test-engineering, version: 1}
`

// library is a skill library in memory: files by skill, version and path, and the skills each person reads.
type library struct {
	files   map[spec.SkillRef]map[string]string
	readers map[string][]uuid.UUID
}

func (l *library) Readable(_ context.Context, user uuid.UUID, names []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, name := range names {
		for _, reader := range l.readers[name] {
			if reader == user {
				out[name] = true
			}
		}
	}
	return out, nil
}

func (l *library) Card(_ context.Context, ref spec.SkillRef) (*runnerpb.SkillCard, error) {
	card := &runnerpb.SkillCard{Name: ref.Name, Version: int32(ref.Version), Description: "About " + ref.Name}
	for path, content := range l.files[ref] {
		card.Files = append(card.Files, &runnerpb.SkillFileInfo{Path: path, Bytes: int64(len(content))})
	}
	return card, nil
}

func (l *library) File(_ context.Context, ref spec.SkillRef, path string) (string, bool, error) {
	content, ok := l.files[ref][path]
	return content, ok, nil
}

func skilledRun(t *testing.T) (*Server, *run, *library) {
	t.Helper()
	parsed, problems := spec.Parse([]byte(skillsSpec))
	if parsed == nil || len(problems) > 0 {
		t.Fatalf("the spec does not parse: %v", problems)
	}
	r := &run{ID: uuid.New(), UserID: uuid.New(), spec: parsed}
	// A long file of two-byte characters, so a cut at the limit would split one if nothing moved it.
	long := "x" + strings.Repeat("é", config.MaxSkillReadBytes)
	lib := &library{
		files: map[spec.SkillRef]map[string]string{
			{Name: "requirements-engineering", Version: 2}: {"SKILL.md": "Write requirements.", "references/long.md": long},
			{Name: "requirements-engineering", Version: 1}: {"SKILL.md": "An older version."},
			{Name: "test-engineering", Version: 1}:         {"SKILL.md": "Test them."},
		},
		readers: map[string][]uuid.UUID{"requirements-engineering": {r.UserID}, "test-engineering": {r.UserID}},
	}
	s := &Server{Skills: lib, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	return s, r, lib
}

func read(s *Server, r *run, name string, version int32, path string, offset int64) *runnerpb.SkillFileResponse {
	return s.skillFile(context.Background(), r, &runnerpb.SkillFileRequest{RequestId: "q", Name: name, Version: version,
		Path: path, Offset: offset}).GetSkillFile()
}

func TestAPinnedSkillReadableByTheRunsPersonIsAnswered(t *testing.T) {
	s, r, _ := skilledRun(t)
	got := read(s, r, "requirements-engineering", 2, "SKILL.md", 0)
	if got.Refusal != "" || got.Content != "Write requirements." || got.Total != 19 || got.Offset != 0 || got.RequestId != "q" {
		t.Fatalf("answer = %+v", got)
	}
}

func TestASkillVersionTheRunDoesNotPinIsRefused(t *testing.T) {
	s, r, _ := skilledRun(t)
	for _, version := range []int32{1, 3} {
		got := read(s, r, "requirements-engineering", version, "SKILL.md", 0)
		if got.Content != "" || !strings.Contains(got.Refusal, "does not give its agents version") {
			t.Fatalf("version %d answered %+v", version, got)
		}
	}
	if got := read(s, r, "other", 1, "SKILL.md", 0); !strings.Contains(got.Refusal, "skill other") {
		t.Fatalf("an unpinned skill answered %+v", got)
	}
}

func TestASkillThePersonMayNoLongerReadIsRefusedOnEachRead(t *testing.T) {
	s, r, lib := skilledRun(t)
	if got := read(s, r, "test-engineering", 1, "SKILL.md", 0); got.Refusal != "" {
		t.Fatalf("a readable skill was refused: %s", got.Refusal)
	}
	lib.readers["test-engineering"] = nil
	got := read(s, r, "test-engineering", 1, "SKILL.md", 0)
	if got.Content != "" || !strings.Contains(got.Refusal, "may no longer read the skill test-engineering") {
		t.Fatalf("answer = %+v", got)
	}
}

func TestAFileTheVersionLacksOrAnOffsetOutsideItIsRefused(t *testing.T) {
	s, r, _ := skilledRun(t)
	if got := read(s, r, "test-engineering", 1, "../SKILL.md", 0); !strings.Contains(got.Refusal, "has no file ../SKILL.md") {
		t.Fatalf("a missing file answered %+v", got)
	}
	for _, offset := range []int64{-1, 11} {
		if got := read(s, r, "test-engineering", 1, "SKILL.md", offset); !strings.Contains(got.Refusal, "outside it") {
			t.Fatalf("offset %d answered %+v", offset, got)
		}
	}
	if got := read(s, r, "test-engineering", 1, "SKILL.md", 10); got.Refusal != "" || got.Content != "" || got.Total != 10 {
		t.Fatalf("the end of the file answered %+v", got)
	}
}

func TestALongFileIsAnsweredInPartsCutBetweenCharacters(t *testing.T) {
	s, r, lib := skilledRun(t)
	whole := lib.files[spec.SkillRef{Name: "requirements-engineering", Version: 2}]["references/long.md"]
	var parts strings.Builder
	offset := int64(0)
	for range 4 {
		got := read(s, r, "requirements-engineering", 2, "references/long.md", offset)
		if got.Refusal != "" || got.Total != int64(len(whole)) || got.Offset != offset {
			t.Fatalf("at %d answered refusal %q, offset %d, total %d", offset, got.Refusal, got.Offset, got.Total)
		}
		if len(got.Content) > config.MaxSkillReadBytes || !utf8.ValidString(got.Content) {
			t.Fatalf("at %d the part has %d bytes, valid %v", offset, len(got.Content), utf8.ValidString(got.Content))
		}
		parts.WriteString(got.Content)
		offset += int64(len(got.Content))
		if offset == got.Total {
			break
		}
	}
	if parts.String() != whole {
		t.Fatalf("the parts read %d bytes of %d", parts.Len(), len(whole))
	}
	// An offset inside a character starts at the next one rather than mid-way.
	got := read(s, r, "requirements-engineering", 2, "references/long.md", 2)
	if got.Offset != 3 || !utf8.ValidString(got.Content) {
		t.Fatalf("offset 2 answered from %d", got.Offset)
	}
}

func TestStartRunCarriesTheSkillsTheRunsPersonReadsWithoutContent(t *testing.T) {
	s, r, lib := skilledRun(t)
	lib.readers["test-engineering"] = nil
	got, err := s.agentSkills(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	cards := got["analyse"].GetSkills()
	if len(got) != 1 || len(cards) != 1 || cards[0].Name != "requirements-engineering" || cards[0].Version != 2 ||
		cards[0].Description != "About requirements-engineering" || len(cards[0].Files) != 2 {
		t.Fatalf("agent skills = %v", got)
	}
	start := startRun(r, nil, nil, nil, got).GetStartRun()
	if start.AgentSkills["analyse"].GetSkills()[0] != cards[0] {
		t.Fatal("StartRun does not carry the agent's skills")
	}
}
