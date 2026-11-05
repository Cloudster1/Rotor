import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { EditorView } from "@codemirror/view";
import type { WorkflowResponse } from "@/api/workflows";
import { s } from "@/strings";
import { ME, renderAt } from "@/test/app";
import { AXE_TIMEOUT_MS, axeViolations } from "@/test/axe";
import { BUILTIN, SKILL, V2, version } from "@/test/skills";

beforeAll(async () => {
  await import("./WorkflowRoutes");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  localStorage.clear();
});

const ID = "0190a5c4-0000-7000-8000-00000000ab01";
const YAML = `apiVersion: rotor.dev/v1
kind: Workflow
metadata:
  name: refine
entry: analyse
nodes:
  - id: analyse
    type: agent
    prompt: Turn the story into requirements.
  - id: review
    type: approval
    message: Good?
`;
const PINNED = YAML.replace(
  "    prompt: Turn the story into requirements.\n",
  "    prompt: Turn the story into requirements.\n    skills:\n      - { name: requirements-engineering, version: 3 }\n",
);

const workflow = (specYAML: string): WorkflowResponse => ({
  workflow: {
    id: ID,
    name: "refine",
    description: "",
    owner: { id: ME.user.id, name: ME.user.name },
    sharedWith: 0,
    currentVersion: 1,
    updatedAt: "2026-11-04T10:00:00Z",
    archived: false,
    canRun: true,
    canEdit: true,
    canAdminister: true,
  },
  version: {
    number: 1,
    comment: "",
    hash: "h",
    createdAt: "2026-11-04T10:00:00Z",
    createdBy: { id: ME.user.id, name: ME.user.name },
    specYAML,
    inputs: null,
    bindings: [],
    unattendedCalls: [],
  },
});

async function openEditor(specYAML: string) {
  const sent: { specYAML?: string } = {};
  await renderAt(`/workflows/${ID}`, {
    answers: {
      [`/api/v1/workflows/${ID}`]: workflow(specYAML),
      "/api/v1/skills": { skills: [BUILTIN, SKILL], total: 2, limit: 200, offset: 0 },
      [`/api/v1/skills/${BUILTIN.id}/versions`]: { versions: [version(3, V2.files), version(1, V2.files)] },
      [`/api/v1/skills/${SKILL.id}/versions`]: { versions: [V2, version(1, V2.files)] },
      [`PUT /api/v1/workflows/${ID}`]: async (request: Request) => {
        Object.assign(sent, await request.json());
        return Response.json(workflow(sent.specYAML ?? ""));
      },
    },
  });
  await screen.findByRole("heading", { level: 1, name: "refine" });
  return sent;
}

function view(): EditorView {
  const found = EditorView.findFromDOM(document.querySelector(".cm-editor") as HTMLElement);
  if (!found) throw new Error("There is no YAML editor on the page.");
  return found;
}

function cursorAt(text: string) {
  const v = view();
  v.dispatch({ selection: { anchor: v.state.doc.toString().indexOf(text) } });
}

async function pick(name: string) {
  const picker = within(await screen.findByRole("region", { name: s.skills.picker.title }));
  await userEvent.selectOptions(await picker.findByLabelText(s.skills.picker.skill), name);
  // Newest first, so the newest is picked unless the person picks another.
  await waitFor(() => expect(picker.getByLabelText(s.skills.picker.version)).toHaveValue("3"));
  return picker;
}

describe("the skill picker", () => {
  it(
    "pins the newest version under skills: of the agent at the cursor, adding skills: when it is missing",
    async () => {
      const sent = await openEditor(YAML);
      const picker = await pick(s.skills.picker.builtin(BUILTIN.name));
      expect(await axeViolations()).toEqual([]);
      cursorAt("Turn the story");
      await userEvent.click(picker.getByRole("button", { name: s.skills.picker.insert }));
      expect(picker.getByRole("status")).toHaveTextContent(s.skills.picker.added(BUILTIN.name, 3, "analyse"));
      expect(view().state.doc.toString()).toBe(PINNED);

      await userEvent.selectOptions(picker.getByLabelText(s.skills.picker.skill), SKILL.name);
      await waitFor(() => expect(picker.getByLabelText(s.skills.picker.version)).toHaveValue("2"));
      await userEvent.click(picker.getByRole("button", { name: s.skills.picker.insert }));
      expect(view().state.doc.toString()).toContain("      - { name: requirements-engineering, version: 3 }\n      - { name: story-style, version: 2 }\n");

      await userEvent.click(screen.getByRole("button", { name: s.workflows.editor.save }));
      await waitFor(() => expect(sent.specYAML).toContain("- { name: story-style, version: 2 }"));
    },
    AXE_TIMEOUT_MS,
  );

  it("refuses outside an agent node and says why", async () => {
    await openEditor(YAML);
    const picker = await pick(s.skills.picker.builtin(BUILTIN.name));
    cursorAt("Good?");
    await userEvent.click(picker.getByRole("button", { name: s.skills.picker.insert }));
    expect(picker.getByRole("status")).toHaveTextContent(s.skills.picker.notAgent("review", "approval"));
    cursorAt("kind: Workflow");
    await userEvent.click(picker.getByRole("button", { name: s.skills.picker.insert }));
    expect(picker.getByRole("status")).toHaveTextContent(s.skills.picker.outside);
    expect(view().state.doc.toString()).toBe(YAML);
  });
});

describe("an agent's skills on the canvas", () => {
  it(
    "are listed in the inspector with their versions, and one comes off",
    async () => {
      await openEditor(PINNED);
      await userEvent.click(screen.getByRole("tab", { name: s.canvas.canvas }));
      const inspector = within(await screen.findByRole("region", { name: s.canvas.node.title }));
      await userEvent.selectOptions(inspector.getByLabelText(s.canvas.node.choose), "analyse");
      expect(inspector.getByText(s.skills.inspector.pin(BUILTIN.name, 3))).toBeInTheDocument();
      expect(await axeViolations()).toEqual([]);
      await userEvent.click(inspector.getByRole("button", { name: s.skills.inspector.removeNamed(BUILTIN.name) }));
      expect(await inspector.findByText(s.skills.inspector.none)).toBeInTheDocument();
      await userEvent.click(screen.getByRole("tab", { name: s.canvas.yaml }));
      await waitFor(() => expect(view().state.doc.toString()).toBe(YAML));
    },
    AXE_TIMEOUT_MS,
  );
});
