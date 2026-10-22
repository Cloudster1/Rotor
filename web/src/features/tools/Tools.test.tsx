import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Tool } from "@/api/tools";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { ME, renderAt } from "@/test/app";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const EXCITER: Tool = {
  id: "0190a5c4-0000-7000-8000-0000000000e1",
  name: "exciter",
  baseURL: "https://exciter.test",
  resource: "https://exciter.test/api/v1/mcp",
  mcpURL: "https://exciter.test/api/v1/mcp",
  issuer: "https://exciter.test",
  capabilities: ["mcp@1", "webhooks@2"],
  missing: [],
  scopesSupported: ["mcp", "mcp:read"],
  registration: "dynamic",
  status: "ready",
  addedAt: "2026-10-18T10:00:00Z",
};

const MEMBER = { ...ME, role: "member", groups: [] };

describe("the tools", () => {
  it("are listed for everyone, but only an administrator may add or remove one", async () => {
    await renderAt("/settings", { me: MEMBER, answers: { "/api/v1/tools": { tools: [EXCITER] } } });
    const row = await screen.findByText("Exciter");
    expect(row).toBeInTheDocument();
    expect(screen.getByText(s.tools.status.ready)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: s.tools.add.submit })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: s.tools.remove })).not.toBeInTheDocument();
  });

  it("are added by their address, and a refusal is shown as the server words it", async () => {
    let tools: Tool[] = [];
    const refusal = "Armature names no authorization server, so people cannot connect to it with OAuth. Switch on its OAuth connections.";
    const { fetch } = await renderAt("/settings", {
      answers: {
        "/api/v1/tools": () => Response.json({ tools }),
        "POST /api/v1/tools": async (request: Request) => {
          const body = (await request.json()) as { baseURL: string };
          if (body.baseURL.includes("armature")) {
            return Response.json({ error: { code: "tool_refused", message: refusal } }, { status: 422 });
          }
          tools = [EXCITER];
          return Response.json({ tool: EXCITER }, { status: 201 });
        },
      },
    });
    const address = await screen.findByLabelText(s.tools.add.baseURL);
    await userEvent.type(address, "https://armature.test");
    await userEvent.click(screen.getByRole("button", { name: s.tools.add.submit }));
    expect(await screen.findByRole("alert")).toHaveTextContent(refusal);

    await userEvent.clear(address);
    await userEvent.type(address, "https://exciter.test");
    await userEvent.click(screen.getByRole("button", { name: s.tools.add.submit }));
    expect(await screen.findByRole("status")).toHaveTextContent(s.tools.add.added("Exciter"));
    expect(await screen.findByText(EXCITER.baseURL, { selector: "p" })).toBeInTheDocument();
    const posts = fetch.mock.calls.filter(([request]) => request.method === "POST");
    expect(posts).toHaveLength(2);
  });

  it("take a client registered by hand, with its secret", async () => {
    let sent: unknown;
    await renderAt("/settings", {
      answers: {
        "POST /api/v1/tools": async (request: Request) => {
          sent = await request.json();
          return Response.json({ tool: { ...EXCITER, registration: "manual" } }, { status: 201 });
        },
      },
    });
    await userEvent.type(await screen.findByLabelText(s.tools.add.baseURL), "https://exciter.test");
    await userEvent.click(screen.getByLabelText(s.tools.add.manual));
    expect(screen.getByText(/connections\/callback/)).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText(s.tools.add.clientID), "rotor");
    await userEvent.type(screen.getByLabelText(s.tools.add.clientSecret), "shh");
    await userEvent.click(screen.getByRole("button", { name: s.tools.add.submit }));
    await waitFor(() => expect(sent).toEqual({ baseURL: "https://exciter.test", clientID: "rotor", clientSecret: "shh" }));
  });

  it("are removed after a confirmation, and one that forgot Rotor is registered again", async () => {
    let tools: Tool[] = [{ ...EXCITER, status: "needs_registration" }];
    const { fetch } = await renderAt("/settings", {
      answers: {
        "/api/v1/tools": () => Response.json({ tools }),
        "POST /api/v1/tools": () => {
          tools = [EXCITER];
          return Response.json({ tool: EXCITER });
        },
        [`DELETE /api/v1/tools/${EXCITER.id}`]: () => {
          tools = [];
          return new Response(null, { status: 204 });
        },
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: s.tools.registerAgain }));
    expect(await screen.findByText(s.tools.status.ready)).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: s.tools.remove }));
    const confirm = screen.getByRole("group", { name: s.tools.confirmRemove("Exciter") });
    await userEvent.click(within(confirm).getByRole("button", { name: s.tools.confirm }));
    expect(await screen.findByText(s.tools.noneAdmin)).toBeInTheDocument();
    const calls = fetch.mock.calls.map(([request]) => `${request.method} ${new URL(request.url).pathname}`);
    expect(calls).toContain(`DELETE /api/v1/tools/${EXCITER.id}`);
  });

  it("pass axe, with the form open", async () => {
    await renderAt("/settings", { answers: { "/api/v1/tools": { tools: [EXCITER, { ...EXCITER, id: "x", name: "stator", status: "needs_registration" }] } } });
    await screen.findByText("Exciter");
    await userEvent.click(screen.getByLabelText(s.tools.add.manual));
    await userEvent.click(screen.getAllByRole("button", { name: s.tools.remove })[0]!);
    expect(await axeViolations()).toEqual([]);
  });
});

describe("a tool's events", () => {
  const HOOK_PATH = `/api/v1/tools/${EXCITER.id}/hook`;
  const unset = { url: `https://rotor.test/api/v1/hooks/${EXCITER.id}`, product: "Exciter", secretSet: false, secretSetAt: null, lastReceivedAt: null };

  it("show the webhook address, copy it, and keep a pasted secret without showing it back", async () => {
    let sent: unknown;
    const writeText = vi.fn(async () => {});
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    await renderAt("/settings", {
      answers: {
        "/api/v1/tools": { tools: [EXCITER] },
        [HOOK_PATH]: unset,
        [`PUT ${HOOK_PATH}`]: async (request: Request) => {
          sent = await request.json();
          return Response.json({ ...unset, secretSet: true, secretSetAt: "2026-10-19T08:00:00Z" });
        },
      },
    });
    const address = await screen.findByLabelText(s.tools.hook.address);
    expect(address).toHaveValue(unset.url);
    expect(screen.getByText(s.tools.hook.notSet, { exact: false })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: s.tools.hook.copy }));
    expect(writeText).toHaveBeenCalledWith(unset.url);
    expect(await screen.findByText(s.tools.hook.copied)).toBeInTheDocument();

    const secret = screen.getByLabelText(s.tools.hook.secret);
    expect(secret).toHaveAttribute("type", "password");
    await userEvent.type(secret, "exciter_whs_abc");
    await userEvent.click(screen.getByRole("button", { name: s.tools.hook.save }));
    expect(await screen.findByText(s.tools.hook.saved)).toBeInTheDocument();
    expect(sent).toEqual({ secret: "exciter_whs_abc" });
    expect(screen.getByLabelText(s.tools.hook.secret)).toHaveValue("");
    expect(screen.getByRole("button", { name: s.tools.hook.remove })).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("say why a secret was refused, as the server words it", async () => {
    const refusal = "That is not this tool's webhook secret.";
    await renderAt("/settings", {
      answers: {
        "/api/v1/tools": { tools: [EXCITER] },
        [HOOK_PATH]: unset,
        [`PUT ${HOOK_PATH}`]: () => Response.json({ error: { code: "validation_failed", message: refusal } }, { status: 422 }),
      },
    });
    await userEvent.type(await screen.findByLabelText(s.tools.hook.secret), "nope");
    await userEvent.click(screen.getByRole("button", { name: s.tools.hook.save }));
    expect(await screen.findByRole("alert")).toHaveTextContent(refusal);
  });

  it("are checked again on asking, and a tool that stopped sending webhooks says so instead of offering a hook", async () => {
    let tool: Tool = EXCITER;
    const sentence = "Exciter does not offer webhooks@1, which Rotor needs to start workflows from its events.";
    const { fetch } = await renderAt("/settings", {
      answers: {
        "/api/v1/tools": () => Response.json({ tools: [tool] }),
        [HOOK_PATH]: unset,
        [`POST /api/v1/tools/${EXCITER.id}/check`]: () => {
          tool = { ...EXCITER, capabilities: ["mcp@1"], missing: [{ capability: "webhooks@1", message: sentence }] };
          return Response.json({ tool });
        },
      },
    });
    expect(await screen.findByLabelText(s.tools.hook.address)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: s.capabilities.checkAgain }));
    expect(await screen.findByText(s.capabilities.lacks("Exciter", "webhooks@1"))).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent(s.capabilities.checked("Exciter"));
    expect(screen.queryByLabelText(s.tools.hook.address)).not.toBeInTheDocument();
    const calls = fetch.mock.calls.map(([request]) => `${request.method} ${new URL(request.url).pathname}`);
    expect(calls).toContain(`POST /api/v1/tools/${EXCITER.id}/check`);
  });

  it("are not offered to members, nor for a tool that sends none", async () => {
    await renderAt("/settings", { me: MEMBER, answers: { "/api/v1/tools": { tools: [EXCITER] } } });
    await screen.findByText("Exciter");
    expect(screen.queryByLabelText(s.tools.hook.address)).not.toBeInTheDocument();
    cleanup();
    await renderAt("/settings", { answers: { "/api/v1/tools": { tools: [{ ...EXCITER, name: "jira" }] } } });
    await screen.findByText("Jira");
    expect(screen.queryByLabelText(s.tools.hook.address)).not.toBeInTheDocument();
  });
});
