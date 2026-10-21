import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { EDITOR_VIEW_STORAGE_KEY } from "@/config";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { renderAt } from "@/test/app";

beforeAll(async () => {
  await import("./WorkflowRoutes");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  localStorage.clear();
});

const NO_PROBLEMS = { valid: true, problems: [], warnings: [] };

/** The new-workflow page, answering every check with the given findings and keeping what a create sends. */
async function openNew(found: unknown = NO_PROBLEMS) {
  const sent: { specYAML?: string } = {};
  await renderAt("/workflows/new", {
    answers: {
      "POST /api/v1/workflows/validate": found,
      "POST /api/v1/workflows": async (request: Request) => {
        Object.assign(sent, await request.json());
        return Response.json({ error: { code: "workflow_invalid", message: "Stopped by the test." } }, { status: 422 });
      },
    },
  });
  await screen.findByRole("heading", { level: 1, name: s.workflows.editor.newTitle });
  return sent;
}

async function sentYAML(sent: { specYAML?: string }) {
  await userEvent.click(screen.getByRole("button", { name: s.workflows.editor.create }));
  await waitFor(() => expect(sent.specYAML).toBeDefined());
  return sent.specYAML as string;
}

describe("the workflow canvas", () => {
  it("draws the YAML as a graph in its own tab, and the browser remembers the tab", async () => {
    await openNew();
    expect(screen.getByRole("tab", { name: s.canvas.yaml })).toHaveAttribute("aria-selected", "true");
    await userEvent.click(screen.getByRole("tab", { name: s.canvas.canvas }));
    const graph = await screen.findByRole("region", { name: s.canvas.graph });
    expect(await within(graph).findByText("ask")).toBeInTheDocument();
    expect(localStorage.getItem(EDITOR_VIEW_STORAGE_KEY)).toBe("canvas");
    expect(screen.getByRole("tab", { name: s.canvas.canvas })).toHaveAttribute("aria-selected", "true");
  });

  it("is used with the keyboard alone: add a node, lead to it, make it the start, and the YAML says so", async () => {
    const sent = await openNew();
    screen.getByRole("tab", { name: s.canvas.yaml }).focus();
    await userEvent.keyboard("{ArrowRight}");
    expect(screen.getByRole("tab", { name: s.canvas.canvas })).toHaveFocus();
    const add = await screen.findByRole("form", { name: s.canvas.add.title });
    await userEvent.selectOptions(within(add).getByLabelText(s.canvas.add.type), "agent");
    await userEvent.type(within(add).getByLabelText(s.canvas.add.id), "Bad id{Enter}");
    expect(within(add).getByRole("alert")).toHaveTextContent(s.canvas.add.badID);
    await userEvent.clear(within(add).getByLabelText(s.canvas.add.id));
    await userEvent.type(within(add).getByLabelText(s.canvas.add.id), "draft{Enter}");

    const inspector = screen.getByRole("region", { name: s.canvas.node.title });
    expect(within(inspector).getByLabelText(s.canvas.node.choose)).toHaveValue("draft");
    await userEvent.type(within(inspector).getByLabelText(s.canvas.node.fields.prompt), "Draft it.");
    await userEvent.click(within(inspector).getByRole("button", { name: s.canvas.node.startHere }));
    expect(within(inspector).getByText(s.canvas.node.isEntry)).toBeInTheDocument();
    await userEvent.selectOptions(within(inspector).getByLabelText(s.canvas.node.addNext), "ask");
    await userEvent.click(within(inspector).getByRole("button", { name: s.canvas.node.addShort }));
    expect(within(inspector).getByText(s.canvas.node.to("ask"))).toBeInTheDocument();

    const yaml = await sentYAML(sent);
    expect(yaml).toContain("entry: draft\n");
    expect(yaml).toContain("  - id: draft\n    type: agent\n    prompt: Draft it.\n");
    expect(yaml).toContain("edges:\n  - { from: draft, to: ask }\n");
    expect(yaml).toContain("    note: { type: string }\n");
  });

  it("marks a node with the problems found on it, and opens the YAML at one", async () => {
    await openNew({
      valid: false,
      problems: [{ path: "/nodes/0/message", line: 18, column: 14, code: "schema", message: "At /nodes/0/message: too short." }],
      warnings: [],
    });
    await userEvent.click(screen.getByRole("tab", { name: s.canvas.canvas }));
    const graph = await screen.findByRole("region", { name: s.canvas.graph });
    expect(await within(graph).findByText(s.canvas.node.problems(1))).toBeInTheDocument();
    const inspector = screen.getByRole("region", { name: s.canvas.node.title });
    await userEvent.click(within(inspector).getByRole("button", { name: /too short/ }));
    expect(screen.getByRole("tab", { name: s.canvas.yaml })).toHaveAttribute("aria-selected", "true");
  });

  it("renames a node everywhere and removes one after asking", async () => {
    const sent = await openNew();
    await userEvent.click(screen.getByRole("tab", { name: s.canvas.canvas }));
    const add = await screen.findByRole("form", { name: s.canvas.add.title });
    await userEvent.type(within(add).getByLabelText(s.canvas.add.id), "later{Enter}");
    const inspector = screen.getByRole("region", { name: s.canvas.node.title });
    await userEvent.selectOptions(within(inspector).getByLabelText(s.canvas.node.choose), "ask");
    const id = within(inspector).getByLabelText(s.canvas.node.id);
    await userEvent.clear(id);
    await userEvent.type(id, "check{Enter}");
    await userEvent.selectOptions(within(inspector).getByLabelText(s.canvas.node.choose), "later");
    await userEvent.click(within(inspector).getByRole("button", { name: s.canvas.node.remove }));
    await userEvent.click(within(inspector).getByRole("button", { name: s.canvas.node.confirm }));
    const yaml = await sentYAML(sent);
    expect(yaml).toContain("entry: check\n");
    expect(yaml).toContain("  - id: check\n");
    expect(yaml).not.toContain("later");
  });

  it("says why it draws nothing when the YAML does not parse, and offers the YAML", async () => {
    await openNew();
    const file = new File(["nodes: [\n"], "broken.yaml", { type: "application/yaml" });
    // jsdom's File has no text(); the browser's does.
    Object.defineProperty(file, "text", { value: async () => "nodes: [\n" });
    await userEvent.upload(document.querySelector("[data-import]") as HTMLInputElement, file);
    await userEvent.click(screen.getByRole("tab", { name: s.canvas.canvas }));
    expect(await screen.findByText(s.canvas.unparsed)).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: s.canvas.graph })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: s.canvas.openYaml }));
    expect(screen.getByRole("tab", { name: s.canvas.yaml })).toHaveAttribute("aria-selected", "true");
  });

  it("passes axe with the canvas and the inspector showing", async () => {
    await openNew();
    await userEvent.click(screen.getByRole("tab", { name: s.canvas.canvas }));
    await screen.findByRole("region", { name: s.canvas.graph });
    await screen.findByText(s.workflows.editor.valid);
    expect(await axeViolations()).toEqual([]);
  });
});
