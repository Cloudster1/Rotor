import { useQueries, useQuery } from "@tanstack/react-query";
import { Link, useSearch } from "@tanstack/react-router";
import { useId, useState } from "react";
import { exportURL, type SkillSummary, skillFileQuery, skillVersionQuery } from "@/api/skills";
import { link } from "@/features/settings/styles";
import { SettingsPage } from "@/features/settings/ui";
import { SectionNote } from "@/features/workflows/settings/shared";
import { s } from "@/strings";
import { FileEditor } from "./FileEditor";
import { ordered, useSkill } from "./shared";
import { secondary } from "./styles";

/** The files of the current version, or of an older one the address names, read-only; an editor changes the current one. */
export function FilesPage() {
  const { existing } = useSkill();
  const { version: asked } = useSearch({ from: "/shell/skills/$skillID/files" });
  const [saved, setSaved] = useState<number>();
  if (!existing) return null;
  const current = existing.skill.currentVersion;
  const number = asked !== undefined && asked !== current ? asked : current;
  return (
    <SettingsPage title={s.skills.layout.pages.files}>
      {/* Present from the start, so a screen reader hears the save it reports. */}
      <p role="status" className="text-sm text-ink lg:col-span-full empty:hidden" data-skill-saved={saved ?? ""}>
        {saved !== undefined && saved === current ? s.skills.files.saved(saved) : ""}
      </p>
      <div className="lg:col-span-full">
        <VersionFiles key={number} skill={existing.skill} number={number} onSaved={setSaved} />
      </div>
    </SettingsPage>
  );
}

function VersionFiles({ skill, number, onSaved }: { skill: SkillSummary; number: number; onSaved: (n: number) => void }) {
  const id = useId();
  const t = s.skills.files;
  const version = useQuery(skillVersionQuery(skill.id, number));
  const files = ordered(version.data?.files ?? []);
  const contents = useQueries({ queries: files.map((f) => skillFileQuery(skill.id, number, f.path)) });
  const [selected, setSelected] = useState<string>();
  const [editing, setEditing] = useState(false);
  const isCurrent = number === skill.currentVersion;
  const editable = skill.canEdit && !skill.archived && !skill.builtin && isCurrent;
  const loaded = version.data !== undefined && contents.every((c) => c.data !== undefined);

  if (version.isError || contents.some((c) => c.isError)) {
    return (
      <p role="alert" className="text-sm text-danger">
        {t.unreadable}
      </p>
    );
  }
  if (!loaded) return <p className="text-sm text-ink-muted">{t.loading}</p>;
  const texts = files.map((f, i) => ({ path: f.path, content: contents[i]?.data ?? "" }));
  if (editing) {
    return (
      <FileEditor
        skill={skill}
        initial={texts}
        onCancel={() => setEditing(false)}
        onSaved={(n) => {
          onSaved(n);
          setEditing(false);
        }}
      />
    );
  }
  const shown = texts.find((f) => f.path === selected) ?? texts[0];
  return (
    <div className="flex flex-col gap-4" data-skill-files={number}>
      {!isCurrent && (
        <div className="flex flex-col items-start gap-2">
          <SectionNote data-opened-version={String(number)}>{t.older(number, skill.currentVersion)}</SectionNote>
          <Link to="/skills/$skillID/files" params={{ skillID: skill.id }} search={{}} className={link}>
            {t.current}
          </Link>
        </div>
      )}
      {isCurrent && !editable && (
        <SectionNote data-read-only="files">{skill.builtin ? t.builtinReadOnly : skill.archived ? t.archivedReadOnly : t.readOnly}</SectionNote>
      )}
      <div className="flex flex-wrap items-center gap-3">
        {editable && (
          <button type="button" className={secondary} onClick={() => setEditing(true)} data-action="edit-files">
            {t.edit}
          </button>
        )}
        <a href={exportURL(skill.id, number)} download className={link} data-action="export-skill">
          {t.download(number)}
        </a>
      </div>
      <div className="grid gap-4 lg:grid-cols-[16rem_minmax(0,1fr)]">
        <nav aria-label={t.list}>
          <ul className="flex flex-col gap-1">
            {texts.map((f) => (
              <li key={f.path}>
                <button
                  type="button"
                  className={`w-full rounded-control px-2 py-1 text-left font-mono text-sm break-all ${f.path === shown?.path ? "bg-accent-subtle text-accent" : "text-ink hover:bg-surface-raised"}`}
                  aria-current={f.path === shown?.path ? "true" : undefined}
                  onClick={() => setSelected(f.path)}
                  data-skill-file={f.path}
                >
                  {f.path}
                </button>
              </li>
            ))}
          </ul>
        </nav>
        {shown && (
          <section className="min-w-0" aria-labelledby={`${id}-path`}>
            <h2 id={`${id}-path`} className="mb-2 font-mono text-sm font-semibold text-ink">
              {shown.path}
            </h2>
            <pre
              className="max-h-[36rem] overflow-auto rounded-control border border-border bg-surface-sunken p-3 font-mono text-xs whitespace-pre-wrap text-ink"
              aria-label={t.content(shown.path)}
              // biome-ignore lint/a11y/noNoninteractiveTabindex: the file scrolls, and a keyboard must be able to scroll it.
              tabIndex={0}
              data-skill-content={shown.path}
            >
              {shown.content}
            </pre>
          </section>
        )}
      </div>
    </div>
  );
}
