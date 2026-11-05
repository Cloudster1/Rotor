import type { Role } from "@/api/access";
import type { SkillResponse, SkillSummary, SkillVersion } from "@/api/skills";
import { ROLES } from "./access";
import { ME } from "./app";

/** A skill the signed-in person made and administers, with two versions, the second current. */
export const SKILL_ID = "0190a5c4-0000-7000-8000-00000000cc01";
export const BUILTIN_ID = "0190a5c4-0000-7000-8000-00000000cc02";

export const SKILL_MD_V2 = "---\nname: story-style\ndescription: How our team words a story.\n---\n\n# Story style\n\nWrite it short.\n";

export const SKILL: SkillSummary = {
  id: SKILL_ID,
  name: "story-style",
  description: "How our team words a story.",
  owner: { id: ME.user.id, name: ME.user.name },
  currentVersion: 2,
  archived: false,
  builtin: false,
  canEdit: true,
  canAdminister: true,
  updatedAt: "2026-11-04T08:00:00Z",
};

export const BUILTIN: SkillSummary = {
  ...SKILL,
  id: BUILTIN_ID,
  name: "requirements-engineering",
  description: "Requirements engineering method.",
  owner: null,
  currentVersion: 3,
  builtin: true,
  canEdit: false,
  canAdminister: false,
};

export const version = (number: number, files: { path: string; bytes: number }[], more: Partial<SkillVersion> = {}): SkillVersion => ({
  number,
  comment: number === 1 ? "" : "Shorter",
  description: SKILL.description,
  hash: `h${number}`,
  bytes: files.reduce((sum, f) => sum + f.bytes, 0),
  createdAt: "2026-11-04T08:00:00Z",
  createdBy: { id: ME.user.id, name: ME.user.name },
  frontMatter: { name: SKILL.name, description: SKILL.description, license: "Apache-2.0" },
  files: files.map((f) => ({ ...f, sha256: `s-${f.path}` })),
  ...more,
});

export const V2 = version(2, [
  { path: "references/examples.md", bytes: 12 },
  { path: "SKILL.md", bytes: SKILL_MD_V2.length },
]);

export const SKILL_RESPONSE: SkillResponse = { skill: SKILL, version: V2 };

/** The skill roles as a fresh installation has them, beside the workflow roles the access fixtures hold. */
export const SKILL_ROLES: Role[] = [
  ...ROLES,
  ...[
    ["skill_administrator", "Skill administrator", ["skill.read", "skill.edit", "skill.administer"]],
    ["skill_editor", "Skill editor", ["skill.read", "skill.edit"]],
    ["skill_reader", "Skill reader", ["skill.read"]],
  ].map(
    ([key, name, permissions]): Role => ({
      key: key as string,
      name: name as string,
      description: `${name} of the test installation.`,
      installationWideOnly: false,
      builtin: true,
      permissions: permissions as string[],
      inUse: 0,
    }),
  ),
];

/** What a skill's pages ask for, its current version holding V2's files. */
export function skillAnswers(skill: SkillSummary = SKILL) {
  const current = { ...V2, number: skill.currentVersion };
  return {
    [`/api/v1/skills/${skill.id}`]: { skill, version: current },
    [`/api/v1/skills/${skill.id}/versions/${current.number}`]: current,
    [`/api/v1/skills/${skill.id}/versions/${current.number}/file`]: (request: Request) => {
      const path = new URL(request.url).searchParams.get("path") ?? "";
      const content = path === "SKILL.md" ? SKILL_MD_V2 : "An example.\n";
      return Response.json({ path, content, bytes: content.length, sha256: `s-${path}` });
    },
    [`/api/v1/skills/${skill.id}/grants`]: { grants: [] },
    "POST /api/v1/skills/validate": { valid: true, problems: [] },
  };
}
