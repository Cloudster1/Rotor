package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/ingestkeys"
)

// IngestKeys are the keys services send spans with (#258); *ingestkeys.Service is the real one.
type IngestKeys interface {
	List(ctx context.Context) ([]ingestkeys.Key, error)
	Create(ctx context.Context, in ingestkeys.Input) (ingestkeys.Key, string, error)
	Rotate(ctx context.Context, id uuid.UUID) (ingestkeys.Key, string, error)
	Revoke(ctx context.Context, id uuid.UUID) error
	Authenticate(ctx context.Context, credential string) (ingestkeys.Sending, error)
}

var (
	errIngestKeysUnavailable = &APIError{Status: http.StatusServiceUnavailable, Code: "ingest_keys_unavailable",
		Message: "Ingest keys cannot be kept on this installation. Ask its operator to check the api's start."}
	errIngestKeyNotFound = ErrNotFound("There is no such ingest key. List them for their ids.")
	errIngestKeyTaken    = &APIError{Status: http.StatusConflict, Code: "ingest_key_taken",
		Message: "Another ingest key has this name. Choose another.", Fields: map[string]string{"name": "Choose a name no other key has."}}
	errIngestKeysFull = &APIError{Status: http.StatusUnprocessableEntity, Code: "ingest_keys_full",
		Message: "Rotor keeps at most " + strconv.Itoa(config.MaxIngestKeys) + " ingest keys. Revoke one before making another."}
	errIngestKeyRotateExpired = &APIError{Status: http.StatusConflict, Code: "ingest_key_expired",
		Message: "This ingest key has expired, so a new secret for it would not send. Make a new key instead."}
)

func ingestKeyError(err error) error {
	var invalid *ingestkeys.Invalid
	switch {
	case errors.As(err, &invalid):
		return ErrValidation(invalid.Fields)
	case errors.Is(err, ingestkeys.ErrNotFound):
		return errIngestKeyNotFound
	case errors.Is(err, ingestkeys.ErrTaken):
		return errIngestKeyTaken
	case errors.Is(err, ingestkeys.ErrFull):
		return errIngestKeysFull
	case errors.Is(err, ingestkeys.ErrExpired):
		return errIngestKeyRotateExpired
	}
	return err
}

func (s *Server) ingestKeysReady(w http.ResponseWriter, r *http.Request) bool {
	if s.IngestKeys == nil {
		respondError(w, r, errIngestKeysUnavailable)
		return false
	}
	return true
}

func (s *Server) handleListIngestKeys(w http.ResponseWriter, r *http.Request) {
	if !s.ingestKeysReady(w, r) {
		return
	}
	keys, err := s.IngestKeys.List(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	now := time.Now()
	out := ingestKeyList{Keys: make([]ingestKey, 0, len(keys))}
	for _, k := range keys {
		out.Keys = append(out.Keys, toIngestKey(k, now))
	}
	respondJSON(w, r, http.StatusOK, out)
}

func (s *Server) handleCreateIngestKey(w http.ResponseWriter, r *http.Request) {
	var req ingestKeyRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if !s.ingestKeysReady(w, r) {
		return
	}
	k, secret, err := s.IngestKeys.Create(r.Context(), ingestkeys.Input{Name: req.Name, AccountEmail: req.AccountEmail,
		ExpiresAt: req.ExpiresAt})
	answerIngestKeySecret(w, r, http.StatusCreated, k, secret, err)
}

func (s *Server) handleRotateIngestKey(w http.ResponseWriter, r *http.Request) {
	id, ok := s.ingestKeyID(w, r)
	if !ok {
		return
	}
	k, secret, err := s.IngestKeys.Rotate(r.Context(), id)
	answerIngestKeySecret(w, r, http.StatusOK, k, secret, err)
}

func (s *Server) handleRevokeIngestKey(w http.ResponseWriter, r *http.Request) {
	id, ok := s.ingestKeyID(w, r)
	if !ok {
		return
	}
	if err := s.IngestKeys.Revoke(r.Context(), id); err != nil {
		respondError(w, r, ingestKeyError(err))
		return
	}
	respondNoContent(w)
}

// ingestKeyID is the path's key, once keys can be kept at all.
func (s *Server) ingestKeyID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, perr := pathUUID(r, "keyID", "ingest key")
	if perr != nil {
		respondError(w, r, perr)
		return uuid.Nil, false
	}
	return id, s.ingestKeysReady(w, r)
}

// answerIngestKeySecret answers the one response that holds the key, which no cache may keep.
func answerIngestKeySecret(w http.ResponseWriter, r *http.Request, status int, k ingestkeys.Key, secret string, err error) {
	if err != nil {
		respondError(w, r, ingestKeyError(err))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respondJSON(w, r, status, ingestKeySecret{Key: toIngestKey(k, time.Now()), Secret: secret})
}

func toIngestKey(k ingestkeys.Key, now time.Time) ingestKey {
	return ingestKey{ID: k.ID.String(), Name: k.Name, Hint: k.Hint, ExpiresAt: k.ExpiresAt, Expired: k.Expired(now),
		LastUsedAt: k.LastUsedAt, CreatedBy: k.CreatedBy, CreatedAt: k.CreatedAt, RotatedAt: k.RotatedAt,
		Account: ingestKeyAccount{ID: k.Account.ID.String(), Name: k.Account.Name, Email: k.Account.Email}}
}
