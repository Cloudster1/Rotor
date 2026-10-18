package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// pkiRole is the OpenBao PKI role that issues the runner protocol's server certificate.
const pkiRole = "pki/issue/runnerctl"

// Certificate is a server certificate with its key and the CA that signed it, all PEM.
type Certificate struct {
	Certificate string
	PrivateKey  string
	IssuingCA   string
	NotAfter    time.Time
}

// IssueCertificate asks OpenBao's PKI for a certificate naming host, valid for ttl.
func (c *Client) IssueCertificate(ctx context.Context, host string, ttl time.Duration) (Certificate, error) {
	if err := c.ensure(ctx); err != nil {
		return Certificate{}, err
	}
	secret, err := c.api.Logical().WriteWithContext(ctx, pkiRole, map[string]any{"common_name": host, "ttl": ttl.String()})
	if err != nil {
		return Certificate{}, fmt.Errorf("vault: issue a certificate for %s: %w", host, err)
	}
	if secret == nil || secret.Data == nil {
		return Certificate{}, fmt.Errorf("vault: OpenBao issued no certificate for %s", host)
	}
	text := func(key string) string { s, _ := secret.Data[key].(string); return s }
	var seconds int64
	switch v := secret.Data["expiration"].(type) {
	case json.Number:
		seconds, _ = v.Int64()
	case float64:
		seconds = int64(v)
	}
	return Certificate{Certificate: text("certificate"), PrivateKey: text("private_key"), IssuingCA: text("issuing_ca"),
		NotAfter: time.Unix(seconds, 0)}, nil
}
