/** The words of Settings and Admin (#199), spread into s as s.settings, s.account and s.admin; strings.ts holds the rest. */
export const settingsStrings = {
  nav: "Settings pages",
  pages: { profile: "Profile", theme: "Theme", assistants: "Assistants", usage: "Usage", accounts: "Tool accounts" },
  // The sentence each page of a person's own opens with, saying what it is for (#206).
  intros: {
    profile: "Who you are in Rotor, as the suite's sign-in gives it, and how dates are written for you.",
    theme: "How Rotor looks in this browser: as your system does, or light or dark whatever it says.",
    assistants:
      "The assistants you connected to Rotor, which act as you with the access you allowed. Rotor makes no access tokens of its own; an assistant is given one when you connect it.",
    accounts: "Your accounts in Exciter, Armature and Stator, which workflows act as when they read and write there for you.",
  },
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
  fromRealm: "Your name, email and groups come from the suite's sign-in; they change there, not here.",
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
  groups: { people: "People and access", work: "Work", integrations: "Integrations", records: "Records" },
  pages: {
    access: "Access",
    users: "Users",
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

/** The words of a person's time zone (#206), spread into s as s.timeZone. */
export const timeZoneStrings = {
  title: "Time zone",
  body: "The zone every date and time in Rotor is written in for you.",
  label: "Time zone",
  browser: (zone: string) => `Follow this browser (${zone})`,
  preview: (now: string) => `Dates are shown as ${now}.`,
  save: "Save time zone",
  saving: "Saving...",
  saved: "Time zone saved.",
  loading: "Asking the server which time zone you chose.",
} as const;

/** The words of the theme page (#206), spread into s as s.themePage; s.theme names the top bar's button. */
export const themePageStrings = {
  title: "Colour scheme",
  body: "Kept in this browser, so each of your devices may look its own way. The button in the top bar steps through the same choices.",
  choices: { system: "As the system", light: "Light", dark: "Dark" },
  hints: {
    system: "Light or dark as your operating system or browser is set.",
    light: "Dark text on a light page, always.",
    dark: "Light text on a dark page, always.",
  },
} as const;
