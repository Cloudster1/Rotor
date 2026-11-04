/** The words of Settings and Admin (#199), spread into s as s.settings, s.account and s.admin; strings.ts holds the rest. */
export const settingsStrings = {
  nav: "Settings pages",
  pages: { account: "Account", assistants: "Assistants", usage: "Usage" },
  buildTitle: "This installation",
  version: "Version",
  commit: "Commit",
  apiVersion: "API version",
  client: "Web client",
  loading: "Asking the server which build it runs.",
  unreachable: "The server did not say which build it runs. Reload the page to ask again.",
} as const;

export const accountStrings = {
  title: "You",
  name: "Name",
  email: "Email",
  role: "Role in Rotor",
  groups: "Groups",
  noGroups: "None",
  roles: { admin: "Administrator", member: "Member" },
} as const;

export const adminStrings = {
  title: "Admin",
  nav: "Admin pages",
  notAllowed: "Only administrators open Admin. Ask one of them if a setting here needs changing.",
  groups: { work: "Work", integrations: "Integrations", records: "Records" },
  pages: {
    workspaces: "Workspaces",
    unattendedWrites: "Unattended writes",
    providers: "Model providers",
    langsmith: "LangSmith",
    tools: "Tools",
    assistants: "Connected apps",
    audit: "Audit log",
    usage: "Usage",
    retention: "Retention",
  },
} as const;
