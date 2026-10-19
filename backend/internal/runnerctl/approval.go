package runnerctl

import (
	"context"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/workflow/spec"
)

// Approval events on a run's stream and the reasons an approval ends a run (#31).
const (
	EventApprovalRequested = "approval.requested"
	EventApprovalDecided   = "approval.decided"
	ReasonRejected         = "rejected"
	ReasonApprovalExpired  = "approval_expired"
)

// ErrNotWaiting is a run no longer waiting for the approval being decided.
var ErrNotWaiting = errors.New("the run is not waiting for this approval")

// interruptValue is what the runner sends when it stops at an approval node: only what the state alone can say.
type interruptValue struct {
	ID      string          `json:"id"`
	Node    string          `json:"node"`
	Message string          `json:"message"`
	Values  json.RawMessage `json:"values"`
	Preview json.RawMessage `json:"preview"`
}

// openApproval records the approval a run now waits at. Who decides, the form and the expiry come from the pinned
// spec, never from the sandbox; an interrupt that names no approval node of it opens nothing.
func openApproval(ctx context.Context, tx db.DBTX, r *run, output []byte, ttl time.Duration) error {
	var v interruptValue
	if json.Unmarshal(output, &v) != nil || v.ID == "" || r.spec == nil {
		return nil
	}
	node, ok := approvalNode(r.spec, v.Node)
	if !ok {
		return nil
	}
	var group *string
	if node.Approvers != nil {
		group = &node.Approvers.Group
	}
	var schema []byte
	if node.Form != nil {
		schema = node.Form.Schema
	}
	values := redact(objectOr(v.Values, "{}"))
	preview := cutPreview(redact(objectOr(v.Preview, "[]")))
	var id uuid.UUID
	var expires time.Time
	err := tx.QueryRow(ctx, `INSERT INTO approval (run_id, starter_id, workflow_id, workflow_name, started_by_name, node,
			interrupt_id, message, preview, form_schema, form_values, approver_group, expires_at)
		SELECT r.id, r.user_id, r.workflow_id, r.workflow_name, r.started_by_name, $2, $3, $4, $5, $6, $7, $8, now() + $9::interval
		FROM run r WHERE r.id = $1
		ON CONFLICT (run_id, interrupt_id) DO NOTHING
		RETURNING id, expires_at`,
		r.ID, node.ID, v.ID, cut(string(redactText(v.Message)), config.MaxApprovalMessageChars), preview, nullJSON(schema), values,
		group, node.ExpiresAfter(ttl).String()).Scan(&id, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"approvalId": id.String(), "node": node.ID, "message": v.Message,
		"approvers": map[string]any{"group": group}, "expiresAt": expires})
	return appendEvent(ctx, tx, r.ID, EventApprovalRequested, node.ID, payload)
}

func approvalNode(s *spec.Spec, id string) (spec.Node, bool) {
	for _, n := range s.Nodes {
		if n.ID == id && n.Type == spec.TypeApproval {
			return n, true
		}
	}
	return spec.Node{}, false
}

// closeApprovals ends the approvals a run still waits for when it ends any other way: expired with it, or cancelled.
func closeApprovals(ctx context.Context, tx db.DBTX, runID uuid.UUID, status string) error {
	ended := "cancelled"
	if status == StatusExpired {
		ended = "expired"
	}
	_, err := tx.Exec(ctx, `UPDATE approval SET status = $2, decided_at = now() WHERE run_id = $1 AND status = 'pending'`, runID, ended)
	return err
}

// Decided is a decision as the run's stream tells it.
type Decided struct {
	ApprovalID uuid.UUID
	Approve    bool
	By         uuid.UUID
	ByName     string
	Comment    string
}

// Decide moves a waiting run on once its approval is decided: approved, a fresh sandbox resumes it from its
// checkpoint with the answer; rejected, it ends cancelled. The caller has recorded the decision in tx.
func Decide(ctx context.Context, tx db.DBTX, runID uuid.UUID, d Decided) error {
	current, err := lockStatus(ctx, tx, runID)
	if err != nil {
		return err
	}
	if current != StatusWaiting {
		return ErrNotWaiting
	}
	decision := "approve"
	if !d.Approve {
		decision = "reject"
	}
	payload, _ := json.Marshal(map[string]any{"approvalId": d.ApprovalID.String(), "decision": decision,
		"by": map[string]string{"id": d.By.String(), "name": d.ByName}, "comment": d.Comment})
	if err := appendEvent(ctx, tx, runID, EventApprovalDecided, "", payload); err != nil {
		return err
	}
	if !d.Approve {
		return endLocked(ctx, tx, runID, StatusCancelled, ReasonRejected, map[string]any{"by": d.By.String()}, nil)
	}
	_, err = tx.Exec(ctx, `UPDATE run SET status = 'starting', attempts = 0, sandbox_id = NULL WHERE id = $1`, runID)
	return err
}

// resumeAnswer is the answer of the run's latest approved approval, which a resumed sandbox hands its interrupt.
func resumeAnswer(ctx context.Context, tx db.DBTX, runID uuid.UUID) (string, []byte, error) {
	var interrupt string
	var answer []byte
	err := tx.QueryRow(ctx, `SELECT interrupt_id, jsonb_build_object('approved', true, 'values', approved_values,
			'comment', comment, 'decidedBy', decided_by_name)
		FROM approval WHERE run_id = $1 AND status = 'approved' ORDER BY decided_at DESC LIMIT 1`, runID).Scan(&interrupt, &answer)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, nil
	}
	return interrupt, answer, err
}

// cutPreview keeps a preview within config.MaxApprovalPreviewBytes, dropping writes from the end; a last entry
// marked truncated says some were dropped.
func cutPreview(preview []byte) []byte {
	if len(preview) <= config.MaxApprovalPreviewBytes {
		return preview
	}
	var writes []json.RawMessage
	if json.Unmarshal(preview, &writes) != nil {
		return []byte("[]")
	}
	note, _ := json.Marshal(map[string]any{"node": "", "binding": "", "tool": "", "arguments": nil, "truncated": true})
	for len(writes) > 0 {
		writes = writes[:len(writes)-1]
		if out, _ := json.Marshal(append(writes, note)); len(out) <= config.MaxApprovalPreviewBytes {
			return out
		}
	}
	return []byte("[" + string(note) + "]")
}

func objectOr(raw json.RawMessage, def string) []byte {
	if len(raw) == 0 || !json.Valid(raw) || string(raw) == "null" {
		return []byte(def)
	}
	return raw
}

func redactText(s string) []byte {
	out, _ := json.Marshal(s)
	var back string
	_ = json.Unmarshal(redact(out), &back)
	return []byte(back)
}

// cut keeps at most n characters of s.
func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
