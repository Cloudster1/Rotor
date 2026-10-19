import { createRoute } from "@tanstack/react-router";
import { UsagePage } from "@/features/usage/UsagePage";
import { shellRoute } from "./shell";

export const usageRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/settings/usage",
  component: UsagePage,
});
