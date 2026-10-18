/** Every word the interface shows, in one place, so a second language is a second copy of this object. */
export const s = {
  shell: {
    skipToContent: "Skip to content",
    navigation: "Places",
    home: "Rotor home",
  },
  theme: {
    label: "Colour scheme",
    system: "Colour scheme: as the system",
    light: "Colour scheme: light",
    dark: "Colour scheme: dark",
  },
  places: {
    workflows: "Workflows",
    runs: "Runs",
    approvals: "Approvals",
    connections: "Connections",
    settings: "Settings",
  },
  workflows: {
    emptyTitle: "No workflows yet",
    emptyBody: "Workflows read and write Exciter, Armature and Stator for you. Writing them arrives with the workflow engine.",
  },
  runs: {
    emptyTitle: "Nothing has run yet",
    emptyBody: "Every run of a workflow shows here, step by step, with what it read and wrote.",
  },
  approvals: {
    emptyTitle: "Nothing waits for you",
    emptyBody: "When a workflow asks before it writes, the question lands here.",
  },
  connections: {
    emptyTitle: "No tools connected",
    emptyBody: "Connect your accounts in Exciter, Armature and Stator once, so workflows can act as you.",
  },
  settings: {
    buildTitle: "This installation",
    version: "Version",
    commit: "Commit",
    apiVersion: "API version",
    client: "Web client",
    loading: "Asking the server which build it runs.",
    unreachable: "The server did not say which build it runs. Reload the page to ask again.",
  },
  notFound: {
    title: "Not found",
    body: "Nothing lives at this address. Pick a place from the sidebar.",
  },
  routeError: {
    title: "Something went wrong",
    fallback: "This page could not be shown.",
    retry: "Try again",
  },
  api: {
    unexpected: (status: number) => `The server answered ${status} without saying why. Try again in a moment.`,
  },
} as const;
