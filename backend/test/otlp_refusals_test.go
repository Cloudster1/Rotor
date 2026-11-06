//go:build integration

package test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// The endpoint answers what the contract says to everything it does not take: Rotor's envelope for who is sending,
// google.rpc.Status for what was sent.
func TestTheOTLPEndpointRefusesWhatTheContractRefuses(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloakAdmin(t)
	member := h.addPerson(t, "member")
	token := h.appToken(t, member, false)
	outsider, outsiderSecret := testClient(t, kc, nil)
	body := jsonOf(t, appSpan(randomTraceID(), "LangGraph", nil))
	cookie := h.sessionCookie(t, member)
	// Zeros pack small: well within what is sent, past what may be unpacked.
	packed := gzipOf(t, make([]byte, config.MaxOTLPDecodedBytes+1))

	for name, c := range map[string]struct {
		call   otlpCall
		status int
		says   string
	}{
		"without a token":                {otlpCall{contentType: mediaJSON, body: body}, http.StatusUnauthorized, "bearer token alone"},
		"with the session cookie":        {otlpCall{contentType: mediaJSON, body: body, cookie: cookie}, http.StatusUnauthorized, "never the session cookie"},
		"with an unknown token of Rotor": {otlpCall{token: "rotor_oat_unknown", contentType: mediaJSON, body: body}, http.StatusUnauthorized, "bearer token alone"},
		"with a grant to read":           {otlpCall{token: h.appToken(t, member, true), contentType: mediaJSON, body: body}, http.StatusForbidden, "read only"},
		"of an account not let in":       {otlpCall{token: clientToken(t, outsider, outsiderSecret), contentType: mediaJSON, body: body}, http.StatusForbidden, "has not been let into Rotor"},
		"past what is sent":              {otlpCall{token: token, contentType: mediaProtobuf, body: make([]byte, config.MaxOTLPBodyBytes+1)}, http.StatusRequestEntityTooLarge, "smaller batches"},
		"past what is unpacked":          {otlpCall{token: token, contentType: mediaProtobuf, encoding: "gzip", body: packed}, http.StatusRequestEntityTooLarge, "smaller batches"},
		"of another type":                {otlpCall{token: token, contentType: "text/plain", body: body}, http.StatusUnsupportedMediaType, "application/x-protobuf"},
		"of another encoding":            {otlpCall{token: token, contentType: mediaJSON, encoding: "br", body: body}, http.StatusUnsupportedMediaType, "gzip"},
		"that is no request":             {otlpCall{token: token, contentType: mediaJSON, body: []byte("spans")}, http.StatusBadRequest, "ExportTraceServiceRequest"},
	} {
		status, _, answer := sendSpans(t, c.call)
		var got struct {
			Message string `json:"message"`
			Error   struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		// A protobuf google.rpc.Status carries its message as the bytes of the sentence.
		says := string(answer)
		if json.Unmarshal(answer, &got) == nil {
			says = got.Message + got.Error.Message
		}
		if status != c.status || !strings.Contains(says, c.says) {
			t.Errorf("a request %s = %d %q, want %d saying %q", name, status, answer, c.status, c.says)
		}
	}

	// The realm's own service account sends as any account Rotor lets in does.
	status, _, answer := sendSpans(t, otlpCall{token: clientToken(t, connectClient, connectSecret), contentType: mediaJSON, body: body})
	if status != http.StatusOK {
		t.Errorf("dynamo-connect's token = %d %q", status, answer)
	}
}

// A sender past its requests or its spans in a minute waits for the window's end, and another sender does not.
func TestASenderPastItsRateIsToldToWait(t *testing.T) {
	h := newHarness(t)
	busy, other := h.addPerson(t, "member"), h.addPerson(t, "member")
	token := h.appToken(t, busy, false)
	empty := otlpCall{token: token, contentType: mediaJSON, body: []byte(`{}`)}
	for i := range config.OTLPRequestsPerMinute {
		if status, _, answer := sendSpans(t, empty); status != http.StatusOK {
			t.Fatalf("request %d = %d %q", i+1, status, answer)
		}
	}
	status, header, answer := sendSpans(t, empty)
	if status != http.StatusTooManyRequests || header.Get("Retry-After") == "" || !strings.Contains(string(answer), "requests in a minute") {
		t.Errorf("a request past the minute's = %d %q %q", status, header.Get("Retry-After"), answer)
	}

	// Spans are counted as sent, before their checks, so nameless ones cost the window without being stored.
	nameless := make([]sentSpan, config.MaxOTLPSpansPerRequest)
	for i := range nameless {
		nameless[i] = appSpan(randomTraceID(), "", nil)
	}
	full := otlpCall{token: h.appToken(t, other, false), contentType: mediaProtobuf, body: protobufOf(t, nameless...)}
	sent := 0
	for sent+config.MaxOTLPSpansPerRequest <= config.OTLPSpansPerMinute {
		if status, _, answer := sendSpans(t, full); status != http.StatusOK {
			t.Fatalf("after %d spans = %d %q", sent, status, answer)
		}
		sent += config.MaxOTLPSpansPerRequest
	}
	status, header, _ = sendSpans(t, full)
	if status != http.StatusTooManyRequests || header.Get("Retry-After") == "" || header.Get("Content-Type") != mediaProtobuf {
		t.Errorf("spans past the minute's = %d %q %q", status, header.Get("Retry-After"), header.Get("Content-Type"))
	}
}
