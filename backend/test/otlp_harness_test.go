//go:build integration

package test

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/httpapi"
	"github.com/cloudster1/rotor/backend/internal/oauth"
)

// The app's service and the media types OTLP/HTTP sends.
const (
	appService    = "langchain-app"
	mediaProtobuf = "application/x-protobuf"
	mediaJSON     = "application/json"
)

// sentSpan is a span as a LangChain app's exporter sends it; ids are hex.
type sentSpan struct {
	traceID, spanID, parent, name string
	attrs                         map[string]string
	ok                            bool
}

// appSpan is a span of the trace with a fresh span id.
func appSpan(traceID, name string, attrs map[string]string) sentSpan {
	var id [8]byte
	_, _ = rand.Read(id[:])
	return sentSpan{traceID: traceID, spanID: hex.EncodeToString(id[:]), name: name, attrs: attrs, ok: true}
}

// sentAt is when every span of a test starts, a millisecond before it ends.
var sentAt = time.Now()

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// protobufOf is spans as the protobuf exporter encodes them, under the app's service.
func protobufOf(t *testing.T, spans ...sentSpan) []byte {
	t.Helper()
	out := make([]*tracepb.Span, 0, len(spans))
	for _, s := range spans {
		sp := &tracepb.Span{TraceId: mustHex(t, s.traceID), SpanId: mustHex(t, s.spanID), Name: s.name, Kind: tracepb.Span_SPAN_KIND_INTERNAL,
			StartTimeUnixNano: uint64(sentAt.UnixNano()), EndTimeUnixNano: uint64(sentAt.Add(time.Millisecond).UnixNano())}
		if s.parent != "" {
			sp.ParentSpanId = mustHex(t, s.parent)
		}
		for k, v := range s.attrs {
			sp.Attributes = append(sp.Attributes, &commonpb.KeyValue{Key: k, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}}})
		}
		if s.ok {
			sp.Status = &tracepb.Status{Code: tracepb.Status_STATUS_CODE_OK}
		}
		out = append(out, sp)
	}
	raw, err := proto.Marshal(&coltracepb.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{{Key: "service.name",
			Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: appService}}}}},
		ScopeSpans: []*tracepb.ScopeSpans{{Spans: out}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// jsonOf is spans as OTLP's JSON writes them, by hand: ids in hex, times as strings, enums as numbers.
func jsonOf(t *testing.T, spans ...sentSpan) []byte {
	t.Helper()
	out := make([]map[string]any, 0, len(spans))
	for _, s := range spans {
		attrs := []map[string]any{}
		for k, v := range s.attrs {
			attrs = append(attrs, map[string]any{"key": k, "value": map[string]any{"stringValue": v}})
		}
		sp := map[string]any{"traceId": s.traceID, "spanId": s.spanID, "parentSpanId": s.parent, "name": s.name, "kind": 1,
			"startTimeUnixNano": strconv.FormatInt(sentAt.UnixNano(), 10),
			"endTimeUnixNano":   strconv.FormatInt(sentAt.Add(time.Millisecond).UnixNano(), 10), "attributes": attrs}
		if s.ok {
			sp["status"] = map[string]any{"code": 1}
		}
		out = append(out, sp)
	}
	raw, err := json.Marshal(map[string]any{"resourceSpans": []any{map[string]any{
		"resource":   map[string]any{"attributes": []any{map[string]any{"key": "service.name", "value": map[string]any{"stringValue": appService}}}},
		"scopeSpans": []any{map[string]any{"scope": map[string]any{"name": "openinference"}, "spans": out}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func gzipOf(t *testing.T, raw []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// otlpCall is one request to the endpoint; an empty token sends none and a cookie rides along when given.
type otlpCall struct {
	token, contentType, encoding string
	body                         []byte
	cookie                       *http.Cookie
}

// sendSpans posts to the endpoint as an exporter would, and answers the status, the headers and the body.
func sendSpans(t *testing.T, c otlpCall) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, apiURL(t)+httpapi.APIPrefix+"/otlp/v1/traces", bytes.NewReader(c.body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", c.contentType)
	if c.encoding != "" {
		req.Header.Set("Content-Encoding", c.encoding)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, body
}

// partial reads a JSON answer's partial_success: how many spans were not taken and why.
func partial(t *testing.T, body []byte) (int, string) {
	t.Helper()
	var answer struct {
		PartialSuccess struct {
			RejectedSpans string `json:"rejectedSpans"`
			ErrorMessage  string `json:"errorMessage"`
		} `json:"partialSuccess"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("the answer %q is no ExportTraceServiceResponse: %v", body, err)
	}
	n, _ := strconv.Atoi(answer.PartialSuccess.RejectedSpans)
	return n, answer.PartialSuccess.ErrorMessage
}

// appToken is an access token of Rotor's for an app the person connected, made behind the policies' back as the
// authorization server would after consent, and removed after the test.
func (h *harness) appToken(t *testing.T, person uuid.UUID, readOnly bool) string {
	t.Helper()
	ctx := context.Background()
	client := "otlp-test-" + uuid.NewString()
	token := oauth.AccessTokenPrefix + uuid.NewString()
	if _, err := h.super.Exec(ctx, `INSERT INTO oauth_client (id, name, redirect_uris, grant_types, registered_from)
		VALUES ($1, 'LangChain app', ARRAY['http://127.0.0.1/callback'], ARRAY['authorization_code'], '127.0.0.1')`, client); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = h.super.Exec(context.Background(), `DELETE FROM oauth_client WHERE id = $1`, client) })
	if _, err := h.super.Exec(ctx, `WITH g AS (INSERT INTO oauth_grant (user_id, client_id, redirect_uri, read_only)
			VALUES ($1, $2, 'http://127.0.0.1/callback', $3) RETURNING id)
		INSERT INTO oauth_token (token_hash, grant_id, kind, read_only, expires_at) SELECT $4, g.id, 'access', $3, now() + interval '1 hour' FROM g`,
		person, client, readOnly, auth.Hash(token)); err != nil {
		t.Fatal(err)
	}
	return token
}

// keepText turns keeping spans' text on or off through the api, as an administrator does, so the api's replica reads
// it at once, and puts it back after the test.
func (h *harness) keepText(t *testing.T, keep bool) {
	t.Helper()
	admin := h.browserFor(t, h.addPerson(t, "admin"))
	status, before := admin.api(http.MethodGet, "/settings/traces", nil)
	if status != http.StatusOK {
		t.Fatalf("read the trace setting: %d %v", status, before)
	}
	set := func(keep bool) {
		if status, got := admin.api(http.MethodPut, "/settings/traces",
			map[string]any{"retentionDays": before["retentionDays"], "keepContent": keep}); status != http.StatusOK {
			t.Fatalf("set the trace setting: %d %v", status, got)
		}
	}
	set(keep)
	t.Cleanup(func() { set(before["keepContent"] == true) })
}

// keptSpan is a span as Postgres holds it, read behind the policies.
type keptSpan struct {
	trace                       uuid.UUID
	runID, ownerID, senderID    *uuid.UUID
	name, kind, service, source string
	status, senderName, parent  string
	attributes                  map[string]string
	sealed                      []byte
	dataKey                     *string
}

// keptSpanOf reads one span by its id among the traces of traceID.
func (h *harness) keptSpanOf(t *testing.T, traceID, spanID string) (keptSpan, bool) {
	t.Helper()
	var k keptSpan
	var attrs []byte
	err := h.super.QueryRow(context.Background(), `SELECT t.id, t.run_id, t.owner_id, t.data_key, s.sender_id, coalesce(s.sender_name, ''),
			s.name, s.kind, s.service, s.source, s.status, s.parent_span_id, s.attributes, s.sealed_content
		FROM trace_span s JOIN trace t ON t.id = s.trace WHERE t.trace_id = $1 AND s.span_id = $2`, traceID, spanID).
		Scan(&k.trace, &k.runID, &k.ownerID, &k.dataKey, &k.senderID, &k.senderName, &k.name, &k.kind, &k.service, &k.source,
			&k.status, &k.parent, &attrs, &k.sealed)
	if err != nil {
		return keptSpan{}, false
	}
	if err := json.Unmarshal(attrs, &k.attributes); err != nil {
		t.Fatal(err)
	}
	return k, true
}

// sessionCookie is a session the person signed in for, as their browser holds it.
func (h *harness) sessionCookie(t *testing.T, person uuid.UUID) *http.Cookie {
	t.Helper()
	var token string
	sessions := auth.NewService(h.cluster, h.cfg.Auth.SessionTTL)
	if err := h.cluster.WriteAdmin(context.Background(), func(ctx context.Context, tx db.DBTX) error {
		var err error
		token, err = sessions.Open(ctx, tx, auth.SessionInput{UserID: person})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: h.cfg.Auth.SessionCookie, Value: token}
}
