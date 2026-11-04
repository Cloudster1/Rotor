package httpapi

import (
	"net/http"
	"reflect"
	"time"

	"github.com/cloudster1/rotor/backend/internal/openapi"
)

// The rows of #204: who waits to be let in on Admin's Users page, and single sign-on on its Access page;
// docs/api-contract-access.md says what they mean.
func init() {
	operations = append(operations, signOnOperations...)
}

var signOnOperations = []operation{
	{method: "GET", path: "/users/requests", handler: "handleListJoinRequests", tag: "access", admin: true,
		summary:   "Who signed in through the identity provider and is waiting to be let in, oldest first.",
		responses: ok(env{"requests": []joinRequestView{}})},
	{method: "POST", path: "/users/requests/{userID}/admit", handler: "handleAdmitJoinRequest", tag: "access", admin: true,
		summary:     "Let a waiting person in, as a member or as an administrator.",
		description: "They join Everyone and sign in from their next attempt. Somebody not waiting answers 404.",
		request:     admitRequest{},
		responses:   map[int]any{http.StatusOK: env{"user": userView{}}, http.StatusNotFound: notFound, http.StatusUnprocessableEntity: refused}},
	{method: "DELETE", path: "/users/requests/{userID}", handler: "handleDeclineJoinRequest", tag: "access", admin: true,
		summary:   "Turn a waiting person away; they may ask again by signing in again.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusNotFound: notFound}},
	{method: "GET", path: "/oidc-provider", handler: "handleGetOIDCProvider", tag: "access", admin: true,
		summary:     "Single sign-on: the identity provider people sign in through, with whether a client secret is held.",
		description: "The secret itself is never answered. provider is null while none is set up.",
		responses:   ok(oidcProviderResponse{})},
	{method: "PUT", path: "/oidc-provider", handler: "handleSaveOIDCProvider", tag: "access", admin: true,
		summary: "Set single sign-on up or change it; it applies from the next sign-in.",
		description: "A blank clientSecret keeps the one held. A provider that could not work is refused with 422: no issuer, " +
			"no client ID, no secret, or an issuer whose discovery document cannot be read. Turning it off while it is the " +
			"only way in is refused with 409.",
		request:   saveOIDCProviderRequest{},
		responses: map[int]any{http.StatusOK: oidcProviderResponse{}, http.StatusConflict: refused, http.StatusUnprocessableEntity: refused}},
}

// admitStanding is what a waiting person is let in as: a member, or a member who also administers Rotor.
type admitStanding string

const (
	admitMember admitStanding = "member"
	admitAdmin  admitStanding = "admin"
)

func registerSignOnSchemas(b *openapi.Builder) {
	b.Enums[reflect.TypeOf(admitStanding(""))] = []string{string(admitMember), string(admitAdmin)}
}

// joinRequestView is somebody waiting to be let in.
type joinRequestView struct {
	UserID      string    `json:"userId"`
	Email       string    `json:"email"`
	Name        string    `json:"name"`
	RequestedAt time.Time `json:"requestedAt"`
}

// admitRequest lets somebody in; a member when role is left out.
type admitRequest struct {
	Role admitStanding `json:"role,omitempty"`
}

// oidcProviderView is single sign-on without its secret.
type oidcProviderView struct {
	Issuer       string `json:"issuer"`
	ClientID     string `json:"clientId"`
	HasSecret    bool   `json:"hasSecret"`
	GroupsClaim  string `json:"groupsClaim"`
	AdmitByGroup bool   `json:"admitByGroup"`
	Enabled      bool   `json:"enabled"`
	// ChangedBy names who saved it last, absent when the environment seeded it.
	ChangedBy *string   `json:"changedBy,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// oidcProviderResponse is the provider, what its client must allow, and whether it may be turned off.
type oidcProviderResponse struct {
	Provider *oidcProviderView `json:"provider"`
	// RedirectURL is where the provider sends people back after signing in.
	RedirectURL string `json:"redirectUrl"`
	// CanTurnOff is false while single sign-on is the only way into Rotor.
	CanTurnOff bool `json:"canTurnOff"`
}

// saveOIDCProviderRequest sets the provider; enabled left out keeps it on, and a blank clientSecret keeps the one held.
type saveOIDCProviderRequest struct {
	Issuer       string `json:"issuer"`
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret,omitempty"`
	GroupsClaim  string `json:"groupsClaim,omitempty"`
	AdmitByGroup bool   `json:"admitByGroup,omitempty"`
	Enabled      *bool  `json:"enabled,omitempty"`
}
