package audit

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAnExportedCellNeverReadsAsAFormula(t *testing.T) {
	for in, want := range map[string]string{"": "", "run": "run", "=SUM(A1)": "'=SUM(A1)", "+1": "'+1", "-2": "'-2", "@x": "'@x", "\tx": "'\tx"} {
		if got := Cell(in); got != want {
			t.Errorf("Cell(%q) = %q, want %q", in, got, want)
		}
	}
	actor := uuid.New()
	line := CSVLine(Record{CreatedAt: time.Date(2026, 10, 19, 8, 0, 0, 0, time.UTC), Action: "run.started", ActorID: &actor,
		ActorName: "=bob", TargetType: "run", TargetID: "r", Data: []byte(`{"n":1}`)})
	if len(line) != len(CSVHeader) || line[2] != actor.String() || line[3] != "'=bob" {
		t.Errorf("the line = %q", line)
	}
}

func TestAFilterSaysWhatItNarrowsBy(t *testing.T) {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	got := Filter{Action: "run.started", TargetID: "x", From: &from}.Describe()
	if len(got) != 3 || got["from"] != "2026-10-01T00:00:00Z" {
		t.Errorf("Describe = %v", got)
	}
	where, args := Filter{}.where()
	if where != "true" || args != nil {
		t.Errorf("an empty filter = %q %v", where, args)
	}
}
