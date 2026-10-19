package runnerctl

import (
	"context"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/envelope"
	"github.com/cloudster1/rotor/backend/internal/redact"
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
func openApproval(ctx context.Context, tx db.DBTX, keys *envelope.Keys, r *run, output []byte, ttl time.Duration) error {
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
	box, err := keys.For(ctx, r.ID, r.dataKey)
	if err != nil {
		return err
	}
	// Redacted, then sealed with the run's key and bound to the interrupt, like the rest of the run's data (#91).
	message := box.Seal(envelope.ColumnApprovalMessage, v.ID, []byte(cut(redact.Text(v.Message), config.MaxApprovalMessageChars)))
	values := box.Seal(envelope.ColumnApprovalValues, v.ID, redact.JSON(objectOr(v.Values, "{}")))
	preview := box.Seal(envelope.ColumnApprovalPreview, v.ID, cutPreview(redact.JSON(objectOr(v.Preview, "[]"))))
	var id uuid.UUID
	var expires time.Time
	err = tx.QueryRow(ctx, `INSERT INTO approval (run_id, starter_id, workflow_id, workflow_name, started_by_name, node,
			interrupt_id, sealed_message, sealed_preview, form_schema, sealed_form_values, approver_group, expires_at)
		SELECT r.id, r.user_id, r.workflow_id, r.workflow_name, r.started_by_name, $2, $3, $4, $5, $6, $7, $8, now() + $9::interval
		FROM run r WHERE r.id = $1
		ON CONFLICT (run_id, interrupt_id) DO NOTHING
		RETURNING id, expires_at`,
		r.ID, node.ID, v.ID, message, preview, nullJSON(schema), values, group, node.ExpiresAfter(ttl).String()).Scan(&id, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"approvalId": id.String(), "node": node.ID, "message": v.Message,
		"approvers": map[string]any{"group": group}, "expiresAt": expires})
	if err := appendEvent(ctx, tx, keys, r.ID, EventApprovalRequested, node.ID, payload); err != nil {
		return err
	}
	return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionApprovalRequested, TargetType: "approval", TargetID: id.String(),
		Data: map[string]any{"run": r.ID.String(), "node": node.ID, "group": group}})
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
func Decide(ctx context.Context, tx db.DBTX, keys *envelope.Keys, runID uuid.UUID, d Decided) error {
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
	if err := appendEvent(ctx, tx, keys, runID, EventApprovalDecided, "", payload); err != nil {
		return err
	}
	if !d.Approve {
		return endLocked(ctx, tx, keys, runID, StatusCancelled, ReasonRejected, map[string]any{"by": d.By.String()}, nil)
	}
	_, err = tx.Exec(ctx, `UPDATE run SET status = 'starting', attempts = 0, sandbox_id = NULL WHERE id = $1`, runID)
	return err
}

// approved is the run's latest approved approval as stored, which answers the interrupt a resumed sandbox stops at.
type approved struct {
	interrupt, comment, decidedBy string
	sealedValues                  []byte
}

// latestApproved reads the run's latest approved approval, nil when it has none.
func latestApproved(ctx context.Context, tx db.DBTX, runID uuid.UUID) (*approved, error) {
	var a approved
	err := tx.QueryRow(ctx, `SELECT interrupt_id, comment, decided_by_name, sealed_approved_values
		FROM approval WHERE run_id = $1 AND status = 'approved' ORDER BY decided_at DESC LIMIT 1`, runID).
		Scan(&a.interrupt, &a.comment, &a.decidedBy, &a.sealedValues)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &a, err
}

// answer is what a resumed sandbox hands the interrupt: the decision, the approved values opened with the run's key,
// the comment and who decided.
func (a *approved) answer(box *envelope.Box) ([]byte, error) {
	values, err := box.OpenOr(envelope.ColumnApprovalApproved, a.interrupt, a.sealedValues)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"approved": true, "values": json.RawMessage(values), "comment": a.comment,
		"decidedBy": a.decidedBy})
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

// cut keeps at most n characters of s.
func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
