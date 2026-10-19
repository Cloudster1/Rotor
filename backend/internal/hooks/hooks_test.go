package hooks

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// RFC 4231, test case 2: the vector Exciter's and Armature's signing is checked against.
func TestSignMatchesTheRFCVector(t *testing.T) {
	if got := Sign([]byte("what do ya want for nothing?"), "Jefe"); got != "sha256=5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843" {
		t.Errorf("Sign = %s", got)
	}
}

func TestATimestampedSignatureHoldsOnlyWithinTheWindow(t *testing.T) {
	body, secret, now := []byte(`{"id":"e1"}`), "exciter_whs_x", time.Unix(1_800_000_000, 0)
	window := 5 * time.Minute
	header := SignTimestamped(body, secret, now)
	ts := "1800000000"
	cases := []struct {
		name               string
		body               []byte
		secret, ts, header string
		now                time.Time
		want               bool
	}{
		{"as sent", body, secret, ts, header, now, true},
		{"four minutes late", body, secret, ts, header, now.Add(4 * time.Minute), true},
		{"six minutes late", body, secret, ts, header, now.Add(6 * time.Minute), false},
		{"from the future", body, secret, ts, header, now.Add(-6 * time.Minute), false},
		{"another body", []byte(`{"id":"e2"}`), secret, ts, header, now, false},
		{"another secret", body, "exciter_whs_y", ts, header, now, false},
		{"a padded timestamp", body, secret, "01800000000", header, now, false},
		{"no timestamp", body, secret, "", header, now, false},
		{"no secret", body, "", ts, header, now, false},
	}
	for _, c := range cases {
		if got := VerifyTimestamped(c.body, c.secret, c.ts, c.header, window, c.now); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestEachToolsEnvelopeReadsIntoOneEvent(t *testing.T) {
	v1 := `{"version":1,"id":"ev-1","topic":"issue.created","orgId":"o","occurredAt":"2026-10-19T08:00:00Z","payload":{"issue":{"key":"CP-12"}}}`
	ce := `{"specversion":"1.0","id":"ev-2","source":"https://exciter","type":"dev.exciter.review.decided","time":"2026-10-19T08:00:00Z",` +
		`"datacontenttype":"application/json","dataversion":1,"data":{"review":{"decision":"approve"}}}`
	for _, c := range []struct {
		name, body, header, product, topic, id string
		err                                    error
	}{
		{"armature v1", v1, "1", "armature", "issue.created", "ev-1", nil},
		{"stator v1 without a header", v1, "", "stator", "issue.created", "ev-1", nil},
		{"exciter cloudevents", ce, "2", "exciter", "review.decided", "ev-2", nil},
		{"cloudevents from armature", ce, "2", "armature", "", "", ErrUnknownVersion},
		{"v1 sent as 2", v1, "2", "exciter", "", "", ErrUnknownVersion},
		{"version 3", `{"version":3,"id":"e","topic":"a.b"}`, "3", "exciter", "", "", ErrUnknownVersion},
		{"no id", `{"version":1,"topic":"a.b"}`, "1", "exciter", "", "", ErrNotEnvelope},
		{"not json", `nope`, "1", "exciter", "", "", ErrNotEnvelope},
	} {
		e, err := Decode([]byte(c.body), c.header, c.product)
		if !errors.Is(err, c.err) || (err == nil && (e.Topic != c.topic || e.ID != c.id || e.Tool != c.product)) {
			t.Errorf("%s: %+v, %v", c.name, e, err)
		}
	}
}

func event(tool, topic, payload string) Event {
	return Event{Tool: tool, Topic: topic, ID: "e", Payload: json.RawMessage(payload)}
}

func TestAFilterMatchesTopicsProjectsAndConditions(t *testing.T) {
	approved := event("exciter", "review.decided", `{"review":{"decision":"approve","round":2},"requirement":{"project":{"key":"RTR"}}}`)
	issue := event("armature", "issue.created", `{"issue":{"key":"CP-12","priority":"high"}}`)
	page := event("stator", "page.published", `{"page":{"space":{"key":"DOC"}}}`)
	cond := func(path, op, value string) Condition {
		return Condition{Path: path, Op: op, Value: json.RawMessage(value)}
	}
	for _, c := range []struct {
		name   string
		filter Filter
		e      Event
		want   bool
	}{
		{"its topic", Filter{Topics: []string{"review.decided"}}, approved, true},
		{"another topic", Filter{Topics: []string{"review.requested"}}, approved, false},
		{"exciter project", Filter{Topics: []string{"review.decided"}, Projects: []string{"RTR"}}, approved, true},
		{"another project", Filter{Topics: []string{"review.decided"}, Projects: []string{"XYZ"}}, approved, false},
		{"armature project from the key", Filter{Topics: []string{"issue.created"}, Projects: []string{"CP"}}, issue, true},
		{"stator space", Filter{Topics: []string{"page.published"}, Projects: []string{"DOC"}}, page, true},
		{"equals", Filter{Topics: []string{"review.decided"}, Conditions: []Condition{cond("payload.review.decision", OpEquals, `"approve"`)}}, approved, true},
		{"equals a number", Filter{Topics: []string{"review.decided"}, Conditions: []Condition{cond("payload.review.round", OpEquals, `2`)}}, approved, true},
		{"not equal", Filter{Topics: []string{"review.decided"}, Conditions: []Condition{cond("payload.review.decision", OpEquals, `"reject"`)}}, approved, false},
		{"in", Filter{Topics: []string{"issue.created"}, Conditions: []Condition{cond("payload.issue.priority", OpIn, `["high","urgent"]`)}}, issue, true},
		{"exists", Filter{Topics: []string{"issue.created"}, Conditions: []Condition{cond("payload.issue.key", OpExists, ``)}}, issue, true},
		{"missing", Filter{Topics: []string{"issue.created"}, Conditions: []Condition{cond("payload.issue.sprint", OpExists, ``)}}, issue, false},
		{"all conditions", Filter{Topics: []string{"issue.created"}, Conditions: []Condition{
			cond("payload.issue.priority", OpEquals, `"high"`), cond("topic", OpEquals, `"issue.updated"`)}}, issue, false},
	} {
		if got := c.filter.Matches(c.e); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestAFilterSaysWhatIsWrongWithIt(t *testing.T) {
	bad := Filter{Topics: []string{"Issue Created"}, Projects: []string{" "}, Conditions: []Condition{
		{Path: "payload..x", Op: OpEquals, Value: json.RawMessage(`"a"`)},
		{Path: "payload.x", Op: "matches", Value: json.RawMessage(`"a"`)},
		{Path: "payload.x", Op: OpEquals, Value: json.RawMessage(`{"a":1}`)},
		{Path: "payload.x", Op: OpIn, Value: json.RawMessage(`[]`)},
	}}
	problems := bad.Problems()
	for _, field := range []string{"event/topics/0", "event/projects/0", "event/conditions/0", "event/conditions/1", "event/conditions/2", "event/conditions/3"} {
		if problems[field] == "" {
			t.Errorf("no problem at %s: %v", field, problems)
		}
	}
	if problems := (Filter{}).Problems(); problems["event/topics"] == "" {
		t.Error("a filter without topics passes")
	}
	good := Filter{Topics: []string{"review.decided"}, Conditions: []Condition{{Path: "payload.review.decision", Op: OpIn, Value: json.RawMessage(`["approve"]`)}}}
	if problems := good.Problems(); len(problems) != 0 {
		t.Errorf("a good filter has %v", problems)
	}
}
