package domain

import "time"

type TeamRole string

const (
	TeamRoleOwner  TeamRole = "owner"
	TeamRoleAdmin  TeamRole = "admin"
	TeamRoleMember TeamRole = "member"
)

type Team struct {
	ID          int64     `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	Description string    `json:"description" db:"description"`
	OwnerID     int64     `json:"owner_id" db:"owner_id"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

type TeamMember struct {
	TeamID int64    `json:"team_id" db:"team_id"`
	UserID int64    `json:"user_id" db:"user_id"`
	Role   TeamRole `json:"role" db:"role"`
}

type CreateTeamRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type InviteRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

type TeamStats struct {
	ID          int64  `json:"id" db:"id"`
	Name        string `json:"name" db:"name"`
	MemberCount int    `json:"member_count" db:"member_count"`
	DoneLast7D  int    `json:"done_last_7d" db:"done_last_7d"`
}

type TopContributor struct {
	UserID       int64  `json:"user_id" db:"id"`
	FullName     string `json:"full_name" db:"full_name"`
	TeamID       int64  `json:"team_id" db:"team_id"`
	TeamName     string `json:"team_name" db:"team_name"`
	TasksCreated int    `json:"tasks_created" db:"tasks_created"`
	Rank         int    `json:"rank" db:"rn"`
}
