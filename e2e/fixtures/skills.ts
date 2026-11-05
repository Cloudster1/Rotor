import { expect, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { crc32 } from "node:zlib";

/** A skill name no other run has taken: names are unique in the installation and a skill is archived, never deleted. */
export function skillName(): string {
  return `e2e-${randomUUID().slice(0, 12)}`;
}

/** A SKILL.md the library's checks pass. */
export function skillMD(name: string, body = "Write the story in one sentence."): string {
  return `---\nname: ${name}\ndescription: How an e2e run words a story, for its agents.\n---\n\n# ${name}\n\n${body}\n`;
}

// The zip format's fixed numbers: each record's signature, the version needed to extract a stored entry, and the
// sizes of the parts written after a local header and in the end record.
const LOCAL_HEADER = 0x04034b50;
const CENTRAL_HEADER = 0x02014b50;
const END_OF_CENTRAL = 0x06054b50;
const VERSION_STORED = 10;
const LOCAL_SIZE = 30;
const CENTRAL_SIZE = 46;
const END_SIZE = 22;

/** A zip of files stored without compression, which every reader takes: enough to import a skill without a library. */
export function zipOf(files: Record<string, string>): Buffer {
  const locals: Buffer[] = [];
  const centrals: Buffer[] = [];
  let offset = 0;
  for (const [path, text] of Object.entries(files)) {
    const name = Buffer.from(path, "utf8");
    const data = Buffer.from(text, "utf8");
    const crc = crc32(data);
    const local = Buffer.alloc(LOCAL_SIZE);
    local.writeUInt32LE(LOCAL_HEADER, 0);
    local.writeUInt16LE(VERSION_STORED, 4);
    local.writeUInt32LE(crc, 14);
    local.writeUInt32LE(data.length, 18);
    local.writeUInt32LE(data.length, 22);
    local.writeUInt16LE(name.length, 26);
    const central = Buffer.alloc(CENTRAL_SIZE);
    central.writeUInt32LE(CENTRAL_HEADER, 0);
    central.writeUInt16LE(VERSION_STORED, 4);
    central.writeUInt16LE(VERSION_STORED, 6);
    central.writeUInt32LE(crc, 16);
    central.writeUInt32LE(data.length, 20);
    central.writeUInt32LE(data.length, 24);
    central.writeUInt16LE(name.length, 28);
    central.writeUInt32LE(offset, 42);
    locals.push(local, name, data);
    centrals.push(central, name);
    offset += LOCAL_SIZE + name.length + data.length;
  }
  const directory = Buffer.concat(centrals);
  const end = Buffer.alloc(END_SIZE);
  end.writeUInt32LE(END_OF_CENTRAL, 0);
  end.writeUInt16LE(Object.keys(files).length, 8);
  end.writeUInt16LE(Object.keys(files).length, 10);
  end.writeUInt32LE(directory.length, 12);
  end.writeUInt32LE(offset, 16);
  return Buffer.concat([...locals, directory, end]);
}

/** The id of the skill of a name, as the page's person reads the list. */
export async function skillID(page: Page, name: string): Promise<string> {
  const listed = (await (await page.request.get(`/api/v1/skills?q=${encodeURIComponent(name)}`)).json()) as { skills: { id: string; name: string }[] };
  const found = listed.skills.find((each) => each.name === name);
  expect(found, `the skill ${name}`).toBeDefined();
  return found?.id ?? "";
}

/** Archives a skill a spec made, which takes it out of everybody's lists; there is no deleting one. */
export async function archiveSkill(page: Page, id: string): Promise<void> {
  if (!id) return;
  const gone = await page.request.delete(`/api/v1/skills/${id}`);
  expect([204, 404, 409], `archiving skill ${id}`).toContain(gone.status());
}
