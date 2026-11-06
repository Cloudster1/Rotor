package tracedest

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/migrations"
)

func ptr[T any](v T) *T { return &v }

func TestADestinationIsCheckedFieldByField(t *testing.T) {
	ok := Input{Name: " Jaeger ", Endpoint: "http://jaeger:4318/v1/traces"}
	got, headers, err := check(ok)
	if err != nil || got.Name != "Jaeger" || headers != nil {
		t.Fatalf("a plain destination = %+v %v %v", got, headers, err)
	}
	many := map[string]string{}
	for i := range config.MaxDestinationHeaders + 1 {
		many["x-h"+strconv.Itoa(i)] = "v"
	}
	cases := map[string]struct {
		in    Input
		field string
	}{
		"no name":            {Input{Endpoint: ok.Endpoint}, "name"},
		"a long name":        {Input{Name: strings.Repeat("n", config.MaxDestinationNameChars+1), Endpoint: ok.Endpoint}, "name"},
		"no scheme":          {Input{Name: "a", Endpoint: "jaeger:4318/v1/traces"}, "endpoint"},
		"another scheme":     {Input{Name: "a", Endpoint: "ftp://jaeger/v1/traces"}, "endpoint"},
		"a user":             {Input{Name: "a", Endpoint: "https://me:pw@host/v1/traces"}, "endpoint"},
		"a query":            {Input{Name: "a", Endpoint: "https://host/v1/traces?key=1"}, "endpoint"},
		"a fragment":         {Input{Name: "a", Endpoint: "https://host/v1/traces#x"}, "endpoint"},
		"a long endpoint":    {Input{Name: "a", Endpoint: "https://host/" + strings.Repeat("p", config.MaxDestinationURLChars)}, "endpoint"},
		"Host":               {Input{Name: "a", Endpoint: ok.Endpoint, Headers: &map[string]string{"host": "x"}}, "headers"},
		"Content-Type":       {Input{Name: "a", Endpoint: ok.Endpoint, Headers: &map[string]string{"Content-Type": "x"}}, "headers"},
		"a name with spaces": {Input{Name: "a", Endpoint: ok.Endpoint, Headers: &map[string]string{"x key": "v"}}, "headers"},
		"a value on two lines": {Input{Name: "a", Endpoint: ok.Endpoint,
			Headers: &map[string]string{"x-key": "a\r\nInjected: 1"}}, "headers"},
		"an empty value":   {Input{Name: "a", Endpoint: ok.Endpoint, Headers: &map[string]string{"x-key": " "}}, "headers"},
		"twice in a case":  {Input{Name: "a", Endpoint: ok.Endpoint, Headers: &map[string]string{"X-Key": "1", "x-key": "2"}}, "headers"},
		"too many headers": {Input{Name: "a", Endpoint: ok.Endpoint, Headers: &many}, "headers"},
		"a link without the trace": {Input{Name: "a", Endpoint: ok.Endpoint,
			TraceLink: ptr("http://jaeger:16686/search")}, "traceLink"},
		"a link of another scheme": {Input{Name: "a", Endpoint: ok.Endpoint,
			TraceLink: ptr("javascript:alert('{traceId}')")}, "traceLink"},
	}
	for name, c := range cases {
		_, _, err := check(c.in)
		invalid, isInvalid := err.(*Invalid)
		if !isInvalid || invalid.Fields[c.field] == "" {
			t.Errorf("%s = %v, want the field %s named", name, err, c.field)
		}
	}

	in := ok
	in.Headers, in.TraceLink = &map[string]string{"Authorization": " Basic abc "}, ptr(" ")
	got, headers, err = check(in)
	if err != nil || headers["Authorization"] != "Basic abc" || got.TraceLink != nil {
		t.Errorf("headers and an empty link = %+v %v %v", got, headers, err)
	}
	in.Headers = &map[string]string{}
	if _, headers, err = check(in); err != nil || headers == nil || len(headers) != 0 {
		t.Errorf("no headers = %v %v, want an empty map that removes them", headers, err)
	}
}

func TestATraceLinkIsFilledFromTheTraceAndItsRun(t *testing.T) {
	got := fillLink("https://phoenix:6006/trace/{traceId}?run={runId}&again={traceId}", "abc", "r1")
	if got != "https://phoenix:6006/trace/abc?run=r1&again=abc" {
		t.Errorf("the link = %q", got)
	}
}

// The database refuses what Go refuses only while both say the same numbers.
func TestTheMigrationsNumbersAreTheConstants(t *testing.T) {
	raw, err := migrations.FS.ReadFile("01520_trace_destinations.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, _, _ := strings.Cut(string(raw), "-- +goose Down")
	cases := map[string]struct {
		pattern string
		want    int
	}{
		"MaxTraceDestinations":    {`FROM trace_destination\) >= (\d+)`, config.MaxTraceDestinations},
		"MaxDestinationNameChars": {`char_length\(name\) <= (\d+)`, config.MaxDestinationNameChars},
		"MaxDestinationHeaders":   {`cardinality\(header_names\) <= (\d+)`, config.MaxDestinationHeaders},
		"MaxDestinationURLChars":  {`char_length\(endpoint\) <= (\d+)`, config.MaxDestinationURLChars},
	}
	for name, c := range cases {
		match := regexp.MustCompile(c.pattern).FindStringSubmatch(up)
		if match == nil {
			t.Errorf("%s: the migration no longer says %q", name, c.pattern)
			continue
		}
		if got, _ := strconv.Atoi(match[1]); got != c.want {
			t.Errorf("%s: the migration says %d, config says %d", name, got, c.want)
		}
	}
	if !strings.Contains(up, "'"+config.DefaultLangSmithEndpoint+"'") {
		t.Errorf("the carried-over destination is not sent to %s", config.DefaultLangSmithEndpoint)
	}
}
