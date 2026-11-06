package traces

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/migrations"
)

// keepTraces is the migration whose trigger and checks hold the cap whoever inserts.
const keepTraces = "01500_keep_traces.sql"

// The cap is enforced by the database, which cannot read Go: the numbers written into the migration must be the
// constants the rest of Rotor reads, or a change to one would leave the other behind.
func TestTheMigrationsNumbersAreTheConstants(t *testing.T) {
	raw, err := migrations.FS.ReadFile(keepTraces)
	if err != nil {
		t.Fatal(err)
	}
	up, _, _ := strings.Cut(string(raw), "-- +goose Down")
	cases := map[string]struct {
		pattern string
		want    int
	}{
		"MaxTraceSpans":             {`t\.spans >= (\d+)`, config.MaxTraceSpans},
		"MaxTraceBytes":             {`t\.bytes \+ NEW\.bytes > (\d+)`, config.MaxTraceBytes},
		"MaxSenderSpans":            {`o\.owner_id = t\.owner_id\) >= (\d+)`, config.MaxSenderSpans},
		"DefaultTraceRetentionDays": {`retention_days integer NOT NULL DEFAULT (\d+)`, config.DefaultTraceRetentionDays},
		"MaxTraceRetentionDays":     {`retention_days BETWEEN 1 AND (\d+)`, config.MaxTraceRetentionDays},
		"MaxSpanNameChars":          {`char_length\(name\) <= (\d+)`, config.MaxSpanNameChars},
		"MaxSpanAttrChars":          {`char_length\(status_message\) <= (\d+)`, config.MaxSpanAttrChars},
	}
	for name, c := range cases {
		match := regexp.MustCompile(c.pattern).FindStringSubmatch(up)
		if match == nil {
			t.Errorf("%s: %s no longer says %q", name, keepTraces, c.pattern)
			continue
		}
		if got, _ := strconv.Atoi(match[1]); got != c.want {
			t.Errorf("%s: the migration says %d, config says %d", name, got, c.want)
		}
	}

	// The kinds the column takes are the kinds the store writes.
	list := regexp.MustCompile(`(?s)kind\s+text NOT NULL DEFAULT 'UNKNOWN' CHECK \(kind IN \(([^)]*)\)\)`).FindStringSubmatch(up)
	if list == nil {
		t.Fatal("the migration no longer lists the span kinds")
	}
	var inSQL []string
	for _, k := range regexp.MustCompile(`'([A-Z]+)'`).FindAllStringSubmatch(list[1], -1) {
		inSQL = append(inSQL, k[1])
	}
	var inGo []string
	for k := range kinds {
		inGo = append(inGo, k)
	}
	slices.Sort(inSQL)
	slices.Sort(inGo)
	if !slices.Equal(inSQL, inGo) {
		t.Errorf("the migration takes the kinds %v, the store writes %v", inSQL, inGo)
	}
}
