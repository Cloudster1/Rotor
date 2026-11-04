import type { Assignment, Permission, Role } from "@/api/access";
import type { Group, Person } from "@/api/groups";
import type { ManagedUser } from "@/api/users";
import { ME } from "./app";

/** The permissions and built-in roles as a fresh installation has them, from backend/internal/perm. */
export const PERMISSIONS: Permission[] = [
  { permission: "workflow.read", words: "See a workflow, its versions and what changed between them" },
  { permission: "workflow.run", words: "Start runs of a workflow and follow their own" },
  { permission: "workflow.edit", words: "Save new versions of a workflow" },
  { permission: "workflow.administer", words: "Share and archive a workflow, and see and cancel everyone's runs of it" },
  { permission: "workflow.create", words: "Create workflows" },
  { permission: "rotor.administer", words: "Administer Rotor" },
];

const role = (key: string, name: string, permissions: string[], more: Partial<Role> = {}): Role => ({
  key,
  name,
  description: `${name} of the test installation.`,
  installationWideOnly: false,
  builtin: true,
  permissions,
  inUse: 0,
  ...more,
});

export const ROLES: Role[] = [
  role(
    "administrator",
    "Administrator",
    PERMISSIONS.map((p) => p.permission),
    { installationWideOnly: true, inUse: 1 },
  ),
  role("workflow_administrator", "Workflow administrator", ["workflow.read", "workflow.run", "workflow.edit", "workflow.administer"]),
  role("editor", "Editor", ["workflow.read", "workflow.run", "workflow.edit"]),
  role("runner", "Runner", ["workflow.read", "workflow.run"], { inUse: 2 }),
  role("author", "Author", ["workflow.create"], { installationWideOnly: true }),
];

/** A role an administrator added, which can go, with its grants. */
export const AUDITOR = role("auditor", "Auditor", ["workflow.read"], { builtin: false, inUse: 3 });

export const EVERYONE: Group = {
  id: "0190a5c4-0000-7000-8000-0000000e0001",
  name: "Everyone",
  description: "",
  source: "local",
  externalRef: "",
  builtin: "everyone",
  memberCount: 4,
  roleCount: 1,
};
export const ENGINEERING: Group = {
  id: "0190a5c4-0000-7000-8000-0000000e0002",
  name: "engineering",
  description: "",
  source: "oidc",
  externalRef: "engineering",
  builtin: "",
  memberCount: 2,
  roleCount: 1,
};
export const RELEASE: Group = {
  id: "0190a5c4-0000-7000-8000-0000000e0003",
  name: "Release managers",
  description: "",
  source: "local",
  externalRef: "",
  builtin: "",
  memberCount: 1,
  roleCount: 0,
};

export const BOB: Person = { id: "0190a5c4-0000-7000-8000-000000000002", name: "Bob Builder", email: "bob@suite.test" };
export const CAROL: Person = { id: "0190a5c4-0000-7000-8000-000000000003", name: "Carol Checker", email: "carol@suite.test" };

/** A grant as the API answers it, made at one moment so rows compare equal. */
export const grant = (id: string, more: Partial<Assignment>): Assignment => ({
  id,
  role: "runner",
  roleName: "Runner",
  createdAt: "2026-11-01T08:00:00Z",
  ...more,
});

export const ADMIN_GRANT = grant("a1", {
  role: "administrator",
  roleName: "Administrator",
  userId: ME.user.id,
  userName: ME.user.name,
  userEmail: ME.user.email,
});

export const user = (person: Person, more: Partial<ManagedUser> = {}): ManagedUser => ({
  ...person,
  isActive: true,
  createdAt: "2026-11-01T08:00:00Z",
  groups: [{ id: EVERYONE.id, name: EVERYONE.name, source: "local", builtin: "everyone" }],
  grants: [],
  ...more,
});

/** What the Access page asks beyond renderAt's defaults, so each tab draws in full. */
export const ACCESS_ANSWERS = {
  "/api/v1/roles": { roles: ROLES },
  "/api/v1/permissions": { permissions: PERMISSIONS },
  "/api/v1/groups": { groups: [EVERYONE, ENGINEERING, RELEASE] },
  [`/api/v1/groups/${ENGINEERING.id}`]: { group: { ...ENGINEERING, members: [{ userId: ME.user.id, name: ME.user.name, email: ME.user.email }] } },
  [`/api/v1/groups/${RELEASE.id}`]: { group: { ...RELEASE, members: [] } },
  "/api/v1/role-assignments": { assignments: [ADMIN_GRANT], total: 1, limit: 50, offset: 0 },
  "/api/v1/people": { people: [] },
  "/api/v1/users": { users: [user({ ...ME.user })], total: 1, limit: 50, offset: 0 },
};
