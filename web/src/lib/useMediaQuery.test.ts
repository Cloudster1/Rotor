import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { COARSE_POINTER_QUERY } from "@/config";
import { matchesMedia, useCoarsePointer } from "./useMediaQuery";

/** A browser whose media queries match as the test says, and tell their listeners when that changes. */
function fakeMedia(initially: Record<string, boolean>) {
  const state = { ...initially };
  const listeners = new Map<string, Set<() => void>>();
  const matchMedia = vi.fn((query: string) => ({
    get matches() {
      return state[query] ?? false;
    },
    addEventListener: (_: string, listener: () => void) => {
      const set = listeners.get(query) ?? new Set();
      set.add(listener);
      listeners.set(query, set);
    },
    removeEventListener: (_: string, listener: () => void) => listeners.get(query)?.delete(listener),
  }));
  vi.stubGlobal("matchMedia", matchMedia);
  return {
    set(query: string, matches: boolean) {
      state[query] = matches;
      for (const listener of listeners.get(query) ?? []) listener();
    },
    listening: (query: string) => listeners.get(query)?.size ?? 0,
  };
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("the coarse pointer", () => {
  it("is not assumed where the browser cannot say, as without matchMedia", () => {
    expect(matchesMedia(COARSE_POINTER_QUERY)).toBe(false);
    expect(renderHook(() => useCoarsePointer()).result.current).toBe(false);
  });

  it("follows the main pointer, and changes when a mouse takes over from the finger", () => {
    const media = fakeMedia({ [COARSE_POINTER_QUERY]: true });
    const { result, unmount } = renderHook(() => useCoarsePointer());
    expect(result.current).toBe(true);
    act(() => media.set(COARSE_POINTER_QUERY, false));
    expect(result.current).toBe(false);
    unmount();
    expect(media.listening(COARSE_POINTER_QUERY)).toBe(0);
  });
});
