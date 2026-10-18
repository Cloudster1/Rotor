import { useQuery } from "@tanstack/react-query";
import { meQuery } from "@/api/auth";
import { metaQuery } from "@/api/meta";
import { APP_VERSION } from "@/config";
import { PageHeader } from "@/features/shell/EmptyState";
import { ToolsSection } from "@/features/tools/ToolsSection";
import { s } from "@/strings";

/** Who is signed in and what this installation runs: the server's build as /meta names it, and the client's own. */
export function SettingsPage() {
  const meta = useQuery(metaQuery);
  const me = useQuery(meQuery);
  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={s.places.settings} />
      {me.data && (
        <section className="max-w-xl rounded-overlay border border-border bg-surface p-5 shadow-1" aria-labelledby="account-title">
          <h2 id="account-title" className="mb-3 text-lg font-semibold text-ink">
            {s.account.title}
          </h2>
          <dl className="grid grid-cols-[max-content_1fr] gap-x-6 gap-y-2 text-sm" data-account>
            <dt className="text-ink-muted">{s.account.name}</dt>
            <dd className="text-ink">{me.data.user.name}</dd>
            <dt className="text-ink-muted">{s.account.email}</dt>
            <dd className="text-ink">{me.data.user.email}</dd>
            <dt className="text-ink-muted">{s.account.role}</dt>
            <dd className="text-ink" data-role={me.data.role}>
              {(s.account.roles as Record<string, string>)[me.data.role] ?? me.data.role}
            </dd>
            <dt className="text-ink-muted">{s.account.groups}</dt>
            <dd className="text-ink" data-groups>
              {me.data.groups?.length ? me.data.groups.join(", ") : s.account.noGroups}
            </dd>
          </dl>
        </section>
      )}
      {me.data && <ToolsSection />}
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
