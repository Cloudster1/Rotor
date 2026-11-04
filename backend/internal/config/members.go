package config

import (
	"fmt"
	"strings"
)

// Bootstrap members (#171), as Stator and Exciter read theirs, so Dynamo's DYNAMO_MEMBERS means the same here.
const (
	// BootstrapMembersVar names the people let in by their address, before or at their first sign-in.
	BootstrapMembersVar = "ROTOR_BOOTSTRAP_MEMBERS"
	// MemberRoleAdmin also grants Administrator to a person the list lets in; owner is the siblings' word for it.
	MemberRoleAdmin  = "admin"
	MemberRoleOwner  = "owner"
	MemberRoleMember = "member"
	// DefaultGroupsClaim is the claim the realm's groups mapper writes, seeding the provider's groups claim.
	DefaultGroupsClaim = "groups"
)

// Member is one person the bootstrap list lets in, by address; Admin grants them Administrator as well.
type Member struct {
	Email string
	Admin bool
}

// members reads key as entries separated by commas or white space, each an address or address=role, and names
// every entry it cannot read, which stops the start.
func (l *loader) members(key string) []Member {
	var out []Member
	seen := map[string]bool{}
	for _, entry := range strings.FieldsFunc(l.str(key, ""), func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' }) {
		member, ok := parseMember(entry)
		if !ok {
			l.problem(fmt.Sprintf("%s has %q; write each entry as an email address, such as alice@example.com, or as "+
				"email=role with the role admin or member, and separate them with commas or spaces.", key, entry))
			continue
		}
		if seen[member.Email] {
			continue
		}
		seen[member.Email] = true
		out = append(out, member)
	}
	return out
}

// parseMember reads "alice@example.com" or "alice@example.com=admin"; the address is lowercased, as sign-in does.
func parseMember(entry string) (Member, bool) {
	email, role, hasRole := strings.Cut(entry, "=")
	email = strings.ToLower(strings.TrimSpace(email))
	local, domain, found := strings.Cut(email, "@")
	if !found || local == "" || domain == "" || strings.Contains(domain, "@") || strings.ContainsAny(email, "<>\"") {
		return Member{}, false
	}
	switch role = strings.ToLower(strings.TrimSpace(role)); {
	case !hasRole, role == MemberRoleMember:
		return Member{Email: email}, true
	case role == MemberRoleAdmin, role == MemberRoleOwner:
		return Member{Email: email, Admin: true}, true
	default:
		return Member{}, false
	}
}
