package domain

import "time"

// TeamRole is the role of a user in a team.
type TeamRole string

// Team roles. Every team has exactly one owner, its creator; ownership cannot be transferred.
const (
	TeamRoleOwner  TeamRole = "owner"
	TeamRoleAdmin  TeamRole = "admin"
	TeamRoleMember TeamRole = "member"
)

// Valid reports whether r is one of the defined roles.
func (r TeamRole) Valid() bool {
	switch r {
	case TeamRoleOwner, TeamRoleAdmin, TeamRoleMember:
		return true
	}
	return false
}

// CanManageMembers reports whether r may add and remove members and see the team's
// orphaned assignments.
func (r TeamRole) CanManageMembers() bool {
	return r == TeamRoleOwner || r == TeamRoleAdmin
}

// Team is a group of users that share tasks.
type Team struct {
	ID          int64
	Name        string
	Description string
	OwnerID     int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// TeamSummary is a team as listed for one of its members: the team plus that member's role.
type TeamSummary struct {
	Team
	Role TeamRole
}

// TeamMember is the membership of a user in a team.
type TeamMember struct {
	TeamID int64
	UserID int64
	Role   TeamRole
}

// MemberProfile is a team member together with the account details other members may see.
type MemberProfile struct {
	UserID   int64
	Email    string
	FullName string
	Role     TeamRole
}

// TeamStats summarizes the activity of a team. DoneLast7Days counts the tasks completed in the
// seven days before the statistics were taken.
type TeamStats struct {
	TeamID        int64
	Name          string
	MemberCount   int
	OpenTasks     int
	DoneLast7Days int
}

// TopContributor is a team member ranked by the number of tasks they completed. Members with
// the same count share a rank, and the next rank skips accordingly (1, 1, 3).
type TopContributor struct {
	UserID         int64
	FullName       string
	TasksCompleted int
	Rank           int
}

// OrphanedAssignment is a task assigned to a user who has since left the task's team.
type OrphanedAssignment struct {
	TaskID       int64
	TaskTitle    string
	AssigneeID   int64
	AssigneeName string
}
