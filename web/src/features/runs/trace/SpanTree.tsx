import { type KeyboardEvent, useEffect, useRef, useState } from "react";
import { Icon } from "@/features/shell/icons";
import { s } from "@/strings";
import { bar, isFailed, type MoreRow, type Row, type SpanRow, type TimeWindow } from "./tree";

/** How far each level of the tree is set in, in rem. */
const INDENT_REM = 1;

// A chosen row is ringed on a surface rather than tinted: a failed span's danger text keeps its contrast on surfaces only.
const item =
  "grid cursor-pointer grid-cols-[minmax(0,1fr)_4.5rem] items-center gap-2 rounded-control px-2 py-1 text-sm text-ink sm:grid-cols-[minmax(0,1fr)_minmax(0,35%)_4.5rem]";

interface TreeProps {
  rows: Row[];
  timeline: TimeWindow;
  selected: string | null;
  labelledBy: string;
  onSelect: (spanID: string) => void;
  onToggle: (spanID: string) => void;
  onMore: (row: MoreRow) => void;
}

function Chevron({ row, onToggle }: { row: SpanRow; onToggle: (spanID: string) => void }) {
  if (row.span.children.length === 0) return <span className="w-4 shrink-0" />;
  return (
    // The keyboard opens a span with the arrows, so this is a larger target for the pointer alone.
    <span
      aria-hidden="true"
      className="flex w-4 shrink-0 items-center justify-center text-ink-muted"
      onClick={(event) => {
        event.stopPropagation();
        onToggle(row.span.spanId);
      }}
      data-action="toggle-span"
    >
      <Icon.Chevron className={row.open ? "rotate-90" : ""} />
    </span>
  );
}

function SpanItem({
  row,
  timeline,
  selected,
  focused,
  onSelect,
  onToggle,
  refFor,
}: {
  row: SpanRow;
  timeline: TimeWindow;
  selected: boolean;
  focused: boolean;
  onSelect: (spanID: string) => void;
  onToggle: (spanID: string) => void;
  refFor: (key: string) => (node: HTMLDivElement | null) => void;
}) {
  const { span } = row;
  const failed = isFailed(span);
  const place = bar(span, timeline);
  return (
    <div
      ref={refFor(row.key)}
      role="treeitem"
      aria-level={row.depth + 1}
      aria-posinset={row.pos}
      aria-setsize={row.size}
      aria-expanded={span.children.length > 0 ? row.open : undefined}
      aria-selected={selected}
      tabIndex={focused ? 0 : -1}
      className={`${item} ${selected ? "bg-surface-raised ring-1 ring-inset ring-accent" : "hover:bg-surface-raised"}`}
      onClick={() => onSelect(span.spanId)}
      data-span={span.spanId}
      data-span-name={span.name}
      data-span-failed={failed || undefined}
    >
      <span className="flex min-w-0 items-center gap-1.5" style={{ paddingLeft: `${row.depth * INDENT_REM}rem` }}>
        <Chevron row={row} onToggle={onToggle} />
        <span className={`truncate ${failed ? "font-medium text-danger" : ""}`}>{span.name}</span>
        <span className="shrink-0 rounded-control border border-border px-1 text-xs text-ink" data-span-kind={span.kind}>
          {s.trace.kinds[span.kind] ?? span.kind}
        </span>
        {failed && (
          <span className="flex shrink-0 items-center gap-1 text-xs font-medium text-danger" data-span-status="error">
            <Icon.Warning />
            {s.trace.failed}
          </span>
        )}
        {span.source === "otlp" && span.sender && <span className="truncate text-xs text-ink-muted">{s.trace.sentBy(span.sender.name)}</span>}
        {span.omitted > 0 && <span className="shrink-0 text-xs text-ink-muted">{s.trace.omitted(span.omitted)}</span>}
      </span>
      <span className="relative hidden h-2 rounded-full bg-surface-sunken sm:block" aria-hidden="true">
        <span
          className={`absolute inset-y-0 min-w-px rounded-full ${failed ? "bg-danger" : "bg-accent"}`}
          style={{ left: `${place.left}%`, width: `${place.width}%` }}
        />
      </span>
      <span className="text-right text-xs tabular-nums text-ink-muted">{s.trace.duration(span.durationMs)}</span>
    </div>
  );
}

/** The next row to focus for a key, or undefined when the key is not the tree's; toggling is answered apart. */
function moveFor(key: string, rows: Row[], at: number): number | undefined {
  const row = rows[at];
  if (!row) return undefined;
  switch (key) {
    case "ArrowDown":
      return Math.min(rows.length - 1, at + 1);
    case "ArrowUp":
      return Math.max(0, at - 1);
    case "Home":
      return 0;
    case "End":
      return rows.length - 1;
    case "ArrowRight":
      return row.kind === "span" && row.open ? at + 1 : undefined;
    case "ArrowLeft": {
      if (row.kind === "span" && row.open) return undefined;
      const parent = rows.findIndex((r) => r.key === row.parentKey);
      return parent >= 0 ? parent : at;
    }
    default:
      return undefined;
  }
}

/**
 * The spans as a tree to the keyboard and a screen reader: the arrows move, open and close, Enter or Space shows a
 * span's details. One row at a time takes Tab, so the tree is one stop.
 */
export function SpanTree({ rows, timeline, selected, labelledBy, onSelect, onToggle, onMore }: TreeProps) {
  const [focusKey, setFocusKey] = useState<string | null>(null);
  const nodes = useRef(new Map<string, HTMLDivElement>());
  // Focus follows the keyboard only, so a trace growing under a live run never takes it.
  const moved = useRef(false);
  // Showing more takes away the row that asked, so focus goes to the first span it showed, at the same place.
  const shownAt = useRef<number | null>(null);
  const focusAt = rows.findIndex((r) => r.key === (focusKey ?? selected));
  const tabStop = focusAt >= 0 ? focusAt : 0;

  useEffect(() => {
    if (shownAt.current !== null) {
      const key = rows[Math.min(shownAt.current, rows.length - 1)]?.key ?? null;
      shownAt.current = null;
      setFocusKey(key);
      return;
    }
    if (!moved.current) return;
    moved.current = false;
    const key = rows[tabStop]?.key;
    if (key) nodes.current.get(key)?.focus();
  }, [rows, tabStop]);

  const more = (row: MoreRow, at: number, byKeyboard: boolean) => {
    shownAt.current = at;
    moved.current = byKeyboard;
    onMore(row);
  };

  const refFor = (key: string) => (node: HTMLDivElement | null) => {
    if (node) nodes.current.set(key, node);
    else nodes.current.delete(key);
  };

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const row = rows[tabStop];
    if (!row) return;
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      if (row.kind === "span") onSelect(row.span.spanId);
      else more(row, tabStop, true);
      return;
    }
    const toggles =
      row.kind === "span" && row.span.children.length > 0 && ((event.key === "ArrowRight" && !row.open) || (event.key === "ArrowLeft" && row.open));
    const next = moveFor(event.key, rows, tabStop);
    if (!toggles && next === undefined) return;
    event.preventDefault();
    if (!toggles && next === tabStop) return;
    moved.current = true;
    if (toggles) {
      onToggle(row.span.spanId);
      setFocusKey(row.key);
      return;
    }
    setFocusKey(rows[next as number]?.key ?? null);
  };

  return (
    <div role="tree" aria-labelledby={labelledBy} className="flex min-w-0 flex-col gap-0.5" onKeyDown={onKeyDown} data-span-tree>
      {rows.map((row, at) =>
        row.kind === "span" ? (
          <SpanItem
            key={row.key}
            row={row}
            timeline={timeline}
            selected={row.span.spanId === selected}
            focused={at === tabStop}
            onSelect={(id) => {
              setFocusKey(row.key);
              onSelect(id);
            }}
            onToggle={onToggle}
            refFor={refFor}
          />
        ) : (
          <div
            key={row.key}
            ref={refFor(row.key)}
            role="treeitem"
            aria-level={row.depth + 1}
            aria-selected={false}
            tabIndex={at === tabStop ? 0 : -1}
            className="cursor-pointer rounded-control px-2 py-1 text-sm font-medium text-accent hover:bg-surface-raised"
            style={{ paddingLeft: `${row.depth * INDENT_REM + INDENT_REM}rem` }}
            onClick={() => more(row, at, false)}
            data-action="more-spans"
          >
            {s.trace.more(row.shown, row.remaining)}
          </div>
        ),
      )}
    </div>
  );
}
