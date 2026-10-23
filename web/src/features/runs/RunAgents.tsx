import type { Run } from "@/api/runs";
import { s } from "@/strings";

/** Which model each agent of the run ran on, and what its calls used at that model's prices (#133). */
export function RunAgents({ agents }: { agents: NonNullable<Run["usage"]["agents"]> }) {
  if (agents.length === 0) return null;
  return (
    <section className="flex flex-col gap-1" aria-label={s.usage.agents} data-run-agents>
      <h2 className="text-sm font-medium text-ink">{s.usage.agents}</h2>
      <ul className="flex flex-col gap-1 text-sm text-ink-muted">
        {agents.map((a) => (
          <li key={a.node} data-run-agent={a.node} data-model={a.model}>
            {s.usage.agent(a.node, a.model, a.provider, a.inputTokens, a.outputTokens, a.cost)}
          </li>
        ))}
      </ul>
    </section>
  );
}
