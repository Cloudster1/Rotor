package config

import (
	"fmt"
	"time"
)

// OpenBao defaults and timings; the layout behind them is in docs/api-contract-m1.md.
const (
	DefaultOpenBaoRole  = "rotor-control-plane"
	DefaultOpenBaoMount = "rotor"
	// VaultTimeout bounds one call to OpenBao or to the identity provider on its behalf.
	VaultTimeout = 5 * time.Second
	// VaultLoginRetry is the pause before logging in again after OpenBao or the provider refused.
	VaultLoginRetry = 5 * time.Second
	// OpenBaoAuthJWT signs in with a token of the realm's service account; OpenBaoAuthKubernetes with the pod's own
	// ServiceAccount token, as the control plane does in a cluster (#26).
	OpenBaoAuthJWT        = "jwt"
	OpenBaoAuthKubernetes = "kubernetes"
	// DefaultServiceAccountToken is where Kubernetes projects a pod's ServiceAccount token.
	DefaultServiceAccountToken = "/var/run/secrets/kubernetes.io/serviceaccount/token"
)

// OpenBao is where every long-lived secret lives, and how the api signs in to it.
type OpenBao struct {
	Addr  string
	Role  string
	Mount string
	// Auth is how the control plane signs in: jwt, or kubernetes with the token at TokenPath.
	Auth      string
	TokenPath string
	// ClientID and ClientSecret are the realm's service account whose token OpenBao's JWT auth trusts.
	ClientID     string
	ClientSecret string
}

func loadOpenBao(l *loader) OpenBao {
	return OpenBao{
		Addr:         l.str("ROTOR_OPENBAO_ADDR", ""),
		Role:         l.str("ROTOR_OPENBAO_ROLE", DefaultOpenBaoRole),
		Mount:        l.str("ROTOR_OPENBAO_MOUNT", DefaultOpenBaoMount),
		Auth:         l.str("ROTOR_OPENBAO_AUTH", OpenBaoAuthJWT),
		TokenPath:    l.str("ROTOR_OPENBAO_TOKEN_PATH", DefaultServiceAccountToken),
		ClientID:     l.str("ROTOR_CONTROL_PLANE_CLIENT_ID", ""),
		ClientSecret: l.raw("ROTOR_CONTROL_PLANE_CLIENT_SECRET"),
	}
}

func checkOpenBao(l *loader, c Config) {
	if c.OpenBao.Addr == "" {
		if c.Env != EnvDevelopment {
			l.problem("Set ROTOR_OPENBAO_ADDR to OpenBao's address, such as https://openbao.example.com:8200; Rotor keeps every refresh token there.")
		}
		return
	}
	switch c.OpenBao.Auth {
	case OpenBaoAuthKubernetes:
		return
	case OpenBaoAuthJWT:
	default:
		l.problem(fmt.Sprintf("ROTOR_OPENBAO_AUTH is %q; set it to jwt, or kubernetes in a cluster.", c.OpenBao.Auth))
		return
	}
	if c.OpenBao.ClientID == "" || c.OpenBao.ClientSecret == "" {
		l.problem("ROTOR_OPENBAO_ADDR is set, so set ROTOR_CONTROL_PLANE_CLIENT_ID and ROTOR_CONTROL_PLANE_CLIENT_SECRET too: the realm client Rotor signs in to OpenBao as, or set ROTOR_OPENBAO_AUTH=kubernetes in a cluster.")
	}
	if c.OIDC.Issuer == "" {
		l.problem("ROTOR_OPENBAO_ADDR is set, so set ROTOR_OIDC_ISSUER too: Rotor signs in to OpenBao with a token from that realm, or set ROTOR_OPENBAO_AUTH=kubernetes in a cluster.")
	}
}

// SecretsConfigured says whether OpenBao is set; without it nobody can connect a tool.
func (c Config) SecretsConfigured() bool { return c.OpenBao.Addr != "" }
