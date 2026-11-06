import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { type FormEvent, useId } from "react";
import { ApiError } from "@/api/client";
import { type SpanHit, type SpanSearch, spanSearchQuery } from "@/api/spans";
import { workflowsQuery } from "@/api/workflows";
import { SPAN_SEARCH_DEFAULT_DAYS, SPAN_SEARCH_WINDOWS } from "@/config";
import { select } from "@/features/access/ui";
import { PageIntro } from "@/features/settings/ui";
import { input, link, primary, secondary } from "@/features/settings/styles";
import { PageHeader } from "@/features/shell/EmptyState";
import { s } from "@/strings";
import { SpanHits } from "./SpanHits";

const KINDS = Object.keys(s.trace.kinds) as SpanHit["kind"][];
const TEXT_FIELDS = ["name", "model", "service", "workflow", "attrKey", "attrValue"] as const;
const field = "flex min-w-0 flex-col gap-1 text-sm font-medium text-ink";

/** Reads a search from the address, dropping anything that is not part of one. */
export function spanSearch(raw: Record<string, unknown>): SpanSearch {
  const out: SpanSearch = {};
  for (const key of TEXT_FIELDS) {
    const value = raw[key];
    if (typeof value === "string" && value.trim() !== "") out[key] = value.trim();
  }
  if (typeof raw.kind === "string" && (KINDS as string[]).includes(raw.kind)) out.kind = raw.kind as SpanHit["kind"];
  if (raw.failed === true || raw.failed === "true") out.failed = true;
  const days = Number(raw.days);
  if ((SPAN_SEARCH_WINDOWS as readonly number[]).includes(days) && days !== SPAN_SEARCH_DEFAULT_DAYS) out.days = days;
  return out;
}

/** The search the form holds, read from its fields as the address would carry it. */
function fromForm(form: HTMLFormElement): SpanSearch {
  const data = new FormData(form);
  return spanSearch(Object.fromEntries([...data.entries()].map(([k, v]) => [k, typeof v === "string" ? v : ""])));
}

/** Why the search did not run: each field the API named, or its sentence. */
function Refusal({ error }: { error: Error }) {
  const fields = error instanceof ApiError ? Object.values(error.fields) : [];
  return (
    <div role="alert" className="text-sm text-danger" data-span-search-error>
      {fields.length > 0 ? fields.map((f) => <p key={f}>{s.traces.search.failedSearch(f)}</p>) : <p>{s.traces.search.failedSearch(error.message)}</p>}
    </div>
  );
}

function SearchForm({ search, onSearch }: { search: SpanSearch; onSearch: (next: SpanSearch) => void }) {
  const t = s.traces.search;
  const id = useId();
  const workflows = useQuery(workflowsQuery(false));
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    onSearch(fromForm(e.currentTarget));
  };
  const text = (name: (typeof TEXT_FIELDS)[number], label: string, placeholder?: string, hint?: string) => (
    <div className={field}>
      <label htmlFor={`${id}-${name}`}>{label}</label>
      <input
        id={`${id}-${name}`}
        name={name}
        className={input}
        defaultValue={search[name] ?? ""}
        placeholder={placeholder}
        aria-describedby={hint ? `${id}-${name}-hint` : undefined}
      />
      {hint && (
        <span id={`${id}-${name}-hint`} className="text-xs font-normal text-ink-muted">
          {hint}
        </span>
      )}
    </div>
  );
  return (
    <form key={JSON.stringify(search)} aria-label={t.form} onSubmit={submit} className="grid items-end gap-4 sm:grid-cols-2 lg:grid-cols-4" data-span-search>
      {text("name", t.name, t.namePlaceholder)}
      <label className={field} htmlFor={`${id}-kind`}>
        {t.kind}
        <select id={`${id}-kind`} name="kind" className={select} defaultValue={search.kind ?? ""}>
          <option value="">{t.anyKind}</option>
          {KINDS.map((k) => (
            <option key={k} value={k}>
              {s.trace.kinds[k]}
            </option>
          ))}
        </select>
      </label>
      <label className={field} htmlFor={`${id}-workflow`}>
        {t.workflow}
        <select id={`${id}-workflow`} name="workflow" className={select} defaultValue={search.workflow ?? ""}>
          <option value="">{t.anyWorkflow}</option>
          {(workflows.data ?? []).map((w) => (
            <option key={w.id} value={w.id}>
              {w.name}
            </option>
          ))}
        </select>
      </label>
      <label className={field} htmlFor={`${id}-days`}>
        {t.window}
        <select id={`${id}-days`} name="days" className={select} defaultValue={String(search.days ?? SPAN_SEARCH_DEFAULT_DAYS)}>
          {SPAN_SEARCH_WINDOWS.map((days) => (
            <option key={days} value={days}>
              {t.windowOf(days)}
            </option>
          ))}
        </select>
      </label>
      {text("model", t.model)}
      {text("service", t.service)}
      {text("attrKey", t.attrKey, undefined, t.attrKeyHint)}
      {text("attrValue", t.attrValue)}
      <label className="flex h-9 items-center gap-2 text-sm text-ink">
        <input type="checkbox" name="failed" value="true" defaultChecked={search.failed === true} data-span-search-failed />
        {t.failed}
      </label>
      <div className="flex gap-2 lg:col-span-3">
        <button type="submit" className={primary}>
          {t.submit}
        </button>
        <button type="button" className={`${secondary} h-9`} onClick={() => onSearch({})}>
          {t.clear}
        </button>
      </div>
    </form>
  );
}

/** Spans across the traces a person reads (#261), found by name, kind, status, model, service, workflow or attribute. */
export function SpanSearchPage({ search }: { search: SpanSearch }) {
  const t = s.traces.search;
  const navigate = useNavigate();
  const found = useInfiniteQuery(spanSearchQuery(search, search.days ?? SPAN_SEARCH_DEFAULT_DAYS));
  const hits = found.data?.pages.flatMap((page) => page.spans) ?? [];
  return (
    <div className="flex flex-col gap-6">
      <Link to="/traces" className={link}>
        {s.traces.page.back}
      </Link>
      <PageHeader title={t.title} />
      <PageIntro>{t.intro}</PageIntro>
      <SearchForm search={search} onSearch={(next) => void navigate({ to: "/traces/search", search: next })} />
      {found.isPending && <p className="text-sm text-ink-muted">{t.searching}</p>}
      {found.isError && <Refusal error={found.error} />}
      {found.isSuccess && hits.length === 0 && (
        <p className="text-sm text-ink-muted" data-span-search-none>
          {t.none}
        </p>
      )}
      {hits.length > 0 && (
        <div className="flex flex-col gap-3">
          <p className="text-sm text-ink-muted" aria-live="polite" data-span-search-count>
            {t.shown(hits.length)}
          </p>
          <SpanHits hits={hits} />
          {found.hasNextPage && (
            <button type="button" className={`${secondary} h-9 self-start`} disabled={found.isFetchingNextPage} onClick={() => void found.fetchNextPage()}>
              {found.isFetchingNextPage ? t.searching : t.more}
            </button>
          )}
        </div>
      )}
    </div>
  );
}
