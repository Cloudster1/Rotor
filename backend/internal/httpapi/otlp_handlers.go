package httpapi

// How an app or a tool sends its spans to Rotor over OTLP/HTTP (#243): a bearer token alone, the body read through
// limits, the spans judged as the sender, and the answers OTLP's exporters act on.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/observability"
	"github.com/cloudster1/rotor/backend/internal/traces"
	"github.com/cloudster1/rotor/backend/internal/traces/otlp"
)

// SpanStore keeps what senders post to the OTLP endpoint (#243); *traces.Service is the real one.
type SpanStore interface {
	Receive(ctx context.Context, sender traces.Sender, spans []observability.RunnerSpan) (otlp.Rejections, error)
}

// errBearerOnly refuses a request without a token at the OTLP endpoint, a session cookie included: a browser sends
// no OTLP, and taking no cookie leaves nothing for another site to forge.
var errBearerOnly = ErrUnauthorized("The OTLP endpoint takes a bearer token alone, never the session cookie. Send " +
	"Authorization: Bearer with an ingest key, a Rotor access token or an access token of the realm that has not " +
	"expired, as OTEL_EXPORTER_OTLP_HEADERS sets it.")

// mebibyte writes the body limits as the contract does.
const mebibyte = 1 << 20

// What the endpoint answers a body it does not take, in a sentence an exporter's log shows.
var (
	sayMediaType = "Send the spans as application/x-protobuf or application/json, gzip or not encoded, as an " +
		"OpenTelemetry OTLP/HTTP exporter does."
	sayTooLarge = fmt.Sprintf("The request is past %d MiB as sent or %d MiB unpacked. Send smaller batches, such as with "+
		"a lower OTEL_BSP_MAX_EXPORT_BATCH_SIZE.", config.MaxOTLPBodyBytes/mebibyte, config.MaxOTLPDecodedBytes/mebibyte)
	sayMalformed = "The body is not an OTLP ExportTraceServiceRequest. Send what an OpenTelemetry OTLP/HTTP exporter sends."
	sayRequests  = fmt.Sprintf("You sent more than %d requests in a minute. Send again after Retry-After, in larger batches.",
		config.OTLPRequestsPerMinute)
	saySpans = fmt.Sprintf("You sent more than %d spans in a minute. Send again after Retry-After, or sample fewer spans.",
		config.OTLPSpansPerMinute)
)

// tokensOnly lets in the sender a bearer token names at the rows marked tokensOnly: an ingest key or an assistant's or
// an app's token of Rotor, which authenticate read, or a realm's of any account Rotor lets in. Else 401.
func (s *Server) tokensOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if _, bearer := credentialFrom(r, s.CookieName); !bearer {
			w.Header().Set("WWW-Authenticate", "Bearer")
			respondError(w, r, errBearerOnly)
			return
		}
		// A bearer request is signed in only through a token of Rotor's, never a cookie.
		if auth.PrincipalFrom(ctx) != nil {
			next.ServeHTTP(w, r)
			return
		}
		token := realmTokenFrom(ctx)
		if token == "" {
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
			respondError(w, r, errBearerOnly)
			return
		}
		if s.ServiceAccounts == nil {
			respondError(w, r, errRealmUnconfigured)
			return
		}
		p, err := s.ServiceAccounts.AuthenticateSender(ctx, token)
		if err != nil {
			respondError(w, r, realmTokenError(w, r, err))
			return
		}
		next.ServeHTTP(w, r.WithContext(db.WithUser(auth.WithPrincipal(ctx, p), p.UserID)))
	})
}

// handleReceiveTraces takes OTLP/HTTP spans; refusals before the spans are read are google.rpc.Status in the
// request's encoding, and a request that was read answers 200 with partial_success naming what was not taken.
func (s *Server) handleReceiveTraces(w http.ResponseWriter, r *http.Request) {
	format, err := otlp.FormatOf(r.Header.Get("Content-Type"), r.Header.Get("Content-Encoding"))
	if err != nil {
		otlpRefuse(w, r, format, http.StatusUnsupportedMediaType, sayMediaType)
		return
	}
	var busy *otlp.Busy
	if err := s.admitOTLP(r.Context()); errors.As(err, &busy) {
		otlpBusy(w, r, format, busy.Wait, sayRequests)
		return
	} else if err != nil {
		respondError(w, r, err)
		return
	}
	if r.ContentLength > config.MaxOTLPBodyBytes {
		otlpRefuse(w, r, format, http.StatusRequestEntityTooLarge, sayTooLarge)
		return
	}
	body, err := otlp.Read(r.Body, r.Header.Get("Content-Encoding"))
	if errors.Is(err, otlp.ErrTooLarge) {
		otlpRefuse(w, r, format, http.StatusRequestEntityTooLarge, sayTooLarge)
		return
	}
	if err != nil {
		otlpRefuse(w, r, format, http.StatusBadRequest, sayMalformed)
		return
	}
	req, err := otlp.Decode(format, body)
	if err != nil {
		otlpRefuse(w, r, format, http.StatusBadRequest, sayMalformed)
		return
	}
	rejected, err := s.takeOTLP(r.Context(), req)
	if errors.As(err, &busy) {
		otlpBusy(w, r, format, busy.Wait, saySpans)
		return
	}
	if err != nil {
		respondError(w, r, ErrInternal(err))
		return
	}
	answer, err := otlp.Answer(format, rejected)
	if err != nil {
		respondError(w, r, ErrInternal(err))
		return
	}
	writeOTLP(w, r, format, http.StatusOK, answer)
}

// admitOTLP is what either transport asks once it knows the sender (#262): a store to keep spans in, and a request
// within the sender's rate. It answers an *APIError, or an *otlp.Busy for a sender over its requests a minute.
func (s *Server) admitOTLP(ctx context.Context) error {
	if s.Spans == nil {
		return errTracesUnavailable
	}
	if wait, ok := s.otlpRates.Request(auth.PrincipalFrom(ctx).UserID); !ok {
		return &otlp.Busy{Wait: wait}
	}
	return nil
}

// takeOTLP counts a request's spans against the sender's rate, keeps those that pass the checks as the sender, and
// answers what was not kept; a sender over its spans a minute is an *otlp.Busy, and nothing is kept.
func (s *Server) takeOTLP(ctx context.Context, req *coltracepb.ExportTraceServiceRequest) (otlp.Rejections, error) {
	p := auth.PrincipalFrom(ctx)
	if wait, ok := s.otlpRates.Spans(p.UserID, min(otlp.Count(req), config.MaxOTLPSpansPerRequest)); !ok {
		return otlp.Rejections{}, &otlp.Busy{Wait: wait, Spans: true}
	}
	spans, rejected := otlp.Spans(req)
	if len(spans) == 0 {
		return rejected, nil
	}
	ctx, cancel := context.WithTimeout(ctx, config.TraceStoreTimeout)
	defer cancel()
	notKept, err := s.Spans.Receive(ctx, traces.Sender{ID: p.UserID, Name: senderName(p), Key: ingestKeyFrom(ctx)}, spans)
	if err != nil {
		return rejected, err
	}
	rejected.Merge(notKept)
	return rejected, nil
}

// senderName is how a span says who sent it: the person's name, or the service account's address.
func senderName(p *auth.Principal) string {
	if p.Name != "" {
		return p.Name
	}
	return p.Email
}

func otlpRefuse(w http.ResponseWriter, r *http.Request, format otlp.Format, status int, message string) {
	body, err := otlp.Status(format, status, message)
	if err != nil {
		respondError(w, r, ErrInternal(err))
		return
	}
	loggerFrom(r.Context()).Debug("spans refused", "status", status, "message", message)
	writeOTLP(w, r, format, status, body)
}

// otlpBusy is 429 with Retry-After in seconds to the window's end, which OTLP's exporters wait for before trying again.
func otlpBusy(w http.ResponseWriter, r *http.Request, format otlp.Format, wait time.Duration, message string) {
	w.Header().Set("Retry-After", strconv.Itoa(int(wait/time.Second)))
	otlpRefuse(w, r, format, http.StatusTooManyRequests, message)
}

func writeOTLP(w http.ResponseWriter, r *http.Request, format otlp.Format, status int, body []byte) {
	w.Header().Set("Content-Type", format.MediaType())
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		loggerFrom(r.Context()).Debug("could not answer an OTLP export", "error", err)
	}
}
