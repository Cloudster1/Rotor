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
	if s.IngestKeys == nil {
		respondError(w, r, errIngestKeyUnknown)
		return
	}
	sending, err := s.IngestKeys.Authenticate(ctx, key)
	switch {
	case errors.Is(err, ingestkeys.ErrUnknown):
		respondError(w, r, errIngestKeyUnknown)
		return
	case errors.Is(err, ingestkeys.ErrExpired):
		respondError(w, r, errIngestKeyExpired)
		return
	case errors.Is(err, ingestkeys.ErrNotLetIn):
		w.Header().Del("WWW-Authenticate")
		respondError(w, r, errIngestKeyNotLetIn)
		return
	case errors.Is(err, auth.ErrSwitchedOff):
		w.Header().Del("WWW-Authenticate")
		respondError(w, r, errSwitchedOff)
		return
	case err != nil:
		respondError(w, r, ErrInternal(err))
		return
	}
	w.Header().Del("WWW-Authenticate")
	p := sending.Principal
	if err := s.resolveAccess(ctx, p); err != nil {
		respondError(w, r, ErrInternal(err))
		return
	}
	ctx = context.WithValue(db.WithUser(auth.WithPrincipal(ctx, p), p.UserID), ctxKeyIngestKey{}, sending.KeyID)
	next.ServeHTTP(w, r.WithContext(ctx))
}
