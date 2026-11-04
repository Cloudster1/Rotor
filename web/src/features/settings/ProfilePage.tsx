import { useQuery } from "@tanstack/react-query";
import { meQuery } from "@/api/auth";
import { metaQuery } from "@/api/meta";
import { APP_VERSION } from "@/config";
import { s } from "@/strings";
import { TimeZoneCard } from "./TimeZoneCard";
import { SettingsCard, SettingsPage } from "./ui";

const facts = "grid grid-cols-[max-content_1fr] gap-x-6 gap-y-2 text-sm";

/** Who is signed in, the zone their dates are written in, and the server's and the client's builds. */
export function ProfilePage() {
  const meta = useQuery(metaQuery);
  const me = useQuery(meQuery);
  return (
    <SettingsPage title={s.settings.pages.profile} intro={s.settings.intros.profile}>
      {me.data && (
        <SettingsCard titleId="account-title" title={s.account.title} description={s.account.fromRealm}>
          <dl className={facts} data-account>
            <dt className="text-ink-muted">{s.account.name}</dt>
            <dd className="text-ink">{me.data.user.name}</dd>
            <dt className="text-ink-muted">{s.account.email}</dt>
            <dd className="break-all text-ink">{me.data.user.email}</dd>
            <dt className="text-ink-muted">{s.account.role}</dt>
            <dd className="text-ink" data-role={me.data.role}>
              {(s.account.roles as Record<string, string>)[me.data.role] ?? me.data.role}
            </dd>
            <dt className="text-ink-muted">{s.account.groups}</dt>
            <dd className="text-ink" data-groups>
              {me.data.groups?.length ? me.data.groups.join(", ") : s.account.noGroups}
            </dd>
          </dl>
        </SettingsCard>
      )}
      <TimeZoneCard />
      <SettingsCard titleId="build-title" title={s.settings.buildTitle}>
        {meta.isPending && <p className="text-sm text-ink-muted">{s.settings.loading}</p>}
        {meta.isError && (
          <p role="alert" className="text-sm text-danger">
            {s.settings.unreachable}
          </p>
        )}
        {meta.data && (
          <dl className={facts} data-build>
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
      </SettingsCard>
    </SettingsPage>
  );
}
