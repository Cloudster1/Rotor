package oidc

import "strings"

// identityFromClaims reads the claims Rotor cares about out of a token that has already been verified.
func identityFromClaims(issuer, subject string, claims map[string]any) (*Identity, error) {
	identity := Identity{
		Issuer:  issuer,
		Subject: subject,
		Email:   strings.ToLower(strings.TrimSpace(asString(claims["email"]))),
		Name:    strings.TrimSpace(asString(claims["name"])),
		SID:     asString(claims["sid"]),
	}
	if identity.Email == "" {
		return nil, ErrNoEmail
	}
	// An address the provider never checked is not the provider vouching for the person.
	switch verified := claims["email_verified"].(type) {
	case bool:
		if !verified {
			return nil, ErrEmailUnverified
		}
	case string:
		if strings.EqualFold(verified, "false") {
			return nil, ErrEmailUnverified
		}
	}
	if identity.Name == "" {
		identity.Name = strings.TrimSpace(asString(claims["given_name"]) + " " + asString(claims["family_name"]))
	}
	if identity.Name == "" {
		identity.Name = strings.TrimSpace(asString(claims["preferred_username"]))
	}
	if identity.Name == "" {
		identity.Name, _, _ = strings.Cut(identity.Email, "@")
	}
	identity.Groups = normalizeGroups(asStrings(claims[groupsClaim]))
	return &identity, nil
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

// asStrings reads a list of strings, tolerating a provider that sends one string for one group.
func asStrings(v any) []string {
	switch value := v.(type) {
	case string:
		return []string{value}
	case []any:
		out := make([]string, 0, len(value))
		for _, each := range value {
			if s, ok := each.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// normalizeGroups trims, drops blanks, a leading slash and repeats, and keeps the order sent.
func normalizeGroups(groups []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, g := range groups {
		g = strings.TrimPrefix(strings.TrimSpace(g), "/")
		if g == "" || seen[g] {
			continue
		}
		seen[g] = true
		out = append(out, g)
	}
	return out
}
