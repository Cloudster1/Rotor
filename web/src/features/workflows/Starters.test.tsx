import { readdirSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { renderAt } from "@/test/app";
import { SHIPPED_STARTERS, headingAnchor } from "./starters";
import { NEW_WORKFLOW } from "./template";

// The tests run in web/ with the whole checkout mounted, as canvas/fixtures.ts relies on.
const examples = resolve(process.cwd(), "..", "examples");
const onDisk = readdirSync(examples)
  .filter((f) => f.endsWith(".yaml"))
  .sort()
  .map((f) => ({ file: f, yaml: readFileSync(resolve(examples, f), "utf8") }));

beforeAll(async () => {
  await import("./WorkflowRoutes");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  localStorage.clear();
});

/** The new workflow at an address, keeping what a create sends. */
async function openNew(address: string) {
  const sent: { specYAML?: string } = {};
  await renderAt(address, {
    answers: {
      "POST /api/v1/workflows/validate": { valid: true, problems: [], warnings: [] },
      "POST /api/v1/workflows": async (request: Request) => {
        Object.assign(sent, await request.json());
        return Response.json({ error: { code: "workflow_invalid", message: "Stopped by the test." } }, { status: 422 });
      },
    },
  });
  await screen.findByRole("group", { name: s.workflows.starters.title });
  return sent;
}

async function created(sent: { specYAML?: string }) {
  await userEvent.click(screen.getByRole("button", { name: s.workflows.editor.create }));
  await waitFor(() => expect(sent.specYAML).toBeDefined());
  return sent.specYAML as string;
}

const picked = () => screen.getAllByRole("radio").filter((r) => (r as HTMLInputElement).checked);

describe("the starters", () => {
  it("are the files of examples/, by name", () => {
    expect(SHIPPED_STARTERS).toHaveLength(onDisk.length);
    expect(SHIPPED_STARTERS.map((x) => x.name)).toEqual(onDisk.map((x) => x.file.replace(/\.yaml$/, "")));
    for (const starter of SHIPPED_STARTERS) expect(starter.description).not.toBe("");
  });

  it("anchor a heading the way GitHub does", () => {
    expect(headingAnchor("Clarify a story, then write its requirements")).toBe("clarify-a-story-then-write-its-requirements");
    expect(headingAnchor("Draft requirements, file them after approval")).toBe("draft-requirements-file-them-after-approval");
  });

  it("are offered after a blank start, each with its name, description and guide link", async () => {
    await openNew("/workflows/new");
    const group = screen.getByRole("group", { name: s.workflows.starters.title });
    const radios = within(group).getAllByRole("radio");
    expect(radios).toHaveLength(SHIPPED_STARTERS.length + 1);
    expect(radios[0]).toHaveAttribute("data-starter", "blank");
    expect(picked()).toEqual([radios[0]]);
    for (const starter of SHIPPED_STARTERS) {
      expect(within(group).getByRole("radio", { name: new RegExp(`^${starter.name}`) })).toBeInTheDocument();
      expect(within(group).getByRole("link", { name: s.workflows.starters.docsNamed(starter.name) })).toHaveAttribute("href", starter.docs);
    }
    expect(await axeViolations()).toEqual([]);
  });

  it("fill the editor when chosen, and the blank start brings back the template", async () => {
    const sent = await openNew("/workflows/new");
    const refinement = onDisk.find((x) => x.file === "story-refinement.yaml");
    await userEvent.click(screen.getByRole("radio", { name: /^story-refinement/ }));
    expect(await created(sent)).toBe(refinement?.yaml);
    delete sent.specYAML;
    await userEvent.click(screen.getByRole("radio", { name: new RegExp(`^${s.workflows.starters.blank}`) }));
    expect(await created(sent)).toBe(NEW_WORKFLOW);
  });

  it("open from ?start=, and an unknown name opens blank", async () => {
    const sent = await openNew("/workflows/new?start=story-to-requirements");
    expect(picked()).toHaveLength(1);
    expect(picked()[0]).toHaveAttribute("data-starter", "story-to-requirements");
    expect(await created(sent)).toBe(onDisk.find((x) => x.file === "story-to-requirements.yaml")?.yaml);
    cleanup();
    const blank = await openNew("/workflows/new?start=no-such-workflow");
    expect(picked()[0]).toHaveAttribute("data-starter", "blank");
    expect(await created(blank)).toBe(NEW_WORKFLOW);
  });
});
