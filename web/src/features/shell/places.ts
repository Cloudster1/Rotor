import { s } from "@/strings";
import type { IconName } from "./icons";

/** A page the reader can go to; the sidebar lists them in this order. */
export interface Place {
  path: "/workflows" | "/runs" | "/approvals" | "/connections" | "/settings";
  icon: IconName;
  label: string;
}

export const PLACES: readonly Place[] = [
  { path: "/workflows", icon: "Workflow", label: s.places.workflows },
  { path: "/runs", icon: "Run", label: s.places.runs },
  { path: "/approvals", icon: "Approval", label: s.places.approvals },
  { path: "/connections", icon: "Connection", label: s.places.connections },
  { path: "/settings", icon: "Settings", label: s.places.settings },
];
