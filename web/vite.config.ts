// vitest/config re-exports Vite's defineConfig with the `test` block typed, so one file configures both.
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { readFileSync } from "node:fs";
import { availableParallelism, loadavg } from "node:os";
import { fileURLToPath, URL } from "node:url";

// The root VERSION file is the one releases bump, so the client reads it rather than keeping a second copy.
const version = readFileSync(fileURLToPath(new URL("../VERSION", import.meta.url)), "utf8").trim();

// The port inside the container; make web-dev publishes it on a port of the checkout's own.
const DEV_SERVER_PORT = 5173;

// Gates in parallel worktrees share one machine, so a run takes at most a share of the idle cores.
const TEST_CORE_SHARE = 0.5;
const TEST_MIN_WORKERS = 2;
const cores = availableParallelism();
const [busyCores = 0] = loadavg();
const idleCores = cores - Math.round(busyCores);
const testWorkers = Math.max(TEST_MIN_WORKERS, Math.min(Math.floor(cores * TEST_CORE_SHARE), idleCores));

export default defineConfig({
  plugins: [react(), tailwindcss()],
  define: {
    __APP_VERSION__: JSON.stringify(version),
  },
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  server: {
    host: "0.0.0.0",
    port: DEV_SERVER_PORT,
    // Vite rejects a Host header it does not know, which would block the compose service name.
    allowedHosts: ["localhost", "127.0.0.1", "web"],
    // One origin for the browser, so the session cookie is first-party and no CORS preflight is involved.
    proxy: {
      "/api": {
        target: process.env.VITE_API_PROXY || "http://localhost:8080",
        changeOrigin: true,
      },
    },
  },
  test: {
    globals: true,
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    maxWorkers: testWorkers,
  },
});
