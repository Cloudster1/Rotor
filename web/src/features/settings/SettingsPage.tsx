import { useQuery } from "@tanstack/react-query";
import { metaQuery } from "@/api/meta";
import { APP_VERSION } from "@/config";
import { PageHeader } from "@/features/shell/EmptyState";
import { s } from "@/strings";

/** What this installation runs: the server's build as /meta names it, and the web client's own. */
export function SettingsPage() {
  const meta = useQuery(metaQuery);
  return (
    <div>
      <PageHeader title={s.places.settings} />
      <section className="max-w-xl rounded-overlay border border-border bg-surface p-5 shadow-1" aria-labelledby="build-title">
        <h2 id="build-title" className="mb-3 text-lg font-semibold text-ink">
          {s.settings.buildTitle}
        </h2>
        {meta.isPending && <p className="text-sm text-ink-muted">{s.settings.loading}</p>}
        {meta.isError && (
          <p role="alert" className="text-sm text-danger">
            {s.settings.unreachable}
          </p>
        )}
        {meta.data && (
          <dl className="grid grid-cols-[max-content_1fr] gap-x-6 gap-y-2 text-sm" data-build>
            <dt className="text-ink-muted">{s.settings.version}</dt>
            <dd className="text-ink" data-build-version>
              {meta.data.build.version}
            </dd>
            <dt className="text-ink-muted">{s.settings.commit}</dt>
            <dd className="font-mono text-ink">{meta.data.build.commit ?? "-"}</dd>
            <dt className="text-ink-muted">{s.settings.apiVersion}</dt>
            <dd className="text-ink">{meta.data.apiVersion}</dd>
            <dt className="text-ink-muted">{s.settings.client}</dt>
            <dd className="text-ink">{APP_VERSION}</dd>
          </dl>
        )}
      </section>
    </div>
  );
}
