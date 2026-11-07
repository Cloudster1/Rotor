// Package ingestkeys keeps the long-lived keys a service sends spans to the OTLP endpoint with (#258), each sending as
// one account. docs/api-contract-m18.md says what a key may do.
package ingestkeys

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
)

// Prefix tells an ingest key from Rotor's other tokens before any lookup, and lets redaction find it.
const Prefix = "rotor_ik_"

// encodedLen is how long the random part of a key is once written in unpadded base64url.
var encodedLen = base64.RawURLEncoding.EncodedLen(config.IngestKeySecretBytes)

// Secret is a new key as its administrator sees it once, with what Rotor keeps of it.
type Secret struct {
	Value string
	Hash  []byte
	Hint  string
}

// New makes a key from fresh randomness.
func New() (Secret, error) {
	buf := make([]byte, config.IngestKeySecretBytes)
	if _, err := rand.Read(buf); err != nil {
		return Secret{}, fmt.Errorf("read randomness: %w", err)
	}
	value := Prefix + base64.RawURLEncoding.EncodeToString(buf)
	return Secret{Value: value, Hash: Hash(value), Hint: hint(value)}, nil
}

// IsKey says whether a bearer credential claims to be an ingest key, which is good at the OTLP endpoint alone.
func IsKey(credential string) bool {
	return strings.HasPrefix(credential, Prefix)
}

// WellFormed says whether a credential has the shape New makes, so one of another shape costs no query.
func WellFormed(credential string) bool {
	rest, ok := strings.CutPrefix(credential, Prefix)
	if !ok || len(rest) != encodedLen {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(rest)
	return err == nil && len(raw) == config.IngestKeySecretBytes
}

// Hash is what the database keeps of a key: its SHA-256, as of sessions and assistants' tokens.
func Hash(value string) []byte {
	return auth.Hash(value)
}

func hint(value string) string {
	return value[len(value)-config.IngestKeyHintChars:]
}
