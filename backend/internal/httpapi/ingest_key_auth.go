package httpapi

// How a service's ingest key is let in (#258): at the OTLP endpoint alone, as the account it sends as, refused before
// any lookup everywhere else.

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/ingestkeys"
)

var (
	errIngestKeyOTLPOnly = &APIError{Status: http.StatusUnauthorized, Code: "invalid_token",
		Message: "An ingest key sends spans to " + otlpPath + " alone. Sign in to Rotor in your browser for anything else."}
	errIngestKeyUnknown = &APIError{Status: http.StatusUnauthorized, Code: "invalid_token",
		Message: "Rotor knows no such ingest key: it was revoked or rotated, or never made. Ask an administrator of Rotor " +
			"for a key under Admin, Traces."}
	errIngestKeyExpired = &APIError{Status: http.StatusUnauthorized, Code: "invalid_token",
		Message: "This ingest key has expired. Ask an administrator of Rotor for a new one under Admin, Traces."}
	errIngestKeyNotLetIn = ErrForbidden("The account this ingest key sends as is no longer let into Rotor. Ask an " +
		"administrator of Rotor to let it in again, or to make a key for another account.")
)

type ctxKeyIngestKey struct{}

// ingestKeyFrom is the key a request's spans were sent with, uuid.Nil for a token.
func ingestKeyFrom(ctx context.Context) uuid.UUID {
	id, _ := ctx.Value(ctxKeyIngestKey{}).(uuid.UUID)
	return id
}

// authenticateIngestKey lets in the account a key sends as at the OTLP endpoint, and refuses the key anywhere else
// without looking it up, so a key never reads or administers anything.
func (s *Server) authenticateIngestKey(w http.ResponseWriter, r *http.Request, key string, next http.Handler) {
	ctx := r.Context()
	w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
	if r.URL.Path != otlpPath || inProcessCall(ctx) {
		respondError(w, r, errIngestKeyOTLPOnly)
		return
	}
	p, keyID, refused := s.ingestKeySender(ctx, key)
	if refused != nil {
		// A key that was taken for what it is but whose account may not send is no challenge to answer anew.
		if refused.Status == http.StatusForbidden {
			w.Header().Del("WWW-Authenticate")
		}
		respondError(w, r, refused)
		return
	}
	w.Header().Del("WWW-Authenticate")
	next.ServeHTTP(w, r.WithContext(withIngestKeySender(ctx, p, keyID)))
}

// ingestKeySender is the account a key sends as, with its permissions, and the key's id; or why the key is refused.
// OTLP over HTTP and over gRPC (#262) both ask it.
func (s *Server) ingestKeySender(ctx context.Context, key string) (*auth.Principal, uuid.UUID, *APIError) {
	if s.IngestKeys == nil {
		return nil, uuid.Nil, errIngestKeyUnknown
	}
	sending, err := s.IngestKeys.Authenticate(ctx, key)
	switch {
	case errors.Is(err, ingestkeys.ErrUnknown):
		return nil, uuid.Nil, errIngestKeyUnknown
	case errors.Is(err, ingestkeys.ErrExpired):
		return nil, uuid.Nil, errIngestKeyExpired
	case errors.Is(err, ingestkeys.ErrNotLetIn):
		return nil, uuid.Nil, errIngestKeyNotLetIn
	case errors.Is(err, auth.ErrSwitchedOff):
		return nil, uuid.Nil, errSwitchedOff
	case err != nil:
		return nil, uuid.Nil, ErrInternal(err)
	}
	p := sending.Principal
	if err := s.resolveAccess(ctx, p); err != nil {
		return nil, uuid.Nil, ErrInternal(err)
	}
	return p, sending.KeyID, nil
}

// withIngestKeySender acts as the key's account and remembers the key, which its spans name.
func withIngestKeySender(ctx context.Context, p *auth.Principal, keyID uuid.UUID) context.Context {
	return context.WithValue(db.WithUser(auth.WithPrincipal(ctx, p), p.UserID), ctxKeyIngestKey{}, keyID)
}
