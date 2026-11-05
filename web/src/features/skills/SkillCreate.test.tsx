import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Assignment } from "@/api/access";
import { s } from "@/strings";
import { ACCESS_ANSWERS, BOB, EVERYONE, grant } from "@/test/access";
import { renderAt } from "@/test/app";
import { AXE_TIMEOUT_MS, axeViolations } from "@/test/axe";
import { SKILL, SKILL_ID, SKILL_RESPONSE, SKILL_ROLES, skillAnswers, V2 } from "@/test/skills";
import { NEW_SKILL_MD } from "./template";

beforeAll(async () => {
  await Promise.all([import("./SkillPages"), import("./NewSkillPage"), import("./ImportSkillPage")]);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("a skill's sharing", () => {
  it(
    "offers the skill roles and Everyone, and grants and revokes on the skill",
    async () => {
      const bobReads = grant("g1", { role: "skill_reader", roleName: "Skill reader", userId: BOB.id, userName: BOB.name, userEmail: BOB.email });
      const everyoneReads = grant("g2", {
        role: "skill_reader",
        roleName: "Skill reader",
        groupId: EVERYONE.id,
        groupName: "Everyone",
        groupBuiltin: "everyone",
      });
      let grants: Assignment[] = [bobReads];
      const sent: string[] = [];
      await renderAt(`/skills/${SKILL_ID}/sharing`, {
        answers: {
          ...ACCESS_ANSWERS,
          ...skillAnswers(),
          "/api/v1/roles": { roles: SKILL_ROLES },
          [`/api/v1/skills/${SKILL_ID}/grants`]: () => Response.json({ grants }),
          [`POST /api/v1/skills/${SKILL_ID}/grants`]: async (request: Request) => {
            sent.push(`POST ${JSON.stringify(await request.json())}`);
            grants = [...grants, everyoneReads];
            return Response.json({ grant: everyoneReads }, { status: 201 });
          },
          [`DELETE /api/v1/skills/${SKILL_ID}/grants/g1`]: () => {
            sent.push("DELETE g1");
            grants = grants.filter((each) => each.id !== "g1");
            return new Response(null, { status: 204 });
          },
        },
      });
      const panel = within(await screen.findByRole("region", { name: s.skills.sharing.title }));
      expect(panel.getByText(s.skills.sharing.everyone)).toBeInTheDocument();
      const form = within(panel.getByRole("form", { name: s.skills.sharing.share }));
      const role = form.getByLabelText(s.skills.sharing.role);
      await waitFor(() =>
        expect(
          within(role)
            .getAllByRole("option")
            .map((o) => o.textContent),
        ).toEqual(["Skill reader", "Skill editor", "Skill administrator"]),
      );
      const who = form.getByLabelText(s.skills.sharing.who);
      await within(who).findByRole("option", { name: s.access.groups.everyone });
      await userEvent.selectOptions(who, `group:${EVERYONE.id}`);
      await userEvent.click(form.getByRole("button", { name: s.skills.sharing.share }));
      await waitFor(() => expect(sent).toEqual([`POST ${JSON.stringify({ role: "skill_reader", groupId: EVERYONE.id })}`]));
      expect(await axeViolations()).toEqual([]);

      await userEvent.click(panel.getByRole("button", { name: s.skills.sharing.removeFrom("Skill reader", BOB.name) }));
      const dialog = screen.getByRole("dialog", { name: s.skills.sharing.removeTitle(BOB.name) });
      await userEvent.click(within(dialog).getByRole("button", { name: s.skills.sharing.removeGrant }));
      await waitFor(() => expect(sent.at(-1)).toBe("DELETE g1"));
    },
    AXE_TIMEOUT_MS,
  );
});

describe("a new skill", () => {
  it(
    "starts from a template the checks pass, checks it as it is typed, and opens the skill once made",
    async () => {
      const checked: string[] = [];
      let created: unknown;
      const problem = { code: "skill_description", path: "SKILL.md", message: "Give the skill a description.", line: 0, column: 0 };
      const { router } = await renderAt("/skills/new", {
        answers: {
          ...skillAnswers(),
          "POST /api/v1/skills/validate": async (request: Request) => {
            const { files } = (await request.json()) as { files: { content: string }[] };
            checked.push(files[0]?.content ?? "");
            const bad = !files[0]?.content.includes("description:");
            return Response.json({ valid: !bad, problems: bad ? [problem] : [] });
          },
          "POST /api/v1/skills": async (request: Request) => {
            created = await request.json();
            return Response.json(SKILL_RESPONSE, { status: 201 });
          },
        },
      });
      const text = await screen.findByLabelText(s.skills.create.label);
      expect(text).toHaveValue(NEW_SKILL_MD);
      expect(await screen.findByText(s.skills.files.clean)).toBeInTheDocument();
      expect(checked[0]).toBe(NEW_SKILL_MD);
      expect(await axeViolations()).toEqual([]);

      fireEvent.change(text, { target: { value: NEW_SKILL_MD.replace(/description:.*\n/, "") } });
      expect(await screen.findByText("Give the skill a description.")).toBeInTheDocument();
      expect(text).toHaveAttribute("aria-invalid", "true");
      fireEvent.change(text, { target: { value: NEW_SKILL_MD } });
      await screen.findByText(s.skills.files.clean);

      await userEvent.type(screen.getByLabelText(s.skills.create.comment), "First");
      await userEvent.click(screen.getByRole("button", { name: s.skills.create.create }));
      await waitFor(() => expect(router.state.location.pathname).toBe(`/skills/${SKILL_ID}/files`));
      expect(created).toEqual({ files: [{ path: "SKILL.md", content: NEW_SKILL_MD }], comment: "First" });
    },
    AXE_TIMEOUT_MS,
  );
});

describe("importing a skill", () => {
  const zip = () => new File([new Uint8Array([0x50, 0x4b, 0x03, 0x04])], "story-style.zip", { type: "application/zip" });

  async function importWith(answer: (request: Request) => Response | Promise<Response>) {
    const seen: { type?: string | null; comment?: string | null; bytes?: number } = {};
    await renderAt("/skills/import", {
      answers: {
        "POST /api/v1/skills/import": async (request: Request) => {
          seen.type = request.headers.get("content-type");
          seen.comment = new URL(request.url).searchParams.get("comment");
          seen.bytes = (await request.clone().arrayBuffer()).byteLength;
          return answer(request);
        },
      },
    });
    await userEvent.upload(await screen.findByLabelText(s.skills.import.file), zip());
    await userEvent.type(screen.getByLabelText(s.skills.import.comment), "From Claude Code");
    await userEvent.click(screen.getByRole("button", { name: s.skills.import.import }));
    return seen;
  }

  it(
    "sends the zip as it is and says a skill was made",
    async () => {
      const seen = await importWith(() => Response.json({ ...SKILL_RESPONSE, skill: { ...SKILL, currentVersion: 1 }, created: true }, { status: 201 }));
      expect(await screen.findByText(s.skills.import.created(SKILL.name))).toBeInTheDocument();
      expect(screen.getByRole("link", { name: s.skills.import.open(SKILL.name) })).toHaveAttribute("href", `/skills/${SKILL_ID}/files`);
      expect(seen).toEqual({ type: "application/zip", comment: "From Claude Code", bytes: 4 });
      expect(await axeViolations()).toEqual([]);
    },
    AXE_TIMEOUT_MS,
  );

  it("says a version was added to the skill of that name", async () => {
    await importWith(() => Response.json({ ...SKILL_RESPONSE, created: false }));
    expect(await screen.findByText(s.skills.import.added(SKILL.name, V2.number))).toBeInTheDocument();
  });

  it("names each problem the zip was refused for, at its file", async () => {
    const problems = [{ code: "skill_path", path: ".DS_Store", message: "A path may not start with a dot.", line: 0, column: 0 }];
    await importWith(() => Response.json({ error: { code: "skill_invalid", message: "The skill has problems.", problems } }, { status: 422 }));
    const alert = await screen.findByText(s.skills.import.refused);
    const list = alert.closest("[role=alert]") as HTMLElement;
    expect(within(list).getByText(s.skills.files.at(".DS_Store"))).toBeInTheDocument();
    expect(within(list).getByText("A path may not start with a dot.")).toBeInTheDocument();
  });
});
