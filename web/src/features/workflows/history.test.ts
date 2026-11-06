import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { CANVAS_HISTORY_LIMIT } from "@/config";
import { useTextHistory } from "./history";

describe("the canvas's history", () => {
  it("undoes and redoes the canvas's edits in order", () => {
    const { result } = renderHook(() => useTextHistory("a"));
    act(() => result.current.edit("b"));
    act(() => result.current.edit("c"));
    act(() => result.current.undo());
    expect(result.current.text).toBe("b");
    act(() => result.current.undo());
    expect(result.current).toMatchObject({ text: "a", canUndo: false, canRedo: true });
    act(() => result.current.redo());
    act(() => result.current.redo());
    expect(result.current).toMatchObject({ text: "c", canUndo: true, canRedo: false });
  });

  it("forgets what was undone once a new edit is made", () => {
    const { result } = renderHook(() => useTextHistory("a"));
    act(() => result.current.edit("b"));
    act(() => result.current.undo());
    act(() => result.current.edit("x"));
    expect(result.current).toMatchObject({ text: "x", canRedo: false });
  });

  it("starts again when the text is typed, but not when the editor hands back the same text", () => {
    const { result } = renderHook(() => useTextHistory("a"));
    act(() => result.current.edit("b"));
    act(() => result.current.replace("b"));
    expect(result.current.canUndo).toBe(true);
    act(() => result.current.replace("typed"));
    expect(result.current).toMatchObject({ text: "typed", canUndo: false, canRedo: false });
  });

  it("keeps at most the limit of edits", () => {
    const { result } = renderHook(() => useTextHistory("0"));
    for (let i = 1; i <= CANVAS_HISTORY_LIMIT + 5; i++) act(() => result.current.edit(String(i)));
    for (let i = 0; i < CANVAS_HISTORY_LIMIT + 5; i++) act(() => result.current.undo());
    expect(result.current.text).toBe("5");
  });
});
