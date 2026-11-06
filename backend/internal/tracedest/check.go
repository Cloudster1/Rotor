package tracedest

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// Placeholders a trace link names, filled from the trace and its run.
const (
	TraceIDPlaceholder = "{traceId}"
	RunIDPlaceholder   = "{runId}"
)

// A trace link is checked with ids of the shapes it is filled with.
const (
	sampleTraceID = "00000000000000000000000000000000"
	sampleRunID   = "00000000-0000-0000-0000-000000000000"
)

// Invalid is input Rotor will not keep, with a sentence per field an administrator fixes.
type Invalid struct{ Fields map[string]string }

func (e *Invalid) Error() string { return "the trace destination needs other values" }

// headerName is an HTTP field name, a token as RFC 9110 writes it.
var headerName = regexp.MustCompile("^[A-Za-z0-9!#$%&'*+.^_`|~-]+$")

// reservedHeaders are the ones the exporter sets itself, so a destination may not change them.
var reservedHeaders = map[string]bool{"content-type": true, "content-encoding": true, "content-length": true, "host": true}

// Sentences an administrator reads beside the field.
var (
	nameSentence     = fmt.Sprintf("Name the destination in at most %d characters.", config.MaxDestinationNameChars)
	endpointSentence = fmt.Sprintf("Give the whole OTLP/HTTP traces URL, http or https, at most %d characters, without a user, "+
		"query or fragment, such as http://jaeger:4318/v1/traces.", config.MaxDestinationURLChars)
	headersSentence = fmt.Sprintf("Give at most %d headers, each a name of letters, digits and dashes and a value of at most %d "+
		"characters on one line; Content-Type, Content-Encoding, Content-Length and Host are Rotor's.",
		config.MaxDestinationHeaders, config.MaxDestinationHeaderChars)
	linkSentence = fmt.Sprintf("Give an http or https address of at most %d characters holding %s, and %s if you like, "+
		"such as http://localhost:16686/trace/%s.", config.MaxDestinationURLChars, TraceIDPlaceholder, RunIDPlaceholder,
		TraceIDPlaceholder)
)

// check tidies in and says what is wrong with it; headers is nil when in keeps the ones held.
func check(in Input) (Input, map[string]string, error) {
	fields := map[string]string{}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > config.MaxDestinationNameChars || strings.IndexFunc(in.Name, unicode.IsControl) >= 0 {
		fields["name"] = nameSentence
	}
	in.Endpoint = strings.TrimSpace(in.Endpoint)
	if !endpointOK(in.Endpoint) {
		fields["endpoint"] = endpointSentence
	}
	var headers map[string]string
	if in.Headers != nil {
		var ok bool
		if headers, ok = headersOK(*in.Headers); !ok {
			fields["headers"] = headersSentence
		}
	}
	if in.TraceLink != nil {
		link := strings.TrimSpace(*in.TraceLink)
		switch {
		case link == "":
			in.TraceLink = nil
		case !linkOK(link):
			fields["traceLink"] = linkSentence
		default:
			in.TraceLink = &link
		}
	}
	if len(fields) > 0 {
		return in, nil, &Invalid{Fields: fields}
	}
	return in, headers, nil
}

func endpointOK(raw string) bool {
	if raw == "" || len(raw) > config.MaxDestinationURLChars {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil &&
		u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && !strings.Contains(raw, "#")
}

func headersOK(in map[string]string) (map[string]string, bool) {
	if len(in) > config.MaxDestinationHeaders {
		return nil, false
	}
	out := make(map[string]string, len(in))
	seen := map[string]bool{}
	for name, value := range in {
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		lower := strings.ToLower(name)
		if !headerName.MatchString(name) || len(name) > config.MaxDestinationHeaderChars || reservedHeaders[lower] || seen[lower] ||
			value == "" || len(value) > config.MaxDestinationHeaderChars || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return nil, false
		}
		seen[lower] = true
		out[name] = value
	}
	return out, true
}

func linkOK(raw string) bool {
	if len(raw) > config.MaxDestinationURLChars || !strings.Contains(raw, TraceIDPlaceholder) {
		return false
	}
	u, err := url.Parse(fillLink(raw, sampleTraceID, sampleRunID))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil
}

// fillLink is a destination's trace link for one trace of a run.
func fillLink(template, traceID, runID string) string {
	return strings.NewReplacer(TraceIDPlaceholder, traceID, RunIDPlaceholder, runID).Replace(template)
}
