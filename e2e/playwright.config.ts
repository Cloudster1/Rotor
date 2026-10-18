import { defineConfig, devices, type ReporterDescription } from "@playwright/test";
import { ALICE_STATE } from "./fixtures/auth";
import { WEB_URL } from "./fixtures/stack";

// A spec that only means something at one width carries @desktop or @mobile and runs in that project alone.
const MOBILE_VIEWPORT = { width: 360, height: 740 };

// CI's browser jobs each leave a blob their report job merges (merge.config.ts); anywhere else the suite
// writes the HTML report and the JSON the gate's summary reads.
const REPORTS_DIR = "../reports";
const reporter: ReporterDescription[] = process.env.CI
  ? [["list"], ["blob", { outputDir: `${REPORTS_DIR}/e2e-blob` }]]
  : [["list"], ["html", { outputFolder: "playwright-report", open: "never" }], ["json", { outputFile: `${REPORTS_DIR}/e2e.json` }]];

const TEST_TIMEOUT_MS = 20_000;
const EXPECT_TIMEOUT_MS = 5_000;
const ACTION_TIMEOUT_MS = 5_000;
const NAVIGATION_TIMEOUT_MS = 10_000;

export default defineConfig({
  testDir: "./tests",
  outputDir: "./test-results",
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: 0,
  timeout: TEST_TIMEOUT_MS,
  expect: { timeout: EXPECT_TIMEOUT_MS },
  reporter,
  use: {
    baseURL: WEB_URL,
    locale: "en-US",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
    actionTimeout: ACTION_TIMEOUT_MS,
    navigationTimeout: NAVIGATION_TIMEOUT_MS,
  },
  // Every browser project starts signed in as alice, by the one real sign-in the setup project makes.
  projects: [
    { name: "setup", testMatch: /.*\.setup\.ts/ },
    {
      name: "chromium",
      grepInvert: /@mobile/,
      dependencies: ["setup"],
      use: { ...devices["Desktop Chrome"], storageState: ALICE_STATE },
    },
    {
      name: "mobile",
      grepInvert: /@desktop/,
      dependencies: ["setup"],
      use: { ...devices["Desktop Chrome"], viewport: MOBILE_VIEWPORT, isMobile: true, hasTouch: true, storageState: ALICE_STATE },
    },
  ],
});
