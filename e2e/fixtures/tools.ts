import { test as base } from "@playwright/test";

/** Notes the address of a tool a test is about to add, so its teardown removes the tool even when the test fails. */
type TrackTool = (address: string) => void;

export const test = base.extend<{ trackTool: TrackTool }>({
  trackTool: async ({ page }, use) => {
    const addresses: string[] = [];
    await use((address) => void addresses.push(address));
    if (addresses.length === 0) return;
    const response = await page.request.get("/api/v1/tools");
    if (!response.ok()) return;
    const body = (await response.json()) as { tools: { id: string; baseURL: string }[] };
    for (const tool of body.tools.filter((t) => addresses.includes(t.baseURL))) {
      await page.request.delete(`/api/v1/tools/${tool.id}`);
    }
  },
});
