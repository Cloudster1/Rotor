# API contract for access: roles, groups and grants

Access is held as roles, groups and grants (#202, `docs/architecture.md`),
and Admin's People and access section keeps them (#203): the Access page with
its Roles, Matrix and Groups tabs, the Users page, and each workflow's grants,
which replace its sharing. Letting people in and single sign-on followed in
#204, and bootstrap members in #171. #205 retired sharing and the role word.

Conventions are M2's: paths under `/api/v1`; refusals are sentences saying
what to do; a page of a listing takes `limit` (50, at most 200) and `offset`
and answers `total`.

## Migrations

| Version | Issue | What |
|---|---|---|
| 01300, 01310 | #202 | `role`, `user_group`, `group_member`, `role_assignment`, `actor_can()` and the policies |
| 01320 | #203 | `app_user_keeps_an_administrator`: the last active administrator is never switched off |
| 01330 | #204 | `app_user.standing` (everybody already known let in), `join_request`, `oidc_provider`; `actor_can()` and the Everyone guards ask `standing` |
| 01340 | #205 | drops `workflow.sharing`, `workflow.shared_groups`, their trigger and `app_user.role`; `workflow_change_guard` asks `workflow.administer` for archiving and restoring. Down rebuilds sharing from runner grants to groups and Everyone, and the role from the Administrator grant |

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
| `PUT /workflows/{workflowID}/sharing` | | removed in #205; grant on `/workflows/{workflowID}/grants` |
| `GET /users/requests` | admin | who signed in and waits to be let in, oldest first: `userId`, `email`, `name`, `requestedAt` (#204) |
| `POST /users/requests/{userID}/admit` | admin | `{role?}`, `member` (the default) or `admin`, which also grants Administrator; answers the `user`; somebody not waiting is 404 |
| `DELETE /users/requests/{userID}` | admin | turns them away; they may ask again by signing in; somebody not waiting is 404 |
| `GET /oidc-provider` | admin | `provider` (`issuer`, `clientId`, `hasSecret`, `groupsClaim`, `admitByGroup`, `enabled`, `changedBy`, `updatedAt`) or null, `redirectUrl`, `canTurnOff` |
| `PUT /oidc-provider` | admin | `{issuer, clientId, clientSecret?, groupsClaim?, admitByGroup?, enabled?}`; a blank secret keeps the one held; 422 with `fields` for a provider that could not work, 409 `only_way_in` for turning it off |

A workflow one may not see answers 404 on its grants, as on the workflow
itself; one seen but not administered answers 403 to a change.

`/users` and `/people` list the people let in alone; somebody waiting is a
request, and somebody turned away is in neither. The client secret is never in
a response, the database or the audit log: it goes to OpenBao, and the row and
`oidc.saved` say only whether one was set or kept.

## Who reads what

`rotor_app` reads only its own `app_user` row and its own memberships, so
names, members and the Users page are read as `rotor_admin`, each after the
same transaction asks whether the acting person may (see Architecture). Every
write of a role, a group or a grant goes through `rotor_app`, so the policies
decide; a membership is written as `rotor_admin`, since `rotor_app` writes
none, after the same check. Join requests and `standing` are read and written
as `rotor_admin` alone, after the same check for Admin's Users page: `rotor_app`
holds no privilege on `join_request` and writes no `app_user` row. Single
sign-on is read and saved through `rotor_app`, whose policies let only an
administrator of Rotor, in a browser, see or change the row, and nobody delete
it; sign-in reads it as `rotor_admin`.

The audit log records `role.defined`, `role.redefined`, `role.granted`,
`role.revoked`, `role.removed`, `group.created`, `group.deleted`,
`group.member_added`, `group.member_removed`, `user.switched_off`,
`user.switched_on`, `member.admitted` (with `via`: `users_page`, `sign_in` and
the `group`, or `bootstrap`), `member.declined` and `oidc.saved`.
