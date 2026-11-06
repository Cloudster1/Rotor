package tracedest

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/migrations"
)

func TestAWorkflowsTracingIsCheckedFieldByField(t *testing.T) {
	jaeger, phoenix := Offered{ID: uuid.New(), Name: "Jaeger"}, Offered{ID: uuid.New(), Name: "Phoenix"}
	all := []Offered{jaeger, phoenix}
	names, err := checkWorkflow(WorkflowTracing{Content: ContentNone, Destinations: &[]uuid.UUID{phoenix.ID}, SampleRatio: 0}, all)
	if err != nil || len(names) != 1 || names[0] != "Phoenix" {
		t.Fatalf("a subset without text = %v %v", names, err)
	}
	if names, err := checkWorkflow(WorkflowTracing{Content: ContentInherit, SampleRatio: 1}, all); err != nil || names != nil {
		t.Errorf("every destination = %v %v, want no names", names, err)
	}
	if names, err := checkWorkflow(WorkflowTracing{Content: ContentInherit, Destinations: &[]uuid.UUID{}, SampleRatio: 1}, all); err != nil ||
		names == nil || len(names) != 0 {
		t.Errorf("no destination = %v %v, want an empty list", names, err)
	}

	cases := map[string]struct {
		in    WorkflowTracing
		field string
	}{
		"more text than the installation": {WorkflowTracing{Content: "keep", SampleRatio: 1}, "content"},
		"no content":                      {WorkflowTracing{SampleRatio: 1}, "content"},
		"a ratio past 1":                  {WorkflowTracing{Content: ContentInherit, SampleRatio: 1.5}, "sampleRatio"},
		"a ratio below 0":                 {WorkflowTracing{Content: ContentInherit, SampleRatio: -0.1}, "sampleRatio"},
		"no ratio at all":                 {WorkflowTracing{Content: ContentInherit, SampleRatio: math.NaN()}, "sampleRatio"},
		"a destination that is none": {WorkflowTracing{Content: ContentInherit, SampleRatio: 1,
			Destinations: &[]uuid.UUID{uuid.New()}}, "destinations"},
		"a destination twice": {WorkflowTracing{Content: ContentInherit, SampleRatio: 1,
			Destinations: &[]uuid.UUID{jaeger.ID, jaeger.ID}}, "destinations"},
	}
	for what, c := range cases {
		_, err := checkWorkflow(c.in, all)
		var invalid *Invalid
		if !errors.As(err, &invalid) || invalid.Fields[c.field] == "" {
			t.Errorf("%s = %v, want %s refused", what, err, c.field)
		}
	}
}

// The database refuses as many chosen destinations as Go does.
func TestTheWorkflowTracingMigrationsCapIsTheConstant(t *testing.T) {
	raw, err := migrations.FS.ReadFile("01550_workflow_tracing.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, _, _ := strings.Cut(string(raw), "-- +goose Down")
	match := regexp.MustCompile(`cardinality\(destinations\) <= (\d+)`).FindStringSubmatch(up)
	if match == nil {
		t.Fatal("the migration no longer caps the destinations a workflow chooses")
	}
	if got, _ := strconv.Atoi(match[1]); got != config.MaxTraceDestinations {
		t.Errorf("the migration caps %d destinations, config %d", got, config.MaxTraceDestinations)
	}
}
