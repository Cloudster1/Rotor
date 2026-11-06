import { useCallback, useReducer } from "react";
import { CANVAS_HISTORY_LIMIT } from "@/config";

interface History {
  past: string[];
  text: string;
  future: string[];
}

type Step = { kind: "edit" | "replace"; text: string } | { kind: "undo" | "redo" };

function step(h: History, s: Step): History {
  switch (s.kind) {
    case "edit":
      if (s.text === h.text) return h;
      return { past: [...h.past, h.text].slice(-CANVAS_HISTORY_LIMIT), text: s.text, future: [] };
    case "replace":
      // The YAML editor hands back the text it was given, so only a different text is typing that ends the history.
      if (s.text === h.text) return h;
      return { past: [], text: s.text, future: [] };
    case "undo": {
      const before = h.past.at(-1);
      return before === undefined ? h : { past: h.past.slice(0, -1), text: before, future: [h.text, ...h.future] };
    }
    case "redo": {
      const [after, ...rest] = h.future;
      return after === undefined ? h : { past: [...h.past, h.text], text: after, future: rest };
    }
  }
}

/** A workflow's text with the canvas's edits to undo and redo; any other change, typing in the YAML or opening a
 * version, starts the history again, so an undo never takes back what was typed. */
export function useTextHistory(initial: string) {
  const [h, dispatch] = useReducer(step, { past: [], text: initial, future: [] });
  return {
    text: h.text,
    edit: useCallback((text: string) => dispatch({ kind: "edit", text }), []),
    replace: useCallback((text: string) => dispatch({ kind: "replace", text }), []),
    undo: useCallback(() => dispatch({ kind: "undo" }), []),
    redo: useCallback(() => dispatch({ kind: "redo" }), []),
    canUndo: h.past.length > 0,
    canRedo: h.future.length > 0,
  };
}

/** What the canvas offers of the history. */
export type TextHistory = Pick<ReturnType<typeof useTextHistory>, "undo" | "redo" | "canUndo" | "canRedo">;
