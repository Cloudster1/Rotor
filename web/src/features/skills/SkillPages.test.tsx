import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { SkillSummary } from "@/api/skills";
import { s } from "@/strings";
import { renderAt } from "@/test/app";
import { AXE_TIMEOUT_MS, axeViolations } from "@/test/axe";
import { BUILTIN, SKILL, SKILL_ID, SKILL_MD_V2, skillAnswers, V2, version } from "@/test/skills";

beforeAll(async () => {
  await import("./SkillPages");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const t = s.skills.files;
const V1 = version(1, [{ path: "SKILL.md", bytes: 40 }]);
const V3 = version(3, V2.files);

/** The skill's pages against a stand-in that saves: a PUT answered 200 makes version 3 the current one. */
function serve(put?: (body: unknown) => Response) {
  let current: SkillSummary = SKILL;
  const sent: unknown[] = [];
  const answers = {
    ...skillAnswers(),
    [`/api/v1/skills/${SKILL_ID}`]: () => Response.json({ skill: current, version: current.currentVersion === 3 ? V3 : V2 }),
    [`/api/v1/skills/${SKILL_ID}/versions/1`]: V1,
    [`/api/v1/skills/${SKILL_ID}/versions/1/file`]: { path: "SKILL.md", content: "old\n", bytes: 4, sha256: "x" },
    [`/api/v1/skills/${SKILL_ID}/versions/3`]: V3,
    [`/api/v1/skills/${SKILL_ID}/versions/3/file`]: skillAnswers()[`/api/v1/skills/${SKILL_ID}/versions/2/file`],
    [`/api/v1/skills/${SKILL_ID}/versions`]: () => Response.json({ versions: current.currentVersion === 3 ? [V3, V2, V1] : [V2, V1] }),
    [`/api/v1/skills/${SKILL_ID}/diff`]: {
      from: 1,
      to: 2,
      files: [
        {
          path: "SKILL.md",
          status: "changed",
          hunks: [
            {
              fromLine: 5,
              toLine: 5,
              lines: [
                { op: "delete", text: "old" },
                { op: "insert", text: "# Story style" },
              ],
            },
          ],
        },
        { path: "references/examples.md", status: "added", hunks: [{ fromLine: 0, toLine: 1, lines: [{ op: "insert", text: "An example." }] }] },
      ],
    },
    [`PUT /api/v1/skills/${SKILL_ID}`]: async (request: Request) => {
      const body = await request.json();
      sent.push(body);
      const refused = put?.(body);
      if (refused) return refused;
      current = { ...SKILL, currentVersion: 3 };
      return Response.json({ skill: current, version: V3 });
    },
    [`DELETE /api/v1/skills/${SKILL_ID}`]: () => {
      sent.push("archive");
      current = { ...current, archived: true };
      return new Response(null, { status: 204 });
    },
    [`POST /api/v1/skills/${SKILL_ID}/restore`]: () => {
      sent.push("restore");
      current = { ...current, archived: false };
      return Response.json({ skill: current, version: V2 });
    },
  };
  return { answers, sent };
}

const fileButtons = () =>
  within(screen.getByRole("navigation", { name: t.list }))
    .getAllByRole("button")
    .map((b) => b.getAttribute("data-skill-file"));

async function startEditing(answers: Record<string, unknown>) {
  await renderAt(`/skills/${SKILL_ID}/files`, { answers });
  await userEvent.click(await screen.findByRole("button", { name: t.edit }));
  return screen.findByLabelText(t.content("SKILL.md"));
}

describe("a skill's files", () => {
  it(
    "shows SKILL.md first and lists every file, with the version to download",
    async () => {
      await renderAt(`/skills/${SKILL_ID}/files`, { answers: serve().answers });
      expect(await screen.findByRole("heading", { level: 1, name: s.skills.layout.pages.files })).toBeInTheDocument();
      const content = await screen.findByLabelText(t.content("SKILL.md"));
      expect(content).toHaveTextContent("Write it short.");
      expect(fileButtons()).toEqual(["SKILL.md", "references/examples.md"]);
      expect(screen.getByRole("link", { name: t.download(2) })).toHaveAttribute("href", `/api/v1/skills/${SKILL_ID}/versions/2/export`);
      await userEvent.click(screen.getByRole("button", { name: "references/examples.md" }));
      expect(screen.getByLabelText(t.content("references/examples.md"))).toHaveTextContent("An example.");
      expect(await axeViolations()).toEqual([]);
    },
    AXE_TIMEOUT_MS,
  );

  it(
    "saves the files, changed, added to and taken from, as the next version with a comment",
    async () => {
      const server = serve();
      const skillMD = await startEditing(server.answers);
      expect(await axeViolations()).toEqual([]);
      await userEvent.type(skillMD, "Keep it plain.");
      await userEvent.click(screen.getByRole("button", { name: t.removeNamed("references/examples.md") }));
      await userEvent.type(screen.getByLabelText(t.addPath), "references/terms.md");
      await userEvent.click(screen.getByRole("button", { name: t.addButton }));
      await userEvent.type(await screen.findByLabelText(t.content("references/terms.md")), "Story: what a person wants.");
      await userEvent.type(screen.getByLabelText(t.comment), "Plainer");
      await userEvent.click(screen.getByRole("button", { name: t.save(3) }));
      await waitFor(() =>
        expect(server.sent).toEqual([
          {
            baseVersion: 2,
            comment: "Plainer",
            files: [
              { path: "SKILL.md", content: `${SKILL_MD_V2}Keep it plain.` },
              { path: "references/terms.md", content: "Story: what a person wants." },
            ],
          },
        ]),
      );
      expect(await screen.findByText(t.saved(3))).toBeInTheDocument();
      expect(screen.queryByRole("button", { name: t.save(4) })).toBeNull();
    },
    AXE_TIMEOUT_MS,
  );

  it("says plainly when someone saved in the meantime", async () => {
    const stale = () => Response.json({ error: { code: "skill_changed", message: "Someone saved this skill." } }, { status: 409 });
    await startEditing(serve(stale).answers);
    await userEvent.click(screen.getByRole("button", { name: t.save(3) }));
    const alert = await screen.findByText(t.stale);
    expect(alert.closest("[role=alert]")).not.toBeNull();
    await userEvent.click(screen.getByRole("button", { name: t.reload }));
    expect(await screen.findByRole("button", { name: t.edit })).toBeInTheDocument();
  });

  it("shows what a save was refused for at the file it is in", async () => {
    const problems = [
      { code: "skill_description", path: "SKILL.md", message: "Give the skill a description.", line: 0, column: 0 },
      { code: "skill_too_many_files", path: "-", message: "A skill holds at most 50 files.", line: 0, column: 0 },
    ];
    const invalid = () => Response.json({ error: { code: "skill_invalid", message: "The skill has problems.", problems } }, { status: 422 });
    const skillMD = await startEditing(serve(invalid).answers);
    await userEvent.click(screen.getByRole("button", { name: t.save(3) }));
    await waitFor(() => expect(skillMD).toHaveAttribute("aria-invalid", "true"));
    expect(within(screen.getByRole("button", { name: /^SKILL\.md/ })).getByText(`(${t.problemsIn(1)})`)).toBeInTheDocument();
    const list = document.querySelector("[data-skill-problems]") as HTMLElement;
    expect(within(list.querySelector('[data-problems-at="SKILL.md"]') as HTMLElement).getByText("Give the skill a description.")).toBeInTheDocument();
    expect(within(list.querySelector('[data-problems-at="-"]') as HTMLElement).getByText(t.general)).toBeInTheDocument();
    expect(screen.getByText("Give the skill a description.", { selector: "[data-file-problems] li" })).toBeInTheDocument();
  });

  it("is read-only for a built-in skill, which has no sharing", async () => {
    await renderAt(`/skills/${BUILTIN.id}/files`, { answers: skillAnswers(BUILTIN) });
    expect(await screen.findByText(t.builtinReadOnly)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: t.edit })).toBeNull();
    const nav = screen.getByRole("navigation", { name: s.skills.layout.nav });
    expect(within(nav).queryByRole("link", { name: s.skills.layout.pages.sharing })).toBeNull();
  });
});

describe("a skill's versions", () => {
  it(
    "lists them, compares two file by file, and opens an older one read-only",
    async () => {
      await renderAt(`/skills/${SKILL_ID}/versions`, { answers: serve().answers });
      const versions = await screen.findByRole("region", { name: s.skills.versions.title });
      await within(versions).findByRole("link", { name: s.skills.versions.openNamed(1) });
      expect(within(versions).getByRole("link", { name: s.skills.versions.downloadNamed(1) })).toHaveAttribute(
        "href",
        `/api/v1/skills/${SKILL_ID}/versions/1/export`,
      );
      expect(within(versions).getByLabelText(s.skills.versions.from)).toHaveValue("1");
      await userEvent.click(within(versions).getByRole("button", { name: s.skills.versions.compare }));
      const changed = await screen.findByRole("region", { name: /^SKILL\.md/ });
      expect(within(changed).getByText(s.skills.versions.status.changed)).toBeInTheDocument();
      expect(changed.querySelector('[data-op="insert"]')).toHaveTextContent("# Story style");
      const added = screen.getByRole("region", { name: /^references\/examples\.md/ });
      expect(within(added).getByText(s.skills.versions.status.added)).toBeInTheDocument();
      expect(await axeViolations()).toEqual([]);

      await userEvent.click(within(versions).getByRole("link", { name: s.skills.versions.openNamed(1) }));
      expect(await screen.findByText(t.older(1, 2))).toBeInTheDocument();
      expect(await screen.findByLabelText(t.content("SKILL.md"))).toHaveTextContent("old");
      expect(screen.queryByRole("button", { name: t.edit })).toBeNull();
    },
    AXE_TIMEOUT_MS,
  );
});

describe("a skill's general page", () => {
  it(
    "shows its front matter, and archives it with an Undo",
    async () => {
      const server = serve();
      await renderAt(`/skills/${SKILL_ID}/general`, { answers: server.answers });
      const details = await screen.findByRole("region", { name: s.skills.general.detailsTitle });
      expect(within(details).getByText("license: Apache-2.0")).toBeInTheDocument();
      expect(await axeViolations()).toEqual([]);
      await userEvent.click(screen.getByRole("button", { name: s.skills.general.archive }));
      expect(await screen.findByText(s.skills.general.archived(SKILL.name))).toBeInTheDocument();
      await userEvent.click(screen.getByRole("button", { name: s.skills.general.undo }));
      expect(await screen.findByText(s.skills.general.restored(SKILL.name))).toBeInTheDocument();
      expect(server.sent).toEqual(["archive", "restore"]);
    },
    AXE_TIMEOUT_MS,
  );
});
