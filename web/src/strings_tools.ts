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
} as const;
