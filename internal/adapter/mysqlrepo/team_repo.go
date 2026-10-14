package mysqlrepo

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
)

const teamColumns = "t.id, t.name, t.description, t.owner_id, t.created_at, t.updated_at"

type teamRow struct {
	ID          int64     `db:"id"`
	Name        string    `db:"name"`
	Description string    `db:"description"`
	OwnerID     int64     `db:"owner_id"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

type teamSummaryRow struct {
	teamRow
	Role string `db:"role"`
}

type memberRow struct {
	TeamID int64  `db:"team_id"`
	UserID int64  `db:"user_id"`
	Role   string `db:"role"`
}

type memberProfileRow struct {
	UserID   int64  `db:"user_id"`
	Email    string `db:"email"`
	FullName string `db:"full_name"`
	Role     string `db:"role"`
}

type teamStatsRow struct {
	TeamID        int64  `db:"team_id"`
	Name          string `db:"name"`
	MemberCount   int    `db:"member_count"`
	OpenTasks     int    `db:"open_tasks"`
	DoneLast7Days int    `db:"done_last_7_days"`
}

type contributorRow struct {
	UserID         int64  `db:"user_id"`
	FullName       string `db:"full_name"`
	TasksCompleted int    `db:"tasks_completed"`
	Rank           int    `db:"contributor_rank"`
}

// TeamRepo stores teams and their members.
type TeamRepo struct {
	db *sqlx.DB
}

// NewTeamRepo returns a TeamRepo on db.
func NewTeamRepo(db *sqlx.DB) *TeamRepo {
	return &TeamRepo{db: db}
}

// Create inserts team and returns its id.
func (r *TeamRepo) Create(ctx context.Context, team *domain.Team) (int64, error) {
	result, err := getQuerier(ctx, r.db).ExecContext(ctx,
		"INSERT INTO teams (name, description, owner_id) VALUES (?, ?, ?)",
		team.Name, team.Description, team.OwnerID,
	)
	if err != nil {
		return 0, apperror.Internal("create team", err)
	}
	return insertedID(result, "create team")
}

// GetByID returns the team with the given id.
func (r *TeamRepo) GetByID(ctx context.Context, id int64) (*domain.Team, error) {
	var row teamRow
	err := getQuerier(ctx, r.db).GetContext(ctx, &row, "SELECT "+teamColumns+" FROM teams t WHERE t.id = ?", id)
	if err != nil {
		return nil, lookupError(err, "get team", "team", id)
	}
	team := domain.Team(row)
	return &team, nil
}

// ListForUser returns the teams the user is a member of, with the user's role, newest first.
func (r *TeamRepo) ListForUser(ctx context.Context, userID int64) ([]domain.TeamSummary, error) {
	var rows []teamSummaryRow
	err := getQuerier(ctx, r.db).SelectContext(ctx, &rows, `
		SELECT `+teamColumns+`, tm.role
		FROM teams t
		JOIN team_members tm ON tm.team_id = t.id
		WHERE tm.user_id = ?
		ORDER BY t.created_at DESC, t.id DESC`, userID,
	)
	if err != nil {
		return nil, apperror.Internal("list teams", err)
	}
	teams := make([]domain.TeamSummary, 0, len(rows))
	for _, row := range rows {
		teams = append(teams, domain.TeamSummary{Team: domain.Team(row.teamRow), Role: domain.TeamRole(row.Role)})
	}
	return teams, nil
}

// AddMember inserts member.
func (r *TeamRepo) AddMember(ctx context.Context, member domain.TeamMember) error {
	_, err := getQuerier(ctx, r.db).ExecContext(ctx,
		"INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, ?)",
		member.TeamID, member.UserID, member.Role,
	)
	if err != nil {
		if isDuplicateEntry(err) {
			return apperror.Conflict("team member", "user_id", member.UserID)
		}
		return apperror.Internal("add team member", err)
	}
	return nil
}

// RemoveMember deletes the membership of the user in the team.
func (r *TeamRepo) RemoveMember(ctx context.Context, teamID, userID int64) error {
	result, err := getQuerier(ctx, r.db).ExecContext(ctx,
		"DELETE FROM team_members WHERE team_id = ? AND user_id = ?", teamID, userID,
	)
	if err != nil {
		return apperror.Internal("remove team member", err)
	}
	removed, err := affectedOne(result, "remove team member")
	if err != nil {
		return err
	}
	if !removed {
		return apperror.NotFound("team member", userID)
	}
	return nil
}

// GetMember returns the membership of the user in the team.
func (r *TeamRepo) GetMember(ctx context.Context, teamID, userID int64) (*domain.TeamMember, error) {
	var row memberRow
	err := getQuerier(ctx, r.db).GetContext(ctx, &row,
		"SELECT team_id, user_id, role FROM team_members WHERE team_id = ? AND user_id = ?",
		teamID, userID,
	)
	if err != nil {
		return nil, lookupError(err, "get team member", "team member", userID)
	}
	return &domain.TeamMember{TeamID: row.TeamID, UserID: row.UserID, Role: domain.TeamRole(row.Role)}, nil
}

// ListMembers returns the members of the team: the owner, then admins, then members, each
// group ordered by name.
func (r *TeamRepo) ListMembers(ctx context.Context, teamID int64) ([]domain.MemberProfile, error) {
	var rows []memberProfileRow
	err := getQuerier(ctx, r.db).SelectContext(ctx, &rows, `
		SELECT u.id AS user_id, u.email, u.full_name, tm.role
		FROM team_members tm
		JOIN users u ON u.id = tm.user_id
		WHERE tm.team_id = ?
		ORDER BY FIELD(tm.role, 'owner', 'admin', 'member'), u.full_name, u.id`, teamID,
	)
	if err != nil {
		return nil, apperror.Internal("list team members", err)
	}
	members := make([]domain.MemberProfile, 0, len(rows))
	for _, row := range rows {
		members = append(members, domain.MemberProfile{
			UserID:   row.UserID,
			Email:    row.Email,
			FullName: row.FullName,
			Role:     domain.TeamRole(row.Role),
		})
	}
	return members, nil
}

// Stats summarizes every team the user is a member of. Each figure is a correlated subquery
// of its own: joining members and tasks in one query would multiply the rows of one by the
// other.
func (r *TeamRepo) Stats(ctx context.Context, userID int64, since time.Time) ([]domain.TeamStats, error) {
	var rows []teamStatsRow
	err := getQuerier(ctx, r.db).SelectContext(ctx, &rows, `
		SELECT t.id AS team_id, t.name,
			(SELECT COUNT(*) FROM team_members m WHERE m.team_id = t.id) AS member_count,
			(SELECT COUNT(*) FROM tasks k WHERE k.team_id = t.id AND k.status <> 'done') AS open_tasks,
			(SELECT COUNT(*) FROM tasks k WHERE k.team_id = t.id AND k.completed_at >= ?) AS done_last_7_days
		FROM teams t
		JOIN team_members tm ON tm.team_id = t.id
		WHERE tm.user_id = ?
		ORDER BY t.name, t.id`, since, userID,
	)
	if err != nil {
		return nil, apperror.Internal("get team stats", err)
	}
	stats := make([]domain.TeamStats, 0, len(rows))
	for _, row := range rows {
		stats = append(stats, domain.TeamStats(row))
	}
	return stats, nil
}

// TopContributors ranks the current members of the team by the tasks assigned to them that
// were completed at or after since, and returns those ranked limit or better. RANK gives
// equal counts the same rank, so ties at the cut-off are all included.
func (r *TeamRepo) TopContributors(ctx context.Context, teamID int64, since time.Time, limit int) ([]domain.TopContributor, error) {
	var rows []contributorRow
	err := getQuerier(ctx, r.db).SelectContext(ctx, &rows, `
		WITH completed AS (
			SELECT k.assignee_id AS user_id, COUNT(*) AS tasks_completed
			FROM tasks k
			JOIN team_members m ON m.team_id = k.team_id AND m.user_id = k.assignee_id
			WHERE k.team_id = ? AND k.completed_at >= ?
			GROUP BY k.assignee_id
		), ranked AS (
			SELECT c.user_id, u.full_name, c.tasks_completed,
				RANK() OVER (ORDER BY c.tasks_completed DESC) AS contributor_rank
			FROM completed c
			JOIN users u ON u.id = c.user_id
		)
		SELECT user_id, full_name, tasks_completed, contributor_rank
		FROM ranked
		WHERE contributor_rank <= ?
		ORDER BY contributor_rank, full_name, user_id`, teamID, since, limit,
	)
	if err != nil {
		return nil, apperror.Internal("get top contributors", err)
	}
	contributors := make([]domain.TopContributor, 0, len(rows))
	for _, row := range rows {
		contributors = append(contributors, domain.TopContributor(row))
	}
	return contributors, nil
}
