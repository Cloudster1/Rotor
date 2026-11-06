package traces

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// A search always has a window: the last week when it names none, a week before its end when it names only that.
func TestASearchWithoutAWindowReadsTheLastWeek(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	since, until := Window(nil, nil, now)
	if !until.Equal(now) || !since.Equal(now.AddDate(0, 0, -config.SpanSearchDefaultDays)) {
		t.Errorf("the default window is %v to %v", since, until)
	}
	end := now.Add(-48 * time.Hour)
	if since, _ := Window(nil, &end, now); !since.Equal(end.AddDate(0, 0, -config.SpanSearchDefaultDays)) {
		t.Errorf("a window ending %v starts %v", end, since)
	}
}

// What a search may not ask is refused by field, with what to do; text is never searched.
func TestASearchIsRefusedPastItsBoundsAndForText(t *testing.T) {
	now := time.Now()
	ok := SpanFilter{Since: now.AddDate(0, 0, -config.SpanSearchMaxDays).Add(time.Minute), Until: now}
	if p := ok.Problems(now); p != nil {
		t.Fatalf("a search of the last 30 days is refused: %v", p)
	}
	cases := map[string]func(*SpanFilter){
		"since":     func(f *SpanFilter) { f.Since = now.AddDate(0, 0, -config.SpanSearchMaxDays-1) },
		"until":     func(f *SpanFilter) { f.Until = f.Since },
		"kind":      func(f *SpanFilter) { f.Kind = "WIDGET" },
		"status":    func(f *SpanFilter) { f.Status = "failed" },
		"name":      func(f *SpanFilter) { f.Name = strings.Repeat("n", config.MaxSpanNameChars+1) },
		"attrKey":   func(f *SpanFilter) { f.AttrKey, f.AttrValue = "input.value", "the story" },
		"attrValue": func(f *SpanFilter) { f.AttrKey = "tool.name" },
	}
	for field, change := range cases {
		f := ok
		change(&f)
		if p := f.Problems(now); p[field] == "" {
			t.Errorf("%s: refused as %v", field, p)
		}
	}
	plain := ok
	plain.AttrKey, plain.AttrValue = "tool.name", "whoami"
	if p := plain.Problems(now); p != nil {
		t.Errorf("an attribute without text is refused: %v", p)
	}
}

// A cursor reads back as the place it was made from, and anything else is no cursor.
func TestACursorReadsBackAndNothingElseDoes(t *testing.T) {
	c := Cursor{At: time.Date(2026, 11, 6, 12, 0, 0, 123456000, time.UTC), Trace: uuid.New(), SpanID: "00f067aa0ba902b7"}
	got, ok := ParseCursor(c.String())
	if !ok || !got.At.Equal(c.At) || got.Trace != c.Trace || got.SpanID != c.SpanID {
		t.Errorf("the cursor reads back as %+v, %v", got, ok)
	}
	bad := Cursor{At: c.At, Trace: c.Trace, SpanID: "'; DROP TABLE x"}
	for _, raw := range []string{"", "not base64!", bad.String(), (Cursor{SpanID: c.SpanID}).String()} {
		if _, ok := ParseCursor(raw); ok {
			t.Errorf("%q reads as a cursor", raw)
		}
	}
}

// Only the conditions a search asks for are written, so the planner sees each and picks the index that fits.
func TestASearchWritesOnlyWhatItAsks(t *testing.T) {
	now := time.Now()
	query, args := SearchQuery(SpanFilter{Since: now.Add(-time.Hour), Until: now, Kind: "TOOL", Limit: 10})
	if strings.Contains(query, "s.status = ") || !strings.Contains(query, "s.kind = $3") || len(args) != 4 || args[3] != 11 {
		t.Errorf("the search reads %s with %v", query, args)
	}
}
