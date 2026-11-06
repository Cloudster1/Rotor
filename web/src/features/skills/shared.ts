import { useQuery } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { type SkillResponse, skillQuery } from "@/api/skills";
import { SKILL_MD } from "@/config";

/** The skill the address names, as every section reads it; the layout waits for it, so a section always has it. */
export function useSkill(): { skillID: string; existing: SkillResponse | undefined; failed: boolean } {
  const { skillID } = useParams({ from: "/shell/skills/$skillID" });
  const skill = useQuery(skillQuery(skillID));
  return { skillID, existing: skill.data, failed: skill.isError };
}

/** SKILL.md first, as an agent reads it first, then the other files by path. */
export function ordered<T extends { path: string }>(files: readonly T[]): T[] {
  return [...files].sort((a, b) => (a.path === SKILL_MD ? -1 : b.path === SKILL_MD ? 1 : a.path < b.path ? -1 : a.path > b.path ? 1 : 0));
}
