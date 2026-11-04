import { s } from "@/strings";

/** A page of Settings or Admin, as its side nav lists it; slug names it for tests. */
export interface SettingsNavPage {
  slug: string;
  path:
    | "/settings/account"
    | "/settings/assistants"
    | "/settings/usage"
    | "/admin/access"
    | "/admin/users"
    | "/admin/workspaces"
    | "/admin/unattended-writes"
    | "/admin/providers"
    | "/admin/langsmith"
    | "/admin/tools"
    | "/admin/assistants"
    | "/admin/audit"
    | "/admin/usage"
    | "/admin/retention";
  label: string;
}

/** Pages under one heading of the side nav; a group without a title stands alone. */
export interface SettingsNavGroup {
  id: string;
  title?: string;
  pages: readonly SettingsNavPage[];
}

export const SETTINGS_GROUPS: readonly SettingsNavGroup[] = [
  {
    id: "yours",
    pages: [
      { slug: "account", path: "/settings/account", label: s.settings.pages.account },
      { slug: "assistants", path: "/settings/assistants", label: s.settings.pages.assistants },
      { slug: "usage", path: "/settings/usage", label: s.settings.pages.usage },
    ],
  },
];

// The sections of the suite's administration standard (#161), People and access first as in Armature.
export const ADMIN_GROUPS: readonly SettingsNavGroup[] = [
  {
    id: "people",
    title: s.admin.groups.people,
    pages: [
      { slug: "access", path: "/admin/access", label: s.admin.pages.access },
      { slug: "users", path: "/admin/users", label: s.admin.pages.users },
    ],
  },
  {
    id: "work",
    title: s.admin.groups.work,
    pages: [
      { slug: "workspaces", path: "/admin/workspaces", label: s.admin.pages.workspaces },
      { slug: "unattended-writes", path: "/admin/unattended-writes", label: s.admin.pages.unattendedWrites },
    ],
  },
  {
    id: "integrations",
    title: s.admin.groups.integrations,
    pages: [
      { slug: "providers", path: "/admin/providers", label: s.admin.pages.providers },
      { slug: "langsmith", path: "/admin/langsmith", label: s.admin.pages.langsmith },
      { slug: "tools", path: "/admin/tools", label: s.admin.pages.tools },
      { slug: "assistants", path: "/admin/assistants", label: s.admin.pages.assistants },
    ],
  },
  {
    id: "records",
    title: s.admin.groups.records,
    pages: [
      { slug: "audit", path: "/admin/audit", label: s.admin.pages.audit },
      { slug: "usage", path: "/admin/usage", label: s.admin.pages.usage },
      { slug: "retention", path: "/admin/retention", label: s.admin.pages.retention },
    ],
  },
];

/** Where /admin opens: the first page of its first group. */
export const FIRST_ADMIN_PATH = ADMIN_GROUPS[0]!.pages[0]!.path;
