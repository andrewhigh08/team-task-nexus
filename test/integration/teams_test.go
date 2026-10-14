package integration

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTeamsAndTasksAreInvisibleToOutsiders(t *testing.T) {
	t.Parallel()
	c := newClient(t)
	owner := c.mustRegister(t, "Team Owner")
	team := c.mustCreateTeam(t, owner, "Private team")
	task := c.mustCreateTask(t, owner, team.ID, "Secret task", nil)
	outsider := c.mustRegister(t, "Outsider")
	ownTeam := c.mustCreateTeam(t, outsider, "Outsider team")
	ownTask := c.mustCreateTask(t, outsider, ownTeam.ID, "Own task", nil)

	tests := []struct {
		name    string
		method  string
		path    string
		body    any
		message string
	}{
		{name: "get team", method: http.MethodGet, path: teamPath(team.ID, ""), message: "team not found"},
		{name: "list members", method: http.MethodGet, path: teamPath(team.ID, "/members"), message: "team not found"},
		{
			name: "add member", method: http.MethodPost, path: teamPath(team.ID, "/members"),
			body: map[string]string{"email": outsider.Email}, message: "team not found",
		},
		{name: "remove member", method: http.MethodDelete, path: teamPath(team.ID, "/members/"+strconv.FormatInt(owner.ID, 10)), message: "team not found"},
		{name: "top contributors", method: http.MethodGet, path: teamPath(team.ID, "/top-contributors"), message: "team not found"},
		{name: "orphaned assignees", method: http.MethodGet, path: teamPath(team.ID, "/orphaned-assignees"), message: "team not found"},
		{name: "list tasks of the team", method: http.MethodGet, path: "/api/v1/tasks?team_id=" + strconv.FormatInt(team.ID, 10), message: "team not found"},
		{
			name: "create task in the team", method: http.MethodPost, path: "/api/v1/tasks",
			body: map[string]any{"team_id": team.ID, "title": "Intrusion"}, message: "team not found",
		},
		{name: "get task", method: http.MethodGet, path: taskPath(task.ID, ""), message: "task not found"},
		{name: "update task", method: http.MethodPatch, path: taskPath(task.ID, ""), body: map[string]string{"title": "Hijacked"}, message: "task not found"},
		{name: "task history", method: http.MethodGet, path: taskPath(task.ID, "/history"), message: "task not found"},
		{name: "list comments", method: http.MethodGet, path: taskPath(task.ID, "/comments"), message: "task not found"},
		{name: "add comment", method: http.MethodPost, path: taskPath(task.ID, "/comments"), body: map[string]string{"content": "Hi"}, message: "task not found"},
		// Ids that exist nowhere are answered alike, so outsiders cannot probe for teams and tasks.
		{name: "team that does not exist", method: http.MethodGet, path: teamPath(team.ID+1_000_000, ""), message: "team not found"},
		{name: "task that does not exist", method: http.MethodGet, path: taskPath(task.ID+1_000_000, ""), message: "task not found"},
	}
	// Sequential, so that the checks below see every attempt made.
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errBody := requireError(t, c.do(t, tc.method, tc.path, tc.body, outsider.Token), http.StatusNotFound, "NOT_FOUND")
			assert.Equal(t, tc.message, errBody.Message)
		})
	}

	// The attempts changed nothing.
	assert.Equal(t, "Secret task", c.mustGetTask(t, owner, task.ID).Title)
	assert.Empty(t, c.history(t, owner, task.ID))
	comments := expect[[]commentData](t, c.do(t, http.MethodGet, taskPath(task.ID, "/comments"), nil, owner.Token), http.StatusOK)
	assert.Empty(t, comments)
	members := expect[[]memberData](t, c.do(t, http.MethodGet, teamPath(team.ID, "/members"), nil, owner.Token), http.StatusOK)
	require.Len(t, members, 1)
	assert.Equal(t, owner.ID, members[0].UserID)

	// Lists without a team cover the caller's own teams only.
	teams := expect[[]teamListItem](t, c.do(t, http.MethodGet, "/api/v1/teams", nil, outsider.Token), http.StatusOK)
	require.Len(t, teams, 1)
	assert.Equal(t, ownTeam.ID, teams[0].ID)
	assert.Equal(t, "owner", teams[0].Role)
	assert.Equal(t, []int64{ownTask.ID}, taskIDs(t, c, outsider, "/api/v1/tasks"))
	assert.Equal(t, []int64{task.ID}, taskIDs(t, c, owner, "/api/v1/tasks"))
}

func TestTeamMembershipRules(t *testing.T) {
	t.Parallel()
	c := newClient(t)
	owner := c.mustRegister(t, "Olivia Owner")
	team := c.mustCreateTeam(t, owner, "Membership")
	admin := c.mustJoin(t, owner, team.ID, "Adam Admin", "admin")
	// Admins add plain members; the role defaults to member.
	mia := c.mustJoin(t, admin, team.ID, "Mia Member", "")
	liam := c.mustJoin(t, owner, team.ID, "Liam Member", "member")
	outsider := c.mustRegister(t, "Oscar Outsider")

	members := expect[[]memberData](t, c.do(t, http.MethodGet, teamPath(team.ID, "/members"), nil, mia.Token), http.StatusOK)
	assert.Equal(t, []memberData{
		{UserID: owner.ID, Email: owner.Email, FullName: owner.FullName, Role: "owner"},
		{UserID: admin.ID, Email: admin.Email, FullName: admin.FullName, Role: "admin"},
		{UserID: liam.ID, Email: liam.Email, FullName: liam.FullName, Role: "member"},
		{UserID: mia.ID, Email: mia.Email, FullName: mia.FullName, Role: "member"},
	}, members, "ordered by role, then by name")

	adds := []struct {
		name    string
		caller  account
		email   string
		role    string
		status  int
		code    string
		message string
	}{
		{name: "admin adds an admin", caller: admin, email: outsider.Email, role: "admin", status: http.StatusForbidden, code: "FORBIDDEN", message: "only the team owner may add admins"},
		{name: "member adds a member", caller: mia, email: outsider.Email, status: http.StatusForbidden, code: "FORBIDDEN", message: "only the team owner and admins may do this"},
		{name: "unknown email", caller: owner, email: "nobody-" + outsider.Email, status: http.StatusNotFound, code: "NOT_FOUND", message: "user not found"},
		{name: "already a member", caller: owner, email: mia.Email, status: http.StatusConflict, code: "CONFLICT", message: "team member with this user_id already exists"},
		{name: "second owner", caller: owner, email: outsider.Email, role: "owner", status: http.StatusBadRequest, code: "VALIDATION_ERROR", message: "invalid input"},
	}
	for _, tc := range adds {
		t.Run(tc.name, func(t *testing.T) {
			errBody := requireError(t, c.addMember(t, tc.caller, team.ID, tc.email, tc.role), tc.status, tc.code)
			assert.Equal(t, tc.message, errBody.Message)
		})
	}

	removals := []struct {
		name    string
		caller  account
		target  int64
		status  int
		code    string
		message string
	}{
		{name: "member removes an admin", caller: mia, target: admin.ID, status: http.StatusForbidden, code: "FORBIDDEN", message: "only the team owner and admins may do this"},
		{name: "member removes a member", caller: mia, target: liam.ID, status: http.StatusForbidden, code: "FORBIDDEN", message: "only the team owner and admins may do this"},
		{name: "admin removes the owner", caller: admin, target: owner.ID, status: http.StatusForbidden, code: "FORBIDDEN", message: "admins may remove only members"},
		{name: "owner leaves", caller: owner, target: owner.ID, status: http.StatusForbidden, code: "FORBIDDEN", message: "the team owner cannot leave the team"},
		{name: "not a member", caller: owner, target: outsider.ID, status: http.StatusNotFound, code: "NOT_FOUND", message: "team member not found"},
	}
	for _, tc := range removals {
		t.Run(tc.name, func(t *testing.T) {
			errBody := requireError(t, c.removeMember(t, tc.caller, team.ID, tc.target), tc.status, tc.code)
			assert.Equal(t, tc.message, errBody.Message)
		})
	}

	// A member leaves on their own and loses access at once; an admin removes a member.
	requireStatus(t, c.removeMember(t, liam, team.ID, liam.ID), http.StatusNoContent)
	requireError(t, c.do(t, http.MethodGet, teamPath(team.ID, ""), nil, liam.Token), http.StatusNotFound, "NOT_FOUND")
	requireStatus(t, c.removeMember(t, admin, team.ID, mia.ID), http.StatusNoContent)
	requireStatus(t, c.removeMember(t, owner, team.ID, admin.ID), http.StatusNoContent)

	members = expect[[]memberData](t, c.do(t, http.MethodGet, teamPath(team.ID, "/members"), nil, owner.Token), http.StatusOK)
	require.Len(t, members, 1)
	assert.Equal(t, owner.ID, members[0].UserID)
}

func TestRemovedMembersLeaveOrphanedAssignments(t *testing.T) {
	t.Parallel()
	c := newClient(t)
	owner := c.mustRegister(t, "Owner")
	team := c.mustCreateTeam(t, owner, "Orphans")
	admin := c.mustJoin(t, owner, team.ID, "Admin", "admin")
	member := c.mustJoin(t, owner, team.ID, "Member", "member")
	leaver := c.mustJoin(t, owner, team.ID, "Leaver", "member")

	byMember := c.mustCreateTask(t, owner, team.ID, "Member's task", map[string]any{"assignee_id": member.ID})
	byLeaver := c.mustCreateTask(t, owner, team.ID, "Leaver's task", map[string]any{"assignee_id": leaver.ID})
	c.mustCreateTask(t, owner, team.ID, "Owner's task", map[string]any{"assignee_id": owner.ID})
	c.mustCreateTask(t, owner, team.ID, "Nobody's task", nil)

	orphanedPath := teamPath(team.ID, "/orphaned-assignees")
	assert.Empty(t, expect[[]orphanData](t, c.do(t, http.MethodGet, orphanedPath, nil, owner.Token), http.StatusOK))
	forbidden := requireError(t, c.do(t, http.MethodGet, orphanedPath, nil, member.Token), http.StatusForbidden, "FORBIDDEN")
	assert.Equal(t, "only the team owner and admins may do this", forbidden.Message)

	requireStatus(t, c.removeMember(t, admin, team.ID, member.ID), http.StatusNoContent)
	requireStatus(t, c.removeMember(t, leaver, team.ID, leaver.ID), http.StatusNoContent)

	// The assignments are kept, so that managers can see and reassign them.
	assert.Equal(t, &member.ID, c.mustGetTask(t, owner, byMember.ID).AssigneeID)
	want := []orphanData{
		{TaskID: byMember.ID, TaskTitle: "Member's task", AssigneeID: member.ID, AssigneeName: "Member"},
		{TaskID: byLeaver.ID, TaskTitle: "Leaver's task", AssigneeID: leaver.ID, AssigneeName: "Leaver"},
	}
	for _, manager := range []account{owner, admin} {
		assert.Equal(t, want, expect[[]orphanData](t, c.do(t, http.MethodGet, orphanedPath, nil, manager.Token), http.StatusOK))
	}

	// Reassigning a task within the team resolves it.
	c.mustPatchTask(t, admin, byLeaver.ID, map[string]any{"assignee_id": admin.ID})
	assert.Equal(t, want[:1], expect[[]orphanData](t, c.do(t, http.MethodGet, orphanedPath, nil, owner.Token), http.StatusOK))
}
