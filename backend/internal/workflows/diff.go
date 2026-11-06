package workflows

import "github.com/cloudster1/rotor/backend/internal/textdiff"

// The diff lives in textdiff, which skills share; these names keep the workflows' callers as they were.
const (
	OpEqual  = textdiff.OpEqual
	OpInsert = textdiff.OpInsert
	OpDelete = textdiff.OpDelete
)

type (
	// Line is one line of a diff and what happened to it.
	Line = textdiff.Line
	// Hunk is a run of changes with the unchanged lines around them.
	Hunk = textdiff.Hunk
)

// LineDiff answers what changed from one text to another, line by line, in hunks with context.
func LineDiff(from, to string) []Hunk { return textdiff.LineDiff(from, to) }
