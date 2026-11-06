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
/** A save made from a version that is no longer current, among others (#219). */
export const HTTP_CONFLICT = 409;

/** Where the index sends a visitor, the first place of the sidebar. */
export const HOME_PATH = "/workflows";

/** Where the sign-in page lives, which a returning visitor must never be sent back to. */
export const LOGIN_PATH = "/login";
/** Where a person lets an assistant connect as them (#47); the api's authorization server sends the browser here. */
export const OAUTH_CONSENT_PATH = "/oauth/consent";

/** How long a query's answer stays fresh before a refetch. */
export const QUERY_STALE_MS = 10_000;

/** What the server says it is changes only with a deploy, so the answer is kept five minutes. */
export const META_STALE_MS = 300_000;

/** Where this browser remembers the colour scheme the person chose. */
export const THEME_STORAGE_KEY = "rotor.theme";

/** The zone dates are written in when neither the person nor the browser names one. */
export const FALLBACK_TIME_ZONE = "UTC";

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

/** How long after a run ends its page still asks for the trace: the runner sends its last spans in batches seconds apart. */
export const TRACE_SETTLE_MS = 10_000;

/** How often the sidebar asks how many approvals wait for the person; an approval waits hours, not seconds. */
export const APPROVALS_POLL_MS = 15_000;

/** The longest comment a decision carries, as the server allows (MaxApprovalCommentChars). */
export const MAX_APPROVAL_COMMENT = 2_000;

/** The longest text an inputs field takes before the form asks for less; the server bounds the whole run's inputs. */
export const MAX_INPUT_TEXT = 10_000;

/** How far apart the run graph draws nodes side by side, and one layer below the next, in pixels. */
export const GRAPH_COLUMN_PX = 200;
export const GRAPH_ROW_PX = 100;

/** How far a loop's edge back swings out of its nodes, and its dashes, in pixels, so it reads as a way round (#134). */
export const GRAPH_LOOP_OFFSET_PX = 28;
export const GRAPH_LOOP_DASH = "6 4";

/** How tall the run graph is drawn, in pixels; it fits the graph inside. */
export const GRAPH_HEIGHT_PX = 360;

/** How tall the workflow editor draws its canvas: taller than a run's, since it is worked in, yet short enough on a
 * phone to leave the page room to scroll past it (#265). */
export const EDITOR_GRAPH_HEIGHT = "clamp(320px, 60svh, 600px)";

/** The largest zoom a fitted graph is drawn at, so a workflow of one node is not blown up to twice its size (#265). */
export const GRAPH_FIT_MAX_ZOOM = 1;

/** How far past its outermost nodes a graph can be moved, in pixels, so it is never flung out of reach (#265). */
export const GRAPH_EXTENT_MARGIN_PX = 200;

/** A finger as the main pointer, which scrolls the page over a canvas instead of moving its graph (#265). */
export const COARSE_POINTER_QUERY = "(pointer: coarse)";

/** Below Tailwind's lg breakpoint the editor's inspector stacks under its canvas, so choosing a node scrolls to it,
 * smoothly unless the person asked for less motion. */
export const STACKED_LAYOUT_QUERY = "(width < 64rem)";
export const REDUCED_MOTION_QUERY = "(prefers-reduced-motion: reduce)";

/** How many of the canvas's edits it can undo (#198). */
export const CANVAS_HISTORY_LIMIT = 100;

/** How much room the canvas leaves round a node it brings into view, as a share of the view, and how long it glides. */
export const GRAPH_REVEAL_PADDING = 0.4;
export const GRAPH_REVEAL_MS = 200;

/** How many lines the canvas shows of a prompt or source, and of an approval's message, before it scrolls. */
export const CANVAS_TEXT_ROWS = 5;
export const CANVAS_MESSAGE_ROWS = 2;

/** The most problems a node's badge counts before it says more; the inspector lists them all. */
export const MAX_BADGE_PROBLEMS = 9;

/** Where this browser remembers whether the person edits workflows as YAML or on the canvas. */
export const EDITOR_VIEW_STORAGE_KEY = "rotor.workflowEditorView";

/** The workflows guide on GitHub, where the editor links each shipped workflow's section. */
export const WORKFLOWS_DOC_URL = "https://github.com/Cloudster1/Rotor/blob/main/docs/workflows.md";

/** The products whose webhook deliveries start event triggers (#35), as the server names them. */
export const HOOK_PRODUCTS = ["armature", "exciter", "stator"] as const;
/** What Rotor needs of a tool to act in it and to take its events, as backend/internal/tools/needs.go names them (#43). */
export const CAPABILITY_MCP = "mcp@1";
export const CAPABILITY_WEBHOOKS = "webhooks@1";
/** How long the hook panel says a copied address was copied. */
export const COPIED_NOTICE_MS = 2_000;
/** The topics an event trigger offers per product, from docs/triggers.md; any other may be typed in. */
export const HOOK_TOPICS: Record<string, string[]> = {
  exciter: ["requirement.created", "requirement.revised", "requirement.changed", "review.requested", "review.decided", "comment.created"],
  armature: ["issue.created", "issue.updated", "issue.transitioned", "comment.added"],
  stator: ["page.published", "page.moved", "page.deleted", "comment.created"],
};

/** How many grants the Roles tab shows a page, and how many people the Users page shows (#203). */
export const ROLE_ASSIGNMENT_PAGE_SIZE = 50;
export const USERS_PAGE_SIZE = 50;
/** How long typing in a search pauses before the server is asked, so each key is not a request. */
export const SEARCH_DELAY_MS = 250;
/** The roles a workflow's own grants offer, as backend/internal/perm names them; the others are installation-wide. */
export const WORKFLOW_GRANT_ROLES = ["runner", "editor", "workflow_administrator"] as const;
/** The role and the permission that always go together, so somebody can still administer Rotor. */
export const ADMINISTRATOR_ROLE = "administrator";
export const ADMINISTER_PERMISSION = "rotor.administer";
/** What a new role starts with, as in Armature: seeing the workflows it is granted over. */
export const NEW_ROLE_PERMISSIONS = ["workflow.read"];
/** The built-in group every person is in, as the API marks it. */
export const EVERYONE_GROUP = "everyone";
/** The longest name and description a workflow's metadata takes, as api/workflow.v1.schema.json bounds them (#205). */
export const MAX_WORKFLOW_NAME = 63;
export const MAX_WORKFLOW_DESCRIPTION = 2000;
/** The roles a skill's own grants offer (#219); Skill reader to Everyone makes a skill available to everyone. */
export const SKILL_GRANT_ROLES = ["skill_reader", "skill_editor", "skill_administrator"] as const;
/** How many skills the Skills page lists a page, and how many the editor's picker asks for, the server's most (#223). */
export const SKILL_PAGE_SIZE = 50;
export const SKILL_PICKER_LIMIT = 200;
/** The file every skill has at its root, which the Files page shows first. */
export const SKILL_MD = "SKILL.md";
/** Where a problem that belongs to no one file of a skill is placed, as the library answers it. */
export const SKILL_NO_PATH = "-";
/** The largest zip an import takes (MaxSkillImportBytes), checked before it is sent. */
export const MAX_SKILL_IMPORT_BYTES = 2 * 1024 * 1024;

/** What the server takes of a trace destination (MaxTraceDestinations, MaxDestinationNameChars, MaxDestinationHeaders, MaxDestinationHeaderChars, MaxDestinationURLChars). */
export const MAX_TRACE_DESTINATIONS = 8;
export const MAX_DESTINATION_NAME = 64;
export const MAX_DESTINATION_HEADERS = 16;
export const MAX_DESTINATION_HEADER = 4096;
export const MAX_DESTINATION_URL = 2048;
/** The longest a trace is kept (MaxTraceRetentionDays) and the shortest. */
export const MAX_TRACE_RETENTION_DAYS = 90;
export const MIN_TRACE_RETENTION_DAYS = 1;
/** Headers the server sets itself and refuses to take from an administrator. */
export const RESERVED_DESTINATION_HEADERS = ["Content-Type", "Content-Encoding", "Content-Length", "Host"] as const;
