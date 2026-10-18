import { useId } from "react";
import { MAX_INPUT_TEXT } from "@/config";
import { s } from "@/strings";

const input = "h-9 w-full rounded-control border border-border-strong bg-surface px-3 text-sm text-ink";
const area = "min-h-24 w-full rounded-control border border-border-strong bg-surface px-3 py-2 font-mono text-sm text-ink";

/** One field of a workflow's inputs, as the form asks for it. */
export interface Field {
  name: string;
  label: string;
  description?: string;
  kind: "text" | "number" | "integer" | "boolean" | "choice" | "json";
  choices?: string[];
  required: boolean;
}

/** What the person typed, field by field: text for every kind but a boolean. */
export type Values = Record<string, string>;

interface Schema {
  type?: string;
  title?: string;
  description?: string;
  enum?: unknown[];
  properties?: Record<string, Schema>;
  required?: string[];
}

/** The fields of a flat object schema; null when the schema takes something the form cannot lay out, asked as JSON. */
export function fieldsOf(schema: unknown): Field[] | null {
  if (schema === null || schema === undefined) return [];
  const root = schema as Schema;
  if (root.type !== "object" || !root.properties) return null;
  const required = new Set(root.required ?? []);
  return Object.entries(root.properties).map(([name, prop]) => {
    const base = { name, label: prop.title ?? name, description: prop.description, required: required.has(name) };
    if (Array.isArray(prop.enum) && prop.enum.every((v) => typeof v === "string")) return { ...base, kind: "choice", choices: prop.enum as string[] };
    switch (prop.type) {
      case "string":
        return { ...base, kind: "text" };
      case "number":
        return { ...base, kind: "number" };
      case "integer":
        return { ...base, kind: "integer" };
      case "boolean":
        return { ...base, kind: "boolean" };
      default:
        return { ...base, kind: "json" };
    }
  });
}

/** The inputs a run starts with, from what was typed; errors name each field that is not what its kind needs. */
export function inputsOf(fields: Field[] | null, values: Values): { inputs: Record<string, unknown>; errors: Record<string, string> } {
  const errors: Record<string, string> = {};
  if (fields === null) {
    try {
      const whole = JSON.parse(values[WHOLE] || "{}") as unknown;
      if (typeof whole !== "object" || whole === null || Array.isArray(whole)) throw new Error("not an object");
      return { inputs: whole as Record<string, unknown>, errors };
    } catch {
      return { inputs: {}, errors: { [WHOLE]: s.runs.dialog.notJSON(s.runs.dialog.json) } };
    }
  }
  const inputs: Record<string, unknown> = {};
  for (const f of fields) {
    const raw = values[f.name] ?? "";
    if (raw === "") continue;
    if (raw.length > MAX_INPUT_TEXT) {
      errors[f.name] = s.runs.dialog.tooLong(f.label, MAX_INPUT_TEXT);
      continue;
    }
    switch (f.kind) {
      case "number":
      case "integer":
        inputs[f.name] = Number(raw);
        break;
      case "boolean":
        inputs[f.name] = raw === "true";
        break;
      case "json":
        try {
          inputs[f.name] = JSON.parse(raw) as unknown;
        } catch {
          errors[f.name] = s.runs.dialog.notJSON(f.label);
        }
        break;
      default:
        inputs[f.name] = raw;
    }
  }
  return { inputs, errors };
}

/** The key under which a schema the form cannot lay out is typed as one JSON document. */
export const WHOLE = "";

/** The fields of a workflow's inputs, with what is wrong with each, from the form or from the server. */
export function InputsForm({
  fields,
  values,
  errors,
  onChange,
}: {
  fields: Field[] | null;
  values: Values;
  errors: Record<string, string>;
  onChange: (name: string, value: string) => void;
}) {
  const id = useId();
  if (fields !== null && fields.length === 0) return <p className="text-sm text-ink-muted">{s.runs.dialog.noInputs}</p>;
  const all: Field[] = fields ?? [{ name: WHOLE, label: s.runs.dialog.json, kind: "json", required: true }];
  return (
    <div className="flex flex-col gap-4">
      {all.map((f) => {
        const fieldID = `${id}-${f.name}`;
        const hintID = f.description ? `${fieldID}-hint` : undefined;
        const errorID = errors[f.name] ? `${fieldID}-error` : undefined;
        const describedBy = [hintID, errorID].filter(Boolean).join(" ") || undefined;
        const common = { id: fieldID, "aria-describedby": describedBy, "aria-invalid": errorID ? true : undefined, "data-input": f.name };
        return (
          <div key={f.name} className="flex flex-col gap-1">
            <label htmlFor={fieldID} className="text-sm font-medium text-ink">
              {f.label}
              {f.required && <span className="ml-2 text-xs font-normal text-ink-muted">{s.runs.dialog.required}</span>}
            </label>
            {f.kind === "choice" || f.kind === "boolean" ? (
              <select {...common} className={input} value={values[f.name] ?? ""} onChange={(e) => onChange(f.name, e.target.value)}>
                <option value="">{s.runs.dialog.unset}</option>
                {(f.kind === "boolean" ? ["true", "false"] : (f.choices ?? [])).map((c) => (
                  <option key={c} value={c}>
                    {f.kind === "boolean" ? (c === "true" ? s.runs.dialog.yes : s.runs.dialog.no) : c}
                  </option>
                ))}
              </select>
            ) : f.kind === "json" ? (
              <textarea {...common} className={area} value={values[f.name] ?? ""} onChange={(e) => onChange(f.name, e.target.value)} spellCheck={false} />
            ) : (
              <input
                {...common}
                className={input}
                type={f.kind === "text" ? "text" : "number"}
                step={f.kind === "integer" ? 1 : "any"}
                value={values[f.name] ?? ""}
                onChange={(e) => onChange(f.name, e.target.value)}
              />
            )}
            {hintID && (
              <p id={hintID} className="text-xs text-ink-muted">
                {f.description}
              </p>
            )}
            {errorID && (
              <p id={errorID} className="text-sm text-danger">
                {errors[f.name]}
              </p>
            )}
          </div>
        );
      })}
    </div>
  );
}
