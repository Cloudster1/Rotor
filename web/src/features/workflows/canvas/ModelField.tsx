import { useQuery } from "@tanstack/react-query";
import { modelsQuery } from "@/api/providers";
import { s } from "@/strings";
import type { ModelRef } from "./specDoc";
import { input, label } from "./styles";

const WORKFLOWS = "";
const keyOf = (m: ModelRef) => JSON.stringify([m.provider, m.name]);

/** An agent's own model among those administrators allow, or the workflow's (#133); one no longer offered stays shown. */
export function ModelField({ model, readOnly, onModel }: { model?: ModelRef; readOnly: boolean; onModel: (model?: ModelRef) => void }) {
  const models = useQuery(modelsQuery);
  const offered: ModelRef[] = (models.data ?? []).map((m) => ({ name: m.name, provider: m.provider.name }));
  const current = model ? offered.find((m) => m.name === model.name && (!model.provider || m.provider === model.provider)) : undefined;
  const value = model ? keyOf(model) : WORKFLOWS;
  return (
    <label className={label}>
      {s.canvas.node.model}
      <select
        className={input}
        value={value}
        disabled={readOnly}
        name="node-model"
        onChange={(e) => {
          const chosen = e.target.value;
          if (chosen === WORKFLOWS) return onModel(undefined);
          const [provider, name] = JSON.parse(chosen) as [string, string];
          onModel({ name, provider });
        }}
      >
        <option value={WORKFLOWS}>{s.canvas.node.workflowModel}</option>
        {model && !(current && model.provider) && (
          <option value={keyOf(model)}>{current ? s.canvas.node.modelOf(model.name, model.provider) : s.canvas.node.unlisted(model.name)}</option>
        )}
        {offered.map((m) => (
          <option key={keyOf(m)} value={keyOf(m)}>
            {s.canvas.node.modelOf(m.name, m.provider)}
          </option>
        ))}
      </select>
    </label>
  );
}
