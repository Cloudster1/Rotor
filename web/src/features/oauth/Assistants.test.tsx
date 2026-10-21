import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { ME, renderAt } from "@/test/app";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const REQUEST = {
  id: "0190a5c4-0000-7000-8000-0000000000aa",
  client: { id: "client-1", name: "Claude Code", uri: "" },
  redirectHost: "127.0.0.1:33418",
  readOnly: false,
  expiresAt: "2026-10-21T09:00:00Z",
};

const GRANT = {
  id: "0190a5c4-0000-7000-8000-0000000000bb",
  client: { id: "client-1", name: "Claude Code", uri: "" },
  redirectHost: "127.0.0.1:33418",
  readOnly: true,
  createdAt: "2026-10-21T08:00:00Z",
  lastUsedAt: null,
};

describe("the consent page", () => {
  it("lets a person allow an assistant to read only, and sends the browser back", async () => {
    const sent: unknown[] = [];
    const assign = vi.fn();
    vi.stubGlobal("location", { ...window.location, assign });
    await renderAt(`/oauth/consent?request=${REQUEST.id}`, {
      answers: {
        [`/api/v1/oauth/requests/${REQUEST.id}`]: { request: REQUEST },
        [`POST /api/v1/oauth/requests/${REQUEST.id}`]: async (request: Request) => {
          sent.push(await request.json());
          return Response.json({ redirectTo: "http://127.0.0.1:33418/cb?code=rotor_oac_x&state=s" });
        },
      },
    });
    const consent = await screen.findByRole("region", { name: s.assistants.asks("Claude Code") });
    expect(within(consent).getByText(s.assistants.goesBack("127.0.0.1:33418"))).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(consent).getByLabelText(s.assistants.readOnly));
    await userEvent.click(within(consent).getByRole("button", { name: s.assistants.allow }));
    await waitFor(() => expect(assign).toHaveBeenCalledWith("http://127.0.0.1:33418/cb?code=rotor_oac_x&state=s"));
    expect(sent).toEqual([{ allow: true, readOnly: true }]);
  });

  it("keeps a request to read at reading, and denies without granting anything", async () => {
    const sent: unknown[] = [];
    vi.stubGlobal("location", { ...window.location, assign: vi.fn() });
    await renderAt(`/oauth/consent?request=${REQUEST.id}`, {
      answers: {
        [`/api/v1/oauth/requests/${REQUEST.id}`]: { request: { ...REQUEST, readOnly: true } },
        [`POST /api/v1/oauth/requests/${REQUEST.id}`]: async (request: Request) => {
          sent.push(await request.json());
          return Response.json({ redirectTo: "http://127.0.0.1:33418/cb?error=access_denied" });
        },
      },
    });
    const box = await screen.findByLabelText(s.assistants.readOnly);
    expect(box).toBeChecked();
    expect(box).toBeDisabled();
    await userEvent.click(screen.getByRole("button", { name: s.assistants.deny }));
    await waitFor(() => expect(sent).toEqual([{ allow: false, readOnly: false }]));
  });

  it("says why an assistant was refused, and when a request ran out", async () => {
    await renderAt("/oauth/consent?error=invalid_redirect");
    expect(await screen.findByRole("alert")).toHaveTextContent(s.assistants.refused.invalid_redirect ?? "");
    cleanup();
    await renderAt(`/oauth/consent?request=${REQUEST.id}`);
    expect(await screen.findByRole("alert")).toHaveTextContent(s.assistants.expired);
  });
});

describe("the connected assistants", () => {
  it("lists a person's assistants and disconnects one", async () => {
    let grants = [GRANT];
    await renderAt("/settings", {
      me: { ...ME, role: "member" },
      answers: {
        "/api/v1/oauth/grants": () => Response.json({ grants }),
        [`DELETE /api/v1/oauth/grants/${GRANT.id}`]: () => {
          grants = [];
          return new Response(null, { status: 204 });
        },
      },
    });
    const section = await screen.findByRole("region", { name: s.assistants.sectionTitle });
    expect(await within(section).findByText(/Reads only/)).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: s.assistants.everyoneTitle })).toBeNull();
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(section).getByRole("button", { name: s.assistants.disconnectNamed("Claude Code") }));
    expect(await within(section).findByText(s.assistants.none)).toBeInTheDocument();
  });

  it("lets an administrator switch assistants off and end anybody's", async () => {
    const switched: unknown[] = [];
    let everyone = [{ ...GRANT, owner: { id: "u-2", name: "Bob Member", email: "bob@suite.test" } }];
    await renderAt("/settings", {
      answers: {
        "/api/v1/oauth/grants/everyone": () => Response.json({ grants: everyone }),
        "PUT /api/v1/settings/assistants": async (request: Request) => {
          const body = (await request.json()) as { connectionsOn: boolean };
          switched.push(body);
          return Response.json({ connectionsOn: body.connectionsOn, updatedAt: "2026-10-21T09:00:00Z" });
        },
        [`DELETE /api/v1/oauth/grants/everyone/${GRANT.id}`]: () => {
          everyone = [];
          return new Response(null, { status: 204 });
        },
      },
    });
    const section = await screen.findByRole("region", { name: s.assistants.everyoneTitle });
    const on = await within(section).findByLabelText(s.assistants.switchedOn);
    expect(on).toBeChecked();
    await userEvent.click(on);
    await waitFor(() => expect(within(section).getByLabelText(s.assistants.switchedOn)).not.toBeChecked());
    expect(switched).toEqual([{ connectionsOn: false }]);
    await userEvent.click(within(section).getByRole("button", { name: s.assistants.disconnectFor("Claude Code", "Bob Member") }));
    expect(await within(section).findByText(s.assistants.everyoneNone)).toBeInTheDocument();
  });
});
