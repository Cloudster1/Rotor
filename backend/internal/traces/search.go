package traces

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/observability"
)

// ErrSearchTooBroad is a search that ran past config.SpanSearchTimeout; a narrower one finds its spans sooner.
var ErrSearchTooBroad = errors.New("the span search ran out of time")

// The sentences a search's fields are refused with, which the API answers under each field's name.
const (
	sinceSentence     = "A search starts at most 30 days back. Choose a later start, or leave it out for the last 7 days."
	windowSentence    = "The search's end comes before its start. Choose an end after the start, or leave it out for now."
	kindSentence      = "That is not a span kind. Choose one of CHAIN, LLM, TOOL, AGENT, RETRIEVER, EMBEDDING, RERANKER, GUARDRAIL, EVALUATOR, PROMPT or UNKNOWN."
	statusSentence    = "A span's status is unset, ok or error. Choose one, or leave it out."
	textKeySentence   = "That attribute carries text a person, a tool or a model wrote, which Rotor seals and never searches. Choose an attribute without text, such as rotor.node or tool.name."
	keyLongSentence   = "An attribute's key is at most 128 characters. Check the key."
	valueSentence     = "Search an attribute by its key and its value together. Give both, or neither."
	valueLongSentence = "An attribute's value is at most 512 characters. Check the value."
	termLongSentence  = "A search term is at most 128 characters. Shorten it."
)

// SpanFilter is what a search asks of the spans the caller reads; empty fields do not narrow it.
type SpanFilter struct {
	// Name and Model match anywhere in the span's name or model, whatever the case.
	Name, Model string
	// Kind, Status and Service match exactly.
	Kind, Status, Service string
	Workflow              *uuid.UUID
	// AttrKey and AttrValue match one attribute without text exactly.
	AttrKey, AttrValue string
	// Since and Until bound the spans' start; a span started at Until is not in the window.
	Since, Until time.Time
	After        *Cursor
	Limit        int
}

// Cursor is the place of the last span a page answered, in the search's newest-first order.
type Cursor struct {
	At     time.Time `json:"at"`
	Trace  uuid.UUID `json:"trace"`
	SpanID string    `json:"span"`
}

// SpanHit is a span a search found, with the run or the sender's own trace it belongs to.
type SpanHit struct {
	SpanID, Name, Kind    string
	Service, Source       string
	Status, StatusMessage string
	StartedAt, EndedAt    time.Time
	Model                 string
	TraceID               string
	RunID                 *uuid.UUID
	// Workflow is the run's, and its Name empty when the caller reads the run but not its workflow.
	Workflow *WorkflowRef
	// Owner is the sender whose own trace holds the span, nil for a run's.
	Owner *Sender
	At    Cursor
}

// WorkflowRef names a run's workflow.
type WorkflowRef struct {
	ID   uuid.UUID
	Name string
}

// SpanPage is a page of hits, newest first; More says a next page may hold more, from the last hit's cursor.
type SpanPage struct {
	Hits []SpanHit
	More bool
}

// Window is the window a search reads when it names none or part of one, as of now.
func Window(since, until *time.Time, now time.Time) (time.Time, time.Time) {
	end := now
	if until != nil {
		end = *until
	}
	start := end.AddDate(0, 0, -config.SpanSearchDefaultDays)
	if since != nil {
		start = *since
	}
	return start, end
}

// Problems are the fields of f a search refuses, each with what to do, as of now; nil when it may run.
func (f SpanFilter) Problems(now time.Time) map[string]string {
	out := map[string]string{}
	if f.Since.Before(now.AddDate(0, 0, -config.SpanSearchMaxDays)) {
		out["since"] = sinceSentence
	}
	if !f.Until.After(f.Since) {
		out["until"] = windowSentence
	}
	if f.Kind != "" && !kinds[f.Kind] {
		out["kind"] = kindSentence
	}
	if f.Status != "" && f.Status != statusUnset && f.Status != statusOK && f.Status != statusError {
		out["status"] = statusSentence
	}
	for field, term := range map[string]string{"name": f.Name, "model": f.Model, "service": f.Service} {
		if utf8.RuneCountInString(term) > config.MaxSpanNameChars {
			out[field] = termLongSentence
		}
	}
	switch {
	case (f.AttrKey == "") != (f.AttrValue == ""):
		out["attrValue"] = valueSentence
	case utf8.RuneCountInString(f.AttrKey) > config.MaxSpanNameChars:
		out["attrKey"] = keyLongSentence
	case f.AttrKey != "" && observability.IsText(f.AttrKey):
		out["attrKey"] = textKeySentence
	case utf8.RuneCountInString(f.AttrValue) > config.MaxSpanAttrChars:
		out["attrValue"] = valueLongSentence
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// String is the cursor as a page answers it: opaque to the caller, who hands it back for the next page.
func (c Cursor) String() string {
	raw, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// ParseCursor reads a cursor a page answered; ok is false for anything else.
func ParseCursor(s string) (Cursor, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, false
	}
	var c Cursor
	if json.Unmarshal(raw, &c) != nil || c.At.IsZero() || c.Trace == uuid.Nil || !spanID(c.SpanID) {
		return Cursor{}, false
	}
	return c, true
}

// spanID says s is a span id as trace_span keeps one, so a made-up cursor never reaches the query.
func spanID(s string) bool {
	_, err := oteltrace.SpanIDFromHex(s)
	return err == nil
}
