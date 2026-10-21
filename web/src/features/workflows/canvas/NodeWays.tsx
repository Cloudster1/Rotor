import { type FormEvent, useId, useState } from "react";
import { s } from "@/strings";
import type { SpecNode, SpecShape } from "./specDoc";
import { input, label, secondary } from "./styles";

/** Where a run goes after a node; each change is a call the canvas turns into a YAML edit. */
export interface WayEdits {
  addEdge: (to: string) => void;
  removeEdge: (to: string) => void;
  setCase: (value: string, to: string) => void;
  removeCase: (value: string) => void;
  setDefault: (to: string | undefined) => void;
}

const remove = "text-sm text-danger underline-offset-2 hover:underline disabled:opacity-60";

/** The ways out of a node: its edges, or for a router its cases and where it goes when none matches. */
export function NodeWays({ node, spec, readOnly, edits }: { node: SpecNode; spec: SpecShape; readOnly: boolean; edits: WayEdits }) {
  const ids = spec.nodes.map((n) => n.id).filter((id) => id !== "");
  return node.type === "router" ? (
    <Cases node={node} ids={ids} readOnly={readOnly} edits={edits} />
  ) : (
    <Next node={node} spec={spec} ids={ids} readOnly={readOnly} edits={edits} />
  );
}

function Next({ node, spec, ids, readOnly, edits }: { node: SpecNode; spec: SpecShape; ids: string[]; readOnly: boolean; edits: WayEdits }) {
  const id = useId();
  const next = spec.edges.filter((e) => e.from === node.id).map((e) => e.to);
  const [target, setTarget] = useState("");
  return (
    <fieldset className="flex flex-col gap-2" data-next>
      <legend className="text-sm font-semibold text-ink">{s.canvas.node.next}</legend>
      {next.length === 0 ? (
        <p className="text-sm text-ink-muted">{s.canvas.node.noNext}</p>
      ) : (
        <ul className="flex flex-col gap-1">
          {next.map((to, i) => (
            <li key={`${to}-${i}`} className="flex items-center justify-between gap-2 text-sm text-ink">
              <span>{s.canvas.node.to(to)}</span>
              {!readOnly && (
                <button type="button" className={remove} onClick={() => edits.removeEdge(to)} aria-label={s.canvas.node.removeEdge(to)}>
                  {s.canvas.node.removeShort}
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      {!readOnly && (
        <div className="flex items-end gap-2">
          <label className={`${label} flex-1`} htmlFor={`${id}-next`}>
            {s.canvas.node.addNext}
            <select id={`${id}-next`} className={input} value={target} onChange={(e) => setTarget(e.target.value)} name="next-node">
              <option value="">{s.canvas.node.chooseNode}</option>
              {ids.map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </select>
          </label>
          <button
            type="button"
            className={secondary}
            disabled={target === ""}
            onClick={() => {
              edits.addEdge(target);
              setTarget("");
            }}
          >
            {s.canvas.node.addShort}
          </button>
        </div>
      )}
    </fieldset>
  );
}

function Cases({ node, ids, readOnly, edits }: { node: SpecNode; ids: string[]; readOnly: boolean; edits: WayEdits }) {
  const id = useId();
  const [value, setValue] = useState("");
  const [target, setTarget] = useState("");
  const [error, setError] = useState("");
  const options = (chosen: string) => [...new Set([...ids, ...(chosen ? [chosen] : [])])];

  function add(event: FormEvent) {
    event.preventDefault();
    if (node.cases.some(([v]) => v === value)) return setError(s.canvas.node.caseTaken(value));
    setError("");
    edits.setCase(value, target);
    setValue("");
    setTarget("");
  }

  return (
    <fieldset className="flex flex-col gap-2" data-cases>
      <legend className="text-sm font-semibold text-ink">{s.canvas.node.cases}</legend>
      {node.cases.length === 0 && <p className="text-sm text-ink-muted">{s.canvas.node.noCases}</p>}
      <ul className="flex flex-col gap-2">
        {node.cases.map(([v, to]) => (
          <li key={v} className="flex items-end gap-2">
            <label className={`${label} flex-1`}>
              {`${s.canvas.node.caseValue} ${v}`}
              <select className={input} value={to} disabled={readOnly} onChange={(e) => edits.setCase(v, e.target.value)}>
                {options(to).map((n) => (
                  <option key={n} value={n}>
                    {n}
                  </option>
                ))}
              </select>
            </label>
            {!readOnly && (
              <button type="button" className={`${remove} h-9`} onClick={() => edits.removeCase(v)} aria-label={s.canvas.node.removeCase(v)}>
                {s.canvas.node.removeShort}
              </button>
            )}
          </li>
        ))}
      </ul>
      {!readOnly && (
        <form className="flex flex-wrap items-end gap-2" onSubmit={add}>
          <label className={`${label} min-w-28 flex-1`}>
            {s.canvas.node.caseValue}
            <input className={input} value={value} onChange={(e) => setValue(e.target.value)} name="case-value" />
          </label>
          <label className={`${label} min-w-28 flex-1`}>
            {s.canvas.node.caseTarget}
            <select className={input} value={target} onChange={(e) => setTarget(e.target.value)} name="case-target">
              <option value="">{s.canvas.node.chooseNode}</option>
              {ids.map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </select>
          </label>
          <button type="submit" className={secondary} disabled={value === "" || target === ""}>
            {s.canvas.node.addCase}
          </button>
          {error && (
            <p role="alert" className="w-full text-sm text-danger">
              {error}
            </p>
          )}
        </form>
      )}
      <label className={label} htmlFor={`${id}-default`}>
        {s.canvas.node.otherwise}
        <select
          id={`${id}-default`}
          className={input}
          value={node.default ?? ""}
          disabled={readOnly}
          onChange={(e) => edits.setDefault(e.target.value === "" ? undefined : e.target.value)}
          name="router-default"
        >
          <option value="">{s.canvas.node.nowhere}</option>
          {options(node.default ?? "").map((n) => (
            <option key={n} value={n}>
              {n}
            </option>
          ))}
        </select>
      </label>
    </fieldset>
  );
}
