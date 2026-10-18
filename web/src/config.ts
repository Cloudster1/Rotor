/** Tunable values shared across the client. */

/** The product's name, as the title bar and the sidebar show it. */
export const APP_NAME = "Rotor";

/** The release this client was built from, read from the root VERSION file at build time. */
export const APP_VERSION: string = __APP_VERSION__;

/** Where the API lives, relative to the page; the dev server and nginx proxy it. */
export const API_BASE = "/api/v1";

/** The status the API answers when nobody is signed in. */
export const HTTP_UNAUTHORIZED = 401;

/** Where the index sends a visitor, the first place of the sidebar. */
export const HOME_PATH = "/workflows";

/** Where the sign-in page lives, which a returning visitor must never be sent back to. */
export const LOGIN_PATH = "/login";

/** How long a query's answer stays fresh before a refetch. */
export const QUERY_STALE_MS = 10_000;

/** What the server says it is changes only with a deploy, so the answer is kept five minutes. */
export const META_STALE_MS = 300_000;

/** Where this browser remembers the colour scheme the person chose. */
export const THEME_STORAGE_KEY = "rotor.theme";

/** The product's mark beside its name in the sidebar, in pixels. */
export const MARK_WIDTH_PX = 24;

/** The icon grid and stroke: every glyph is drawn to these two numbers. */
export const ICON_SIZE_PX = 16;
export const ICON_STROKE = 1.5;
