import { type Document, isMap, isScalar, isSeq, type Node, type Pair, parseDocument, type Scalar, visit, type YAMLMap, type YAMLSeq } from "yaml";
import { type ModelRef, type NodeType, nodeMap } from "./specDoc";
import {
  addPair,
  appendItem,
  applySplices,
  flowPadding,
  flowText,
  inFlow,
  pairOf,
  removeItem,
  removePair,
  renameIn,
  replaceScalar,
  type Splice,
  type Value,
} from "./yamlText";

// Each edit reads the text, splices it, and reads the result again before the next step, so a step always sees
// the offsets of the text it changes. An edit that cannot hold throws; the canvas then sends the person to the YAML.

/** What a new node of each kind starts with: its required fields, empty, so the checks say what each still needs. */
const TEMPLATES: Record<NodeType, { [key: string]: Value }> = {
  agent: { prompt: "" },
  tool: { tool: "", call: "" },
  code: { source: "", output: { schema: { type: "object" } } },
  approval: { message: "" },
  router: { on: "", cases: {} },
};

function read(source: string): Document.Parsed {
  const doc = parseDocument(source, { version: "1.2" });
  if (doc.errors.length > 0 || !isMap(doc.contents)) throw new Error("The workflow's YAML does not parse.");
  return doc;
}

function checked(source: string): string {
  read(source);
  return source;
}

const top = (doc: Document.Parsed) => doc.contents as YAMLMap;

function topPair(doc: Document.Parsed, key: string): Pair<Node, Node> | undefined {
  return pairOf(top(doc), key)?.pair;
}

function node(doc: Document.Parsed, id: string) {
  const found = nodeMap(doc, id);
  if (!found) throw new Error(`There is no node ${id}.`);
  return { ...found, flow: inFlow(top(doc), found.seq, found.map) };
}

const textOf = (n: unknown) => (isScalar(n) ? String(n.value ?? "") : "");

/** Appends an item to a top-level sequence, writing the sequence when the spec has none yet. */
function appendTop(source: string, key: string, item: { [key: string]: Value }): string {
  const doc = read(source);
  const flowTop = inFlow(top(doc));
  const pair = topPair(doc, key);
  if (pair && isSeq(pair.value)) return checked(applySplices(source, [appendItem(source, pair.value, item, flowTop)]));
  const written = `[${flowText(item, flowPadding(source))}]`;
  // A key with no list under it, such as `edges:`, gets the list in its place.
  if (pair) {
    const colon = source.indexOf(":", pair.key.range?.[1] ?? 0);
    const end = pair.value?.range?.[1] ?? colon + 1;
    return checked(applySplices(source, [{ from: colon + 1, to: Math.max(end, colon + 1), insert: ` ${written}` }]));
  }
  const pairs = top(doc).items as Pair<Node, Node>[];
  const last = pairs[pairs.length - 1];
  if (flowTop && last) {
    const end = (last.value ?? last.key).range?.[1] ?? 0;
    return checked(applySplices(source, [{ from: end, to: end, insert: `, ${key}: ${written}` }]));
  }
  const nodes = topPair(doc, "nodes")?.value;
  const first = isSeq(nodes) && !nodes.flow ? (nodes.items[0] as Node | undefined)?.range?.[0] : undefined;
  const dash = first === undefined ? 2 : source.lastIndexOf("-", first) - (source.lastIndexOf("\n", first - 1) + 1);
  const at = top(doc).range?.[2] ?? source.length;
  const lead = at === 0 || source.slice(0, at).endsWith("\n") ? "" : "\n";
  const line = `${" ".repeat(dash)}- ${flowText(item, flowPadding(source))}`;
  return checked(applySplices(source, [{ from: at, to: at, insert: `${lead}${key}:\n${line}\n` }]));
}

/** Adds a node of a kind at the end of the nodes, in the style of the nodes before it. */
export function addNode(source: string, type: NodeType, id: string): string {
  return appendTop(source, "nodes", { id, type, ...TEMPLATES[type] });
}

/** Draws an edge from one node to the next. */
export function addEdge(source: string, from: string, to: string): string {
  return appendTop(source, "edges", { from, to });
}

/** Removes the first edge from one node to another. */
export function removeEdge(source: string, from: string, to: string): string {
  const doc = read(source);
  const edges = topPair(doc, "edges");
  if (!edges || !isSeq(edges.value)) return source;
  const index = edges.value.items.findIndex((e) => isMap(e) && textOf(e.get("from", true)) === from && textOf(e.get("to", true)) === to);
  return index < 0 ? source : dropEdge(source, doc, index);
}

/** Removes one edge; the last one takes the optional `edges:` with it, so drawing one and taking it back is the text again. */
function dropEdge(source: string, doc: Document.Parsed, index: number): string {
  const edges = topPair(doc, "edges") as Pair<Node, Node>;
  if ((edges.value as YAMLSeq).items.length > 1) return checked(applySplices(source, [removeItem(source, edges, index)]));
  const at = pairOf(top(doc), "edges") as { index: number };
  return checked(applySplices(source, [removePair(source, undefined, top(doc), at.index)]));
}

/** Makes a node the one the workflow starts at. */
export function setEntry(source: string, id: string): string {
  const doc = read(source);
  const entry = topPair(doc, "entry");
  if (entry && isScalar(entry.value)) return checked(applySplices(source, [replaceScalar(source, entry.value, id, inFlow(top(doc)))]));
  return checked(applySplices(source, [addPair(source, top(doc), "entry", id, inFlow(top(doc)))]));
}

/** Sets one of a node's main fields, adding the field when the node has none. */
export function setField(source: string, id: string, key: string, value: string): string {
  const doc = read(source);
  const { map, flow } = node(doc, id);
  const found = pairOf(map, key);
  if (found && isScalar(found.pair.value) && found.pair.value.range) {
    return checked(applySplices(source, [replaceScalar(source, found.pair.value as Scalar, value, flow)]));
  }
  const without = found ? checked(applySplices(source, [removePair(source, undefined, map, found.index)])) : source;
  const again = read(without);
  const at = node(again, id);
  return checked(applySplices(without, [addPair(without, at.map, key, value, at.flow)]));
}

/** Takes one of a node's fields out, when it has it. */
export function clearField(source: string, id: string, key: string): string {
  const doc = read(source);
  const { map } = node(doc, id);
  const found = pairOf(map, key);
  return found ? checked(applySplices(source, [removePair(source, undefined, map, found.index)])) : source;
}

/** Gives an agent a model of its own, or takes it away so the agent uses the workflow's (#133). */
export function setModel(source: string, id: string, model: ModelRef | undefined): string {
  const without = clearField(source, id, "model");
  if (!model) return without;
  const at = node(read(without), id);
  const value: { [key: string]: Value } = model.provider ? { name: model.name, provider: model.provider } : { name: model.name };
  return checked(applySplices(without, [addPair(without, at.map, "model", value, at.flow)]));
}

/** Sends a router's matching value to a node, adding the case when it is new. */
export function setCase(source: string, router: string, value: string, target: string): string {
  const doc = read(source);
  const { map, flow } = node(doc, router);
  const cases = pairOf(map, "cases");
  if (!cases || !isMap(cases.pair.value)) {
    const without = cases ? checked(applySplices(source, [removePair(source, undefined, map, cases.index)])) : source;
    const at = node(read(without), router);
    return checked(applySplices(without, [addPair(without, at.map, "cases", { [value]: target }, at.flow)]));
  }
  const existing = pairOf(cases.pair.value, value);
  const caseFlow = flow || inFlow(cases.pair.value);
  if (existing && isScalar(existing.pair.value)) {
    return checked(applySplices(source, [replaceScalar(source, existing.pair.value as Scalar, target, caseFlow)]));
  }
  return checked(applySplices(source, [addPair(source, cases.pair.value, value, target, caseFlow)]));
}

/** Removes a router's case for one value. */
export function removeCase(source: string, router: string, value: string): string {
  const doc = read(source);
  const cases = pairOf(node(doc, router).map, "cases");
  if (!cases || !isMap(cases.pair.value)) return source;
  const found = pairOf(cases.pair.value, value);
  return found ? checked(applySplices(source, [removePair(source, cases.pair, cases.pair.value, found.index)])) : source;
}

/** Sets where a router goes when no case matches, or removes that way when target is undefined. */
export function setDefault(source: string, router: string, target: string | undefined): string {
  if (target === undefined) return clearField(source, router, "default");
  return setField(source, router, "default", target);
}

/** Removes a node, every edge to or from it, and the router cases and defaults that lead to it. */
export function removeNode(source: string, id: string): string {
  let out = source;
  for (;;) {
    const doc = read(out);
    const edges = topPair(doc, "edges");
    const items = edges && isSeq(edges.value) ? edges.value.items : [];
    const index = items.findIndex((e) => isMap(e) && (textOf(e.get("from", true)) === id || textOf(e.get("to", true)) === id));
    if (!edges || index < 0) break;
    out = dropEdge(out, doc, index);
  }
  const routers = (read(out).get("nodes") as YAMLSeq | undefined)?.items ?? [];
  for (const r of routers.filter((r): r is YAMLMap => isMap(r) && textOf(r.get("type", true)) === "router")) {
    const routerID = textOf(r.get("id", true));
    if (routerID === id) continue;
    const cases = r.get("cases", true);
    const values = isMap(cases) ? cases.items.filter((p) => textOf(p.value) === id).map((p) => textOf(p.key)) : [];
    for (const value of values) out = removeCase(out, routerID, value);
    if (textOf(r.get("default", true)) === id) out = setDefault(out, routerID, undefined);
  }
  const doc = read(out);
  const at = node(doc, id);
  const nodes = topPair(doc, "nodes") as Pair<Node, Node>;
  return checked(applySplices(out, [removeItem(out, nodes, at.index)]));
}

/** A template reference to a node, {{ nodes.<id>... }}, or a router's statePath starting at it. */
const refersTo = (id: string) => new RegExp(`(\\{\\{[^}]*?\\bnodes\\.)${id.replace(/[-]/g, "\\-")}(?![a-z0-9_-])`, "g");
const pathTo = (id: string) => new RegExp(`^(["']?nodes\\.)${id.replace(/[-]/g, "\\-")}(?![a-z0-9_-])`);

/** Renames a node and everything that names it: edges, the entry, router cases, defaults and template references. */
export function renameNode(source: string, from: string, to: string): string {
  const doc = read(source);
  const splices: Splice[] = [];
  const rename = (n: unknown) => {
    if (isScalar(n) && textOf(n) === from && n.range) splices.push(renameIn(source, n, from, to));
  };
  const seen = new Set<Node>();
  const nodes = topPair(doc, "nodes")?.value;
  for (const item of isSeq(nodes) ? nodes.items : []) {
    if (!isMap(item)) continue;
    for (const key of ["id", "default"]) rename(item.get(key, true));
    const cases = item.get("cases", true);
    if (isMap(cases)) for (const p of cases.items) rename(p.value);
    const on = item.get("on", true);
    if (textOf(item.get("type", true)) === "router" && isScalar(on) && on.range) {
      const slice = source.slice(on.range[0], on.range[1]);
      const renamed = slice.replace(pathTo(from), `$1${to}`);
      if (renamed !== slice) splices.push({ from: on.range[0], to: on.range[1], insert: renamed });
      seen.add(on);
    }
  }
  const edges = topPair(doc, "edges")?.value;
  for (const e of isSeq(edges) ? edges.items : []) {
    if (isMap(e)) for (const key of ["from", "to"]) rename(e.get(key, true));
  }
  rename(topPair(doc, "entry")?.value);
  // Text that names the node in a template keeps its own quoting: only the id inside the braces changes.
  visit(doc, {
    Scalar(_, n) {
      if (seen.has(n) || typeof n.value !== "string" || !n.range || !n.value.includes("{{")) return;
      const slice = source.slice(n.range[0], n.range[1]);
      const renamed = slice.replace(refersTo(from), `$1${to}`);
      if (renamed !== slice) splices.push({ from: n.range[0], to: n.range[1], insert: renamed });
    },
  });
  return checked(applySplices(source, splices));
}
