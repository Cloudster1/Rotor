package config

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// clean starts every test from an environment with only the required setting,
// so a variable exported on the developer's machine cannot change the outcome.
func clean(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"ROTOR_ENV", "ROTOR_HTTP_ADDR", "ROTOR_LOG_LEVEL", "ROTOR_APP_URL", "ROTOR_CORS_ORIGINS",
		"ROTOR_REQUEST_TIMEOUT", "ROTOR_DB_ADMIN_URL", "ROTOR_DB_MAX_CONNS", "ROTOR_DB_MIN_CONNS",
		"ROTOR_DB_CONN_MAX_LIFETIME", "ROTOR_OIDC_ISSUER", "ROTOR_OIDC_CLIENT_ID", "ROTOR_OIDC_CLIENT_SECRET",
		"ROTOR_OIDC_REDIRECT_URL", "ROTOR_SESSION_TTL", "ROTOR_SECURE_COOKIES", "ROTOR_OPENBAO_ADDR",
		"ROTOR_OPENBAO_ROLE", "ROTOR_OPENBAO_MOUNT", "ROTOR_CONTROL_PLANE_CLIENT_ID", "ROTOR_CONTROL_PLANE_CLIENT_SECRET",
		"ROTOR_TOOLS_BACKCHANNEL",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("ROTOR_DB_URL", "postgres://app@db/rotor")
}

func TestDefaults(t *testing.T) {
	clean(t)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Env != EnvDevelopment || c.HTTPAddr != DefaultHTTPAddr || c.RequestTimeout != DefaultRequestTimeout {
		t.Errorf("process defaults = %+v", c)
	}
	if c.DB.MaxConns != DefaultMaxConns || c.DB.MinConns != DefaultMinConns || c.DB.ConnMaxLifetime != DefaultConnMaxLifetime {
		t.Errorf("pool defaults = %+v", c.DB)
	}
	if c.DB.AdminURL != c.DB.URL {
		t.Errorf("the admin URL should fall back to the app URL, got %q", c.DB.AdminURL)
	}
	if c.AppBaseURL != DefaultAppBaseURL {
		t.Errorf("app URL = %q", c.AppBaseURL)
	}
}

func TestListsAreTrimmedAndTheAppURLLosesItsSlash(t *testing.T) {
	clean(t)
	t.Setenv("ROTOR_CORS_ORIGINS", " http://a , ,http://b ")
	t.Setenv("ROTOR_APP_URL", "http://localhost:30040/")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.CORSOrigins) != 2 || c.CORSOrigins[0] != "http://a" || c.CORSOrigins[1] != "http://b" {
		t.Fatalf("origins = %q", c.CORSOrigins)
	}
	if c.AppBaseURL != "http://localhost:30040" {
		t.Fatalf("app URL = %q", c.AppBaseURL)
	}
}

func TestSettingsAreRead(t *testing.T) {
	clean(t)
	t.Setenv("ROTOR_ENV", EnvProduction)
	t.Setenv("ROTOR_SECURE_COOKIES", "true")
	t.Setenv("ROTOR_OIDC_ISSUER", "https://id.example.com/realms/dynamo")
	t.Setenv("ROTOR_OIDC_CLIENT_ID", "rotor")
	t.Setenv("ROTOR_OIDC_CLIENT_SECRET", "s")
	t.Setenv("ROTOR_DB_ADMIN_URL", "postgres://admin@db/rotor")
	t.Setenv("ROTOR_DB_MAX_CONNS", "7")
	t.Setenv("ROTOR_REQUEST_TIMEOUT", "5s")
	t.Setenv("ROTOR_OPENBAO_ADDR", "https://openbao.example.com:8200")
	t.Setenv("ROTOR_CONTROL_PLANE_CLIENT_ID", "rotor-control-plane")
	t.Setenv("ROTOR_CONTROL_PLANE_CLIENT_SECRET", "s")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsProduction() || c.DB.AdminURL != "postgres://admin@db/rotor" || c.DB.MaxConns != 7 || c.RequestTimeout != 5*time.Second {
		t.Fatalf("config = %+v", c)
	}
}

func TestEveryProblemIsReportedAtOnceAsSentences(t *testing.T) {
	clean(t)
	t.Setenv("ROTOR_DB_URL", "")
	t.Setenv("ROTOR_ENV", "moon")
	t.Setenv("ROTOR_DB_MAX_CONNS", "many")
	t.Setenv("ROTOR_REQUEST_TIMEOUT", "soon")
	_, err := Load()
	var cfgErr *Error
	if !errors.As(err, &cfgErr) {
		t.Fatalf("want *Error, got %v", err)
	}
	// An environment that is not development also asks for secure cookies, an identity provider and OpenBao.
	if len(cfgErr.Problems) != 7 {
		t.Fatalf("want 7 problems, got %d: %q", len(cfgErr.Problems), cfgErr.Problems)
	}
	for _, p := range cfgErr.Problems {
		if !strings.HasSuffix(p, ".") || !strings.Contains(p, "ROTOR_") {
			t.Errorf("not a sentence naming its setting: %q", p)
		}
	}
}

func TestPoolBoundsAreChecked(t *testing.T) {
	clean(t)
	t.Setenv("ROTOR_DB_MAX_CONNS", "2")
	t.Setenv("ROTOR_DB_MIN_CONNS", "5")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "ROTOR_DB_MIN_CONNS") {
		t.Fatalf("want the pool bounds refused, got %v", err)
	}
	clean(t)
	t.Setenv("ROTOR_DB_MAX_CONNS", "0")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "at least 1") {
		t.Fatalf("want an empty pool refused, got %v", err)
	}
}

func TestSignInSettings(t *testing.T) {
	clean(t)
	for _, key := range []string{"ROTOR_OIDC_ISSUER", "ROTOR_OIDC_CLIENT_ID", "ROTOR_OIDC_CLIENT_SECRET", "ROTOR_OIDC_REDIRECT_URL",
		"ROTOR_OIDC_BACKCHANNEL", "ROTOR_SESSION_COOKIE", "ROTOR_SECURE_COOKIES", "ROTOR_SESSION_TTL"} {
		t.Setenv(key, "")
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.SignInConfigured() || c.Auth.SessionCookie != DefaultSessionCookie || c.Auth.SessionTTL != DefaultSessionTTL || c.Auth.SecureCookies {
		t.Errorf("sign-in defaults = %+v %+v", c.Auth, c.OIDC)
	}
	if c.OIDC.RedirectURL != DefaultAppBaseURL+CallbackPath {
		t.Errorf("the callback defaults to %q", c.OIDC.RedirectURL)
	}

	t.Setenv("ROTOR_OIDC_ISSUER", "http://localhost:30042/realms/dynamo/")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "ROTOR_OIDC_CLIENT_ID") {
		t.Errorf("an issuer without a client = %v", err)
	}
	t.Setenv("ROTOR_OIDC_CLIENT_ID", "rotor")
	t.Setenv("ROTOR_OIDC_CLIENT_SECRET", "rotor-dev-secret")
	if c, err = Load(); err != nil || c.OIDC.Issuer != "http://localhost:30042/realms/dynamo" || !c.SignInConfigured() {
		t.Errorf("a full provider = %+v, %v", c.OIDC, err)
	}
}

func TestOutsideDevelopmentSignInAndSecureCookiesAreRequired(t *testing.T) {
	clean(t)
	t.Setenv("ROTOR_ENV", EnvStaging)
	t.Setenv("ROTOR_OIDC_ISSUER", "")
	t.Setenv("ROTOR_SECURE_COOKIES", "")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "ROTOR_SECURE_COOKIES") || !strings.Contains(err.Error(), "ROTOR_OIDC_ISSUER") {
		t.Fatalf("staging without sign-in or secure cookies = %v", err)
	}
	t.Setenv("ROTOR_SECURE_COOKIES", "maybe")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "true or false") {
		t.Fatalf("a cookie flag that is no boolean = %v", err)
	}
}

func TestOpenBaoSettings(t *testing.T) {
	clean(t)
	c, err := Load()
	if err != nil || c.SecretsConfigured() || c.OpenBao.Role != DefaultOpenBaoRole || c.OpenBao.Mount != DefaultOpenBaoMount {
		t.Fatalf("development without OpenBao = %+v, %v", c.OpenBao, err)
	}

	t.Setenv("ROTOR_OPENBAO_ADDR", "http://openbao:8200")
	_, err = Load()
	for _, want := range []string{"ROTOR_CONTROL_PLANE_CLIENT_ID", "ROTOR_OIDC_ISSUER"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("OpenBao without its sign-in = %v, want it to name %s", err, want)
		}
	}

	t.Setenv("ROTOR_OIDC_ISSUER", "http://id.test/realms/dynamo")
	t.Setenv("ROTOR_OIDC_CLIENT_ID", "rotor")
	t.Setenv("ROTOR_OIDC_CLIENT_SECRET", "s")
	t.Setenv("ROTOR_CONTROL_PLANE_CLIENT_ID", "rotor-control-plane")
	t.Setenv("ROTOR_CONTROL_PLANE_CLIENT_SECRET", " secret ")
	c, err = Load()
	if err != nil || !c.SecretsConfigured() || c.OpenBao.ClientSecret != " secret " {
		t.Fatalf("a full OpenBao = %+v, %v", c.OpenBao, err)
	}

	clean(t)
	t.Setenv("ROTOR_ENV", EnvStaging)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "ROTOR_OPENBAO_ADDR") {
		t.Fatalf("staging without OpenBao = %v", err)
	}
}
