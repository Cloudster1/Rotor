package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/oidc"
)

type fakeSessions struct {
	people map[string]*auth.Principal
	err    error
	ended  []string
}

func (f *fakeSessions) Authenticate(_ context.Context, token string) (*auth.Principal, error) {
	if f.err != nil {
		return nil, f.err
	}
	if p, ok := f.people[token]; ok {
		return p, nil
	}
	return nil, auth.ErrNoSession
}

func (f *fakeSessions) End(_ context.Context, token string) error {
	f.ended = append(f.ended, token)
	return nil
}

type fakeSignIn struct {
	startErr, finishErr, logoutErr error
	startedWith, loggedOut         string
}

func (f *fakeSignIn) Start(_ context.Context, next string) (string, error) {
	f.startedWith = next
	return "https://id.test/auth?state=s", f.startErr
}

func (f *fakeSignIn) Finish(_ context.Context, _, _ string, _ oidc.Browser) (string, string, error) {
	return "token-1", "/runs", f.finishErr
}

func (f *fakeSignIn) BackchannelLogout(_ context.Context, token string) error {
	f.loggedOut = token
	return f.logoutErr
}

func signedInServer(t *testing.T) (*Server, *fakeSessions, *fakeSignIn) {
	t.Helper()
	s := newServer(t)
	sessions := &fakeSessions{people: map[string]*auth.Principal{
		"alice": {UserID: uuid.New(), Email: "alice@suite.test", Name: "Alice Admin", Role: auth.RoleAdmin, Groups: []string{"rotor-administrators"}},
	}}
	signIn := &fakeSignIn{}
	s.Sessions, s.SignIn, s.SessionTTL = sessions, signIn, time.Hour
	return s, sessions, signIn
}

func withSession(req *http.Request, token string) *http.Request {
	req.AddCookie(&http.Cookie{Name: "rotor_session", Value: token})
	return req
}

func TestANextOnlyEverLandsInsideRotor(t *testing.T) {
	for in, want := range map[string]string{
		"/runs?status=failed": "/runs?status=failed", "": "/", "https://evil.test": "/", "//evil.test": "/",
		"/\\evil.test": "/", "/runs\r\nSet-Cookie: x": "/", "/login?next=/": "/", "runs": "/",
	} {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStartSendsTheBrowserToTheProvider(t *testing.T) {
	s, _, signIn := signedInServer(t)
	resp, _ := serve(t, s.Routes(nil), httptest.NewRequest(http.MethodGet, APIPrefix+"/auth/oidc/start?next=//evil.test", nil))
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "https://id.test/auth?state=s" {
		t.Fatalf("start = %d to %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if signIn.startedWith != "/" {
		t.Errorf("the sign-in was started for %q, want the next made safe", signIn.startedWith)
	}

	for err, code := range map[error]string{oidc.ErrNotConfigured: "sign_in_unavailable", fmt.Errorf("%w: down", oidc.ErrUnreachable): "provider_unreachable"} {
		signIn.startErr = err
		resp, body := serve(t, s.Routes(nil), httptest.NewRequest(http.MethodGet, APIPrefix+"/auth/oidc/start", nil))
		if resp.StatusCode != http.StatusServiceUnavailable || errorOf(t, body)["code"] != code {
			t.Errorf("start with %v = %d %v", err, resp.StatusCode, body)
		}
	}
}

func TestTheCallbackSetsAnHttpOnlyCookieOrSaysWhy(t *testing.T) {
	s, _, signIn := signedInServer(t)
	resp, _ := serve(t, s.Routes(nil), httptest.NewRequest(http.MethodGet, APIPrefix+"/auth/oidc/callback?state=s&code=c", nil))
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/runs" {
		t.Fatalf("callback = %d to %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	cookie := resp.Cookies()[0]
	if cookie.Name != "rotor_session" || cookie.Value != "token-1" || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != 3600 {
		t.Errorf("cookie = %+v", cookie)
	}

	for err, reason := range map[error]string{
		auth.ErrSwitchedOff: "switched_off", oidc.ErrUnknownLogin: "expired", oidc.ErrEmailUnverified: "no_email", errors.New("boom"): "failed",
	} {
		signIn.finishErr = err
		resp, _ := serve(t, s.Routes(nil), httptest.NewRequest(http.MethodGet, APIPrefix+"/auth/oidc/callback?state=s&code=c", nil))
		if resp.Header.Get("Location") != "/login?error="+reason || len(resp.Cookies()) != 0 {
			t.Errorf("callback failing with %v = %q, cookies %v", err, resp.Header.Get("Location"), resp.Cookies())
		}
	}
	resp, _ = serve(t, s.Routes(nil), httptest.NewRequest(http.MethodGet, APIPrefix+"/auth/oidc/callback?error=access_denied", nil))
	if resp.Header.Get("Location") != "/login?error=denied" {
		t.Errorf("a denied sign-in lands on %q", resp.Header.Get("Location"))
	}
}

func TestMeNeedsASessionAndSaysWhoIsSignedIn(t *testing.T) {
	s, sessions, _ := signedInServer(t)
	h := s.Routes(nil)
	resp, body := serve(t, h, httptest.NewRequest(http.MethodGet, APIPrefix+"/auth/me", nil))
	if resp.StatusCode != http.StatusUnauthorized || errorOf(t, body)["code"] != "unauthorized" {
		t.Fatalf("anonymous me = %d %v", resp.StatusCode, body)
	}
	resp, body = serve(t, h, withSession(httptest.NewRequest(http.MethodGet, APIPrefix+"/auth/me", nil), "alice"))
	if resp.StatusCode != http.StatusOK || body["role"] != auth.RoleAdmin || body["user"].(map[string]any)["email"] != "alice@suite.test" {
		t.Fatalf("me = %d %v", resp.StatusCode, body)
	}

	// A dead token is cleared and treated as anonymous; a switched-off person is refused outright.
	resp, _ = serve(t, h, withSession(httptest.NewRequest(http.MethodGet, APIPrefix+"/auth/me", nil), "stale"))
	if resp.StatusCode != http.StatusUnauthorized || len(resp.Cookies()) != 1 || resp.Cookies()[0].MaxAge >= 0 {
		t.Errorf("a stale session = %d, cookies %v", resp.StatusCode, resp.Cookies())
	}
	sessions.err = auth.ErrSwitchedOff
	resp, body = serve(t, h, withSession(httptest.NewRequest(http.MethodGet, APIPrefix+"/meta", nil), "alice"))
	if resp.StatusCode != http.StatusForbidden || errorOf(t, body)["code"] != "account_switched_off" {
		t.Errorf("a switched-off person = %d %v", resp.StatusCode, body)
	}
}

func TestLogoutEndsTheSessionAndClearsTheCookie(t *testing.T) {
	s, sessions, _ := signedInServer(t)
	req := withSession(httptest.NewRequest(http.MethodPost, APIPrefix+"/auth/logout", nil), "alice")
	req.Header.Set("Origin", "http://app.test")
	resp, _ := serve(t, s.Routes(nil), req)
	if resp.StatusCode != http.StatusNoContent || len(sessions.ended) != 1 || sessions.ended[0] != "alice" || resp.Cookies()[0].MaxAge >= 0 {
		t.Fatalf("logout = %d, ended %v, cookies %v", resp.StatusCode, sessions.ended, resp.Cookies())
	}
}

func TestTheProviderEndsSessionsWithAForm(t *testing.T) {
	s, _, signIn := signedInServer(t)
	post := func(form url.Values) (*http.Response, map[string]any) {
		req := httptest.NewRequest(http.MethodPost, APIPrefix+"/auth/oidc/backchannel-logout", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return serve(t, s.Routes(nil), req)
	}
	resp, _ := post(url.Values{"logout_token": {"jwt"}})
	if resp.StatusCode != http.StatusOK || signIn.loggedOut != "jwt" || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("logout = %d, token %q, cache %q", resp.StatusCode, signIn.loggedOut, resp.Header.Get("Cache-Control"))
	}
	resp, body := post(url.Values{})
	if resp.StatusCode != http.StatusBadRequest || errorOf(t, body)["code"] != "bad_request" {
		t.Errorf("no token = %d %v", resp.StatusCode, body)
	}
	signIn.logoutErr = fmt.Errorf("%w: replayed", oidc.ErrBadLogoutToken)
	resp, body = post(url.Values{"logout_token": {"jwt"}})
	if resp.StatusCode != http.StatusBadRequest || errorOf(t, body)["code"] != "invalid_logout_token" || resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("a refused token = %d %v", resp.StatusCode, body)
	}
}
