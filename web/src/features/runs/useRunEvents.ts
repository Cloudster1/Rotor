import { useEffect, useRef, useState } from "react";
import { type RunEvent, runEventsURL } from "@/api/runs";

/** The event types a run's stream sends, each by its own name. */
export const EVENT_TYPES = [
  "node.started",
  "node.finished",
  "llm.output",
  "llm.usage",
  "tool.called",
  "log",
  "interrupt",
  "approval.requested",
  "approval.decided",
  "run.finished",
  "runner.lost",
];

/** The states a run never leaves; a run.finished saying one of them is the stream's last event. */
const ENDED = new Set(["succeeded", "failed", "cancelled", "expired"]);

/**
 * A run's events, from the first and then live; a dropped stream resumes after the last event seen, which the
 * browser sends as Last-Event-ID. onEvent hears each new event, such as to ask for the run again.
 */
export function useRunEvents(runID: string, onEvent?: (event: RunEvent) => void): { events: RunEvent[]; live: boolean } {
  const [events, setEvents] = useState<RunEvent[]>([]);
  const [live, setLive] = useState(true);
  const handler = useRef(onEvent);
  useEffect(() => {
    handler.current = onEvent;
  });

  useEffect(() => {
    setEvents([]);
    setLive(true);
    const source = new EventSource(runEventsURL(runID), { withCredentials: true });
    let last = 0;
    const receive = (message: MessageEvent<string>) => {
      let event: RunEvent;
      try {
        event = JSON.parse(message.data) as RunEvent;
      } catch {
        return;
      }
      // A replay after a reconnect starts after the last event seen; anything older is a repeat.
      if (event.seq <= last) return;
      last = event.seq;
      setEvents((seen) => [...seen, event]);
      handler.current?.(event);
      if (event.type === "run.finished" && ENDED.has(String(event.payload.status))) {
        source.close();
        setLive(false);
      }
    };
    for (const type of EVENT_TYPES) source.addEventListener(type, receive as EventListener);
    return () => source.close();
  }, [runID]);

  return { events, live };
}
