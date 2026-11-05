package config

// Skill limits (#219); docs/api-contract-m17.md names each, and MaxSkillNameChars sits with the workflow limits.
const (
	// MaxSkillDescriptionChars bounds a skill's description, as Agent Skills and the table do.
	MaxSkillDescriptionChars = 1024
	// MaxSkillFiles bounds the files of one version, SKILL.md included.
	MaxSkillFiles = 50
	// MaxSkillFileBytes bounds one file of a skill.
	MaxSkillFileBytes = 256 << 10
	// MaxSkillVersionBytes bounds all of a version's files together.
	MaxSkillVersionBytes = 1 << 20
	// MaxSkillPathDepth is how many folders below the skill's root a file may sit.
	MaxSkillPathDepth = 5
	// MaxSkillReadBytes bounds what one read_skill call answers, so a long file costs its tokens in parts (#222).
	MaxSkillReadBytes = 64 << 10
	// MaxSkillBodyBytes bounds a request carrying a version's files: JSON escaping can double text, plus the keys.
	MaxSkillBodyBytes = 2*MaxSkillVersionBytes + 64<<10
	// SkillPageSize and SkillMaxPage bound one page of the skills listing.
	SkillPageSize = 50
	SkillMaxPage  = 200
)
