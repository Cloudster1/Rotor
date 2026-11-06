import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useId, useState } from "react";
import { archiveSkill, restoreSkill, type SkillResponse, type SkillSummary, skillsQueryKey } from "@/api/skills";
import { SettingsCard, SettingsPage } from "@/features/settings/ui";
import { Refusal } from "@/features/shell/Dialog";
import { s } from "@/strings";
import { useSkill } from "./shared";
import { secondary } from "./styles";

// Name and description are shown above the rest, so the list of other front matter leaves them out.
const SHOWN_KEYS = ["name", "description"];

/** What the skill says about itself, and archiving it with an Undo, as a workflow's General page does. */
export function GeneralPage() {
  const { existing } = useSkill();
  if (!existing) return null;
  return (
    <SettingsPage title={s.skills.layout.pages.general}>
      <Details existing={existing} />
      <ArchiveCard skill={existing.skill} />
    </SettingsPage>
  );
}

function Details({ existing }: { existing: SkillResponse }) {
  const id = useId();
  const t = s.skills.general;
  const { skill, version } = existing;
  const rest = Object.entries(version.frontMatter).filter(([key]) => !SHOWN_KEYS.includes(key));
  const row = "grid gap-1 sm:grid-cols-[10rem_minmax(0,1fr)]";
  return (
    <SettingsCard titleId={`${id}-title`} title={t.detailsTitle} description={t.detailsBody} data-skill-details="">
      <dl className="flex flex-col gap-2 text-sm">
        <div className={row}>
          <dt className="font-medium text-ink">{t.name}</dt>
          <dd className="font-mono text-ink">{skill.name}</dd>
        </div>
        <div className={row}>
          <dt className="font-medium text-ink">{t.description}</dt>
          <dd className="text-ink">{skill.description}</dd>
        </div>
        <div className={row}>
          <dt className="font-medium text-ink">{t.owner}</dt>
          <dd className="text-ink">{skill.owner?.name ?? s.skills.list.rotor}</dd>
        </div>
        <div className={row}>
          <dt className="font-medium text-ink">{t.current}</dt>
          <dd className="text-ink">{skill.currentVersion}</dd>
        </div>
        {rest.length > 0 && (
          <div className={row}>
            <dt className="font-medium text-ink">{t.frontMatter}</dt>
            <dd>
              <pre className="overflow-x-auto font-mono text-xs whitespace-pre-wrap text-ink">
                {rest.map(([key, value]) => `${key}: ${typeof value === "string" ? value : JSON.stringify(value)}`).join("\n")}
              </pre>
            </dd>
          </div>
        )}
      </dl>
      {skill.builtin && <p className="mt-3 text-xs text-ink-muted">{t.builtin}</p>}
    </SettingsCard>
  );
}

/** Archiving at once with an Undo beside it rather than asking first; restoring is the undo, and stays offered. */
function ArchiveCard({ skill }: { skill: SkillSummary }) {
  const id = useId();
  const client = useQueryClient();
  const t = s.skills.general;
  // What this visit did, so the line under the button says it and offers to take it back.
  const [done, setDone] = useState<"archived" | "restored" | null>(null);
  const refresh = () => client.invalidateQueries({ queryKey: skillsQueryKey });
  const archive = useMutation({
    mutationFn: () => archiveSkill(skill.id),
    onSuccess: async () => {
      await refresh();
      setDone("archived");
    },
  });
  const restore = useMutation({
    mutationFn: () => restoreSkill(skill.id),
    onSuccess: async () => {
      await refresh();
      setDone("restored");
    },
  });
  const pending = archive.isPending || restore.isPending;
  const administers = skill.canAdminister && !skill.builtin;

  return (
    <SettingsCard titleId={`${id}-title`} title={t.archiveTitle} description={t.archiveBody} data-skill-archive="">
      <div className="flex flex-col gap-3">
        {skill.archived && done !== "archived" && <p className="text-sm text-ink">{t.isArchived}</p>}
        {!administers && (
          <p className="text-xs text-ink-muted" data-read-only="archive">
            {skill.builtin ? t.builtinArchive : t.archiveReadOnly}
          </p>
        )}
        {administers && !skill.archived && (
          <div>
            <button type="button" className={secondary} disabled={pending} onClick={() => archive.mutate()} data-action="archive-skill">
              {archive.isPending ? t.archiving : t.archive}
            </button>
          </div>
        )}
        {administers && skill.archived && done !== "archived" && (
          <div>
            <button type="button" className={secondary} disabled={pending} onClick={() => restore.mutate()} data-action="restore-skill">
              {restore.isPending ? t.restoring : t.restore}
            </button>
          </div>
        )}
        {/* Present from the start, so a screen reader hears what changes in it. */}
        <div role="status" className="flex flex-wrap items-center gap-2 text-sm text-ink" data-archive-status={done ?? ""}>
          {done === "archived" && skill.archived && (
            <>
              <span>{t.archived(skill.name)}</span>
              <button type="button" className={secondary} disabled={pending} onClick={() => restore.mutate()} data-action="undo-archive">
                {t.undo}
              </button>
            </>
          )}
          {done === "restored" && !skill.archived && <span>{t.restored(skill.name)}</span>}
        </div>
        <Refusal error={archive.error ?? restore.error} />
      </div>
    </SettingsCard>
  );
}
