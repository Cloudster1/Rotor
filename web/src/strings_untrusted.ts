/** The words about untrusted tool content (#40), spread into s as s.untrusted; strings.ts holds the rest. */
export const untrustedStrings = {
  warnings: (n: number) => (n === 1 ? "1 warning" : `${n} warnings`),
  warningsHint: "Saving still works. Text in what this workflow reads could steer these writes; an approval node before them lets a person check each one.",
  linksRemoved: (count: number, hosts: string[]) =>
    `${count === 1 ? "A link" : `${count} links`} to ${hosts.join(", ") || "another host"} removed. Links may only point at the run's tools.`,
  linksRemovedWhere: {
    output: "In the answer",
    arguments: "In what was sent to a tool",
  },
} as const;
