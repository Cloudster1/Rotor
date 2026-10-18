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
		"ROTOR_DB_CONN_MAX_LIFETIME",
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
	t.Setenv("ROTOR_DB_ADMIN_URL", "postgres://admin@db/rotor")
	t.Setenv("ROTOR_DB_MAX_CONNS", "7")
	t.Setenv("ROTOR_REQUEST_TIMEOUT", "5s")
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
	if len(cfgErr.Problems) != 4 {
		t.Fatalf("want 4 problems, got %d: %q", len(cfgErr.Problems), cfgErr.Problems)
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
