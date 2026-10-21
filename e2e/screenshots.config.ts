import { defineConfig, devices } from "@playwright/test";
import { ALICE_STATE } from "./fixtures/auth";
import { WEB_URL } from "./fixtures/stack";

// make screenshots: the pictures in docs/screenshots, taken from the running stack; outside the gate's suite.
const SHOT_VIEWPORT = { width: 1440, height: 900 };
const SHOTS_TIMEOUT_MS = 600_000;
const EXPECT_TIMEOUT_MS = 10_000;

export default defineConfig({
  outputDir: "./test-results",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: SHOTS_TIMEOUT_MS,
  expect: { timeout: EXPECT_TIMEOUT_MS },
  reporter: [["list"]],
  use: { baseURL: WEB_URL, locale: "en-US", trace: "retain-on-failure" },
  projects: [
    { name: "setup", testDir: "./tests", testMatch: /auth\.setup\.ts/ },
    {
      name: "screenshots",
      testDir: "./screenshots",
      dependencies: ["setup"],
      use: { ...devices["Desktop Chrome"], viewport: SHOT_VIEWPORT, storageState: ALICE_STATE },
    },
  ],
});
