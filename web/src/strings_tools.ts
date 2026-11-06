/** The words for what a tool offers Rotor (#43), spread into s as s.capabilities; strings.ts holds the rest. */
const needs: Record<string, string> = {
  "mcp@1": "act in it and connect people to it",
  "webhooks@1": "start workflows from its events",
};

export const capabilityStrings = {
  missing: "Missing",
  lacks: (tool: string, capability: string) =>
    needs[capability]
      ? `${tool} does not offer ${capability}, which Rotor needs to ${needs[capability]}. Ask its administrator to update ${tool}.`
      : `${tool} does not offer ${capability}. Ask its administrator to update ${tool}.`,
  checkAgain: "Check again",
  checking: "Checking...",
  checked: (tool: string) => `${tool} is read again.`,
  outdated: "Needs updating",
  awaitingStatus: "Waiting to be allowed",
  awaiting: (tool: string) => `${tool} holds Rotor until one of its administrators allows it, under Settings, Connected apps there, or by connecting first.`,
} as const;

/** The words for a tool's name, which workflows bind (#267), spread into s as s.toolNames. */
export const toolNameStrings = {
  name: "Name",
  nameHint: (example: string) =>
    `What workflows bind the tool by, under tools:. Leave it empty to use the product's own name, such as ${example}. No two tools may share one.`,
  product: "Product",
  rename: "Rename",
  renameLabel: (name: string) => `New name for ${name}`,
  renameHint: "Lower case letters, digits, dots, dashes or underscores. The product, client and every connection stay.",
  save: "Rename it",
  saving: "Renaming...",
  keep: "Keep the name",
  binders: (name: string) => `These workflows bind ${name}, so they no longer act in this tool once it is renamed. Their owners change tool: to the new name:`,
  noBinders: (name: string) => `No workflow binds ${name} now, so renaming breaks none.`,
  loadingBinders: "Asking which workflows bind this tool.",
  renamed: (from: string, to: string) => `${from} is now ${to}.`,
  stillBound: (from: string) => `These workflows still bind ${from}; tell their owners:`,
  owner: (name: string) => `owned by ${name}`,
  shared: "Some tools share a name from before names had to differ, so workflows binding that name find none of them. Rename all but one of each:",
  sharedBy: (name: string, count: number) => `${count} tools are called ${name}`,
  ambiguousRun:
    "More than one tool has a name this workflow binds, so it cannot tell which to act in. Ask an administrator to rename all but one under Admin, Tools:",
} as const;
