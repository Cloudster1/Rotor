import { useQuery } from "@tanstack/react-query";
import { useId } from "react";
import { toolName, toolsQuery } from "@/api/tools";
import type { TriggerRequest } from "@/api/triggers";
import { HOOK_PRODUCTS, HOOK_TOPICS } from "@/config";
import { s } from "@/strings";

const input = "h-9 w-full rounded-control border border-border-strong bg-surface px-3 text-sm text-ink";
const secondary = "h-8 rounded-control border border-border-strong px-3 text-sm font-medium text-ink hover:bg-surface-raised";

type EventSpec = NonNullable<TriggerRequest["event"]>;
type Op = EventSpec["conditions"][number]["op"];

/** The comparisons in the order the form offers them; the ones without a value and the ones taking a list. */
const OPS: readonly Op[] = ["equals", "notEquals", "in", "notIn", "exists", "absent"];
const UNARY: readonly Op[] = ["exists", "absent"];
const LISTS: readonly Op[] = ["in", "notIn"];

/** An event trigger's filters as the form holds them: text the person types, read into the request on saving. */
export interface EventDraft {
  toolId: string;
  topics: string[];
  other: string;
  projects: string;
  conditions: { path: string; op: Op; value: string }[];
}

function list(text: string): string[] {
  return text
    .split(",")
    .map((t) => t.trim())
    .filter(Boolean);
}

/** A typed value as JSON when it reads as a number, true, false or null, as text otherwise. */
function scalar(text: string): unknown {
  const trimmed = text.trim();
  if (/^(-?\d+(\.\d+)?|true|false|null)$/.test(trimmed)) return JSON.parse(trimmed);
  return trimmed;
}

function shown(value: unknown): string {
  if (Array.isArray(value)) return value.map(shown).join(", ");
  return value === undefined ? "" : typeof value === "string" ? value : JSON.stringify(value);
}

/** The draft of an event trigger made before, or an empty one. */
export function draftOf(event?: EventSpec | null): EventDraft {
  if (!event) return { toolId: "", topics: [], other: "", projects: "", conditions: [] };
  const known = new Set(Object.values(HOOK_TOPICS).flat());
  return {
    toolId: event.toolId,
    topics: event.topics.filter((t) => known.has(t)),
    other: event.topics.filter((t) => !known.has(t)).join(", "),
    projects: event.projects.join(", "),
    conditions: event.conditions.map((c) => ({ path: c.path, op: c.op, value: shown(c.value) })),
  };
}

/** The request's event part from a draft. */
export function eventOf(d: EventDraft): EventSpec {
  return {
    toolId: d.toolId,
    topics: [...d.topics, ...list(d.other)],
    projects: list(d.projects),
    conditions: d.conditions.map((c) => ({
      path: c.path.trim(),
      op: c.op,
      ...(UNARY.includes(c.op) ? {} : { value: LISTS.includes(c.op) ? list(c.value).map(scalar) : scalar(c.value) }),
    })),
  };
}

/** Which tool's events start the trigger, and the topics, projects and conditions they must match (#35). */
export function EventFields({ draft, onChange, errors }: { draft: EventDraft; onChange: (d: EventDraft) => void; errors: Record<string, string> }) {
  const id = useId();
  const tools = useQuery(toolsQuery);
  const offered = (tools.data ?? []).filter((t) => (HOOK_PRODUCTS as readonly string[]).includes(t.name));
  const product = offered.find((t) => t.id === draft.toolId)?.name ?? "";
  const set = (patch: Partial<EventDraft>) => onChange({ ...draft, ...patch });
  const setCondition = (i: number, patch: Partial<EventDraft["conditions"][number]>) =>
    set({ conditions: draft.conditions.map((c, j) => (j === i ? { ...c, ...patch } : c)) });
  const error = (key: string) =>
    errors[key] ? (
      <p id={`${id}-${key.replaceAll("/", "-")}-error`} className="text-sm text-danger">
        {errors[key]}
      </p>
    ) : null;
  const describedBy = (key: string, hint?: string) =>
    [hint, errors[key] ? `${id}-${key.replaceAll("/", "-")}-error` : undefined].filter(Boolean).join(" ") || undefined;

  return (
    <div className="flex flex-col gap-3" data-event-fields>
      <div className="flex flex-col gap-1">
        <label htmlFor={`${id}-tool`} className="text-sm font-medium text-ink">
          {s.triggers.event.tool}
        </label>
        <select
          id={`${id}-tool`}
          className={input}
          value={draft.toolId}
          onChange={(e) => set({ toolId: e.target.value, topics: [] })}
          aria-invalid={errors["event/toolId"] ? true : undefined}
          aria-describedby={describedBy("event/toolId", `${id}-tool-hint`)}
          data-event-tool
        >
          <option value="">{offered.length === 0 ? s.triggers.event.noTools : ""}</option>
          {offered.map((t) => (
            <option key={t.id} value={t.id}>
              {toolName(t)}
            </option>
          ))}
        </select>
        <p id={`${id}-tool-hint`} className="text-xs text-ink-muted">
          {s.triggers.event.toolHint}
        </p>
        {error("event/toolId")}
      </div>
      {product && (
        <fieldset className="flex flex-col gap-1" aria-describedby={describedBy("event/topics")}>
          <legend className="mb-1 text-sm font-medium text-ink">{s.triggers.event.topics}</legend>
          {(HOOK_TOPICS[product] ?? []).map((topic) => (
            <label key={topic} className="flex items-center gap-2 font-mono text-sm text-ink">
              <input
                type="checkbox"
                checked={draft.topics.includes(topic)}
                onChange={(e) => set({ topics: e.target.checked ? [...draft.topics, topic] : draft.topics.filter((t) => t !== topic) })}
                data-event-topic={topic}
              />
              {topic}
            </label>
          ))}
          {error("event/topics")}
        </fieldset>
      )}
      <div className="flex flex-col gap-1">
        <label htmlFor={`${id}-other`} className="text-sm font-medium text-ink">
          {s.triggers.event.other}
        </label>
        <input
          id={`${id}-other`}
          className={`${input} font-mono`}
          value={draft.other}
          onChange={(e) => set({ other: e.target.value })}
          aria-describedby={`${id}-other-hint`}
        />
        <p id={`${id}-other-hint`} className="text-xs text-ink-muted">
          {s.triggers.event.otherHint}
        </p>
      </div>
      <div className="flex flex-col gap-1">
        <label htmlFor={`${id}-projects`} className="text-sm font-medium text-ink">
          {s.triggers.event.projects}
        </label>
        <input
          id={`${id}-projects`}
          className={input}
          value={draft.projects}
          onChange={(e) => set({ projects: e.target.value })}
          aria-describedby={describedBy("event/projects", `${id}-projects-hint`)}
          data-event-projects
        />
        <p id={`${id}-projects-hint`} className="text-xs text-ink-muted">
          {s.triggers.event.projectsHint}
        </p>
        {error("event/projects")}
      </div>
      <fieldset className="flex flex-col gap-2">
        <legend className="mb-1 text-sm font-medium text-ink">{s.triggers.event.conditions}</legend>
        <p className="text-xs text-ink-muted">{s.triggers.event.conditionsHint}</p>
        {draft.conditions.map((c, i) => (
          <div key={i} className="flex flex-col gap-1" data-event-condition={i}>
            <div className="grid gap-2 sm:grid-cols-[2fr_1fr_2fr_auto]">
              <input
                aria-label={s.triggers.event.path(i + 1)}
                className={`${input} font-mono`}
                value={c.path}
                onChange={(e) => setCondition(i, { path: e.target.value })}
                aria-describedby={describedBy(`event/conditions/${i}`)}
              />
              <select aria-label={s.triggers.event.op(i + 1)} className={input} value={c.op} onChange={(e) => setCondition(i, { op: e.target.value as Op })}>
                {OPS.map((op) => (
                  <option key={op} value={op}>
                    {s.triggers.event.ops[op]}
                  </option>
                ))}
              </select>
              <input
                aria-label={s.triggers.event.value(i + 1)}
                className={input}
                value={c.value}
                disabled={UNARY.includes(c.op)}
                onChange={(e) => setCondition(i, { value: e.target.value })}
                aria-describedby={LISTS.includes(c.op) ? `${id}-in-hint` : undefined}
              />
              <button type="button" className={secondary} onClick={() => set({ conditions: draft.conditions.filter((_, j) => j !== i) })}>
                {s.triggers.event.removeCondition(i + 1)}
              </button>
            </div>
            {error(`event/conditions/${i}`)}
          </div>
        ))}
        <p id={`${id}-in-hint`} className="text-xs text-ink-muted">
          {s.triggers.event.valueHint}
        </p>
        <div>
          <button
            type="button"
            className={secondary}
            onClick={() => set({ conditions: [...draft.conditions, { path: "payload.", op: "equals", value: "" }] })}
            data-action="add-condition"
          >
            {s.triggers.event.addCondition}
          </button>
        </div>
      </fieldset>
    </div>
  );
}
