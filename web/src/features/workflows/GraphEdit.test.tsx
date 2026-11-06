import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { renderAt } from "@/test/app";
import { NEW_WORKFLOW } from "./template";

beforeAll(async () => {
  await import("./WorkflowRoutes");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  localStorage.clear();
});

/** The new workflow on its canvas, keeping what a create sends. */
async function openCanvas() {
  const sent: { specYAML?: string } = {};
  await renderAt("/workflows/new", {
    answers: {
      "POST /api/v1/workflows/validate": { valid: true, problems: [], warnings: [] },
      "POST /api/v1/workflows": async (request: Request) => {
        Object.assign(sent, await request.json());
        return Response.json({ error: { code: "workflow_invalid", message: "Stopped by the test." } }, { status: 422 });
      },
    },
  });
  await userEvent.click(await screen.findByRole("tab", { name: s.canvas.canvas }));
  await screen.findByRole("region", { name: s.canvas.graph });
  return sent;
}

async function sentYAML(sent: { specYAML?: string }) {
  await userEvent.click(screen.getByRole("button", { name: s.workflows.editor.create }));
  await waitFor(() => expect(sent.specYAML).toBeDefined());
  return sent.specYAML as string;
}

const tools = () => screen.getByRole("toolbar", { name: s.canvas.toolbar.label });
const tool = (name: string) => within(tools()).getByRole("button", { name });
const kind = (label: string) => within(screen.getByRole("dialog")).getByRole("button", { name: new RegExp(`^${label}`) });

async function addAfterSelected(label: string) {
  await userEvent.click(tool(s.canvas.toolbar.add));
  await userEvent.click(kind(label));
}

describe("building a workflow on the graph", () => {
  it("adds a node after the selected one, connected to it, and selects it", async () => {
    const sent = await openCanvas();
    await userEvent.click(tool(s.canvas.toolbar.add));
    expect(screen.getByRole("dialog", { name: s.canvas.picker.after("ask") })).toBeInTheDocument();
    await userEvent.click(kind(s.canvas.types.agent));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: s.canvas.node.choose })).toHaveValue("agent");
    const yaml = await sentYAML(sent);
    expect(yaml).toContain("  - id: agent\n    type: agent\n");
    expect(yaml).toContain("edges:\n  - { from: ask, to: agent }\n");
  });

  it("undoes and redoes what the canvas changed, by its buttons and keys", async () => {
    const sent = await openCanvas();
    expect(tool(s.canvas.toolbar.undo)).toBeDisabled();
    await addAfterSelected(s.canvas.types.agent);
    await userEvent.click(tool(s.canvas.toolbar.undo));
    expect(tool(s.canvas.toolbar.undo)).toBeDisabled();
    tool(s.canvas.toolbar.fit).focus();
    await userEvent.keyboard("{Control>}{Shift>}z{/Shift}{/Control}");
    await userEvent.keyboard("{Control>}z{/Control}");
    expect(await screen.findByText(s.canvas.graphEdit.undone)).toBeInTheDocument();
    expect(await sentYAML(sent)).toBe(NEW_WORKFLOW);
  });

  it("opens the picker with A while the graph holds the focus, and Cancel closes it", async () => {
    await openCanvas();
    tool(s.canvas.toolbar.fit).focus();
    await userEvent.keyboard("a");
    expect(screen.getByRole("dialog", { name: s.canvas.picker.after("ask") })).toBeInTheDocument();
    await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: s.canvas.picker.cancel }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("makes a node after a router its default, and asks for a case value once the router has one", async () => {
    const sent = await openCanvas();
    await addAfterSelected(s.canvas.types.router);
    await addAfterSelected(s.canvas.types.code);
    await userEvent.selectOptions(screen.getByRole("combobox", { name: s.canvas.node.choose }), "router");
    await userEvent.click(tool(s.canvas.toolbar.add));
    await userEvent.click(kind(s.canvas.types.agent));
    expect(await screen.findByRole("alert")).toHaveTextContent(s.canvas.picker.needCase);
    await userEvent.type(screen.getByRole("textbox", { name: s.canvas.picker.caseValue("router") }), "yes");
    await userEvent.click(kind(s.canvas.types.agent));
    const yaml = await sentYAML(sent);
    expect(yaml).toMatch(/default: code/);
    expect(yaml).toMatch(/yes: agent/);
    expect(yaml).not.toMatch(/from: router/);
  });

  it("removes the selected node, says so, and undo brings it back", async () => {
    const sent = await openCanvas();
    await addAfterSelected(s.canvas.types.approval);
    await userEvent.click(tool(s.canvas.toolbar.remove));
    expect(await screen.findByText(s.canvas.graphEdit.removed)).toBeInTheDocument();
    await userEvent.click(tool(s.canvas.toolbar.undo));
    const yaml = await sentYAML(sent);
    expect(yaml).toContain("{ from: ask, to: approval }");
  });

  it("passes axe with the picker open", async () => {
    await openCanvas();
    await userEvent.click(tool(s.canvas.toolbar.add));
    expect(await axeViolations()).toEqual([]);
  });
});
