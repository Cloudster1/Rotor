import { isMap, isScalar, parseDocument, type Scalar, type YAMLMap } from "yaml";
import { addPair, applySplices, inFlow, pairOf, removePair, replaceScalar } from "./canvas/yamlText";

/** What a workflow's metadata says of it, which its list and its settings show. */
export interface Metadata {
  name: string;
  description: string;
}

function metadataOf(source: string) {
  const doc = parseDocument(source, { version: "1.2" });
  if (doc.errors.length > 0 || !isMap(doc.contents)) throw new Error("The workflow's YAML does not parse.");
  const top = doc.contents as YAMLMap;
  const found = pairOf(top, "metadata");
  if (!found || !isMap(found.pair.value)) throw new Error("The workflow has no metadata.");
  return { top, owner: found.pair, map: found.pair.value as YAMLMap };
}

/** Reads the name and description from a workflow's YAML. */
export function readMetadata(source: string): Metadata {
  const { map } = metadataOf(source);
  const text = (key: string) => {
    const value = pairOf(map, key)?.pair.value;
    return isScalar(value) ? String(value.value ?? "") : "";
  };
  return { name: text("name"), description: text("description") };
}

/** Sets one field of the metadata in the text, as the canvas edits a node: what it does not touch stays byte for byte. */
function setField(source: string, key: keyof Metadata, value: string): string {
  const { top, owner, map } = metadataOf(source);
  const flow = inFlow(top, map);
  const found = pairOf(map, key);
  if (value === "") return found ? applySplices(source, [removePair(source, owner, map, found.index)]) : source;
  if (found && isScalar(found.pair.value) && found.pair.value.range) {
    return applySplices(source, [replaceScalar(source, found.pair.value as Scalar, value, flow)]);
  }
  const without = found ? applySplices(source, [removePair(source, owner, map, found.index)]) : source;
  const again = metadataOf(without);
  return applySplices(without, [addPair(without, again.map, key, value, inFlow(again.top, again.map))]);
}

/** The YAML with its name and description changed; an empty description takes the field out. */
export function withMetadata(source: string, next: Metadata): string {
  const named = setField(source, "name", next.name);
  const described = setField(named, "description", next.description);
  readMetadata(described);
  return described;
}
