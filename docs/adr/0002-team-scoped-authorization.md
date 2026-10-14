# 0002. Team membership decides visibility, roles decide actions

Status: accepted

## Context

Everything the API serves belongs to a team: tasks with their history and comments, member lists,
analytics. The first revision answered 403 "user is not a member of this team", which confirms to
anyone that a team id exists; listed tasks without a `team_id` filter across every team in the
database; and reported the tasks of former members of all teams to any user.

## Decision

- Membership decides visibility. A team the caller is not a member of, or a task of such a team, is
  answered with 404 `team not found` or `task not found`, exactly like an id that does not exist.
  The same holds for everything reached through a task (history, comments) or a team (members, top
  contributors, orphaned assignments), and for `team_id` in task creation and in the task list.
- Roles decide actions, and only for members. A member lacking the role an operation needs gets
  403 `FORBIDDEN`: it already knows the team exists. The owner and admins add and remove members and
  see the orphaned assignments; only the owner adds admins; an admin removes plain members only; any
  member but the owner may remove themselves to leave the team; the owner cannot leave, since
  ownership cannot be transferred.
- The checks live in the service layer: the membership and manager checks in
  `internal/service/access.go`, the member-management rules in `TeamService` (`AddMember`,
  `checkRemoval`). They read the membership from MySQL on every request. The token carries the user id
  and nothing else.
- Every list query is scoped in SQL rather than filtered afterwards. The task list always starts with
  `team_id IN (SELECT team_id FROM team_members WHERE user_id = ?)`, whatever other filters are
  given; the team list and the team statistics join `team_members` on the caller.
- A task may only be assigned to a member of its team, on creation and on update
  (400 `VALIDATION_ERROR` naming `assignee_id`).
- Removing a member keeps their assignments. `GET /api/v1/teams/{id}/orphaned-assignees` lists the
  tasks of the team whose assignee is no longer a member, to the owner and admins only, so that they
  can reassign them.

## Consequences

- Ids cannot be probed: an outsider cannot tell a foreign team or task from a missing one. Members
  of a team learn which operations they are not allowed, which is information they already have.
- Every request on a team or a task costs a membership lookup on the unique `(team_id, user_id)`
  index. In return, a role change or a removal takes effect on the next request, without waiting for
  any token to expire.
- There are no per-task permissions: any member may change any task of the team, reassign it and
  move it to any status.
- Request bodies and query parameters are validated before the membership check, so an outsider
  sending an invalid body gets 400 rather than 404. The answer does not depend on the id and reveals
  nothing about it.
- The assignee check is a plain read without a lock. A member removed at the same moment as a task
  is assigned to them can end up as its assignee; the orphaned assignments report shows that task
  like any other.
