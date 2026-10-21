import { readdirSync, readFileSync } from "node:fs";
import { resolve } from "node:path";

// The tests run in web/ with the whole checkout mounted, so the specs the server and the runner are held to are here too.
const root = resolve(process.cwd(), "..");

function yamlFiles(dir: string): [string, string][] {
  const at = resolve(root, dir);
  return readdirSync(at)
    .filter((f) => f.endsWith(".yaml"))
    .sort()
    .map((f) => [`${dir}/${f}`, readFileSync(resolve(at, f), "utf8")]);
}

/** Every valid golden spec and example, by its path from the checkout's root. */
export const VALID_SPECS: [string, string][] = [...yamlFiles("api/workflow-fixtures/valid"), ...yamlFiles("examples")];

/** Every invalid golden spec, each with the problems its `# expect:` lines name. */
export const INVALID_SPECS: { name: string; source: string; expect: { path: string; code: string }[] }[] = yamlFiles("api/workflow-fixtures/invalid").map(
  ([name, source]) => ({
    name,
    source,
    expect: [...source.matchAll(/^# expect: (\S+) (\S+)$/gm)].map((m) => ({ path: m[1] === "-" ? "" : (m[1] as string), code: m[2] as string })),
  }),
);
