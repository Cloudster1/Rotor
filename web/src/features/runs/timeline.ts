import type { RunEvent } from "@/api/runs";

/** Where a node is in a run, as its events say. */
export type NodeState = "pending" | "running" | "done" | "failed" | "waiting";

/** One line of a run's timeline: an event, or several in a row that read as one, such as a model's chunks. */
export type Entry =
  | { kind: "started"; key: string; node: string; at: string; type: string }
  | { kind: "finished"; key: string; node: string; at: string; ok: boolean; error: string; summary: string }
  | { kind: "tool"; key: string; node: string; at: string; binding: string; tool: string; args: string; result: string; ok: boolean }
  | { kind: "log"; key: string; node: string; at: string; text: string }
  | { kind: "llm"; key: string; node: string; at: string; text: string }
  | { kind: "usage"; key: string; node: string; at: string; input: number; output: number }
  | { kind: "interrupt"; key: string; node: string; at: string; message: string }
  | { kind: "requested"; key: string; node: string; at: string; approval: string; group: string }
  | { kind: "decided"; key: string; node: string; at: string; approval: string; decision: string; by: string; comment: string }
  | { kind: "run"; key: string; node: string; at: string; status: string; reason: string; message: string }
  | { kind: "lost"; key: string; node: string; at: string; replaced: boolean }
  | { kind: "links"; key: string; node: string; at: string; where: string; count: number; hosts: string[] };

const text = (v: unknown): string => (typeof v === "string" ? v : v === undefined || v === null ? "" : JSON.stringify(v, null, 2));

/** Each node's state from the events so far; a node with no event yet is pending. */
export function nodeStates(events: RunEvent[]): Record<string, NodeState> {
  const out: Record<string, NodeState> = {};
  for (const e of events) {
    if (!e.node) continue;
    if (e.type === "node.started") out[e.node] = "running";
    if (e.type === "node.finished") out[e.node] = e.payload.ok === false ? "failed" : "done";
    if (e.type === "interrupt") out[e.node] = "waiting";
  }
  return out;
}

/** The timeline: one entry per event, with a model's chunks and a node's log lines in a row joined into one. */
export function entries(events: RunEvent[]): Entry[] {
  const out: Entry[] = [];
  for (const e of events) {
    const key = String(e.seq);
    const base = { key, node: e.node, at: e.at };
    const last = out[out.length - 1];
    switch (e.type) {
      case "node.started":
        out.push({ ...base, kind: "started", type: text(e.payload.type) });
        break;
      case "node.finished":
        out.push({ ...base, kind: "finished", ok: e.payload.ok !== false, error: text(e.payload.error), summary: text(e.payload.outputSummary) });
        break;
      case "tool.called":
        out.push({
          ...base,
          kind: "tool",
          binding: text(e.payload.binding),
          tool: text(e.payload.tool),
          args: text(e.payload.arguments),
          result: text(e.payload.resultSummary),
          ok: e.payload.ok !== false,
        });
        break;
      case "log":
      case "llm.output": {
        const kind = e.type === "log" ? "log" : "llm";
        const piece = text(e.payload.text);
        if (last && last.kind === kind && last.node === e.node) {
          last.text += piece;
        } else {
          out.push({ ...base, kind, text: piece });
        }
        break;
      }
      case "llm.usage":
        out.push({ ...base, kind: "usage", input: Number(e.payload.inputTokens ?? 0), output: Number(e.payload.outputTokens ?? 0) });
        break;
      case "interrupt":
        out.push({ ...base, kind: "interrupt", message: text(e.payload.message) });
        break;
      case "approval.requested": {
        const approvers = e.payload.approvers as { group?: unknown } | null | undefined;
        out.push({ ...base, kind: "requested", approval: text(e.payload.approvalId), group: text(approvers?.group) });
        break;
      }
      case "approval.decided": {
        // The contract names the decider; a person reference carries the name the timeline shows.
        const by = e.payload.by as { name?: unknown } | string | null | undefined;
        out.push({
          ...base,
          kind: "decided",
          approval: text(e.payload.approvalId),
          decision: text(e.payload.decision),
          by: typeof by === "object" && by !== null ? text(by.name) : text(by),
          comment: text(e.payload.comment),
        });
        break;
      }
      case "run.finished":
        out.push({ ...base, kind: "run", status: text(e.payload.status), reason: text(e.payload.reason), message: text(e.payload.message) });
        break;
      case "runner.lost":
        out.push({ ...base, kind: "lost", replaced: e.payload.replaced === true });
        break;
      case "link.removed": {
        const hosts = Array.isArray(e.payload.hosts) ? e.payload.hosts.map(text) : [];
        out.push({ ...base, kind: "links", where: text(e.payload.where), count: Number(e.payload.count ?? hosts.length), hosts });
        break;
      }
    }
  }
  return out;
}

const urls = /https?:\/\/[^\s"'<>\\)\]}]+/g;

/** The host an address reaches, lowercased, or empty when it does not read as one. */
function hostOf(address: string): string {
  try {
    return new URL(address).hostname.toLowerCase();
  } catch {
    return "";
  }
}

/** The web addresses a tool's answer names on the run's own tools, such as the item it created, each once; any other
 * host stays text, so content cannot plant a link here (#40). */
export function linksIn(answer: string, hosts: readonly string[]): string[] {
  const allowed = new Set(hosts.map((h) => h.toLowerCase()));
  return [...new Set(answer.match(urls) ?? [])].filter((address) => allowed.has(hostOf(address)));
}
