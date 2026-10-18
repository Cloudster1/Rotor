/** What a new workflow starts as: valid, binding no tool, so it saves before anything is connected. */
export const NEW_WORKFLOW = `apiVersion: rotor.dev/v1
kind: Workflow
metadata:
  name: my-workflow
  description: What this workflow is for.
inputs:
  type: object
  properties:
    note: { type: string }
tools: {}
limits:
  maxSteps: 10
  timeout: 5m
entry: ask
nodes:
  - id: ask
    type: approval
    message: Go on?
`;

/** The YAML of one tool binding, ready to go under tools:. */
export function bindingYAML(name: string, tool: string, scope: string, writes: boolean, calls: string[]): string {
  const lines = [`  ${name}:`, `    tool: ${tool}`, `    scopes: ["${scope}"]`];
  if (writes) lines.push("    access: write");
  lines.push(`    allowTools: [${calls.join(", ")}]`);
  return `${lines.join("\n")}\n`;
}

/** A file name for an exported workflow, from its metadata name. */
export function fileName(yaml: string): string {
  const match = /^\s{2}name:\s*([a-z0-9][a-z0-9-]*)\s*$/m.exec(yaml);
  return `${match?.[1] ?? "workflow"}.yaml`;
}
