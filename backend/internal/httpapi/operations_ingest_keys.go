package httpapi

import (
	"net/http"
	"time"
)

// The rows of #258: the keys services send spans with; docs/api-contract-m18.md says what they mean.
func init() {
	operations = append(operations, ingestKeyOperations...)
}

var ingestKeyOperations = []operation{
	{method: "GET", path: "/settings/ingest-keys", handler: "handleListIngestKeys", tag: "traces", admin: true,
		summary:   "The keys services send spans to the OTLP endpoint with, by name; never a key itself.",
		responses: ok(ingestKeyList{})},
	{method: "POST", path: "/settings/ingest-keys", handler: "handleCreateIngestKey", tag: "traces", admin: true,
		summary: "Make a key that sends spans as one account; the answer holds the key, which is never shown again.",
		description: "accountEmail names an account Rotor has let in and not switched off, your own when left out. The key " +
			"is taken at POST /api/v1/otlp/v1/traces alone. A name another key has answers 409 ingest_key_taken; a key past " +
			"the most there can be 422 ingest_keys_full. The audit log records ingest_key.created.",
		request: ingestKeyRequest{},
		responses: map[int]any{http.StatusCreated: ingestKeySecret{}, http.StatusConflict: errorEnvelope{},
			http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "POST", path: "/settings/ingest-keys/{keyID}/rotate", handler: "handleRotateIngestKey", tag: "traces", admin: true,
		summary: "Give a key a new secret, shown once; the old one is refused from the next request.",
		description: "The name, account and expiry stay. An expired key answers 409 ingest_key_expired. The audit log " +
			"records ingest_key.rotated.",
		responses: map[int]any{http.StatusOK: ingestKeySecret{}, http.StatusNotFound: notFound, http.StatusConflict: errorEnvelope{}}},
	{method: "DELETE", path: "/settings/ingest-keys/{keyID}", handler: "handleRevokeIngestKey", tag: "traces", admin: true,
		summary:   "Revoke a key: it is refused from the next request. The audit log records ingest_key.revoked.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusNotFound: notFound}},
}

// ingestKeyRequest makes a key; expiresAt left out makes one that lasts until revoked.
type ingestKeyRequest struct {
	Name         string     `json:"name"`
	AccountEmail string     `json:"accountEmail,omitempty"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
}

// ingestKeyAccount is the account a key sends as.
type ingestKeyAccount struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// ingestKey is a key as an administrator sees it; hint is its last characters, never the key.
type ingestKey struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	Account    ingestKeyAccount `json:"account"`
	Hint       string           `json:"hint"`
	ExpiresAt  *time.Time       `json:"expiresAt"`
	Expired    bool             `json:"expired"`
	LastUsedAt *time.Time       `json:"lastUsedAt"`
	CreatedBy  *string          `json:"createdBy"`
	CreatedAt  time.Time        `json:"createdAt"`
	RotatedAt  *time.Time       `json:"rotatedAt"`
}

type ingestKeyList struct {
	Keys []ingestKey `json:"keys"`
}

// ingestKeySecret is a key just made or rotated, with the secret a service sends; no later answer holds it.
type ingestKeySecret struct {
	Key    ingestKey `json:"key"`
	Secret string    `json:"secret"`
}
