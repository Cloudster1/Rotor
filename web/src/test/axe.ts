import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import axe from "axe-core";

// jsdom starts from an empty document, a browser from index.html: its language and title are copied in.
const shell = new DOMParser().parseFromString(readFileSync(resolve(process.cwd(), "index.html"), "utf8"), "text/html");

/** Runs axe over the whole document and names each violation and where it is. */
export async function axeViolations(): Promise<string[]> {
  document.documentElement.lang = shell.documentElement.lang;
  document.title = shell.title;
  const result = await axe.run(document, {
    // jsdom computes no layout, so contrast cannot be judged here; the browser suite checks it.
    rules: { "color-contrast": { enabled: false } },
  });
  return result.violations.flatMap((violation) => violation.nodes.map((node) => `${violation.id}: ${node.target.join(" ")}`));
}
