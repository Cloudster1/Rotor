import { describe, expect, it } from "vitest";
import { parse } from "yaml";
import { addSkillAt, nodeAt, removeSkill } from "./skills";

const SPEC = `apiVersion: rotor.dev/v1
kind: Workflow
metadata:
  name: refine
entry: analyse
nodes:
  - id: analyse
    type: agent
    prompt: |
      Turn the story into requirements.
    # The reviewer reads what the analyst wrote.
  - id: review
    type: approval
    message: Good?
`;

const RE = { name: "requirements-engineering", version: 2 };
const TE = { name: "test-engineering", version: 1 };
const at = (source: string, text: string) => source.indexOf(text);
const skillsOf = (source: string, index: number) => (parse(source).nodes[index] as { skills?: unknown }).skills;

describe("an agent's skills in the YAML", () => {
  it("adds skills: to the agent at the cursor when it has none, at the node's indent", () => {
    const result = addSkillAt(SPEC, at(SPEC, "Turn the story"), RE);
    expect(result).toMatchObject({ ok: true, node: "analyse", repinned: false });
    if (!result.ok) return;
    expect(result.text).toContain("      Turn the story into requirements.\n    skills:\n      - { name: requirements-engineering, version: 2 }\n");
    expect(skillsOf(result.text, 0)).toEqual([RE]);
    // Only the agent changed: the comment and the next node are as they were.
    expect(result.text).toContain("    # The reviewer reads what the analyst wrote.\n  - id: review\n");
  });

  it("appends to the skills the agent has, in their style", () => {
    const first = addSkillAt(SPEC, at(SPEC, "id: analyse"), RE);
    if (!first.ok) throw new Error("the first skill was refused");
    const second = addSkillAt(first.text, at(first.text, "type: agent"), TE);
    expect(second.ok).toBe(true);
    if (!second.ok) return;
    expect(skillsOf(second.text, 0)).toEqual([RE, TE]);
    expect(second.text).toContain("      - { name: requirements-engineering, version: 2 }\n      - { name: test-engineering, version: 1 }\n");

    const flow = SPEC.replace("    type: agent\n", "    type: agent\n    skills: [{name: requirements-engineering, version: 2}]\n");
    const added = addSkillAt(flow, at(flow, "type: agent"), TE);
    if (!added.ok) throw new Error("the flow list was refused");
    expect(added.text).toContain("skills: [{name: requirements-engineering, version: 2}, {name: test-engineering, version: 1}]");
  });

  it("pins a skill the agent names already to the version picked, rather than naming it twice", () => {
    const first = addSkillAt(SPEC, at(SPEC, "id: analyse"), RE);
    if (!first.ok) throw new Error("the first skill was refused");
    const again = addSkillAt(first.text, at(first.text, "id: analyse"), { ...RE, version: 3 });
    expect(again).toMatchObject({ ok: true, repinned: true });
    if (again.ok) expect(skillsOf(again.text, 0)).toEqual([{ ...RE, version: 3 }]);
  });

  it("gives a node written as a flow map a flow list", () => {
    const flow = "entry: a\nnodes:\n  - { id: a, type: agent, prompt: Go }\n";
    const result = addSkillAt(flow, at(flow, "prompt"), RE);
    if (!result.ok) throw new Error("the flow node was refused");
    expect(result.text).toBe("entry: a\nnodes:\n  - { id: a, type: agent, prompt: Go, skills: [{ name: requirements-engineering, version: 2 }] }\n");
  });

  it("refuses outside an agent node, and says which node the cursor is in", () => {
    expect(addSkillAt(SPEC, at(SPEC, "kind: Workflow"), RE)).toEqual({ ok: false, why: "outside" });
    expect(addSkillAt(SPEC, at(SPEC, "Good?"), RE)).toEqual({ ok: false, why: "notAgent", node: "review", type: "approval" });
    expect(addSkillAt("nodes: [", 0, RE)).toEqual({ ok: false, why: "unparsed" });
  });

  it("counts the dash of a node's first line as the node", () => {
    expect(nodeAt(SPEC, at(SPEC, "  - id: review"))?.get("id")).toBe("review");
    expect(nodeAt(SPEC, at(SPEC, "  - id: analyse"))?.get("id")).toBe("analyse");
  });

  it("takes a skill off, and the last one takes skills: with it", () => {
    const one = addSkillAt(SPEC, at(SPEC, "id: analyse"), RE);
    if (!one.ok) throw new Error("the first skill was refused");
    const two = addSkillAt(one.text, at(one.text, "id: analyse"), TE);
    if (!two.ok) throw new Error("the second skill was refused");
    const back = removeSkill(two.text, "analyse", 1);
    expect(back).toBe(one.text);
    expect(removeSkill(back, "analyse", 0)).toBe(SPEC);
  });
});
