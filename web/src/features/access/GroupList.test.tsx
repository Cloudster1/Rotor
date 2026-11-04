import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Group } from "@/api/groups";
import { s } from "@/strings";
import { ACCESS_ANSWERS, BOB, CAROL, ENGINEERING, EVERYONE, RELEASE } from "@/test/access";
import { renderAt } from "@/test/app";
import { axeViolations } from "@/test/axe";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const FROM_PROVIDER = "engineering is filled by the identity provider. Change its members there.";

/** The groups as a server keeps them, with Release managers' members changing as they are added and removed. */
function serveGroups() {
  let groups: Group[] = [EVERYONE, ENGINEERING, RELEASE];
  let members = [{ userId: CAROL.id, name: CAROL.name, email: CAROL.email }];
  const sent: string[] = [];
  const release = () => ({ group: { ...RELEASE, memberCount: members.length, members } });
  const answers = {
    ...ACCESS_ANSWERS,
    "/api/v1/groups": () => Response.json({ groups }),
    [`/api/v1/groups/${RELEASE.id}`]: () => Response.json(release()),
    "/api/v1/people": (request: Request) => Response.json({ people: new URL(request.url).searchParams.get("q") === "bob" ? [BOB, CAROL] : [] }),
    "POST /api/v1/groups": async (request: Request) => {
      const body = (await request.json()) as { name: string; externalRef?: string };
      sent.push(`POST ${JSON.stringify(body)}`);
      const made: Group = { ...RELEASE, id: "0190a5c4-0000-7000-8000-0000000e0009", name: body.name, memberCount: 0 };
      groups = [...groups, made];
      return Response.json({ group: { ...made, members: [] } }, { status: 201 });
    },
    [`POST /api/v1/groups/${RELEASE.id}/members`]: async (request: Request) => {
      const { userId } = (await request.json()) as { userId: string };
      sent.push(`add ${userId}`);
      members = [...members, { userId, name: BOB.name, email: BOB.email }];
      return Response.json(release());
    },
    [`DELETE /api/v1/groups/${RELEASE.id}/members/${CAROL.id}`]: () => {
      sent.push(`remove ${CAROL.id}`);
      members = members.filter((member) => member.userId !== CAROL.id);
      return Response.json(release());
    },
    [`DELETE /api/v1/groups/${ENGINEERING.id}`]: () => Response.json({ error: { code: "conflict", message: FROM_PROVIDER } }, { status: 409 }),
  };
  return { answers, sent };
}

const cards = () => [...document.querySelectorAll("[data-group]")].map((card) => card.getAttribute("data-group"));

describe("the Groups tab", () => {
  // Everyone holds everybody, so it is read, never edited, and its members are not listed.
  it("shows Everyone first, built in, then the provider's groups, then those made by hand", async () => {
    await renderAt("/admin/access?tab=groups", { answers: serveGroups().answers });
    await waitFor(() => expect(cards()).toEqual(["Everyone", "engineering", "Release managers"]));
    const everyone = within(screen.getByRole("region", { name: "Everyone" }));
    expect(everyone.getByText(s.access.groups.builtin)).toBeInTheDocument();
    expect(everyone.getByText("4 people . 1 role")).toBeInTheDocument();
    expect(everyone.queryByRole("button")).toBeNull();

    const engineering = within(screen.getByRole("region", { name: "engineering" }));
    expect(engineering.getByText(s.access.groups.fromProvider)).toBeInTheDocument();
    expect(engineering.getByText(/matches "engineering"/)).toBeInTheDocument();
    expect(await engineering.findByText("Ada Admin")).toBeInTheDocument();
    expect(engineering.queryByRole("button", { name: /Remove/ })).toBeNull();
    expect(engineering.queryByLabelText(s.access.groups.addTo("engineering"))).toBeNull();
    expect(await axeViolations()).toEqual([]);
  });

  it("adds somebody to a group made by hand and takes somebody out after asking", async () => {
    const server = serveGroups();
    await renderAt("/admin/access?tab=groups", { answers: server.answers });
    const release = within(await screen.findByRole("region", { name: "Release managers" }));
    expect(await release.findByText(CAROL.name)).toBeInTheDocument();
    await userEvent.type(release.getByLabelText(s.access.people.findFor(s.access.groups.addTo("Release managers"))), "bob");
    const picker = release.getByLabelText(s.access.groups.addTo("Release managers"));
    // Carol is in it already, so she is not offered.
    await within(picker).findByRole("option", { name: BOB.name });
    expect(within(picker).queryByRole("option", { name: CAROL.name })).toBeNull();
    await userEvent.selectOptions(picker, BOB.name);
    await userEvent.click(release.getByRole("button", { name: s.access.add }));
    await waitFor(() => expect(server.sent).toEqual([`add ${BOB.id}`]));
    await waitFor(() => expect(document.querySelector(`[data-member="${BOB.name}"]`)).not.toBeNull());

    await userEvent.click(release.getByRole("button", { name: s.access.groups.removeFrom(CAROL.name, "Release managers") }));
    const dialog = screen.getByRole("dialog", { name: s.access.groups.removeTitle(CAROL.name) });
    expect(within(dialog).getByText(s.access.groups.removeBody(CAROL.name, "Release managers"))).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole("button", { name: s.access.groups.removeMember }));
    await waitFor(() => expect(server.sent.at(-1)).toBe(`remove ${CAROL.id}`));
    await waitFor(() => expect(document.querySelector(`[data-member="${CAROL.name}"]`)).toBeNull());
  });

  it("creates a group, and says why the server refused to delete one", async () => {
    const server = serveGroups();
    await renderAt("/admin/access?tab=groups", { answers: server.answers });
    const form = within(await screen.findByRole("form", { name: s.access.groups.newGroup }));
    await userEvent.type(form.getByLabelText(s.access.groups.newGroup), "Reviewers");
    await userEvent.click(form.getByRole("button", { name: s.access.groups.create }));
    await waitFor(() => expect(server.sent).toEqual([`POST ${JSON.stringify({ name: "Reviewers" })}`]));
    await waitFor(() => expect(cards()).toContain("Reviewers"));
    expect(form.getByLabelText(s.access.groups.newGroup)).toHaveValue("");

    await userEvent.click(within(screen.getByRole("region", { name: "engineering" })).getByRole("button", { name: s.access.delete }));
    const dialog = screen.getByRole("dialog", { name: s.access.groups.deleteTitle("engineering") });
    await userEvent.click(within(dialog).getByRole("button", { name: s.access.groups.deleteGroup }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(FROM_PROVIDER);
  });
});
