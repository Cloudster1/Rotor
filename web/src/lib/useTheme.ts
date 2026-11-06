import { useSyncExternalStore } from "react";
import { currentTheme, subscribeTheme, type ThemeChoice } from "./theme";

/** The choice in force, drawn again wherever it is changed. */
export function useTheme(): ThemeChoice {
  return useSyncExternalStore(subscribeTheme, currentTheme);
}
