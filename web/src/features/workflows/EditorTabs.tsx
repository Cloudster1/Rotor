import { type KeyboardEvent, type ReactNode, useCallback, useId, useRef, useState } from "react";
import { EDITOR_VIEW_STORAGE_KEY } from "@/config";
import { s } from "@/strings";

/** The two ways to edit a workflow; both edit the same YAML text. */
export type EditorView = "yaml" | "canvas";
const VIEWS: EditorView[] = ["yaml", "canvas"];

/** The view this browser last used; storage that is blocked or empty means the YAML. */
function remembered(): EditorView {
  try {
    return localStorage.getItem(EDITOR_VIEW_STORAGE_KEY) === "canvas" ? "canvas" : "yaml";
  } catch {
    return "yaml";
  }
}

/** The view, starting from the one this browser last used, and remembering each change. */
export function useEditorView(): [EditorView, (view: EditorView) => void] {
  const [view, setView] = useState<EditorView>(remembered);
  const choose = useCallback((next: EditorView) => {
    setView(next);
    try {
      localStorage.setItem(EDITOR_VIEW_STORAGE_KEY, next);
    } catch {
      // A browser that keeps nothing still switches; it only starts with the YAML next time.
    }
  }, []);
  return [view, choose];
}

/** Tabs over the YAML and the canvas. The YAML stays mounted while hidden, so its cursor and history survive a switch. */
export function EditorTabs({ view, onView, yaml, canvas }: { view: EditorView; onView: (view: EditorView) => void; yaml: ReactNode; canvas: ReactNode }) {
  const id = useId();
  const tabs = useRef<Record<EditorView, HTMLButtonElement | null>>({ yaml: null, canvas: null });

  // Arrow keys move between the tabs, as the tabs pattern of the ARIA practices has them.
  function keyed(event: KeyboardEvent) {
    const step = event.key === "ArrowRight" ? 1 : event.key === "ArrowLeft" ? -1 : 0;
    const to =
      event.key === "Home"
        ? VIEWS[0]
        : event.key === "End"
          ? VIEWS[VIEWS.length - 1]
          : step
            ? VIEWS[(VIEWS.indexOf(view) + step + VIEWS.length) % VIEWS.length]
            : undefined;
    if (!to) return;
    event.preventDefault();
    onView(to);
    tabs.current[to]?.focus();
  }

  return (
    <div className="flex flex-col gap-3">
      <div role="tablist" aria-label={s.canvas.views} className="flex gap-1 border-b border-border" onKeyDown={keyed}>
        {VIEWS.map((v) => (
          <button
            key={v}
            ref={(el) => {
              tabs.current[v] = el;
            }}
            type="button"
            role="tab"
            id={`${id}-${v}-tab`}
            aria-selected={view === v}
            aria-controls={`${id}-${v}`}
            tabIndex={view === v ? 0 : -1}
            className={`-mb-px border-b-2 px-3 py-2 text-sm font-medium ${view === v ? "border-accent text-ink" : "border-transparent text-ink-muted hover:text-ink"}`}
            onClick={() => onView(v)}
            data-editor-view={v}
          >
            {v === "yaml" ? s.canvas.yaml : s.canvas.canvas}
          </button>
        ))}
      </div>
      <div role="tabpanel" id={`${id}-yaml`} aria-labelledby={`${id}-yaml-tab`} hidden={view !== "yaml"} className="flex flex-col gap-3">
        {yaml}
      </div>
      <div role="tabpanel" id={`${id}-canvas`} aria-labelledby={`${id}-canvas-tab`} hidden={view !== "canvas"}>
        {view === "canvas" && canvas}
      </div>
    </div>
  );
}
