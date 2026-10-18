// Package config loads and validates process configuration from the
// environment. Every setting is a ROTOR_* variable; there is no file.
package config

import (
	"fmt"
	"strings"
	"time"
)

// Environments a deployment may declare itself as.
const (
	EnvDevelopment = "development"
	EnvStaging     = "staging"
	EnvProduction  = "production"
)

// Defaults, named so the tests and the documentation can quote them.
const (
	DefaultHTTPAddr        = ":8080"
	DefaultAppBaseURL      = "http://localhost:5173"
	DefaultRequestTimeout  = 30 * time.Second
	DefaultMaxConns        = 20
	DefaultMinConns        = 2
	DefaultConnMaxLifetime = time.Hour
	DefaultSessionCookie   = "rotor_session"
	// DefaultSessionTTL is short because a session can start runs that act in three tools.
	DefaultSessionTTL = 12 * time.Hour
	// CallbackPath is where the identity provider returns the browser, under the web client's origin.
	CallbackPath = "/api/v1/auth/oidc/callback"
)

// Fixed timings the processes rely on, kept here so every tunable value has one home.
const (
	// DBPingTimeout bounds the first round trip, so a boot fails rather than hangs on a quiet database.
	DBPingTimeout = 5 * time.Second
	// ReadinessTimeout bounds the database round trip behind /readyz, inside a probe's own deadline.
	ReadinessTimeout = 3 * time.Second
	// The HTTP server's limits. WriteTimeout outlasts DefaultRequestTimeout, so a slow handler is
	// answered by its own deadline rather than cut off mid-reply.
	ReadHeaderTimeout = 10 * time.Second
	ReadTimeout       = 60 * time.Second
	WriteTimeout      = 120 * time.Second
	IdleTimeout       = 120 * time.Second
	ShutdownGrace     = 25 * time.Second
	HealthcheckWait   = 3 * time.Second
	// The migrate container may start alongside Postgres, so it waits this long for it.
	MigrateConnectAttempts = 30
	MigrateConnectPause    = time.Second
	// OIDCLoginTTL is how long a person may take at the identity provider before the attempt is forgotten.
	OIDCLoginTTL = 15 * time.Minute
	// LogoutTokenLeeway is how far a logout token's iat may stray from our clock.
	LogoutTokenLeeway = 5 * time.Minute
	// ProviderTimeout bounds every call to the identity provider; a sign-in waits on it.
	ProviderTimeout = 10 * time.Second
	// MaxLogoutTokenBody bounds the form a provider posts; a logout token is a few kilobytes at most.
	MaxLogoutTokenBody = 64 << 10
)

// Config is the fully resolved configuration for every binary.
type Config struct {
	Env      string
	HTTPAddr string
	LogLevel string
	// AppBaseURL is where the web client is served; the same-site check accepts writes from it.
	AppBaseURL string
	// CORSOrigins may call the API with credentials from another origin.
	CORSOrigins []string
	// RequestTimeout bounds every request's handler.
	RequestTimeout time.Duration

	DB      DB
	Auth    Auth
	OIDC    OIDC
	OpenBao OpenBao
}

// Auth is how a signed-in browser is remembered.
type Auth struct {
	SessionCookie string
	// SecureCookies sends the cookie over HTTPS only; it must be on outside development.
	SecureCookies bool
	SessionTTL    time.Duration
}

// OIDC is the suite's identity provider, the only way to sign in to Rotor.
type OIDC struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	// Backchannel maps the issuer's public origin to the one this process reaches it at, as public=reachable.
	Backchannel string
}

// DB describes the Postgres database and the two roles Rotor connects as.
type DB struct {
	// URL connects as rotor_app, which every row level security policy binds.
	URL string
	// AdminURL connects as rotor_admin, exempt by policy, for sign-in and jobs. Defaults to URL.
	AdminURL        string
	MaxConns        int32
	MinConns        int32
	ConnMaxLifetime time.Duration
}

// Error lists every problem Load found, so an operator fixes them in one go.
type Error struct {
	Problems []string
}

func (e *Error) Error() string {
	return "Rotor cannot start until its configuration is fixed:\n  - " + strings.Join(e.Problems, "\n  - ")
}

// Load reads configuration from the environment, applies defaults and validates
// the result. It returns every problem it finds at once rather than the first.
func Load() (Config, error) {
	l := &loader{}
	c := Config{
		Env:            l.str("ROTOR_ENV", EnvDevelopment),
		HTTPAddr:       l.str("ROTOR_HTTP_ADDR", DefaultHTTPAddr),
		LogLevel:       l.str("ROTOR_LOG_LEVEL", "info"),
		AppBaseURL:     strings.TrimSuffix(l.str("ROTOR_APP_URL", DefaultAppBaseURL), "/"),
		CORSOrigins:    splitList(l.str("ROTOR_CORS_ORIGINS", "")),
		RequestTimeout: l.duration("ROTOR_REQUEST_TIMEOUT", DefaultRequestTimeout),
		DB: DB{
			URL:             l.str("ROTOR_DB_URL", ""),
			AdminURL:        l.str("ROTOR_DB_ADMIN_URL", ""),
			MaxConns:        int32(l.integer("ROTOR_DB_MAX_CONNS", DefaultMaxConns)),
			MinConns:        int32(l.integer("ROTOR_DB_MIN_CONNS", DefaultMinConns)),
			ConnMaxLifetime: l.duration("ROTOR_DB_CONN_MAX_LIFETIME", DefaultConnMaxLifetime),
		},
		Auth: Auth{
			SessionCookie: l.str("ROTOR_SESSION_COOKIE", DefaultSessionCookie),
			SecureCookies: l.boolean("ROTOR_SECURE_COOKIES", false),
			SessionTTL:    l.duration("ROTOR_SESSION_TTL", DefaultSessionTTL),
		},
		OIDC: OIDC{
			Issuer:       strings.TrimSuffix(l.str("ROTOR_OIDC_ISSUER", ""), "/"),
			ClientID:     l.str("ROTOR_OIDC_CLIENT_ID", ""),
			ClientSecret: l.raw("ROTOR_OIDC_CLIENT_SECRET"),
			RedirectURL:  l.str("ROTOR_OIDC_REDIRECT_URL", ""),
			Backchannel:  l.str("ROTOR_OIDC_BACKCHANNEL", ""),
		},
		OpenBao: loadOpenBao(l),
	}
	if c.OIDC.RedirectURL == "" {
		c.OIDC.RedirectURL = c.AppBaseURL + CallbackPath
	}

	if c.DB.URL == "" {
		l.problem("Set ROTOR_DB_URL to the connection URL of the database, as the role rotor_app.")
	}
	switch c.Env {
	case EnvDevelopment, EnvStaging, EnvProduction:
	default:
		l.problem(fmt.Sprintf("ROTOR_ENV is %q; set it to development, staging or production.", c.Env))
	}
	if c.DB.MaxConns < 1 {
		l.problem("ROTOR_DB_MAX_CONNS must be at least 1.")
	} else if c.DB.MinConns > c.DB.MaxConns {
		l.problem(fmt.Sprintf("ROTOR_DB_MIN_CONNS (%d) exceeds ROTOR_DB_MAX_CONNS (%d); lower the first or raise the second.", c.DB.MinConns, c.DB.MaxConns))
	}
	if c.RequestTimeout <= 0 {
		l.problem("ROTOR_REQUEST_TIMEOUT must be longer than zero, such as 30s.")
	}
	if c.Auth.SessionTTL <= 0 {
		l.problem("ROTOR_SESSION_TTL must be longer than zero, such as 12h.")
	}
	if c.Env != EnvDevelopment && !c.Auth.SecureCookies {
		l.problem("ROTOR_SECURE_COOKIES must be true outside development, so the session cookie never travels unencrypted.")
	}
	if c.OIDC.Issuer != "" && (c.OIDC.ClientID == "" || c.OIDC.ClientSecret == "") {
		l.problem("ROTOR_OIDC_ISSUER is set, so set ROTOR_OIDC_CLIENT_ID and ROTOR_OIDC_CLIENT_SECRET too: the client Rotor signs in as.")
	}
	if c.Env != EnvDevelopment && c.OIDC.Issuer == "" {
		l.problem("Set ROTOR_OIDC_ISSUER to the identity provider's issuer, such as https://id.example.com/realms/dynamo; it is the only way to sign in.")
	}
	checkOpenBao(l, c)
	if c.DB.AdminURL == "" {
		c.DB.AdminURL = c.DB.URL
	}
	if len(l.problems) > 0 {
		return Config{}, &Error{Problems: l.problems}
	}
	return c, nil
}

// SignInConfigured says whether an identity provider is set, without which nobody can sign in.
func (c Config) SignInConfigured() bool { return c.OIDC.Issuer != "" }

// IsProduction reports whether this is the production deployment.
func (c Config) IsProduction() bool { return c.Env == EnvProduction }
