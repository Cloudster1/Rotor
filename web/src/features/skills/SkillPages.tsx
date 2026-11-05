import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { accessQueryKey } from "@/api/access";
import { grantOnSkill, revokeOnSkill, skillGrantsQuery, skillsQueryKey } from "@/api/skills";
import { SKILL_GRANT_ROLES } from "@/config";
import { GrantsPanel } from "@/features/access/GrantsPanel";
import { Tag } from "@/features/access/ui";
import { skillGroups } from "@/features/settings/groups";
import { Layout } from "@/features/settings/SettingsLayout";
import { link } from "@/features/settings/styles";
import { SettingsPage } from "@/features/settings/ui";
import { PageHeader } from "@/features/shell/EmptyState";
import { SectionNote } from "@/features/workflows/settings/shared";
import { s } from "@/strings";
import { useSkill } from "./shared";

export { FilesPage } from "./FilesSection";
export { GeneralPage } from "./GeneralSection";
export { VersionsPage } from "./VersionsSection";

/** A skill's pages: which skill, a way back to the list, and its sections beside the one open (#223). */
export function SkillLayout() {
  const { skillID, existing, failed } = useSkill();
  const t = s.skills.layout;
  if (failed) {
    return (
      <div>
        <PageHeader title={s.places.skills} />
        <p role="alert" className="text-sm text-danger" data-skill-not-found>
          {t.notFound}
        </p>
      </div>
    );
  }
  if (!existing) return <p className="text-sm text-ink-muted">{t.loading}</p>;
  const { skill: summary } = existing;
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="flex flex-wrap items-center gap-2 text-sm font-medium text-ink-muted" data-skill-of={summary.name}>
          {t.of(summary.name)}
          {summary.builtin && <Tag>{s.skills.list.builtin}</Tag>}
          {summary.archived && <Tag>{s.skills.list.archived}</Tag>}
        </p>
        <Link to="/skills" className={link} data-action="all-skills">
          {t.back}
        </Link>
      </div>
      <Layout label={t.nav} groups={skillGroups(skillID, summary.builtin)} />
    </div>
  );
}

/** Who holds a role on the skill; its administrators change it, anybody else reads it; a built-in one is nobody's to share. */
export function SharingPage() {
  const { skillID, existing } = useSkill();
  const client = useQueryClient();
  const builtin = existing?.skill.builtin ?? false;
  const grants = useQuery({ ...skillGrantsQuery(skillID), enabled: existing !== undefined && !builtin });
  if (!existing) return null;
  const { skill } = existing;
  const t = s.skills.sharing;
  // A grant changes what the list says about the skill and what people may do with it.
  const refresh = () => Promise.all([client.invalidateQueries({ queryKey: accessQueryKey }), client.invalidateQueries({ queryKey: skillsQueryKey })]);
  return (
    <SettingsPage title={s.skills.layout.pages.sharing}>
      {builtin ? (
        <SectionNote data-read-only="sharing">{t.builtin}</SectionNote>
      ) : (
        <div className="lg:col-span-full">
          <GrantsPanel
            grants={grants}
            roles={SKILL_GRANT_ROLES}
            administers={skill.canAdminister && !skill.archived}
            words={t}
            grant={(body) => grantOnSkill(skillID, body)}
            revoke={(grantID) => revokeOnSkill(skillID, grantID)}
            refresh={refresh}
            note={<p className="mb-3 text-sm text-ink">{t.everyone}</p>}
            data-skill-grants=""
          />
        </div>
      )}
    </SettingsPage>
  );
}
