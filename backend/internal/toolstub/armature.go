package toolstub

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// A tenant whose product is armature answers as Armature's partner OAuth does (#45, Armature's docs/oauth.md): its
// scopes, its whole API as the resource with metadata at its root only, a client pending until allowed, one access
// token per grant that a refresh replaces and a revocation ends with the grant, and RFC 8693 exchange for runs.

// Armature's scopes; read is required with every other, webhooks.partner is the app's own webhook.
const (
	ArmatureRead            = "read"
	ArmatureIssueWrite      = "issue.write"
	ArmatureIssueTransition = "issue.transition"
	ArmatureCommentWrite    = "comment.write"
	ArmatureWebhooksPartner = "webhooks.partner"
	// grantExchange is RFC 8693's grant type, and tokenTypeAccess the one token type it trades.
	grantExchange   = "urn:ietf:params:oauth:grant-type:token-exchange"
	tokenTypeAccess = "urn:ietf:params:oauth:token-type:access_token"
	// lifetimeParam is Armature's parameter for how many seconds an exchanged token lives.
	lifetimeParam = "armature_expires_in"
)

var (
	armatureScopes     = []string{ArmatureRead, ArmatureIssueWrite, ArmatureIssueTransition, ArmatureCommentWrite, ArmatureWebhooksPartner}
	armatureGrantTypes = []string{"authorization_code", "refresh_token", grantExchange}
	exciterGrantTypes  = []string{"authorization_code", "refresh_token", grantExchange}
)

func (t *tenant) armature() bool { return t.product() == "armature" }

// resourceScopes are what the protected resource metadata lists; Armature leaves out the app's own webhook.
func (t *tenant) resourceScopes() []string {
	if t.armature() {
		return slices.DeleteFunc(slices.Clone(armatureScopes), func(s string) bool { return s == ArmatureWebhooksPartner })
	}
	return scopes
}

func (t *tenant) serverScopes() []string {
	if t.armature() {
		return armatureScopes
	}
	return scopes
}

func (t *tenant) grantTypes() []string {
	if len(t.opts.GrantTypes) > 0 {
		return t.opts.GrantTypes
	}
	if t.armature() {
		return armatureGrantTypes
	}
	return exciterGrantTypes
}

// registrableGrants are what a client may register for: Exciter keeps its exchange to the partner apps an
// administrator names, which trade the identity provider's tokens (#139).
func (t *tenant) registrableGrants() []string {
	if len(t.opts.GrantTypes) > 0 || t.armature() {
		return t.grantTypes()
	}
	return grantTypes
}

// resourceAccepted says whether a token may be asked for that resource: Armature takes its API or its MCP endpoint.
func (c *call) resourceAccepted(asked string) bool {
	asked = strings.TrimRight(asked, "/")
	return asked == c.resource() || (c.t.armature() && asked == c.issuer+mcpPath)
}

// armatureScopesAsked reads a scope as Armature does: its names only, read among them, and no partner webhook here.
func armatureScopesAsked(asked string) ([]string, bool) {
	fields := strings.Fields(asked)
	if !slices.Contains(fields, ArmatureRead) || slices.Contains(fields, ArmatureWebhooksPartner) {
		return nil, false
	}
	for _, f := range fields {
		if !slices.Contains(armatureScopes, f) {
			return nil, false
		}
	}
	slices.Sort(fields)
	return slices.Compact(fields), true
}

// writes says whether Armature scopes let a token write anything.
func writes(granted []string) bool {
	return slices.ContainsFunc(granted, func(s string) bool { return s != ArmatureRead })
}

// replaceAccess ends a grant's access tokens before it gets a new one, since Armature keeps one per grant; tokens
// exchanged from it stay.
func (t *tenant) replaceAccess(g *grant) {
	for k, tk := range t.tokens {
		if tk.grant == g && !tk.refresh && !tk.exchanged {
			delete(t.tokens, k)
		}
	}
}

// handleExchange trades a grant's access token for a narrower, shorter one of its own, as Armature does for Rotor.
func (s *Stub) handleExchange(c *call, cl *client, basic bool) {
	f := c.r.PostForm
	subject, ok := c.t.tokens[string(digest(f.Get("subject_token")))]
	switch {
	case !slices.Contains(cl.grantTypes, grantExchange):
		refuse(c.w, http.StatusBadRequest, errUnauthorizedClient, "The client did not register for "+grantExchange+".", basic)
		return
	case f.Get("subject_token_type") != tokenTypeAccess:
		refuse(c.w, http.StatusBadRequest, errInvalidRequest, "Armature exchanges access tokens only: subject_token_type is "+tokenTypeAccess+".", basic)
		return
	case !ok || subject.refresh || subject.exchanged || subject.grant.clientID != cl.id || !s.now().Before(subject.expires):
		refuse(c.w, http.StatusBadRequest, errInvalidGrant, errGrantGone, basic)
		return
	}
	wanted := subject.grant.scopes
	if asked := f.Get("scope"); asked != "" {
		fields, ok := armatureScopesAsked(asked)
		if !ok || slices.ContainsFunc(fields, func(s string) bool { return !slices.Contains(subject.grant.scopes, s) }) {
			refuse(c.w, http.StatusBadRequest, errInvalidScope, "Ask for at most the grant's scopes, read among them.", basic)
			return
		}
		wanted = fields
	}
	ttl := config.ToolStubMaxExchangedTTL
	if seconds, err := strconv.Atoi(f.Get(lifetimeParam)); err == nil && seconds > 0 && time.Duration(seconds)*time.Second < ttl {
		ttl = time.Duration(seconds) * time.Second
	}
	access := secret(accessPrefix)
	c.t.tokens[string(digest(access))] = &token{grant: subject.grant, exchanged: true, scopes: wanted, readOnly: !writes(wanted),
		expires: s.now().Add(ttl)}
	c.t.stats.Exchanges++
	c.w.Header().Set("Cache-Control", "no-store")
	writeJSON(c.w, http.StatusOK, map[string]any{"access_token": access, "issued_token_type": tokenTypeAccess, "token_type": "Bearer",
		"expires_in": int(ttl.Seconds()), "scope": strings.Join(wanted, " ")})
}

// handleApproveClients allows every pending client, as an Armature administrator does on the consent page.
func (s *Stub) handleApproveClients(w http.ResponseWriter, _ *http.Request, t *tenant) {
	approved := 0
	for _, cl := range t.clients {
		if cl.pending {
			cl.pending = false
			approved++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"approved": approved})
}
