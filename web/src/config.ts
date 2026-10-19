/** Tunable values shared across the client. */

/** The product's name, as the title bar and the sidebar show it. */
export const APP_NAME = "Rotor";

/** The release this client was built from, read from the root VERSION file at build time. */
export const APP_VERSION: string = __APP_VERSION__;

/** Where the API lives, relative to the page; the dev server and nginx proxy it. */
export const API_BASE = "/api/v1";

/** The status the API answers when nobody is signed in. */
export const HTTP_UNAUTHORIZED = 401;

/** The status the API answers when fields of a request need attention, each named in the error's fields. */
export const HTTP_UNPROCESSABLE = 422;

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

/** Where a tool returns the browser after consent; an administrator registers it at tools that need a client by hand. */
export const CONNECTION_CALLBACK_PATH = `${API_BASE}/connections/callback`;

/** How long typing pauses before the workflow editor asks the server what is wrong. */
export const VALIDATE_DELAY_MS = 400;

/** The longest note a saved workflow version carries, as the server allows. */
export const MAX_VERSION_COMMENT = 500;

/** How often a run's page asks for the run again until it ends; #24 streams its events instead. */
export const RUN_POLL_MS = 2_000;

/** How often the sidebar asks how many approvals wait for the person; an approval waits hours, not seconds. */
export const APPROVALS_POLL_MS = 15_000;

/** The longest comment a decision carries, as the server allows (MaxApprovalCommentChars). */
export const MAX_APPROVAL_COMMENT = 2_000;

/** The longest text an inputs field takes before the form asks for less; the server bounds the whole run's inputs. */
export const MAX_INPUT_TEXT = 10_000;

/** How far apart the run graph draws nodes side by side, and one layer below the next, in pixels. */
export const GRAPH_COLUMN_PX = 200;
export const GRAPH_ROW_PX = 100;

/** How tall the run graph is drawn, in pixels; it fits the graph inside. */
export const GRAPH_HEIGHT_PX = 360;

/** The products whose webhook deliveries start event triggers (#35), as the server names them. */
export const HOOK_PRODUCTS = ["armature", "exciter", "stator"] as const;
/** How long the hook panel says a copied address was copied. */
export const COPIED_NOTICE_MS = 2_000;
/** The topics an event trigger offers per product, from docs/triggers.md; any other may be typed in. */
export const HOOK_TOPICS: Record<string, string[]> = {
  exciter: ["requirement.created", "requirement.revised", "requirement.changed", "review.requested", "review.decided", "comment.created"],
  armature: ["issue.created", "issue.updated", "issue.transitioned", "comment.added"],
  stator: ["page.published", "page.moved", "page.deleted", "comment.created"],
};
