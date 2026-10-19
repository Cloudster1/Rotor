/** The words of the retention setting (#41), spread into s as s.retention; strings.ts holds the rest. */
export const retentionStrings = {
  title: "Run data retention",
  body: "Rotor deletes a finished run's inputs, output, events and checkpoints this many days after it ended. A workflow may keep its runs for fewer days with its own retention, never for more. The audit log keeps its record of every run.",
  loading: "Reading the setting...",
  days: "Days to keep",
  daysHint: (max: number) => `A whole number from 1 to ${max}.`,
  current: (days: number) => `Finished runs are kept ${days} ${days === 1 ? "day" : "days"}.`,
  changedBy: (name: string) => `Last changed by ${name}.`,
  save: "Save",
  saving: "Saving...",
  saved: "Saved.",
} as const;
