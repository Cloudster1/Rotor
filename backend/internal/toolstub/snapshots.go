package toolstub

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

// The tools/list snapshots a stub serves, by the tool they were recorded from (make tool-snapshots), and what
// callers wrote at a tenant through the tools that write, which tests read back through the control API (#129).

// The snapshots by name; a tenant serves one, chosen by its snapshot option.
const (
	SnapshotExciter  = "exciter"
	SnapshotArmature = "armature"
)

// snapshot is the list a tenant serves: its option, else Armature's for an Armature, else Exciter's.
func (t *tenant) snapshot() string {
	switch {
	case t.opts.Snapshot != "":
		return t.opts.Snapshot
	case t.armature():
		return SnapshotArmature
	}
	return SnapshotExciter
}

// toolsOf is the recorded list the tenant serves.
func (s *Stub) toolsOf(t *tenant) []map[string]any {
	return s.snapshots[t.snapshot()]
}

// HasSnapshot says whether the stub was given the named snapshot.
func (s *Stub) HasSnapshot(name string) bool {
	_, ok := s.snapshots[name]
	return ok
}

// records are what callers wrote at a tenant, oldest first, in the shapes the tool answered them in.
type records struct {
	comments      map[string][]map[string]any
	remoteLinks   map[string][]map[string]any
	requirements  []*storedRequirement
	armatureLinks map[string][]map[string]any
	reviews       []map[string]any
}

func newRecords() *records {
	return &records{comments: map[string][]map[string]any{}, remoteLinks: map[string][]map[string]any{},
		armatureLinks: map[string][]map[string]any{}}
}

// view is the records as the control API answers them.
func (r *records) view() map[string]any {
	reqs := make([]map[string]any, 0, len(r.requirements))
	for _, req := range r.requirements {
		reqs = append(reqs, req.answer())
	}
	reviews := r.reviews
	if reviews == nil {
		reviews = []map[string]any{}
	}
	return map[string]any{"comments": r.comments, "remoteLinks": r.remoteLinks, "requirements": reqs,
		"armatureLinks": r.armatureLinks, "reviews": reviews}
}

// stubID is an id derived from what it names, so a test can expect it and two tenants never share one.
func stubID(parts ...string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(strings.Join(parts, ":"))).String()
}

// userID is a person's id in every tool the stub plays, as whoami names it.
func userID(username string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("user:"+username)).String()
}

// stubNow is the time a write is stamped with; the stub's clock, in the format both tools answer.
func (s *Stub) stubNow() string { return s.now().UTC().Format(time.RFC3339Nano) }

// answered wraps a structured answer as a tool result, its text the same JSON.
func answered(structured map[string]any) map[string]any {
	text, _ := json.Marshal(structured)
	return map[string]any{"content": []toolContent{{Type: "text", Text: string(text)}}, "structuredContent": structured}
}

// textDocument is plain words as the one-paragraph rich text document Armature stores them as.
func textDocument(text string) map[string]any {
	return map[string]any{"type": "doc", "content": []any{paragraph(text)}}
}

// markdownDocument is Markdown as a rich text document, a paragraph per block between blank lines, which is as much
// of Markdown as a test needs; nil when it says nothing, as Armature's converter answers.
func markdownDocument(markdown string) map[string]any {
	content := []any{}
	for _, block := range strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n\n") {
		if block = strings.TrimSpace(block); block != "" {
			content = append(content, paragraph(block))
		}
	}
	if len(content) == 0 {
		return nil
	}
	return map[string]any{"type": "doc", "content": content}
}

func paragraph(text string) map[string]any {
	return map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": text}}}
}
