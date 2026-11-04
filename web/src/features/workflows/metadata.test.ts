import { describe, expect, it } from "vitest";
import { readMetadata, withMetadata } from "./metadata";

describe("a workflow's metadata", () => {
  it("is read from the YAML", () => {
    expect(readMetadata("apiVersion: rotor.dev/v1\nmetadata:\n  name: triage\n  description: Reviews.\n")).toEqual({ name: "triage", description: "Reviews." });
  });

  it("changes in place, keeping comments and everything else byte for byte", () => {
    const source = "# a workflow\nmetadata:\n  name: triage # shown in the list\n  description: Reviews.\nentry: ask\n";
    expect(withMetadata(source, { name: "triage-v2", description: "Reviews twice." })).toBe(
      "# a workflow\nmetadata:\n  name: triage-v2 # shown in the list\n  description: Reviews twice.\nentry: ask\n",
    );
  });

  it("adds a description it lacked and takes out one emptied, in block and flow style", () => {
    expect(withMetadata("metadata:\n  name: triage\nentry: ask\n", { name: "triage", description: "New: here" })).toBe(
      'metadata:\n  name: triage\n  description: "New: here"\nentry: ask\n',
    );
    expect(withMetadata("metadata: {name: triage, description: Old}\nentry: ask\n", { name: "triage", description: "" })).toBe(
      "metadata: {name: triage}\nentry: ask\n",
    );
  });

  it("refuses YAML it cannot read", () => {
    expect(() => withMetadata("metadata: [", { name: "a", description: "" })).toThrow();
    expect(() => withMetadata("entry: ask\n", { name: "a", description: "" })).toThrow();
  });
});
