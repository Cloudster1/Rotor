import { s } from "@/strings";
import type { IconName } from "./icons";

/** A page the reader can go to; the sidebar lists them in this order. Tool accounts live under Settings since #206. */
export interface Place {
  path: "/workflows" | "/runs" | "/approvals" | "/settings" | "/admin";
  icon: IconName;
  label: string;
  /** Shown to administrators alone; anybody else would only be refused there. */
  adminOnly?: boolean;
}

export const PLACES: readonly Place[] = [
  { path: "/workflows", icon: "Workflow", label: s.places.workflows },
  { path: "/runs", icon: "Run", label: s.places.runs },
  { path: "/approvals", icon: "Approval", label: s.places.approvals },
  { path: "/settings", icon: "Settings", label: s.places.settings },
  { path: "/admin", icon: "Admin", label: s.places.admin, adminOnly: true },
];
