import { useId } from "react";
import { CANVAS_MESSAGE_ROWS, CANVAS_TEXT_ROWS } from "@/config";
import { s } from "@/strings";
import { ModelField } from "./ModelField";
import { LIST_FIELDS, MAIN_FIELDS, type ModelRef, type NodeType, type SpecNode } from "./specDoc";
import { area, input, label } from "./styles";

type Field = keyof typeof s.canvas.node.fields;
const LONG: Field[] = ["prompt", "role", "source", "message"];

/** A node's main fields, each written into the YAML as it is typed; the other fields stay in the YAML. */
export function NodeFields({
  node,
  bindings,
  readOnly,
  onField,
  onModel,
}: {
  node: SpecNode;
  bindings: string[];
  readOnly: boolean;
  onField: (key: string, value: string) => void;
  onModel: (model?: ModelRef) => void;
}) {
  const id = useId();
  const keys = (MAIN_FIELDS[node.type as NodeType] ?? []) as Field[];
  return (
    <>
      {node.type === "agent" && <ModelField model={node.model} readOnly={readOnly} onModel={onModel} />}
      {keys.map((key) => {
        const value = node.fields[key] ?? "";
        const name = `node-${key}`;
        if (key === "tool") {
          return (
            <label key={key} className={label}>
              {s.canvas.node.fields.tool}
              <select className={input} value={value} disabled={readOnly} onChange={(e) => onField(key, e.target.value)} name={name}>
                <option value="">{s.canvas.node.noBinding}</option>
                {[...new Set([...bindings, ...(value ? [value] : [])])].map((b) => (
                  <option key={b} value={b}>
                    {b}
                  </option>
                ))}
              </select>
              {bindings.length === 0 && <span className="text-xs font-normal text-ink-muted">{s.canvas.node.noBindings}</span>}
            </label>
          );
        }
        if (LONG.includes(key)) {
          return (
            <div key={key} className="flex flex-col gap-1">
              <label className={label}>
                {s.canvas.node.fields[key]}
                <textarea
                  className={`${area} ${key === "source" ? "font-mono" : ""}`}
                  rows={key === "message" ? CANVAS_MESSAGE_ROWS : CANVAS_TEXT_ROWS}
                  value={value}
                  readOnly={readOnly}
                  onChange={(e) => onField(key, e.target.value)}
                  name={name}
                  spellCheck={key !== "source"}
                  aria-describedby={key === "role" ? `${id}-role` : undefined}
                />
              </label>
              {key === "role" && (
                <span id={`${id}-role`} className="text-xs text-ink-muted">
                  {s.canvas.node.roleHint}
                </span>
              )}
            </div>
          );
        }
        if (LIST_FIELDS.includes(key)) {
          return (
            <div key={key} className="flex flex-col gap-1">
              <label className={label}>
                {s.canvas.node.fields[key]}
                <input className={`${input} font-mono`} value={value} readOnly name={name} aria-describedby={`${id}-${key}`} />
              </label>
              <span id={`${id}-${key}`} className="text-xs text-ink-muted">
                {s.canvas.node.commandHint}
              </span>
            </div>
          );
        }
        return (
          <label key={key} className={label}>
            {s.canvas.node.fields[key]}
            <input
              className={`${input} font-mono`}
              value={value}
              readOnly={readOnly}
              onChange={(e) => onField(key, e.target.value)}
              name={name}
              aria-describedby={key === "on" ? `${id}-on` : undefined}
            />
            {key === "on" && (
              <span id={`${id}-on`} className="text-xs font-normal text-ink-muted">
                {s.canvas.node.onHint}
              </span>
            )}
          </label>
        );
      })}
    </>
  );
}
