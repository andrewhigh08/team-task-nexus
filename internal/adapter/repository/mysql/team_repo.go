package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"github.com/shalfey088/team-task-nexus/internal/domain"
	"github.com/shalfey088/team-task-nexus/internal/pkg/apperror"
)

type TeamRepo struct {
	db *sqlx.DB
}

func NewTeamRepo(db *sqlx.DB) *TeamRepo {
	return &TeamRepo{db: db}
}

func (r *TeamRepo) Create(ctx context.Context, team *domain.Team) (int64, error) {
	q := getQuerier(ctx, r.db)
	result, err := q.ExecContext(ctx,
		"INSERT INTO teams (name, description, owner_id) VALUES (?, ?, ?)",
		team.Name, team.Description, team.OwnerID,
	)
	if err != nil {
		return 0, apperror.Internal("create team", err)
	}
	return result.LastInsertId()
}

func (r *TeamRepo) GetByID(ctx context.Context, id int64) (*domain.Team, error) {
	q := getQuerier(ctx, r.db)
	var team domain.Team
	err := q.GetContext(ctx, &team, "SELECT * FROM teams WHERE id = ?", id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperror.NotFound("team not found")
		}
		return nil, apperror.Internal("get team", err)
	}
	return &team, nil
}

func (r *TeamRepo) ListByUserID(ctx context.Context, userID int64) ([]domain.Team, error) {
	q := getQuerier(ctx, r.db)
	var teams []domain.Team
	err := q.SelectContext(ctx, &teams,
		`SELECT t.* FROM teams t
		 JOIN team_members tm ON tm.team_id = t.id
		 WHERE tm.user_id = ?
		 ORDER BY t.created_at DESC`, userID,
	)
	if err != nil {
		return nil, apperror.Internal("list teams", err)
	}
	return teams, nil
}

func (r *TeamRepo) AddMember(ctx context.Context, member *domain.TeamMember) error {
	q := getQuerier(ctx, r.db)
	_, err := q.ExecContext(ctx,
		"INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, ?)",
		member.TeamID, member.UserID, member.Role,
	)
	if err != nil {
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return apperror.New(409, "user is already a member of this team")
		}
		return apperror.Internal("add team member", err)
	}
	return nil
}

func (r *TeamRepo) GetMember(ctx context.Context, teamID, userID int64) (*domain.TeamMember, error) {
	q := getQuerier(ctx, r.db)
	var member domain.TeamMember
	err := q.GetContext(ctx, &member,
		"SELECT * FROM team_members WHERE team_id = ? AND user_id = ?",
		teamID, userID,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, apperror.Internal("get team member", err)
	}
	return &member, nil
}

func (r *TeamRepo) GetStats(ctx context.Context, userID int64) ([]domain.TeamStats, error) {
	q := getQuerier(ctx, r.db)
	var stats []domain.TeamStats
	err := q.SelectContext(ctx, &stats, `
		SELECT t.id, t.name,
			COUNT(DISTINCT tm.user_id) AS member_count,
			COUNT(DISTINCT CASE WHEN tk.status='done' AND tk.updated_at >= NOW() - INTERVAL 7 DAY THEN tk.id END) AS done_last_7d
		FROM teams t
		LEFT JOIN team_members tm ON tm.team_id = t.id
		LEFT JOIN tasks tk ON tk.team_id = t.id
		WHERE t.id IN (SELECT team_id FROM team_members WHERE user_id = ?)
		GROUP BY t.id, t.name`, userID,
	)
	if err != nil {
		return nil, apperror.Internal("get team stats", err)
	}
	return stats, nil
}

func (r *TeamRepo) GetTopContributors(ctx context.Context, teamID int64) ([]domain.TopContributor, error) {
	q := getQuerier(ctx, r.db)
	var contributors []domain.TopContributor
	err := q.SelectContext(ctx, &contributors, `
		SELECT * FROM (
			SELECT u.id, u.full_name, tm.team_id, te.name AS team_name,
				COUNT(tk.id) AS tasks_created,
				ROW_NUMBER() OVER (PARTITION BY tm.team_id ORDER BY COUNT(tk.id) DESC) AS rn
			FROM users u
			JOIN team_members tm ON tm.user_id = u.id
			LEFT JOIN tasks tk ON tk.creator_id = u.id AND tk.team_id = tm.team_id
				AND tk.created_at >= NOW() - INTERVAL 30 DAY
			JOIN teams te ON te.id = tm.team_id
			WHERE tm.team_id = ?
			GROUP BY u.id, u.full_name, tm.team_id, te.name
		) ranked WHERE rn <= 3`, teamID,
	)
	if err != nil {
		return nil, apperror.Internal("get top contributors", err)
	}
	return contributors, nil
}
