package config

import (
	"fmt"
	"regexp"
	"time"
)

// Approvals is how long an approval waits when its node sets no expiresIn (#31).
type Approvals struct {
	TTL time.Duration
}

// Approvals (#31); docs/api-contract-m5.md describes them.
const (
	// DefaultApprovalTTL covers a weekend.
	DefaultApprovalTTL = 72 * time.Hour
	// MaxApprovalExpiry is the longest an approval may wait: as long as a run may wait at all.
	MaxApprovalExpiry = WaitingTTL
	// MaxApprovalPreviewBytes bounds the writes an approval shows; a larger preview is cut and says so.
	MaxApprovalPreviewBytes = 32 << 10
	// MaxApprovalMessageChars bounds the message an approval shows, as the spec bounds the template.
	MaxApprovalMessageChars = 2000
	// MaxApprovalCommentChars bounds an approver's comment.
	MaxApprovalCommentChars = 2000
	// MaxApprovalValueBytes bounds the values an approver sends.
	MaxApprovalValueBytes = 256 << 10
	// ApprovalListLimit bounds the approvals one list answers.
	ApprovalListLimit = 200
)

// ReviewDecidingCalls matches the MCP calls that would decide a review in a tool, which no workflow may bind:
// approving a review is a person's act in the tool's own browser (Exciter's decision #182).
var ReviewDecidingCalls = regexp.MustCompile(`(?i)^(approve|reject|decide|sign|submit)_?\w*review`)

func loadApprovals(l *loader) Approvals {
	return Approvals{TTL: l.duration("ROTOR_APPROVAL_TTL", DefaultApprovalTTL)}
}

func checkApprovals(l *loader, c Config) {
	if c.Approvals.TTL <= 0 || c.Approvals.TTL > MaxApprovalExpiry {
		l.problem(fmt.Sprintf("ROTOR_APPROVAL_TTL is %s; set it to a duration longer than zero and at most %s, such as 72h.",
			c.Approvals.TTL, MaxApprovalExpiry))
	}
}
