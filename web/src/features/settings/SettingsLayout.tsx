import { useQuery } from "@tanstack/react-query";
import { Outlet } from "@tanstack/react-router";
import { meQuery } from "@/api/auth";
import { PageHeader } from "@/features/shell/EmptyState";
import { s } from "@/strings";
import { ADMIN_GROUPS, SETTINGS_GROUPS, type SettingsNavGroup } from "./groups";
import { SettingsNav } from "./SettingsNav";

/** A side nav of pages beside the one open; a workflow's settings use it too (#205). */
export function Layout({ label, groups }: { label: string; groups: readonly SettingsNavGroup[] }) {
  return (
    <div className="flex flex-col gap-6 lg:flex-row lg:items-start lg:gap-8">
      <SettingsNav label={label} groups={groups} />
      <div className="min-w-0 flex-1">
        <Outlet />
      </div>
    </div>
  );
}

/** The person's own pages, for everyone signed in. */
export function SettingsLayout() {
  return <Layout label={s.settings.nav} groups={SETTINGS_GROUPS} />;
}

/** The installation's pages, for administrators; anybody else is refused once, without the nav. */
export function AdminLayout() {
  const me = useQuery(meQuery);
  if (!me.data) return null;
  if (me.data.role !== "admin") {
    return (
      <div>
        <PageHeader title={s.admin.title} />
        <p role="alert" className="text-sm text-danger" data-admin-refused>
          {s.admin.notAllowed}
        </p>
      </div>
    );
  }
  return <Layout label={s.admin.nav} groups={ADMIN_GROUPS} />;
}
