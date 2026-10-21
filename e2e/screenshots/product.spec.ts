import { expect, type Page, request, test } from "@playwright/test";
import { createHash, randomBytes } from "node:crypto";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { RUN_TIMEOUT_MS } from "../fixtures/runs";
import { startInScheme } from "../fixtures/shell";
import { toolStubURL, WEB_URL } from "../fixtures/stack";

// The pictures of docs/screenshots.md and the README, taken from a fresh stack (make clean up screenshots).
// Each waits for what its name promises; one that cannot be taken fails the run.

const HERE = dirname(fileURLToPath(import.meta.url));
const SHOTS = resolve(HERE, "../../docs/screenshots");
const EXAMPLE = resolve(HERE, "../../examples/draft-requirements.yaml");

// The tool double's tenant named exciter answers as Exciter, and its stub model takes any key.
const TOOL = "exciter";
const PROVIDER = "stub";
const ASSISTANT = "Release assistant";
const ASSISTANT_SITE = "https://assistant.example";
const CALLBACK = "http://127.0.0.1:9/callback";

/** Reads who runs it in the tool, drafts in a sandbox, waits for a person, then files what they approved. */
const RELEASE_NOTES = `apiVersion: rotor.dev/v1
kind: Workflow
metadata:
  name: release-notes
  description: Drafts the notes of a release and files them once a person has read and approved them.
inputs:
  type: object
  required: [version]
  properties:
    version: { type: string, title: Version, description: The release the notes are for, such as 1.4.0. }
tools:
  exciter:
    tool: exciter
    scopes: [mcp]
    allowTools: [whoami]
limits: { maxSteps: 10, timeout: 5m }
entry: who
nodes:
  - { id: who, type: tool, tool: exciter, call: whoami }
  - id: draft
    type: code
    output: { schema: { type: object, required: [title, notes] } }
    source: |
      def run(state, ctx):
          version = state["inputs"]["version"]
          print("drafting the notes of", version)
          return {"title": "Release " + version, "notes": "A canvas for workflows, and assistants over MCP."}
  - id: review
    type: approval
    message: "File the notes of {{ nodes.draft.title }}?"
    form:
      schema:
        type: object
        required: [title]
        properties:
          title: { type: string, title: Title, maxLength: 200 }
      values:
        title: "{{ nodes.draft.title }}"
  - id: file
    type: code
    output: { schema: { type: object, required: [filed] } }
    source: |
      def run(state, ctx):
          return {"filed": state["nodes"]["review"]["title"]}
edges:
  - { from: who, to: draft }
  - { from: draft, to: review }
  - { from: review, to: file }
`;

/** Saves the page once its fonts are in, animations stopped, as docs/screenshots/<name>.png. */
async function shot(page: Page, name: string): Promise<void> {
  await page.evaluate(() => document.fonts.ready);
  await page.screenshot({ path: `${SHOTS}/${name}.png`, animations: "disabled" });
}

/** Adds the tool unless an earlier run of this spec did. */
async function addTool(page: Page): Promise<void> {
  const address = `${toolStubURL()}/t/${TOOL}`;
  await page.goto("/settings");
  const row = page.locator("[data-tools] li", { hasText: address });
  await expect(page.locator("[data-add-tool]")).toBeVisible();
  if ((await row.count()) === 0) {
    const form = page.locator("[data-add-tool]");
    await form.getByLabel("Address").fill(address);
    await form.locator('[data-action="add-tool"]').click();
  }
  await expect(row).toHaveAttribute("data-status", "ready");
}

/** Adds the stub model as the installation's provider unless an earlier run did. */
async function addProvider(page: Page): Promise<void> {
  const listed = (await (await page.request.get("/api/v1/providers")).json()) as { providers: { name: string }[] };
  if (listed.providers.some((p) => p.name === PROVIDER)) return;
  const made = await page.request.post("/api/v1/providers", {
    data: {
      name: PROVIDER,
      kind: "openai_compatible",
      baseURL: `${toolStubURL()}/t/${TOOL}/llm/v1`,
      models: ["stub-model"],
      defaultModel: "stub-model",
      enabled: true,
      apiKey: "screenshots",
    },
  });
  expect(made.status(), await made.text()).toBe(201);
}

/** Connects the signed-in person to the tool on its consent page unless they are connected. */
async function connect(page: Page): Promise<void> {
  await page.goto("/connections");
  const row = page.locator(`li[data-connection="${TOOL}"]`);
  const button = row.locator('[data-action="connect"]');
  await expect(row).toBeVisible();
  if ((await row.getAttribute("data-status")) !== "connected") {
    await button.click();
    await page.waitForURL(`${toolStubURL()}/**`);
    await page.locator("#username").fill("alice");
    await page.locator('button[data-action="allow"]').click();
    await expect(page).toHaveURL(/\/connections\?connected=/);
  }
  await expect(row).toHaveAttribute("data-status", "connected");
}

/** Saves a workflow unless one of its name exists, and answers its id. */
async function workflow(page: Page, source: string): Promise<string> {
  const name = /^ {2}name: (.+)$/m.exec(source)?.[1] ?? "";
  const listed = (await (await page.request.get("/api/v1/workflows")).json()) as { workflows: { id: string; name: string }[] };
  const found = listed.workflows.find((w) => w.name === name);
  if (found) return found.id;
  const made = await page.request.post("/api/v1/workflows", { data: { specYAML: source, comment: "First version." } });
  expect(made.status(), await made.text()).toBe(201);
  return ((await made.json()) as { workflow: { id: string } }).workflow.id;
}

async function startRun(page: Page, workflowID: string, version: string): Promise<string> {
  const made = await page.request.post(`/api/v1/workflows/${workflowID}/runs`, { data: { inputs: { version } } });
  expect(made.status(), await made.text()).toBe(201);
  return ((await made.json()) as { id: string }).id;
}

/** Opens a run's page and waits until it stops at its approval, answering the approval's address. */
async function waitForApproval(page: Page, runID: string): Promise<string> {
  await page.goto(`/runs/${runID}`);
  await expect(page.locator("[data-run-status]")).toHaveAttribute("data-run-status", "waiting", { timeout: RUN_TIMEOUT_MS });
  return (await page.locator("[data-run-approval] a").getAttribute("href")) ?? "";
}

/** Registers an assistant and asks to connect it, as an MCP client does, answering the consent request. */
async function assistantAsks(): Promise<string> {
  const anyone = await request.newContext({ baseURL: WEB_URL });
  try {
    const made = await anyone.post("/api/v1/oauth/register", {
      data: { redirect_uris: [CALLBACK], client_name: ASSISTANT, client_uri: ASSISTANT_SITE },
    });
    expect(made.status()).toBe(201);
    const client = ((await made.json()) as { client_id: string }).client_id;
    const resource = ((await (await anyone.get("/.well-known/oauth-protected-resource")).json()) as { resource: string }).resource;
    const query = new URLSearchParams({
      response_type: "code",
      client_id: client,
      redirect_uri: CALLBACK,
      state: "screenshots",
      code_challenge: createHash("sha256").update(randomBytes(32).toString("base64url")).digest("base64url"),
      code_challenge_method: "S256",
      resource,
      scope: "mcp",
    });
    const sent = await anyone.get(`/api/v1/oauth/authorize?${query}`, { maxRedirects: 0 });
    expect(sent.status()).toBe(302);
    return new URL(sent.headers()["location"]!).searchParams.get("request") ?? "";
  } finally {
    await anyone.dispose();
  }
}

test("the product, in pictures", async ({ page, browser }) => {
  await startInScheme(page, "light");
  await addTool(page);
  await addProvider(page);
  await connect(page);
  await shot(page, "connections");

  await page.goto("/settings");
  await expect(page.locator("[data-tools] li").first()).toBeVisible();
  await expect(page.locator(`li[data-provider="${PROVIDER}"]`)).toBeVisible();
  await shot(page, "settings");

  const draft = await workflow(page, readFileSync(EXAMPLE, "utf8"));
  const notes = await workflow(page, RELEASE_NOTES);
  await page.goto("/workflows");
  await expect(page.locator('li[data-workflow="release-notes"]')).toBeVisible();
  await shot(page, "workflows");

  await page.goto(`/workflows/${draft}`);
  await expect(page.locator("[data-yaml-editor] .cm-content")).toBeVisible();
  await expect(page.locator("[data-problems]")).toContainText("No problems found.");
  await shot(page, "workflow-editor");
  await page.goto(`/workflows/${notes}`);
  await page.getByRole("tab", { name: "Canvas" }).click();
  await expect(page.locator('[data-canvas] [data-node="review"]')).toBeVisible();
  await shot(page, "workflow-canvas");

  const approved = await startRun(page, notes, "1.4.0");
  const waiting = await startRun(page, notes, "1.5.0");
  const approval = await waitForApproval(page, approved);
  await waitForApproval(page, waiting);
  await page.goto("/approvals");
  await expect(page.locator("li[data-approval]", { hasText: "1.5.0" })).toBeVisible();
  await shot(page, "approvals");

  await page.goto(approval);
  await expect(page.locator('[data-input="title"]')).toHaveValue("Release 1.4.0");
  await page.locator('[data-input="title"]').fill("Rotor 1.4.0: canvas and assistants");
  await page.locator("[data-approval-comment]").fill("Named for what it brings.");
  await shot(page, "approval");
  await page.locator('[data-action="approve"]').click();
  await expect(page.locator("[data-approval-status]")).toHaveAttribute("data-approval-status", "approved");

  await page.goto(`/runs/${approved}`);
  await expect(page.locator("[data-run-status]")).toHaveAttribute("data-run-status", "succeeded", { timeout: RUN_TIMEOUT_MS });
  await expect(page.locator('[data-node="file"]')).toHaveAttribute("data-node-state", "done");
  await shot(page, "run");

  const dark = await browser.newPage({ storageState: await page.context().storageState(), viewport: page.viewportSize()! });
  try {
    await startInScheme(dark, "dark");
    await dark.goto(`/runs/${approved}`);
    await expect(dark.locator("[data-run-status]")).toHaveAttribute("data-run-status", "succeeded");
    await expect(dark.locator('[data-node="file"]')).toHaveAttribute("data-node-state", "done");
    await shot(dark, "run-dark");
  } finally {
    await dark.close();
  }

  await page.goto("/runs");
  await expect(page.locator(`[data-run="${approved}"] [data-run-status]`)).toHaveAttribute("data-run-status", "succeeded");
  await shot(page, "runs");

  await page.goto("/settings/audit");
  await expect(page.locator("[data-audit-row]").first()).toBeVisible();
  await shot(page, "audit");

  await page.goto(`/oauth/consent?request=${await assistantAsks()}`);
  await expect(page.locator("[data-consent]")).toContainText(ASSISTANT);
  await expect(page.locator("[data-consent-address]")).toHaveText(ASSISTANT_SITE);
  await shot(page, "assistant-consent");
});
