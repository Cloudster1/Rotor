package toolstub

import (
	"crypto/subtle"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// The consent page and the token and revocation endpoints. Exciter's consent page is its web client; the stub's is
// one plain form that a browser suite and a Go test can both fill: a username, then Allow or Cancel.

// Consent form fields, which Rotor's browser suite and Go tests fill.
const (
	FieldRequest  = "request"
	FieldUsername = "username"
	FieldReadOnly = "read_only"
	FieldDecision = "decision"
	DecisionAllow = "allow"
	DecisionDeny  = "deny"
)

var consentPage = template.Must(template.New("consent").Parse(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>body{font-family:system-ui,sans-serif;max-width:32rem;margin:2rem auto;padding:0 1rem;color:#1a1a1a;background:#fff}
label,input,button{display:block;font-size:1rem}input[type=text]{margin:.25rem 0 1rem;padding:.5rem;width:100%;box-sizing:border-box}
.check{display:flex;gap:.5rem;align-items:center;margin-bottom:1rem}.actions{display:flex;gap:1rem}
button{padding:.5rem 1rem;border:1px solid #1a1a1a;border-radius:.25rem;background:#fff;color:#1a1a1a;cursor:pointer}
button[value=allow]{background:#1a1a1a;color:#fff}</style></head>
<body><main>
<h1>{{.Title}}</h1>
{{if .Problem}}<p role="alert" data-consent-error="{{.Problem}}">{{.Message}}</p>{{else}}
<p><strong>{{.Client}}</strong> wants to use {{.Product}} as you{{if .ReadOnly}}, only to read{{end}}. It goes back to {{.Host}}.</p>
<form method="post" action="consent">
<input type="hidden" name="request" value="{{.Request}}">
<label for="username">Username</label>
<input type="text" id="username" name="username" autocomplete="username" required>
{{if not .ReadOnly}}<div class="check"><input type="checkbox" id="read_only" name="read_only" value="1">
<label for="read_only">Only let it read</label></div>{{end}}
<div class="actions"><button type="submit" name="decision" value="allow" data-action="allow">Allow</button>
<button type="submit" name="decision" value="deny" data-action="deny" formnovalidate>Cancel</button></div>
</form>{{end}}
</main></body></html>
`))

type consentView struct {
	Title, Client, Product, Host, Request, Problem, Message string
	ReadOnly                                                bool
}

// pageProblems are the reasons the page names when the browser cannot go back to the client, Exciter's.
var pageProblems = map[string]string{
	"invalid_client":   "This tool does not know the client that sent you here. Go back and connect again.",
	"invalid_redirect": "The client sent you here with an address to go back to that it never registered.",
	"expired":          "This request to connect has expired or was answered already. Start connecting again.",
}

func (s *Stub) handleConsentPage(c *call) {
	view := consentView{Title: "Connect to " + c.t.product(), Product: c.t.product()}
	if problem := c.r.URL.Query().Get("error"); problem != "" {
		view.Problem, view.Message = problem, pageProblems[problem]
		if view.Message == "" {
			view.Message = "Something went wrong. Start connecting again."
		}
		s.renderConsent(c, http.StatusBadRequest, view)
		return
	}
	id := c.r.URL.Query().Get(FieldRequest)
	req, ok := c.t.requests[id]
	if !ok || !s.now().Before(req.expires) {
		view.Problem, view.Message = "expired", pageProblems["expired"]
		s.renderConsent(c, http.StatusNotFound, view)
		return
	}
	view.Title = "Connect " + req.client.name + " to " + c.t.product()
	view.Client, view.Host, view.Request, view.ReadOnly = req.client.name, redirectHost(req.redirect), id, req.readOnly
	s.renderConsent(c, http.StatusOK, view)
}

func (s *Stub) renderConsent(c *call, status int, view consentView) {
	c.w.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.w.Header().Set("Cache-Control", "no-store")
	c.w.WriteHeader(status)
	_ = consentPage.Execute(c.w, view)
}

// handleConsent answers a request as the person who typed their name: allowing ends an earlier grant of the same
// client for them, makes the grant and sends the browser back with a code; cancelling sends it back refused.
func (s *Stub) handleConsent(c *call) {
	c.r.Body = http.MaxBytesReader(c.w, c.r.Body, MaxFormBytes)
	if err := c.r.ParseForm(); err != nil {
		http.Error(c.w, "Send the consent form.", http.StatusBadRequest)
		return
	}
	id := c.r.PostForm.Get(FieldRequest)
	req, ok := c.t.requests[id]
	if !ok || !s.now().Before(req.expires) {
		http.Redirect(c.w, c.r, "consent?error=expired", http.StatusSeeOther)
		return
	}
	back := func(q url.Values) {
		q.Set("state", req.state)
		q.Set("iss", c.issuer)
		http.Redirect(c.w, c.r, withQuery(req.redirect, q), http.StatusSeeOther)
	}
	username := strings.TrimSpace(c.r.PostForm.Get(FieldUsername))
	if c.r.PostForm.Get(FieldDecision) != DecisionAllow {
		delete(c.t.requests, id)
		back(url.Values{"error": {errAccessDenied}, "error_description": {"The person declined to connect the client."}})
		return
	}
	if username == "" {
		view := consentView{Title: "Connect " + req.client.name + " to " + c.t.product(), Client: req.client.name,
			Product: c.t.product(), Host: redirectHost(req.redirect), Request: id, ReadOnly: req.readOnly}
		s.renderConsent(c, http.StatusUnprocessableEntity, view)
		return
	}
	delete(c.t.requests, id)
	for gid, g := range c.t.grants {
		if g.clientID == req.client.id && g.username == username {
			c.t.endGrant(gid)
		}
	}
	readOnly := (req.readOnly || c.r.PostForm.Get(FieldReadOnly) != "") && !req.evid
	g := &grant{id: secret(""), clientID: req.client.id, username: username, readOnly: readOnly, evidence: req.evid}
	c.t.grants[g.id] = g
	plain := secret(codePrefix)
	c.t.codes[string(digest(plain))] = &code{grant: g, redirect: req.redirect, challenge: req.challenge, expires: s.now().Add(CodeTTL)}
	c.t.stats.Consents++
	back(url.Values{"code": {plain}})
}

// endGrant ends a grant with every code and token it made.
func (t *tenant) endGrant(id string) {
	g, ok := t.grants[id]
	if !ok {
		return
	}
	delete(t.grants, id)
	for k, cd := range t.codes {
		if cd.grant == g {
			delete(t.codes, k)
		}
	}
	for k, tk := range t.tokens {
		if tk.grant == g {
			delete(t.tokens, k)
		}
	}
	t.stats.GrantsEnded++
}

// clientAuth reads who a client says it is, preferring HTTP Basic, whose parts are form-encoded (RFC 6749, 2.3.1).
func clientAuth(r *http.Request) (id, secret string, basic, ok bool) {
	if id, secret, isBasic := r.BasicAuth(); isBasic {
		id, idErr := url.QueryUnescape(id)
		secret, secretErr := url.QueryUnescape(secret)
		return id, secret, true, idErr == nil && secretErr == nil
	}
	return r.PostForm.Get("client_id"), r.PostForm.Get("client_secret"), false, true
}

// authenticate holds a client to the method it registered with; nil when it is not who it says.
func (t *tenant) authenticate(w http.ResponseWriter, r *http.Request) (*client, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxFormBytes)
	if err := r.ParseForm(); err != nil {
		refuse(w, http.StatusBadRequest, errInvalidRequest, "Send the request as a form, application/x-www-form-urlencoded.", false)
		return nil, false
	}
	id, plain, basic, ok := clientAuth(r)
	cl := t.clients[id]
	wrong := cl == nil || !ok
	if !wrong {
		switch cl.authMethod {
		case "none":
			wrong = plain != ""
		case "client_secret_basic":
			wrong = !basic || subtle.ConstantTimeCompare(digest(plain), cl.secretHash) != 1
		case "client_secret_post":
			wrong = basic || subtle.ConstantTimeCompare(digest(plain), cl.secretHash) != 1
		}
	}
	if wrong {
		refuse(w, http.StatusUnauthorized, errInvalidClient,
			"The client could not be authenticated. Send its secret as it registered, or register it again.", basic)
		return nil, basic
	}
	return cl, basic
}

const errGrantGone = "The code or refresh token is not one this client may use: it expired, was used, or was ended. Connect the client again."

func (s *Stub) handleToken(c *call) {
	cl, basic := c.t.authenticate(c.w, c.r)
	if cl == nil {
		return
	}
	f := c.r.PostForm
	kind := f.Get("grant_type")
	switch {
	case kind != "authorization_code" && kind != "refresh_token":
		refuse(c.w, http.StatusBadRequest, errUnsupportedGrantType, "The grants offered are authorization_code and refresh_token.", basic)
		return
	case !slices.Contains(cl.grantTypes, kind):
		refuse(c.w, http.StatusBadRequest, errUnauthorizedClient, "The client did not register for "+kind+".", basic)
		return
	case f.Get("resource") != "" && strings.TrimRight(f.Get("resource"), "/") != c.resource():
		refuse(c.w, http.StatusBadRequest, errInvalidTarget, "The one resource is the MCP endpoint, "+c.resource()+".", basic)
		return
	}
	var (
		g        *grant
		readOnly bool
		why      string
	)
	if kind == "authorization_code" {
		g, readOnly, why = s.redeemCode(c.t, cl, f)
	} else {
		g, readOnly, why = s.turnRefresh(c.t, cl, f)
	}
	if g == nil {
		code := errInvalidGrant
		if strings.HasPrefix(why, "A refresh may only narrow") {
			code = errInvalidScope
		}
		refuse(c.w, http.StatusBadRequest, code, why, basic)
		return
	}
	access, refresh := secret(accessPrefix), secret(refreshPrefix)
	c.t.tokens[string(digest(access))] = &token{grant: g, readOnly: readOnly, expires: s.now().Add(c.t.accessTTL())}
	c.t.tokens[string(digest(refresh))] = &token{grant: g, refresh: true, readOnly: readOnly, expires: s.now().Add(RefreshTTL)}
	c.w.Header().Set("Pragma", "no-cache")
	writeJSON(c.w, http.StatusOK, map[string]any{"access_token": access, "token_type": "Bearer",
		"expires_in": int(c.t.accessTTL().Seconds()), "refresh_token": refresh, "scope": scopeName(readOnly, g.evidence)})
}

func (s *Stub) redeemCode(t *tenant, cl *client, f url.Values) (*grant, bool, string) {
	cd, ok := t.codes[string(digest(f.Get("code")))]
	switch {
	case !ok || cd.grant.clientID != cl.id:
		return nil, false, errGrantGone
	case cd.used:
		t.endGrant(cd.grant.id)
		t.stats.ReuseEvents++
		return nil, false, "The code was used already, so the grant it made is ended. Connect the client again."
	case !s.now().Before(cd.expires):
		return nil, false, errGrantGone
	case f.Get("redirect_uri") != cd.redirect:
		return nil, false, "Send the redirect_uri the code was asked for with."
	case !verifyPKCE(cd.challenge, f.Get("code_verifier")):
		return nil, false, "The code_verifier is not the one the code_challenge was made from."
	}
	cd.used = true
	t.stats.CodesRedeemed++
	return cd.grant, cd.grant.readOnly, ""
}

// turnRefresh rotates a refresh token. One that was replaced already ends its grant, as a stolen one would.
func (s *Stub) turnRefresh(t *tenant, cl *client, f url.Values) (*grant, bool, string) {
	tk, ok := t.tokens[string(digest(f.Get("refresh_token")))]
	switch {
	case !ok || !tk.refresh || tk.grant.clientID != cl.id:
		return nil, false, errGrantGone
	case tk.replaced:
		t.stats.ReuseEvents++
		if !t.reuseDetection() {
			return nil, false, errGrantGone
		}
		t.endGrant(tk.grant.id)
		return nil, false, "The refresh token was used already, so the grant is ended, as a stolen one would be. Connect the client again."
	case !s.now().Before(tk.expires):
		return nil, false, errGrantGone
	}
	asked := f.Get("scope")
	askedRead, askedEvidence, ok := scopeAsked(asked)
	if !ok || (asked != "" && askedEvidence != tk.grant.evidence) || (tk.readOnly && asked != "" && !askedRead) {
		return nil, false, "A refresh may only narrow the scope, to mcp:read, and an evidence grant stays evidence."
	}
	tk.replaced = true
	t.stats.Refreshes++
	// Narrowing on a refresh sticks for the rest of the chain, as it does at Exciter.
	return tk.grant, !tk.grant.evidence && (tk.readOnly || (asked != "" && askedRead)), ""
}

// handleRevoke ends an access token, or a refresh token and with it the grant (RFC 7009); a token unknown, ended or
// another client's answers 200 all the same.
func (s *Stub) handleRevoke(c *call) {
	cl, basic := c.t.authenticate(c.w, c.r)
	if cl == nil {
		return
	}
	plain := c.r.PostForm.Get("token")
	if plain == "" {
		refuse(c.w, http.StatusBadRequest, errInvalidRequest, "Send the token to end.", basic)
		return
	}
	if tk, ok := c.t.tokens[string(digest(plain))]; ok && tk.grant.clientID == cl.id {
		c.t.stats.Revocations++
		if tk.refresh {
			c.t.endGrant(tk.grant.id)
		} else {
			delete(c.t.tokens, string(digest(plain)))
		}
	}
	c.w.Header().Set("Cache-Control", "no-store")
	c.w.WriteHeader(http.StatusOK)
}
