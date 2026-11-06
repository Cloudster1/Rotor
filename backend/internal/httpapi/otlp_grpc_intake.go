package httpapi

// What OTLP over gRPC (#262) asks of the OTLP endpoint, so it takes the same tokens and counts against the same rates
// and store as OTLP/HTTP; internal/otlpgrpc serves it.

import (
	"context"
	"errors"
	"strings"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/ingestkeys"
	"github.com/cloudster1/rotor/backend/internal/oauth"
	"github.com/cloudster1/rotor/backend/internal/traces/otlp"
)

// bearerPrefix is how a token is written in an authorization header or a call's metadata.
const bearerPrefix = "Bearer "

// OTLPSender lets in the sender an authorization value names, as the HTTP endpoint's middleware does: an ingest key,
// a Rotor access token whose grant may act, or a realm's token of an account let in. A refusal is an *otlp.Refusal.
func (s *Server) OTLPSender(ctx context.Context, authorization string) (context.Context, error) {
	token, ok := strings.CutPrefix(authorization, bearerPrefix)
	token = strings.TrimSpace(token)
	if !ok || token == "" {
		return ctx, s.otlpRefusal(ctx, errBearerOnly)
	}
	if ingestkeys.IsKey(token) {
		p, keyID, refused := s.ingestKeySender(ctx, token)
		if refused != nil {
			return ctx, s.otlpRefusal(ctx, refused)
		}
		return withIngestKeySender(ctx, p, keyID), nil
	}
	if oauth.IsAccessToken(token) {
		return s.otlpAssistant(ctx, token)
	}
	if s.ServiceAccounts == nil {
		return ctx, s.otlpRefusal(ctx, errRealmUnconfigured)
	}
	p, err := s.ServiceAccounts.AuthenticateSender(ctx, token)
	if err != nil {
		refused, _ := realmRefusal(ctx, err)
		return ctx, s.otlpRefusal(ctx, refused)
	}
	return db.WithUser(auth.WithPrincipal(ctx, p), p.UserID), nil
}

// otlpAssistant lets in the person a Rotor access token acts for, refused as the HTTP endpoint refuses it: one Rotor
// does not know as errBearerOnly, and a grant to read as read_only, since sending spans writes into a trace.
func (s *Server) otlpAssistant(ctx context.Context, token string) (context.Context, error) {
	if s.OAuth == nil {
		return ctx, s.otlpRefusal(ctx, errBearerOnly)
	}
	p, err := s.OAuth.Authenticate(ctx, token)
	var refused *APIError
	switch {
	case errors.Is(err, oauth.ErrSwitchedOff):
		return ctx, s.otlpRefusal(ctx, errAssistantsOff)
	case errors.Is(err, auth.ErrNoSession), errors.Is(err, oauth.ErrInvalidToken):
		return ctx, s.otlpRefusal(ctx, errBearerOnly)
	case errors.Is(err, auth.ErrSwitchedOff):
		return ctx, s.otlpRefusal(ctx, errSwitchedOff)
	case errors.As(err, &refused):
		return ctx, s.otlpRefusal(ctx, refused)
	case err != nil:
		return ctx, s.otlpRefusal(ctx, ErrInternal(err))
	}
	if p.ThroughAssistant() && p.Assistant.ReadOnly {
		return ctx, s.otlpRefusal(ctx, errReadOnlyGrant)
	}
	if err := s.resolveAccess(ctx, p); err != nil {
		return ctx, s.otlpRefusal(ctx, ErrInternal(err))
	}
	ctx = db.WithUser(auth.WithPrincipal(ctx, p), p.UserID)
	if p.Assistant != nil {
		ctx = db.WithVia(ctx, p.Assistant.GrantID)
	}
	return ctx, nil
}

// OTLPAdmit asks for a store and a request within the sender's rate, as the HTTP endpoint does once it read the
// media type; it answers an *otlp.Refusal or an *otlp.Busy.
func (s *Server) OTLPAdmit(ctx context.Context) error {
	err := s.admitOTLP(ctx)
	var refused *APIError
	if errors.As(err, &refused) {
		return s.otlpRefusal(ctx, refused)
	}
	return err
}

// OTLPTake keeps a request's spans as the sender on ctx, as the HTTP endpoint does; over the spans a minute it answers
// an *otlp.Busy.
func (s *Server) OTLPTake(ctx context.Context, req *coltracepb.ExportTraceServiceRequest) (otlp.Rejections, error) {
	return s.takeOTLP(ctx, req)
}

// otlpRefusal is a refusal in the words both transports answer; a fault of Rotor's is logged here, since no
// respondError sees it.
func (s *Server) otlpRefusal(ctx context.Context, e *APIError) error {
	if e.cause != nil && s.Log != nil {
		s.Log.ErrorContext(ctx, "an OTLP export over gRPC failed", "error", e.cause)
	}
	return &otlp.Refusal{Status: e.Status, Message: e.Message}
}
