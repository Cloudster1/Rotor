import { type Document, isMap, isScalar, isSeq, type Node, type Pair, parseDocument, type YAMLMap, type YAMLSeq } from "yaml";
import { nodeMap } from "./specDoc";
import { applySplices, column, flowPadding, inFlow, lineAfter, lineStart, pairOf, removeItem, removePair, type Splice } from "./yamlText";

// An agent's skills are pins, `- { name: x, version: 2 }`, spliced into the text as the canvas's edits are, so what
// the edit does not touch keeps its comments and spacing. The version is written bare: it is a number, not text.

/** A skill an agent may follow, pinned to one version (#221). */
export interface SkillPin {
  name: string;
  version: number;
}

/** What adding a skill at the cursor did, or why it could not: the picker says which. */
export type SkillAdded =
  | { ok: true; text: string; node: string; repinned: boolean }
  | { ok: false; why: "unparsed" | "outside" }
  | { ok: false; why: "notAgent"; node: string; type: string };

const textOf = (n: unknown) => (isScalar(n) ? String(n.value ?? "") : "");

function read(source: string): Document.Parsed | undefined {
  const doc = parseDocument(source, { version: "1.2" });
  return doc.errors.length > 0 || !isMap(doc.contents) ? undefined : doc;
}

function checked(source: string): string {
  if (!read(source)) throw new Error("The skill could not be written into the workflow's YAML.");
  return source;
}

const pinText = (pin: SkillPin, pad: string) => `{${pad}name: ${pin.name}, version: ${pin.version}${pad}}`;

/** The node whose text holds an offset: a block item from the start of its dash line, so the dash counts too. */
export function nodeAt(source: string, offset: number): YAMLMap | undefined {
  const doc = read(source);
  const nodes = doc?.get("nodes", true);
  if (!isSeq(nodes)) return undefined;
  let found: YAMLMap | undefined;
  for (const item of nodes.items) {
    if (!isMap(item) || !item.range) continue;
    const start = nodes.flow ? item.range[0] : lineStart(source, item.range[0]);
    // A later node starting where one ends takes the offset, as the cursor at a line's start is on that line.
    if (offset >= start && offset <= item.range[2]) found = item;
  }
  return found;
}

/** Gives the agent node at the cursor a skill, adding `skills:` when it has none; a skill it names already is pinned anew. */
export function addSkillAt(source: string, offset: number, pin: SkillPin): SkillAdded {
  if (!read(source)) return { ok: false, why: "unparsed" };
  const found = nodeAt(source, offset);
  if (!found) return { ok: false, why: "outside" };
  const node = textOf(found.get("id", true));
  const type = textOf(found.get("type", true));
  if (type !== "agent") return { ok: false, why: "notAgent", node, type };
  const splice = skillSplice(source, found, pin);
  return { ok: true, text: checked(applySplices(source, [splice.splice])), node, repinned: splice.repinned };
}

function skillSplice(source: string, map: YAMLMap, pin: SkillPin): { splice: Splice; repinned: boolean } {
  const doc = read(source) as Document.Parsed;
  const pad = flowPadding(source);
  const flow = inFlow(doc.get("nodes", true), map);
  const skills = pairOf(map, "skills");
  if (skills && isSeq(skills.pair.value)) {
    const seq = skills.pair.value;
    const same = seq.items.find((item): item is YAMLMap => isMap(item) && textOf(item.get("name", true)) === pin.name);
    const version = same?.get("version", true);
    if (isScalar(version) && version.range) {
      return { splice: { from: version.range[0], to: version.range[1], insert: String(pin.version) }, repinned: true };
    }
    return { splice: appendPin(source, seq, pin, pad, flow), repinned: false };
  }
  const items = map.items as Pair<Node, Node>[];
  if (skills) {
    // `skills:` with nothing under it gets its list in place.
    const keyEnd = skills.pair.key.range?.[1] ?? 0;
    const colon = source.indexOf(":", keyEnd);
    const end = skills.pair.value?.range?.[1] ?? colon + 1;
    return { splice: { from: colon + 1, to: Math.max(end, colon + 1), insert: ` [${pinText(pin, pad)}]` }, repinned: false };
  }
  if (flow || map.flow) {
    const last = items[items.length - 1] as Pair<Node, Node>;
    const end = (last.value ?? last.key).range?.[1] ?? 0;
    return { splice: { from: end, to: end, insert: `, skills: [${pinText(pin, pad)}]` }, repinned: false };
  }
  const indent = column(source, items[0]?.key.range?.[0] ?? 0);
  // After the value, not the node's whole range: that runs on over comments and the next item's indent.
  const at = lineAfter(source, map.range?.[1] ?? source.length);
  const lead = at === source.length && !source.endsWith("\n") ? "\n" : "";
  const lines = `${" ".repeat(indent)}skills:\n${" ".repeat(indent + 2)}- ${pinText(pin, pad)}\n`;
  return { splice: { from: at, to: at, insert: `${lead}${lines}` }, repinned: false };
}

function appendPin(source: string, seq: YAMLSeq, pin: SkillPin, pad: string, flow: boolean): Splice {
  const last = seq.items[seq.items.length - 1] as Node | undefined;
  if (flow || seq.flow) {
    if (!last?.range) {
      const start = seq.range?.[0] ?? 0;
      return { from: start + 1, to: start + 1, insert: pinText(pin, pad) };
    }
    return { from: last.range[1], to: last.range[1], insert: `, ${pinText(pin, pad)}` };
  }
  const first = (seq.items[0] as Node).range?.[0] ?? 0;
  const indent = column(source, source.lastIndexOf("-", first));
  const at = lineAfter(source, seq.range?.[1] ?? source.length);
  const lead = at === source.length && !source.endsWith("\n") ? "\n" : "";
  return { from: at, to: at, insert: `${lead}${" ".repeat(indent)}- ${pinText(pin, pad)}\n` };
}

/** Takes one skill off an agent; the last one takes `skills:` with it, so adding one and taking it back is the text again. */
export function removeSkill(source: string, id: string, index: number): string {
  const doc = read(source);
  const found = doc && nodeMap(doc, id);
  if (!found) return source;
  const skills = pairOf(found.map, "skills");
  if (!skills || !isSeq(skills.pair.value) || !skills.pair.value.items[index]) return source;
  const splice = skills.pair.value.items.length > 1 ? removeItem(source, skills.pair, index) : removePair(source, undefined, found.map, skills.index);
  return checked(applySplices(source, [splice]));
}
