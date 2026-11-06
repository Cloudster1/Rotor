package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/observability"
	"github.com/cloudster1/rotor/backend/internal/traces"
	"github.com/cloudster1/rotor/backend/internal/traces/otlp"
)

// fakeSpans keeps what it is sent, and refuses the spans named "capped" as the cap's trigger would.
type fakeSpans struct {
	sender traces.Sender
	kept   []observability.RunnerSpan
}

func (f *fakeSpans) Receive(_ context.Context, sender traces.Sender, spans []observability.RunnerSpan) (otlp.Rejections, error) {
	var rejected otlp.Rejections
	f.sender = sender
	for _, sp := range spans {
		if sp.Name == "capped" {
			rejected.Add(traces.ReasonCapped, 1)
			continue
		}
		f.kept = append(f.kept, sp)
	}
	return rejected, nil
}

const otlpJSON = `{"resourceSpans":[{"scopeSpans":[{"spans":[
	{"traceId":"5b8efff798038103d269b633813fc60c","spanId":"eee19b7ec3c1b174","name":"ChatOpenAI"},
	{"traceId":"5b8efff798038103d269b633813fc60c","spanId":"eee19b7ec3c1b175","name":"capped"},
	{"traceId":"5b8efff798038103d269b633813fc60c","spanId":"eee19b7ec3c1b176","name":""}]}]}]}`

func otlpServer(t *testing.T) (http.Handler, *fakeSpans, *fakeServiceAccounts) {
	t.Helper()
	s, accounts, _ := serviceAccountServer(t)
	s.OAuth = &fakeOAuth{tokens: map[string]*auth.Principal{writerToken: assistant(false), readerToken: assistant(true)}}
	spans := &fakeSpans{}
	s.Spans = spans
	return s.Routes(nil), spans, accounts
}

func post(token, contentType string, body []byte) *http.Request {
	req := httptest.NewRequest(http.MethodPost, otlpPath, bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", contentType)
	return req
}

// The endpoint takes a token of Rotor's or of the realm, and nothing else: no cookie, no grant to read.
func TestTheOTLPEndpointTakesATokenAlone(t *testing.T) {
	h, spans, accounts := otlpServer(t)
	cookie := withSession(post("", otlp.MediaJSON, []byte(otlpJSON)), "alice")
	for name, c := range map[string]struct {
		req  *http.Request
		want int
		code string
	}{
		"no token":            {post("", otlp.MediaJSON, []byte(otlpJSON)), http.StatusUnauthorized, "unauthorized"},
		"a session cookie":    {cookie, http.StatusUnauthorized, "unauthorized"},
		"an unknown token":    {post("not-a-token", otlp.MediaJSON, []byte(otlpJSON)), http.StatusUnauthorized, "invalid_token"},
		"a grant to read":     {post(readerToken, otlp.MediaJSON, []byte(otlpJSON)), http.StatusForbidden, "read_only"},
		"a grant to act":      {post(writerToken, otlp.MediaJSON, []byte(otlpJSON)), http.StatusOK, ""},
		"the realm's account": {post(realmToken, otlp.MediaJSON, []byte(otlpJSON)), http.StatusOK, ""},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, c.req)
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != c.want || (c.code != "" && errorOf(t, body)["code"] != c.code) {
			t.Errorf("%s = %d %s, want %d %s", name, rec.Code, rec.Body, c.want, c.code)
		}
	}
	if accounts.asked == 0 || spans.sender.ID != accounts.account.UserID || spans.sender.Name != "dynamo-connect" {
		t.Errorf("the realm's account sent as %v", spans.sender)
	}
}

// A request that was read answers 200 in its own encoding, naming the spans not taken and why.
func TestSpansNotTakenAreAnsweredAsPartialSuccess(t *testing.T) {
	h, spans, _ := otlpServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, post(writerToken, otlp.MediaJSON, []byte(otlpJSON)))
	var answer struct {
		PartialSuccess struct {
			RejectedSpans string `json:"rejectedSpans"`
			ErrorMessage  string `json:"errorMessage"`
		} `json:"partialSuccess"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil || rec.Code != http.StatusOK || answer.PartialSuccess.RejectedSpans != "2" ||
		!strings.Contains(answer.PartialSuccess.ErrorMessage, otlp.ReasonName) ||
		!strings.Contains(answer.PartialSuccess.ErrorMessage, traces.ReasonCapped) || len(spans.kept) != 1 {
		t.Fatalf("partial success = %d %s, kept %d", rec.Code, rec.Body, len(spans.kept))
	}

	req, err := otlp.Decode(otlp.JSON, []byte(otlpJSON))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := proto.Marshal(req)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, post(writerToken, otlp.MediaProtobuf, raw))
	var resp coltracepb.ExportTraceServiceResponse
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != otlp.MediaProtobuf ||
		proto.Unmarshal(rec.Body.Bytes(), &resp) != nil || resp.GetPartialSuccess().GetRejectedSpans() != 2 {
		t.Errorf("the protobuf answer = %d %q %v", rec.Code, rec.Header().Get("Content-Type"), &resp)
	}
}

func TestABodyTheEndpointCannotTakeIsRefusedWithAStatus(t *testing.T) {
	h, _, _ := otlpServer(t)
	gzipBad := post(writerToken, otlp.MediaProtobuf, []byte("not gzip"))
	gzipBad.Header.Set("Content-Encoding", "gzip")
	brotli := post(writerToken, otlp.MediaJSON, []byte(otlpJSON))
	brotli.Header.Set("Content-Encoding", "br")
	for name, c := range map[string]struct {
		req  *http.Request
		want int
	}{
		"plain text":       {post(writerToken, "text/plain", []byte(otlpJSON)), http.StatusUnsupportedMediaType},
		"another encoding": {brotli, http.StatusUnsupportedMediaType},
		"not a request":    {post(writerToken, otlp.MediaJSON, []byte("spans")), http.StatusBadRequest},
		"broken gzip":      {gzipBad, http.StatusBadRequest},
		"too large":        {post(writerToken, otlp.MediaProtobuf, make([]byte, config.MaxOTLPBodyBytes+1)), http.StatusRequestEntityTooLarge},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, c.req)
		if rec.Code != c.want || rec.Body.Len() == 0 {
			t.Errorf("%s = %d %q, want %d with a status", name, rec.Code, rec.Body, c.want)
		}
	}
}

func TestASenderPastItsRequestsWaitsForTheWindow(t *testing.T) {
	h, _, _ := otlpServer(t)
	for range config.OTLPRequestsPerMinute {
		h.ServeHTTP(httptest.NewRecorder(), post(writerToken, otlp.MediaJSON, []byte(`{}`)))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, post(writerToken, otlp.MediaJSON, []byte(`{}`)))
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" || !strings.Contains(rec.Body.String(), "Retry-After") {
		t.Errorf("a request past the window's = %d %q %s", rec.Code, rec.Header().Get("Retry-After"), rec.Body)
	}
	// Another sender is not held to it.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, post(realmToken, otlp.MediaJSON, []byte(`{}`)))
	if rec.Code != http.StatusOK {
		t.Errorf("another sender = %d %s", rec.Code, rec.Body)
	}
}
