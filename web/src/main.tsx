import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";

import { createQueryClient } from "./lib/session";
import { applyTheme, readTheme } from "./lib/theme";
import { buildRouter, sendToLogin } from "./routes";
import "./styles/index.css";

// Before the first paint, so a dark choice never flashes light.
applyTheme(readTheme());

// A 401 anywhere sends the reader to sign in; the router exists by the time any request answers.
const queryClient = createQueryClient((client) => sendToLogin(router, client));
const router = buildRouter(queryClient);

const container = document.getElementById("root");
if (!container) throw new Error("root element is missing from index.html");

createRoot(container).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
);
