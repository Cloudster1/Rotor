package tracedest

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/vault"
)

// What the test span says of itself, so whoever finds it at the destination knows where it came from.
const (
	probeService = "rotor"
	probeName    = "rotor.test"
	traceIDBytes = 16
	spanIDBytes  = 8
)

// TestResult is what a destination answered the test span.
type TestResult struct {
	Delivered bool
	// Status is the HTTP status it answered, 0 when it could not be reached.
	Status  int
	Message string
}

// Test sends the destination one span without text, whether or not it is enabled, and says what it answered.
func (s *Service) Test(ctx context.Context, id uuid.UUID) (TestResult, error) {
	d, err := s.Get(ctx, id)
	if err != nil {
		return TestResult{}, err
	}
	var headers map[string]string
	if d.HeadersSetAt != nil {
		h, err := s.headers.DestinationHeaders(ctx, id)
		if err != nil && !errors.Is(err, vault.ErrNotFound) {
			return TestResult{}, err
		}
		headers = h.Headers
	}
	body, err := probeSpan(s.now())
	if err != nil {
		return TestResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, config.DestinationTestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.Endpoint, bytes.NewReader(body))
	if err != nil {
		return TestResult{}, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return TestResult{Message: fmt.Sprintf("Rotor could not reach %s. Check the endpoint and that the api can reach its host.",
			d.Endpoint)}, nil
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, config.MaxDestinationTestBodyBytes))
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return TestResult{Delivered: true, Status: resp.StatusCode, Message: d.Name + " took the test span."}, nil
	}
	hint := "Check the endpoint."
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		hint = "Check the headers it is sent with."
	}
	return TestResult{Status: resp.StatusCode, Message: fmt.Sprintf("%s answered %s. %s", d.Name, resp.Status, hint)}, nil
}

// probeSpan is an OTLP export of one span of a trace of its own, with no text.
func probeSpan(now time.Time) ([]byte, error) {
	traceID, spanID := make([]byte, traceIDBytes), make([]byte, spanIDBytes)
	if _, err := rand.Read(traceID); err != nil {
		return nil, err
	}
	if _, err := rand.Read(spanID); err != nil {
		return nil, err
	}
	str := func(k, v string) *commonpb.KeyValue {
		return &commonpb.KeyValue{Key: k, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}}}
	}
	return proto.Marshal(&coltracepb.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{str("service.name", probeService)}},
		ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{{
			TraceId: traceID, SpanId: spanID, Name: probeName, Kind: tracepb.Span_SPAN_KIND_INTERNAL,
			StartTimeUnixNano: uint64(now.UnixNano()), EndTimeUnixNano: uint64(now.UnixNano()),
			Attributes: []*commonpb.KeyValue{str("openinference.span.kind", "CHAIN")},
		}}}},
	}}})
}
