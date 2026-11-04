import { useReactFlow } from "@xyflow/react";
import { GRAPH_REVEAL_MS } from "@/config";
import type { TextHistory } from "@/features/workflows/history";
import { s } from "@/strings";

const button =
  "h-8 rounded-control border border-border-strong bg-surface px-2.5 text-xs font-medium text-ink shadow-1 hover:bg-surface-raised disabled:opacity-50 disabled:hover:bg-surface";
const separator = "mx-1 h-6 w-px self-center bg-border";

/** The canvas's own tools over its graph; each one has a key while the graph holds the focus. */
export function CanvasToolbar({
  onAdd,
  onInsert,
  onRemove,
  history,
}: {
  onAdd: () => void;
  /** Absent while no way is selected. */
  onInsert?: () => void;
  /** Absent while nothing is selected. */
  onRemove?: () => void;
  history: TextHistory;
}) {
  const flow = useReactFlow();
  const glide = { duration: GRAPH_REVEAL_MS };
  return (
    <div role="toolbar" aria-label={s.canvas.toolbar.label} className="flex flex-wrap items-center gap-1" data-canvas-toolbar>
      <button type="button" className={button} title={s.canvas.toolbar.addHint} onClick={onAdd} data-tool="add">
        {s.canvas.toolbar.add}
      </button>
      <button type="button" className={button} title={s.canvas.toolbar.insertHint} onClick={onInsert} disabled={!onInsert} data-tool="insert">
        {s.canvas.toolbar.insert}
      </button>
      <button type="button" className={button} title={s.canvas.toolbar.removeHint} onClick={onRemove} disabled={!onRemove} data-tool="remove">
        {s.canvas.toolbar.remove}
      </button>
      <span className={separator} aria-hidden="true" />
      <button type="button" className={button} title={s.canvas.toolbar.undoHint} onClick={history.undo} disabled={!history.canUndo} data-tool="undo">
        {s.canvas.toolbar.undo}
      </button>
      <button type="button" className={button} title={s.canvas.toolbar.redoHint} onClick={history.redo} disabled={!history.canRedo} data-tool="redo">
        {s.canvas.toolbar.redo}
      </button>
      <span className={separator} aria-hidden="true" />
      <button type="button" className={button} aria-label={s.canvas.toolbar.zoomIn} title={s.canvas.toolbar.zoomIn} onClick={() => void flow.zoomIn(glide)}>
        +
      </button>
      <button type="button" className={button} aria-label={s.canvas.toolbar.zoomOut} title={s.canvas.toolbar.zoomOut} onClick={() => void flow.zoomOut(glide)}>
        -
      </button>
      <button type="button" className={button} onClick={() => void flow.fitView(glide)} data-tool="fit">
        {s.canvas.toolbar.fit}
      </button>
    </div>
  );
}
