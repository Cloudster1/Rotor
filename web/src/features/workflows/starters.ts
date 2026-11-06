import { parse } from "yaml";
import { WORKFLOWS_DOC_URL } from "@/config";
import { NEW_WORKFLOW } from "./template";

/** A workflow to begin a new one from: the blank template or a file of examples/. */
export interface Starter {
  /** The `?start=` value; empty for the blank start. */
  name: string;
  description: string;
  yaml: string;
  /** Where docs/workflows.md explains it; absent for the blank start. */
  docs?: string;
}

// examples/ is the single source: whatever .yaml sits there is bundled, so adding a file adds a starter.
const files = import.meta.glob("../../../../examples/*.yaml", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

// docs/workflows.md names a shipped workflow in the heading of its section; one without a heading links to the section above them all.
const DOC_HEADINGS: Record<string, string> = {
  "approved-requirement": "Draft a requirement, let a reviewer correct it, then file it",
  "draft-requirements": "Draft a requirement and file it",
  "implement-feature": "Implement a feature through reviewed, tested code",
  "issue-triage": "Label a new issue from the labels in use",
  "requirement-to-test-spec": "Design a feature's test cases and file them for review",
  "story-refinement": "Refine a story before its requirements",
  "story-to-requirements": "Clarify a story, then write its requirements",
  "weekly-digest": "Sum up a project's week, without a model",
};
const DOC_FALLBACK_HEADING = "Examples";

/** The anchor GitHub gives a heading: lowercase, punctuation dropped, spaces as hyphens. */
export function headingAnchor(heading: string): string {
  return heading
    .toLowerCase()
    .replace(/[^\p{L}\p{N} _-]/gu, "")
    .replace(/ /g, "-");
}

function describe(yaml: string): Starter | undefined {
  const metadata = (parse(yaml) as { metadata?: { name?: unknown; description?: unknown } } | null)?.metadata;
  if (typeof metadata?.name !== "string") return undefined;
  const heading = DOC_HEADINGS[metadata.name] ?? DOC_FALLBACK_HEADING;
  return {
    name: metadata.name,
    description: typeof metadata.description === "string" ? metadata.description : "",
    yaml,
    docs: `${WORKFLOWS_DOC_URL}#${headingAnchor(heading)}`,
  };
}

/** Every workflow Rotor ships, by name. */
export const SHIPPED_STARTERS: Starter[] = Object.values(files)
  .map(describe)
  .filter((x): x is Starter => x !== undefined)
  .sort((a, b) => a.name.localeCompare(b.name));

/** Today's one-node template, offered before the shipped workflows. */
export const BLANK_STARTER: Starter = { name: "", description: "", yaml: NEW_WORKFLOW };

/** The starter a `?start=` value names; an unknown or missing one is the blank start. */
export function starterNamed(name: string | undefined): Starter {
  return SHIPPED_STARTERS.find((x) => x.name === name) ?? BLANK_STARTER;
}
