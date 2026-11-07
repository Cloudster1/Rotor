import { useCallback, useSyncExternalStore } from "react";
import { COARSE_POINTER_QUERY } from "@/config";

/** Whether the browser matches a media query now; false where it cannot tell, as in tests without a layout. */
export function matchesMedia(query: string): boolean {
  return typeof window !== "undefined" && typeof window.matchMedia === "function" && window.matchMedia(query).matches;
}

/** Whether a media query matches, drawn again when that changes, as when a tablet's keyboard and mouse are docked. */
export function useMediaQuery(query: string): boolean {
  const subscribe = useCallback(
    (changed: () => void) => {
      if (typeof window.matchMedia !== "function") return () => {};
      const list = window.matchMedia(query);
      list.addEventListener("change", changed);
      return () => list.removeEventListener("change", changed);
    },
    [query],
  );
  return useSyncExternalStore(subscribe, () => matchesMedia(query));
}

/** Whether the main pointer is a finger, so a drag over a canvas scrolls the page instead of moving the canvas. */
export function useCoarsePointer(): boolean {
  return useMediaQuery(COARSE_POINTER_QUERY);
}
