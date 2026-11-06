import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { type SkillSummary, skillsQuery } from "@/api/skills";
import { SEARCH_DELAY_MS, SKILL_PAGE_SIZE } from "@/config";
import { Pager, shownOf, Tag } from "@/features/access/ui";
import { input } from "@/features/settings/styles";
import { PageIntro } from "@/features/settings/ui";
import { EmptyState, PageHeader } from "@/features/shell/EmptyState";
import { Icon } from "@/features/shell/icons";
import { useDebounced } from "@/lib/debounce";
import { s } from "@/strings";
import { primaryLink, secondaryLink, secondary } from "./styles";

/** The skills the person reads (#223): theirs, those shared with them and the built-in ones; archived ones on request. */
export function SkillsPage() {
  const [archived, setArchived] = useState(false);
  const [words, setWords] = useState("");
  const q = useDebounced(words.trim(), SEARCH_DELAY_MS);
  // A page belongs to one search of one half of the list; another starts at its first page.
  const shown = `${archived}:${q}`;
  const [page, setPage] = useState({ shown, offset: 0 });
  const offset = page.shown === shown ? page.offset : 0;
  const setOffset = (next: number) => setPage({ shown, offset: next });
  const list = useQuery(skillsQuery({ archived, q, offset }));
  const Glyph = Icon.Skill;
  const data = list.data;
  const t = s.skills.list;
  const actions = (
    <div className="flex flex-wrap items-center justify-center gap-3">
      <Link to="/skills/new" className={primaryLink} data-action="new-skill">
        {t.newSkill}
      </Link>
      <Link to="/skills/import" className={secondaryLink} data-action="import-skill">
        {t.importZip}
      </Link>
    </div>
  );
  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={s.places.skills} />
      <PageIntro>{t.intro}</PageIntro>
      <div className="flex flex-wrap items-end gap-3">
        {actions}
        <button type="button" className={secondary} onClick={() => setArchived(!archived)} aria-pressed={archived} data-action="toggle-archived">
          {archived ? t.showActive : t.showArchived}
        </button>
        <label className="flex min-w-48 flex-1 flex-col gap-1 text-sm font-medium text-ink sm:max-w-xs">
          {t.search}
          <input
            className={input}
            type="search"
            value={words}
            onChange={(e) => setWords(e.target.value)}
            aria-describedby="skills-search-hint"
            data-skill-search
          />
        </label>
        <p id="skills-search-hint" className="sr-only">
          {t.searchHint}
        </p>
      </div>
      {list.isPending && <p className="text-sm text-ink-muted">{t.loading}</p>}
      {list.isError && (
        <p role="alert" className="text-sm text-danger">
          {t.unreachable}
        </p>
      )}
      {data && data.skills.length === 0 && (
        <EmptyState
          icon={<Glyph />}
          title={q ? t.noMatchTitle : archived ? t.archivedTitle : t.emptyTitle}
          description={q ? t.noMatch(q) : archived ? t.archivedBody : t.emptyBody}
          action={q || archived ? undefined : actions}
        />
      )}
      {data && data.skills.length > 0 && (
        <div className="flex max-w-3xl flex-col gap-3">
          <p className="text-sm text-ink-muted" data-skill-count>
            {shownOf(data.total, data.offset, data.skills.length, t.count)}
          </p>
          <ul className="flex flex-col gap-3">
            {data.skills.map((skill) => (
              <SkillRow key={skill.id} skill={skill} />
            ))}
          </ul>
          <Pager label={t.pages} total={data.total} offset={data.offset} pageSize={SKILL_PAGE_SIZE} onChange={setOffset} />
        </div>
      )}
    </div>
  );
}

function SkillRow({ skill }: { skill: SkillSummary }) {
  const t = s.skills.list;
  return (
    <li className="rounded-overlay border border-border bg-surface p-4 shadow-1" data-skill={skill.name} data-builtin={skill.builtin}>
      <div className="flex flex-wrap items-center gap-2">
        <Link to="/skills/$skillID/files" params={{ skillID: skill.id }} className="font-mono font-medium text-accent hover:underline">
          {skill.name}
        </Link>
        {skill.builtin && <Tag data-builtin-tag="">{t.builtin}</Tag>}
        {skill.archived && <Tag>{t.archived}</Tag>}
      </div>
      <p className="mt-1 text-sm text-ink-muted">{skill.description}</p>
      <p className="mt-1 text-xs text-ink-muted">
        {t.version(skill.currentVersion)} . {t.owner(skill.owner?.name ?? t.rotor)}
      </p>
    </li>
  );
}
