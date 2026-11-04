import { afterEach, describe, expect, it } from "vitest";
import { browserTimeZone, currentTimeZone, DATE_AND_TIME, formatDateTime, formatTime, setTimeZone, timeZones } from "./dates";

// Noon in UTC on the first of July is midnight starting the second in Auckland and eight in the morning in New York.
const NOON_UTC = "2026-07-01T12:00:00Z";

afterEach(() => setTimeZone(null));

describe("dates", () => {
  it("are written in the browser's zone until the person chooses one", () => {
    expect(currentTimeZone()).toBe(browserTimeZone());
    expect(formatDateTime(NOON_UTC, DATE_AND_TIME)).toBe(new Date(NOON_UTC).toLocaleString(undefined, DATE_AND_TIME));
  });

  it("are written in the zone the person chose", () => {
    setTimeZone("Pacific/Auckland");
    expect(currentTimeZone()).toBe("Pacific/Auckland");
    expect(formatDateTime(NOON_UTC, DATE_AND_TIME)).toBe(new Date(NOON_UTC).toLocaleString(undefined, { ...DATE_AND_TIME, timeZone: "Pacific/Auckland" }));
    expect(formatDateTime(NOON_UTC, { day: "numeric" })).toBe("2");
    expect(formatTime(NOON_UTC, { hour: "numeric", hourCycle: "h23" })).toBe("00");
    setTimeZone("America/New_York");
    expect(formatTime(NOON_UTC, { hour: "numeric", hourCycle: "h23" })).toBe("08");
  });

  it("keep a zone a page names itself, such as a schedule's", () => {
    setTimeZone("Pacific/Auckland");
    expect(formatTime(NOON_UTC, { hour: "numeric", hourCycle: "h23", timeZone: "UTC" })).toBe("12");
  });

  it("follow the browser again once the choice is taken back", () => {
    setTimeZone("Pacific/Auckland");
    setTimeZone("");
    expect(currentTimeZone()).toBe(browserTimeZone());
  });

  it("offer UTC first among the zones", () => {
    expect(timeZones()[0]).toBe("UTC");
    expect(new Set(timeZones()).size).toBe(timeZones().length);
  });
});
