import { isMap, isScalar, isSeq, type Node, type Pair, parse, type Scalar, type YAMLMap, type YAMLSeq } from "yaml";

// Every edit is a splice into the text the person wrote, never a fresh print of the document: what the edit
// does not touch keeps its comments, quoting, spacing and order byte for byte.

/** A change to the text: what lies between from and to becomes insert. */
export interface Splice {
  from: number;
  to: number;
  insert: string;
}

/** A value written into the YAML: text, or a map of them, as a new node's fields are. */
export type Value = string | string[] | { [key: string]: Value };

/** Applies splices made against the same text, last first, so each one's offsets still hold. */
export function applySplices(source: string, splices: Splice[]): string {
  let out = source;
  for (const s of [...splices].sort((a, b) => b.from - a.from)) out = out.slice(0, s.from) + s.insert + out.slice(s.to);
  return out;
}

const lineStart = (source: string, at: number) => source.lastIndexOf("\n", at - 1) + 1;
const column = (source: string, at: number) => at - lineStart(source, at);

/** The start of the line at or after at: a node's end often sits there already. */
function lineAfter(source: string, at: number): number {
  if (at === 0 || source[at - 1] === "\n") return at;
  const next = source.indexOf("\n", at);
  return next === -1 ? source.length : next + 1;
}

const range = (node: Node | null | undefined): [number, number, number] => {
  if (!node?.range) throw new Error("The node has no place in the text.");
  return node.range;
};

/** Whether a value can be written bare, as YAML 1.2 would read it back as the same text. */
function plainSafe(value: string, flow: boolean): boolean {
  if (value === "" || value !== value.trim() || /[\n\t]/.test(value) || !printable(value)) return false;
  if (flow && /[,[\]{}:#]/.test(value)) return false;
  try {
    return parse(value, { version: "1.2" }) === value;
  } catch {
    return false;
  }
}

// The code points YAML 1.2 does not allow bare: C0 and C1 controls but tab and line feed, lone surrogates, and these.
const [TAB, LINE_FEED, SPACE, DELETE, LAST_C1] = [0x09, 0x0a, 0x20, 0x7f, 0x9f];
const [SURROGATES_FROM, SURROGATES_TO] = [0xd800, 0xdfff];
const NONCHARACTERS = [0xfeff, 0xfffe, 0xffff];

/** Whether text holds only what YAML may carry unescaped, carriage returns aside, which it reads as line breaks. */
const printable = (value: string) => {
  for (const ch of value) {
    const c = ch.codePointAt(0) ?? 0;
    const control = (c < SPACE && c !== TAB && c !== LINE_FEED) || (c >= DELETE && c <= LAST_C1);
    const lone = c >= SURROGATES_FROM && c <= SURROGATES_TO;
    if (control || lone || NONCHARACTERS.includes(c)) return false;
  }
  return true;
};

/** A double-quoted YAML string: JSON's escapes are YAML's, and what JSON leaves bare but YAML may not is escaped too. */
const quoted = (value: string) => JSON.stringify(value).replace(/[\x7F-\x9F\uFEFF\uFFFE\uFFFF]/g, (c) => `\\u${c.charCodeAt(0).toString(16).padStart(4, "0")}`);

/** Whether lines can be written as a `|` block: printable, with something on them, the first not indented. */
function blockSafe(value: string): boolean {
  const first = value.split("\n").find((l) => l !== "");
  return first !== undefined && !/^[ \t]/.test(first) && printable(value) && !value.split("\n").some((l) => l !== "" && l.trim() === "");
}

/** How flow maps are spaced in this text, `{ a: b }` or `{a: b}`, so a new one matches its neighbours. */
export function flowPadding(source: string, near?: Node): string {
  const after = near?.range && source[near.range[0]] === "{" ? source[near.range[0] + 1] : undefined;
  if (after !== undefined && after !== "}") return after === " " ? " " : "";
  if (/\{ [^ }]/.test(source)) return " ";
  return /\{[^ \n}]/.test(source) ? "" : " ";
}

/** One value as YAML text, always the same way: bare where that reads back the same, a `|` block for lines, else quoted. */
export function scalarText(value: string, flow: boolean, indent: number): string {
  if (value.includes("\n") && !flow && blockSafe(value)) {
    const body = value.replace(/\n+$/, "");
    const trailing = value.length - body.length;
    const header = trailing === 0 ? "|-" : trailing === 1 ? "|" : "|+";
    const lines = [...body.split("\n"), ...Array(Math.max(trailing - 1, 0)).fill("")];
    return `${header}\n${lines.map((l) => (l === "" ? "" : " ".repeat(indent) + l)).join("\n")}`;
  }
  return plainSafe(value, flow) ? value : quoted(value);
}

/** A value for a flow collection: `{ a: b, c: { d: e } }`, spaced as the text spaces them. */
export function flowText(value: Value, pad: string): string {
  if (typeof value === "string") return scalarText(value, true, 0);
  if (Array.isArray(value)) return `[${value.map((v) => scalarText(v, true, 0)).join(", ")}]`;
  const pairs = Object.entries(value).map(([k, v]) => `${scalarText(k, true, 0)}: ${flowText(v, pad)}`);
  return pairs.length === 0 ? "{}" : `{${pad}${pairs.join(", ")}${pad}}`;
}

/** A map's lines in block style at an indent, without the line breaks between them. */
export function blockLines(value: { [key: string]: Value }, indent: number): string[] {
  const pad = " ".repeat(indent);
  return Object.entries(value).flatMap(([k, v]) => {
    const key = `${pad}${scalarText(k, false, 0)}:`;
    if (typeof v === "string") return [`${key} ${scalarText(v, false, indent + 2)}`];
    if (Array.isArray(v)) return [`${key} ${flowText(v, "")}`];
    if (Object.keys(v).length === 0) return [`${key} {}`];
    return [key, ...blockLines(v, indent + 2)];
  });
}

/** Writes a scalar anew in its place; the same value is always written the same way, so an edit undone is the text again. */
export function replaceScalar(source: string, node: Scalar, value: string, flow: boolean): Splice {
  const [start, end] = range(node);
  const block = node.type === "BLOCK_LITERAL" || node.type === "BLOCK_FOLDED";
  const trailingComment = source.slice(end, lineAfter(source, end)).trim() !== "";
  const indent = keyIndent(source, start) + 2;
  let written = scalarText(value, flow || trailingComment, indent);
  // A block scalar's place takes in its line break; a bare one's does not, so the break stays outside it.
  if (block) written += "\n";
  return { from: start, to: end, insert: written };
}

/** The indent of the line a value starts on, which a block scalar's lines go two deeper than. */
function keyIndent(source: string, at: number): number {
  const from = lineStart(source, at);
  const line = source.slice(from, at);
  const dash = /^(\s*)-\s+/.exec(line);
  return dash ? line.length - line.trimStart().length + (dash[0].length - (dash[1]?.length ?? 0)) : line.length - line.trimStart().length;
}

/** Swaps a name for another inside a scalar's own text, so its quoting stays: ids need no escaping in any style. */
export function renameIn(source: string, node: Scalar, from: string, to: string): Splice {
  const [start, end] = range(node);
  const slice = source.slice(start, end);
  return { from: start, to: end, insert: slice.replace(from, to) };
}

/** Whether a collection, or one it sits in, is written in flow style. */
export const inFlow = (...nodes: unknown[]) => nodes.some((n) => (isMap(n) || isSeq(n)) && n.flow === true);

/** Appends an item to a sequence, in its style: after the last item in flow, as a new `- ` line in block. */
export function appendItem(source: string, seq: YAMLSeq, item: Value, flow: boolean): Splice {
  const last = seq.items[seq.items.length - 1] as Node | undefined;
  const pad = flowPadding(source, last ?? undefined);
  if (flow || seq.flow) {
    if (!last) {
      const [start] = range(seq);
      return { from: start + 1, to: start + 1, insert: flowText(item, pad) };
    }
    return { from: range(last)[1], to: range(last)[1], insert: `, ${flowText(item, pad)}` };
  }
  const first = range(seq.items[0] as Node)[0];
  const dash = source.lastIndexOf("-", first);
  const indent = column(source, dash);
  const at = lineAfter(source, range(seq)[2]);
  const lead = at === source.length && !source.endsWith("\n") ? "\n" : "";
  const blockItem = last && isMap(last) && !last.flow && typeof item !== "string";
  const lines = blockItem
    ? blockLines(item as { [key: string]: Value }, indent + 2).map((l, i) => (i === 0 ? `${" ".repeat(indent)}- ${l.trimStart()}` : l))
    : [`${" ".repeat(indent)}- ${flowText(item, pad)}`];
  return { from: at, to: at, insert: `${lead}${lines.join("\n")}\n` };
}

/** Removes one item of a sequence with its line or its comma; the last one leaves `[]` behind. */
export function removeItem(source: string, owner: Pair, index: number): Splice {
  const seq = owner.value as YAMLSeq;
  const item = seq.items[index] as Node;
  if (seq.items.length === 1) return emptyValue(source, owner, "[]");
  return seq.flow ? removeFlowEntry(seq.items as Node[], index, (n) => range(n)) : removeLines(source, range(item)[0], range(item)[2]);
}

/** Removes one pair of a map with its line or its comma; the last one leaves `{}` behind. */
export function removePair(source: string, owner: Pair | undefined, map: YAMLMap, index: number): Splice {
  const pair = map.items[index] as Pair<Node, Node>;
  if (map.items.length === 1 && owner) return emptyValue(source, owner, "{}");
  const span = (p: Pair<Node, Node>): [number, number, number] => [range(p.key)[0], range(p.value ?? p.key)[1], range(p.value ?? p.key)[2]];
  if (map.flow) return removeFlowEntry(map.items as Pair<Node, Node>[], index, span);
  return removeLines(source, span(pair)[0], span(pair)[2]);
}

function removeLines(source: string, start: number, end: number): Splice {
  return { from: lineStart(source, start), to: lineAfter(source, Math.max(end, start + 1)), insert: "" };
}

function removeFlowEntry<T>(entries: T[], index: number, span: (e: T) => [number, number, number]): Splice {
  const next = entries[index + 1];
  if (next) return { from: span(entries[index] as T)[0], to: span(next)[0], insert: "" };
  const prev = entries[index - 1] as T;
  return { from: span(prev)[1], to: span(entries[index] as T)[1], insert: "" };
}

/** Writes an empty collection as a pair's value, on the key's line. */
function emptyValue(source: string, owner: Pair, empty: string): Splice {
  const key = range(owner.key as Node);
  const value = range(owner.value as Node);
  const colon = source.indexOf(":", key[1]);
  const flowValue = isMap(owner.value) || isSeq(owner.value) ? (owner.value as YAMLMap).flow : true;
  const end = flowValue ? value[1] : lineAfter(source, value[2]);
  return { from: colon + 1, to: end, insert: ` ${empty}${flowValue ? "" : "\n"}` };
}

/** Adds a pair to a map: after the last pair in flow, as a new line at the map's indent in block. */
export function addPair(source: string, map: YAMLMap, key: string, value: Value, flow: boolean): Splice {
  const items = map.items as Pair<Node, Node>[];
  const last = items[items.length - 1];
  if (flow || map.flow) {
    const written = `${scalarText(key, true, 0)}: ${flowText(value, flowPadding(source, map))}`;
    if (!last) {
      const [start] = range(map);
      return { from: start + 1, to: start + 1, insert: written };
    }
    const end = range(last.value ?? last.key)[1];
    return { from: end, to: end, insert: `, ${written}` };
  }
  const indent = items.length > 0 ? column(source, range(items[0]?.key)[0]) : 0;
  const at = lineAfter(source, range(map)[2]);
  const lead = at === source.length && !source.endsWith("\n") ? "\n" : "";
  return { from: at, to: at, insert: `${lead}${blockLines({ [key]: value }, indent).join("\n")}\n` };
}

/** The pair of a map whose key reads as the given text. */
export function pairOf(map: YAMLMap, key: string): { pair: Pair<Node, Node>; index: number } | undefined {
  const index = map.items.findIndex((p) => isScalar(p.key) && String(p.key.value) === key);
  const pair = map.items[index] as Pair<Node, Node> | undefined;
  return pair ? { pair, index } : undefined;
}
