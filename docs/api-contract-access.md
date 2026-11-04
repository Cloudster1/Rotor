# API contract for access: roles, groups and grants

Access is held as roles, groups and grants (#202, `docs/architecture.md`),
and Admin's People and access section keeps them (#203): the Access page with
its Roles, Matrix and Groups tabs, the Users page, and each workflow's grants,
which replace its sharing. Single sign-on and join requests follow in #204.

Conventions are M2's: paths under `/api/v1`; refusals are sentences saying
what to do; a page of a listing takes `limit` (50, at most 200) and `offset`
and answers `total`.

## Migrations

| Version | Issue | What |
|---|---|---|
| 01300, 01310 | #202 | `role`, `user_group`, `group_member`, `role_assignment`, `actor_can()` and the policies |
| 01320 | #203 | `app_user_keeps_an_administrator`: the last active administrator is never switched off |

## Endpoints

"Admin" is `rotor.administer` over the installation, in a browser session; an
assistant never administers. "Grants somewhere" is holding
`workflow.administer` on any workflow, which every Administrator and every
workflow's creator does.

| Method and path | Who | What |
|---|---|---|
| `GET /access/me` | signed in | your grants and the permissions they give (#202) |
| `GET /roles` | signed in | every role, with `permissions`, `builtin`, `installationWideOnly` and `inUse` (grants of it) |
| `GET /permissions` | signed in | every permission with the words the matrix shows, in the matrix's order |
| `POST /roles` | admin | `{key?, name, description?, installationWideOnly?, permissions}`; the key is made from the name |
| `PATCH /roles/{roleKey}` | admin | `{name?, description?, installationWideOnly?, permissions?}`; `permissions` is the whole list |
| `DELETE /roles/{roleKey}` | admin | deletes a role and every grant of it; a built-in one is refused with 409 |
| `GET /groups` | grants somewhere | Everyone, then the realm's groups, then those made by hand, with `memberCount` and `roleCount` |
| `POST /groups` | admin | `{name, description?, externalRef?}`; an `externalRef` makes a realm group matching that claim value |
| `GET /groups/{groupID}` | admin | one group with its `members` |
| `DELETE /groups/{groupID}` | admin | deletes a group and its grants; Everyone is refused with 409 |
| `POST /groups/{groupID}/members` | admin | `{userId}`; Everyone and realm groups are refused with 409 |
| `DELETE /groups/{groupID}/members/{userID}` | admin | the same refusals |
| `GET /role-assignments` | admin | `workflow` (what answers there, installation-wide included), `q`, `role`, a page |
| `POST /role-assignments` | admin | `{role, workflowId?, userId or groupId}`; the same grant twice answers the one grant |
| `DELETE /role-assignments/{assignmentID}` | admin | the last Administrator grant is refused with 409 |
| `GET /workflows/{workflowID}/grants` | sees the workflow | the grants made on it alone |
| `POST /workflows/{workflowID}/grants` | administers the workflow | `{role, userId or groupId}`; an installation-only role is refused with 422 |
| `DELETE /workflows/{workflowID}/grants/{grantID}` | administers the workflow | a grant of another workflow answers 404 |
| `GET /people` | grants somewhere | `q`, `limit` (10, at most 50): the people let in, by name and address |
| `GET /users` | admin | every person with `isActive`, `groups` and `grants` (with the `group` they come through), a page |
| `PATCH /users/{userID}` | admin | `{isActive}`; your own account and the last active administrator are refused with 409 |
| `PUT /workflows/{workflowID}/sharing` | administers the workflow | deprecated: still writes Runner grants (#202) |

A workflow one may not see answers 404 on its grants, as on the workflow
itself; one seen but not administered answers 403 to a change.

## Who reads what

`rotor_app` reads only its own `app_user` row and its own memberships, so
names, members and the Users page are read as `rotor_admin`, each after the
same transaction asks whether the acting person may (see Architecture). Every
write of a role, a group or a grant goes through `rotor_app`, so the policies
decide; a membership is written as `rotor_admin`, since `rotor_app` writes
none, after the same check.

The audit log records `role.defined`, `role.redefined`, `role.granted`,
`role.revoked`, `role.removed`, `group.created`, `group.deleted`,
`group.member_added`, `group.member_removed`, `user.switched_off` and
`user.switched_on`.
