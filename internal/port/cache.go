package port

import (
	"context"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
)

// TaskListCache caches pages of a team's task list under a version of that team's tasks.
// Its keys do not include the viewer, so only queries narrowed to one team, whose members all
// see the same tasks, may be cached.
//
// A reader takes the version before it queries the database and stores the page under that
// version, while every write to the team's tasks moves the version on after it commits. A page
// read before a write can therefore only be stored under a version that is already outdated,
// and is never served.
type TaskListCache interface {
	// Version returns the current version of the team's task list; zero before the first
	// invalidation.
	Version(ctx context.Context, teamID int64) (int64, error)
	// Get returns the page of q cached under version ver of the team's task list and whether
	// there was one.
	Get(ctx context.Context, teamID, ver int64, q domain.TaskQuery) (*domain.TaskPage, bool, error)
	// Set caches page as the result of q under version ver of the team's task list.
	Set(ctx context.Context, teamID, ver int64, q domain.TaskQuery, page *domain.TaskPage) error
	// Invalidate moves the team's task list to a new version, which retires every page cached
	// under the previous ones at once.
	Invalidate(ctx context.Context, teamID int64) error
}
