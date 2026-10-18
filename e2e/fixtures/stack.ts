import { existsSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

/** Where `make stack-up` writes the ports and project name of this checkout's stack. */
export const STACK_ENV_FILE = resolve(dirname(fileURLToPath(import.meta.url)), "../../.cache/stack.env");

function readStackEnv(): Record<string, string> {
  if (!existsSync(STACK_ENV_FILE)) return {};
  const out: Record<string, string> = {};
  for (const line of readFileSync(STACK_ENV_FILE, "utf8").split("\n")) {
    const match = /^([A-Z0-9_]+)=(.*)$/.exec(line.trim());
    if (match) out[match[1]!] = match[2]!;
  }
  return out;
}

const env = readStackEnv();

// The environment wins, so the suite can be pointed at a stack elsewhere.
function setting(name: string): string {
  const value = process.env[name] ?? env[name];
  if (!value) {
    throw new Error(`${name} is not set and ${STACK_ENV_FILE} does not name it. Start the stack with make up or make stack-up first.`);
  }
  return value;
}

/** The web client's origin, which proxies /api/ to the api. */
export const WEB_URL = setting("ROTOR_WEB_URL");

/** Keycloak as the browser sees it; read when first needed, by the sign-in fixtures. */
export function keycloakURL(): string {
  return setting("ROTOR_KEYCLOAK_URL");
}

/** The build the checkout made, which the api and the settings page name. */
export function buildVersion(): string | undefined {
  return process.env.ROTOR_BUILD_VERSION ?? env.ROTOR_BUILD_VERSION;
}
