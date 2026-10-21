import { type FormEvent, useId, useState } from "react";
import { s } from "@/strings";
import { NODE_ID, NODE_TYPES, type NodeType } from "./specDoc";
import { input, label, secondary } from "./styles";

/** A kind and an id, checked as the schema checks them, before a node is added at the end of the nodes. */
export function AddNode({ taken, onAdd }: { taken: string[]; onAdd: (type: NodeType, id: string) => void }) {
  const id = useId();
  const [type, setType] = useState<NodeType>("agent");
  const [name, setName] = useState("");
  const [error, setError] = useState("");

  function submit(event: FormEvent) {
    event.preventDefault();
    const wanted = name.trim();
    if (!NODE_ID.test(wanted)) return setError(s.canvas.add.badID);
    if (taken.includes(wanted)) return setError(s.canvas.add.taken(wanted));
    setError("");
    setName("");
    onAdd(type, wanted);
  }

  return (
    <form className="flex flex-wrap items-end gap-3" onSubmit={submit} aria-labelledby={`${id}-title`} data-add-node>
      <h3 id={`${id}-title`} className="w-full text-sm font-semibold text-ink">
        {s.canvas.add.title}
      </h3>
      <label className={label}>
        {s.canvas.add.type}
        <select className={input} value={type} onChange={(e) => setType(e.target.value as NodeType)} name="node-type">
          {NODE_TYPES.map((t) => (
            <option key={t} value={t}>
              {s.canvas.types[t]}
            </option>
          ))}
        </select>
      </label>
      <label className={`${label} min-w-40 flex-1`}>
        {s.canvas.add.id}
        <input
          className={input}
          value={name}
          onChange={(e) => setName(e.target.value)}
          name="node-id"
          aria-invalid={error !== ""}
          aria-describedby={error ? `${id}-error` : undefined}
        />
      </label>
      <button type="submit" className={secondary}>
        {s.canvas.add.submit}
      </button>
      {error && (
        <p id={`${id}-error`} role="alert" className="w-full text-sm text-danger">
          {error}
        </p>
      )}
    </form>
  );
}
