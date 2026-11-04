import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { JoinRequest } from "@/api/users";
import { s } from "@/strings";
import { ACCESS_ANSWERS, user } from "@/test/access";
import { renderAt } from "@/test/app";
import { axeViolations } from "@/test/axe";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const ERIN: JoinRequest = { userId: "0190a5c4-0000-7000-8000-000000000010", name: "Erin Early", email: "erin@suite.test", requestedAt: "2026-11-04T08:00:00Z" };
const FINN: JoinRequest = { userId: "0190a5c4-0000-7000-8000-000000000011", name: "", email: "finn@suite.test", requestedAt: "2026-11-04T09:00:00Z" };

/** Who waits, as a server keeps them: letting in or turning away takes them off the list. */
function serveRequests(waiting: JoinRequest[]) {
  let requests = waiting;
  const sent: string[] = [];
  const answers: Record<string, unknown> = { ...ACCESS_ANSWERS, "/api/v1/users/requests": () => Response.json({ requests }) };
  for (const each of waiting) {
    answers[`POST /api/v1/users/requests/${each.userId}/admit`] = async (request: Request) => {
      sent.push(`admit ${each.email} ${JSON.stringify(await request.json())}`);
      requests = requests.filter((one) => one.userId !== each.userId);
      return Response.json({ user: user({ id: each.userId, name: each.name, email: each.email }) });
    };
    answers[`DELETE /api/v1/users/requests/${each.userId}`] = () => {
      sent.push(`decline ${each.email}`);
      requests = requests.filter((one) => one.userId !== each.userId);
      return new Response(null, { status: 204 });
    };
  }
  return { answers, sent };
}

const row = (email: string) => document.querySelector(`[data-join-request="${email}"]`) as HTMLElement;

describe("the join requests on the Users page", () => {
  it("take no room when nobody waits", async () => {
    await renderAt("/admin/users", { answers: serveRequests([]).answers });
    expect(await screen.findByRole("heading", { level: 1, name: s.users.title })).toBeInTheDocument();
    await waitFor(() => expect(document.querySelector("[data-users-total]")).not.toBeNull());
    expect(screen.queryByRole("heading", { name: s.joinRequests.title })).not.toBeInTheDocument();
  });

  it("are listed by name and address, and let in as a member with one click", async () => {
    const server = serveRequests([ERIN, FINN]);
    await renderAt("/admin/users", { answers: server.answers });
    expect(await screen.findByRole("heading", { level: 2, name: s.joinRequests.title })).toBeInTheDocument();
    expect(screen.getByText(s.joinRequests.detail)).toBeInTheDocument();
    expect(within(row(ERIN.email)).getByText(ERIN.name)).toBeInTheDocument();
    // Somebody the provider named no one is shown by their address.
    expect(within(row(FINN.email)).getAllByText(new RegExp(FINN.email)).length).toBeGreaterThan(0);
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(screen.getByRole("button", { name: s.joinRequests.admitNamed(ERIN.name) }));
    await waitFor(() => expect(server.sent).toEqual([`admit ${ERIN.email} {"role":"member"}`]));
    expect(await screen.findByText(s.joinRequests.letIn(ERIN.name))).toBeInTheDocument();
    await waitFor(() => expect(row(ERIN.email)).toBeNull());
  });

  it("turn somebody away, who may ask again", async () => {
    const server = serveRequests([FINN]);
    await renderAt("/admin/users", { answers: server.answers });
    await userEvent.click(await screen.findByRole("button", { name: s.joinRequests.turnAwayNamed(FINN.email) }));
    await waitFor(() => expect(server.sent).toEqual([`decline ${FINN.email}`]));
    expect(await screen.findByText(s.joinRequests.turnedAway(FINN.email))).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByRole("heading", { name: s.joinRequests.title })).not.toBeInTheDocument());
  });
});
