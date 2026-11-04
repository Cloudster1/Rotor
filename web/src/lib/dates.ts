import { FALLBACK_TIME_ZONE } from "@/config";

// The zone the person chose under Settings, Profile (#206); undefined follows the browser's. The shell sets it before
// any page draws, so every date below is written in it without each page asking.
let chosen: string | undefined;

/** Writes later dates in zone, or in the browser's when zone is empty. */
export function setTimeZone(zone: string | null | undefined): void {
  chosen = zone || undefined;
}

/** The zone the browser runs in, UTC when it does not say. */
export function browserTimeZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || FALLBACK_TIME_ZONE;
}

/** The zone dates are written in now: the person's choice, or the browser's. */
export function currentTimeZone(): string {
  return chosen ?? browserTimeZone();
}

/** The zones a browser knows, UTC first; an older browser offers UTC alone. */
export function timeZones(): string[] {
  const known = typeof Intl.supportedValuesOf === "function" ? Intl.supportedValuesOf("timeZone") : [];
  return [FALLBACK_TIME_ZONE, ...known.filter((zone) => zone !== FALLBACK_TIME_ZONE)];
}

/** A day and a time of day, the medium date and short time most pages show. */
export const DATE_AND_TIME: Intl.DateTimeFormatOptions = { dateStyle: "medium", timeStyle: "short" };

/** A moment as a date and time in the person's zone; options name the style, and a zone of their own wins. */
export function formatDateTime(at: string | number | Date, options: Intl.DateTimeFormatOptions = {}): string {
  return new Date(at).toLocaleString(undefined, { ...options, timeZone: options.timeZone ?? chosen });
}

/** A moment as a time of day in the person's zone. */
export function formatTime(at: string | number | Date, options: Intl.DateTimeFormatOptions = {}): string {
  return new Date(at).toLocaleTimeString(undefined, { ...options, timeZone: options.timeZone ?? chosen });
}
