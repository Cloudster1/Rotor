import type { Trace, TraceSpan } from "@/api/traces";

/** Children shown at once under one span; more come a page at a time, so a wide trace stays quick to draw. */
export const CHILDREN_PAGE = 100;
/** A trace of at most this many spans opens whole; a larger one opens to OPEN_DEPTH and the way to its failure. */
export const OPEN_ALL_SPANS = 300;
/** Levels open in a large trace: the run and its graph, so its nodes show. */
export const OPEN_DEPTH = 2;
/** The parent key of the roots, which are paged as any span's children are. */
export const ROOT = "";

export type SpanRow = {
  kind: "span";
  key: string;
  span: TraceSpan;
  depth: number;
  pos: number;
  size: number;
  parentKey: string | null;
  open: boolean;
};
export type MoreRow = { kind: "more"; key: string; parent: string; depth: number; shown: number; remaining: number; parentKey: string | null };
export type Row = SpanRow | MoreRow;

export interface Located {
  span: TraceSpan;
  /** The span ids from the root down to the span's parent. */
  ancestors: string[];
}

export function isFailed(span: TraceSpan): boolean {
  return span.status === "error";
}

/** Every span by its id with the way to it, walked without recursion since a sender decides how deep a trace goes. */
export function indexSpans(roots: TraceSpan[]): Map<string, Located> {
  const index = new Map<string, Located>();
  const stack: Located[] = roots.map((span) => ({ span, ancestors: [] }));
  while (stack.length > 0) {
    const item = stack.pop() as Located;
    index.set(item.span.spanId, item);
    const below = [...item.ancestors, item.span.spanId];
    for (const child of item.span.children) stack.push({ span: child, ancestors: below });
  }
  return index;
}

/**
 * Where the trace failed: of the failed spans none of whose descendants failed, the one that started first. A failure
 * marks every span it passes through, so this is the span it came from rather than the run that ended with it.
 */
export function firstFailure(index: Map<string, Located>): Located | null {
  const failedBelow = new Set<string>();
  for (const { span, ancestors } of index.values()) {
    if (isFailed(span)) for (const id of ancestors) failedBelow.add(id);
  }
  let first: Located | null = null;
  for (const item of index.values()) {
    if (!isFailed(item.span) || failedBelow.has(item.span.spanId)) continue;
    if (!first || item.span.startedAt < first.span.startedAt) first = item;
  }
  return first;
}

export interface Visibility {
  /** All spans of the trace, which decides whether it opens whole. */
  total: number;
  /** Spans the person opened or closed against how they start. */
  toggled: ReadonlySet<string>;
  /** Children shown under a span id, or ROOT for the roots, past the first page. */
  shown: ReadonlyMap<string, number>;
  /** The failed span opened for a failed run and its ancestors, kept open and in view. */
  path: ReadonlySet<string>;
}

/** Whether a span is open: as it starts, unless the person turned it the other way. */
export function isOpen(span: TraceSpan, depth: number, v: Visibility): boolean {
  const start = v.total <= OPEN_ALL_SPANS || depth < OPEN_DEPTH || v.path.has(span.spanId);
  return v.toggled.has(span.spanId) ? !start : start;
}

/** The rows the tree shows, in order: open spans' children a page at a time, each page ended by a row for more. */
export function visibleRows(roots: TraceSpan[], v: Visibility): Row[] {
  const rows: Row[] = [];
  type Frame = { children: TraceSpan[]; parent: string; parentKey: string | null; depth: number; next: number; limit: number };
  const frame = (children: TraceSpan[], parent: string, parentKey: string | null, depth: number): Frame => {
    // A page always reaches the child on the way to the failure, so the opened span is in view.
    const onPath = children.findIndex((child) => v.path.has(child.spanId));
    const limit = Math.min(children.length, Math.max(v.shown.get(parent) ?? CHILDREN_PAGE, onPath + 1));
    return { children, parent, parentKey, depth, next: 0, limit };
  };
  const stack: Frame[] = [frame(roots, ROOT, null, 0)];
  for (let top = stack.at(-1); top; top = stack.at(-1)) {
    const span = top.children[top.next];
    if (!span || top.next >= top.limit) {
      stack.pop();
      if (top.limit < top.children.length) {
        rows.push({
          kind: "more",
          key: `more:${top.parent}`,
          parent: top.parent,
          depth: top.depth,
          shown: top.limit,
          remaining: top.children.length - top.limit,
          parentKey: top.parentKey,
        });
      }
      continue;
    }
    top.next += 1;
    const open = span.children.length > 0 && isOpen(span, top.depth, v);
    rows.push({ kind: "span", key: span.spanId, span, depth: top.depth, pos: top.next, size: top.children.length, parentKey: top.parentKey, open });
    if (open) stack.push(frame(span.children, span.spanId, span.spanId, top.depth + 1));
  }
  return rows;
}

export interface TimeWindow {
  start: number;
  length: number;
}

/** The stretch of time the bars are placed on: the trace's, or its spans' while a run still adds to it. */
export function traceWindow(trace: Trace, index: Map<string, Located>): TimeWindow {
  let start = trace.startedAt ? Date.parse(trace.startedAt) : Number.POSITIVE_INFINITY;
  let end = trace.endedAt ? Date.parse(trace.endedAt) : Number.NEGATIVE_INFINITY;
  for (const { span } of index.values()) {
    start = Math.min(start, Date.parse(span.startedAt));
    end = Math.max(end, Date.parse(span.endedAt));
  }
  if (!Number.isFinite(start) || !Number.isFinite(end)) return { start: 0, length: 0 };
  return { start, length: Math.max(0, end - start) };
}

/** Where a span's bar starts and how wide it is, in percent of the window. */
export function bar(span: TraceSpan, w: TimeWindow): { left: number; width: number } {
  if (w.length <= 0) return { left: 0, width: 100 };
  const left = ((Date.parse(span.startedAt) - w.start) / w.length) * 100;
  const width = (span.durationMs / w.length) * 100;
  const clampedLeft = Math.min(100, Math.max(0, left));
  return { left: clampedLeft, width: Math.min(100 - clampedLeft, Math.max(0, width)) };
}

/** An input or output as it reads best: JSON indented, anything else as it came. */
export function readable(value: string, mimeType: string): string {
  if (!mimeType.includes("json")) return value;
  try {
    return JSON.stringify(JSON.parse(value), null, 2);
  } catch {
    // A preview cut short is no longer JSON; it shows as it came.
    return value;
  }
}
