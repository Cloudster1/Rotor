import { useQuery } from "@tanstack/react-query";
import { useId, useState } from "react";
import { toolName, toolsQuery } from "@/api/tools";
import { catalogQuery } from "@/api/workflows";
import { s } from "@/strings";
import { bindingYAML } from "./template";

const card = "rounded-overlay border border-border bg-surface p-4 shadow-1";
const input = "h-8 w-full rounded-control border border-border-strong bg-surface px-2 text-sm text-ink";
const secondary = "h-8 rounded-control border border-border-strong px-3 text-sm font-medium text-ink hover:bg-surface-raised disabled:opacity-60";

/** Builds a tool binding from what the person's connection may call there, with the tools' own hints. */
export function ToolPicker({ onInsert }: { onInsert: (yaml: string) => void }) {
  const id = useId();
  const tools = useQuery(toolsQuery);
  const [toolID, setToolID] = useState("");
  const catalog = useQuery({ ...catalogQuery(toolID), enabled: toolID !== "" });
  const tool = tools.data?.find((t) => t.id === toolID);
  const [binding, setBinding] = useState("");
  const [scope, setScope] = useState("");
  const [picked, setPicked] = useState<string[]>([]);
  const writes = (catalog.data?.tools ?? []).some((t) => picked.includes(t.name) && !t.readOnly);
  const chosenScope = scope || tool?.scopesSupported.find((x) => x !== "evidence") || "";
  return (
    <section className={card} aria-labelledby={`${id}-title`} data-tool-picker>
      <h2 id={`${id}-title`} className="mb-1 text-lg font-semibold text-ink">
        {s.workflows.tools.title}
      </h2>
      <p className="mb-3 text-sm text-ink-muted">{s.workflows.tools.body}</p>
      {tools.data?.length === 0 && <p className="text-sm text-ink-muted">{s.workflows.tools.none}</p>}
      {tools.data && tools.data.length > 0 && (
        <div className="flex flex-col gap-3">
          <label className="flex flex-col gap-1 text-sm text-ink">
            {s.workflows.tools.tool}
            <select
              className={input}
              value={toolID}
              onChange={(e) => {
                setToolID(e.target.value);
                setPicked([]);
                setScope("");
                const chosen = tools.data.find((t) => t.id === e.target.value);
                setBinding(chosen ? chosen.name : "");
              }}
              data-picker-tool
            >
              <option value="">{s.workflows.tools.choose}</option>
              {tools.data.map((t) => (
                <option key={t.id} value={t.id}>
                  {toolName(t)} ({t.baseURL})
                </option>
              ))}
            </select>
          </label>
          {tool && (
            <>
              <label className="flex flex-col gap-1 text-sm text-ink">
                {s.workflows.tools.binding}
                <input className={input} value={binding} onChange={(e) => setBinding(e.target.value)} data-picker-binding />
              </label>
              <label className="flex flex-col gap-1 text-sm text-ink">
                {s.workflows.tools.scope}
                <select className={input} value={chosenScope} onChange={(e) => setScope(e.target.value)}>
                  {tool.scopesSupported.map((x) => (
                    <option key={x} value={x}>
                      {x}
                    </option>
                  ))}
                </select>
              </label>
            </>
          )}
          {catalog.isPending && toolID && <p className="text-sm text-ink-muted">{s.workflows.tools.loading}</p>}
          {catalog.isError && (
            <p role="alert" className="text-sm text-danger" data-picker-error>
              {catalog.error.message}
            </p>
          )}
          {catalog.data && (
            <fieldset className="flex max-h-72 flex-col gap-1 overflow-y-auto" data-picker-calls>
              <legend className="mb-1 text-sm font-medium text-ink">{s.workflows.tools.calls}</legend>
              {catalog.data.tools.map((t) => (
                <label key={t.name} className="flex items-start gap-2 text-sm text-ink" title={t.description}>
                  <input
                    type="checkbox"
                    className="mt-1"
                    checked={picked.includes(t.name)}
                    onChange={(e) => setPicked(e.target.checked ? [...picked, t.name] : picked.filter((x) => x !== t.name))}
                  />
                  <span>
                    <span className="font-mono">{t.name}</span>{" "}
                    <span className="text-xs text-ink-muted">
                      {t.readOnly ? s.workflows.tools.readOnly : s.workflows.tools.writes}
                      {t.destructive ? `, ${s.workflows.tools.destructive}` : ""}
                    </span>
                  </span>
                </label>
              ))}
            </fieldset>
          )}
          <div>
            <button
              type="button"
              className={secondary}
              disabled={!tool || picked.length === 0 || binding === ""}
              onClick={() => tool && onInsert(bindingYAML(binding, tool.name, chosenScope, writes, picked))}
              data-action="insert-binding"
            >
              {s.workflows.tools.insert}
            </button>
          </div>
        </div>
      )}
    </section>
  );
}
