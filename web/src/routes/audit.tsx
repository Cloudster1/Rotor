import { createRoute } from "@tanstack/react-router";
import { AuditRoute } from "@/features/audit/AuditLog";
import { shellRoute } from "./shell";

export const auditRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/settings/audit",
  component: AuditRoute,
});
