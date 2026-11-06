import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useId, useState } from "react";
import { exportURL, skillDiffQuery, skillVersionsQuery } from "@/api/skills";
import { Tag } from "@/features/access/ui";
import { card, link } from "@/features/settings/styles";
import { SettingsPage } from "@/features/settings/ui";
import { DiffHunks } from "@/features/workflows/DiffHunks";
import { formatDateTime } from "@/lib/dates";
import { s } from "@/strings";
import { useSkill } from "./shared";
import { secondary } from "./styles";

const select = "h-8 rounded-control border border-border-strong bg-surface px-2 text-sm text-ink";

/** Every version, newest first, each to open read-only or download, and what changed between any two, file by file. */
export function VersionsPage() {
  const { skillID, existing } = useSkill();
  if (!existing) return null;
  return (
    <SettingsPage title={s.skills.layout.pages.versions}>
      <div className="lg:col-span-full">
        <Versions skillID={skillID} current={existing.skill.currentVersion} />
      </div>
    </SettingsPage>
  );
}

function Versions({ skillID, current }: { skillID: string; current: number }) {
  const id = useId();
  const t = s.skills.versions;
  const versions = useQuery(skillVersionsQuery(skillID));
  const numbers = (versions.data ?? []).map((v) => v.number);
  // A built-in skill's versions may skip numbers, so the one before the current is the list's, not current - 1.
  const [from, setFrom] = useState<number>();
  const [to, setTo] = useState(current);
  const [compared, setCompared] = useState<{ from: number; to: number } | null>(null);
  const diff = useQuery({ ...skillDiffQuery(skillID, compared?.from ?? 0, compared?.to ?? 0), enabled: compared !== null });
  const fromShown = from ?? numbers[1] ?? current;
  return (
    <section className={card} aria-labelledby={`${id}-title`} data-skill-versions>
      <h2 id={`${id}-title`} className="mb-3 text-lg font-semibold text-ink">
        {t.title}
      </h2>
      <ol className="mb-4 flex flex-col gap-2">
        {(versions.data ?? []).map((v) => (
          <li key={v.number} className="flex flex-wrap items-baseline justify-between gap-2 text-sm" data-version={v.number}>
            <span className="text-ink">
              <span className="font-medium">{s.skills.list.version(v.number)}</span>
              {v.comment && <span className="text-ink-muted"> {v.comment}</span>}
            </span>
            <span className="flex flex-wrap items-center gap-2">
              <span className="text-xs text-ink-muted">{t.by(v.createdBy?.name ?? s.skills.list.rotor, formatDateTime(v.createdAt))}</span>
              <Link
                to="/skills/$skillID/files"
                params={{ skillID }}
                search={v.number === current ? {} : { version: v.number }}
                className={`${secondary} inline-flex items-center`}
                aria-label={t.openNamed(v.number)}
                data-action="open-version"
              >
                {t.open}
              </Link>
              <a href={exportURL(skillID, v.number)} download className={link} aria-label={t.downloadNamed(v.number)} data-action="export-version">
                {t.download}
              </a>
            </span>
          </li>
        ))}
      </ol>
      {numbers.length > 1 && (
        <div className="flex flex-wrap items-end gap-3">
          <label className="flex flex-col gap-1 text-sm text-ink">
            {t.from}
            <select className={select} value={fromShown} onChange={(e) => setFrom(Number(e.target.value))} data-diff-from>
              {numbers.map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </select>
          </label>
          <label className="flex flex-col gap-1 text-sm text-ink">
            {t.to}
            <select className={select} value={to} onChange={(e) => setTo(Number(e.target.value))} data-diff-to>
              {numbers.map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </select>
          </label>
          <button type="button" className={secondary} onClick={() => setCompared({ from: fromShown, to })} data-action="compare">
            {t.compare}
          </button>
        </div>
      )}
      {compared && diff.data && (
        <div className="mt-4 flex flex-col gap-4" data-diff>
          {diff.data.files.length === 0 && <p className="text-sm text-ink-muted">{t.same}</p>}
          {diff.data.files.map((file) => (
            <section key={file.path} aria-labelledby={`${id}-${file.path}`} data-diff-file={file.path} data-status={file.status}>
              <h3 id={`${id}-${file.path}`} className="mb-2 flex flex-wrap items-center gap-2 font-mono text-sm font-semibold text-ink">
                {file.path}
                <Tag className="font-sans">{t.status[file.status]}</Tag>
              </h3>
              <DiffHunks hunks={file.hunks} label={t.diffLabel(file.path, compared.from, compared.to)} />
            </section>
          ))}
        </div>
      )}
    </section>
  );
}
