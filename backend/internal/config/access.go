package config

// Access as administrators keep it (#203): roles, groups, grants and the people they go to.
const (
	// MaxRoleKey is the longest key a role may have, as the database's role_key_shape allows.
	MaxRoleKey = 40
	// AccessPageSize and AccessMaxPage bound one page of grants or of people.
	AccessPageSize = 50
	AccessMaxPage  = 200
	// AccessMaxOffset bounds how far into a listing a page may start.
	AccessMaxOffset = 1 << 20
	// PeopleLookupSize and PeopleLookupMax bound the matches a person picker is offered at once.
	PeopleLookupSize = 10
	PeopleLookupMax  = 50
)
