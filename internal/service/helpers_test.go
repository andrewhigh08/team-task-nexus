package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
	"github.com/andrewhigh08/team-task-nexus/test/mocks"
)

// Ids of the team and task the team, task and comment service tests work on, and of users
// holding each role in that team; outsiderUser belongs to another team.
const (
	testTeamID   int64 = 7
	testTaskID   int64 = 11
	ownerUser    int64 = 1
	adminUser    int64 = 2
	memberUser   int64 = 3
	outsiderUser int64 = 4
)

// assertAppError asserts that err is or wraps an AppError with the given code and returns it.
func assertAppError(t *testing.T, err error, code string) *apperror.AppError {
	t.Helper()
	appErr, ok := apperror.AsAppError(err)
	require.True(t, ok, "%v is not an AppError", err)
	assert.Equal(t, code, appErr.Code)
	return appErr
}

// requireNotFound asserts that err is the NOT_FOUND of a missing resource with the given id.
// Outsiders must get exactly this error, so that it tells them nothing.
func requireNotFound(t *testing.T, err error, resource string, id any) {
	t.Helper()
	appErr := assertAppError(t, err, apperror.CodeNotFound)
	assert.Equal(t, resource+" not found", appErr.Message)
	assert.Equal(t, map[string]any{"resource": resource, "id": id}, appErr.Details)
}

// txMarker is the context key under which the fake transaction manager tags the context it
// passes to the transactional function.
type txMarker struct{}

// ctxMatcher matches a context by whether it carries the fake transaction.
type ctxMatcher bool

func (m ctxMatcher) Matches(x any) bool {
	ctx, ok := x.(context.Context)
	if !ok {
		return false
	}
	tagged, _ := ctx.Value(txMarker{}).(bool)
	return tagged == bool(m)
}

func (m ctxMatcher) String() string {
	if m {
		return "a context inside the transaction"
	}
	return "a context outside any transaction"
}

// inTx and afterTx match calls made inside the transaction and after it ended.
const (
	inTx    = ctxMatcher(true)
	afterTx = ctxMatcher(false)
)

// fixture holds gomock collaborators for the team, task and comment services.
type fixture struct {
	teams    *mocks.MockTeamRepository
	users    *mocks.MockUserRepository
	tasks    *mocks.MockTaskRepository
	history  *mocks.MockTaskHistoryRepository
	comments *mocks.MockCommentRepository
	cache    *mocks.MockTaskListCache
	outbox   *mocks.MockOutboxRepository
	tx       *mocks.MockTxManager
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	return &fixture{
		teams:    mocks.NewMockTeamRepository(ctrl),
		users:    mocks.NewMockUserRepository(ctrl),
		tasks:    mocks.NewMockTaskRepository(ctrl),
		history:  mocks.NewMockTaskHistoryRepository(ctrl),
		comments: mocks.NewMockCommentRepository(ctrl),
		cache:    mocks.NewMockTaskListCache(ctrl),
		outbox:   mocks.NewMockOutboxRepository(ctrl),
		tx:       mocks.NewMockTxManager(ctrl),
	}
}

// txResult is what a transactional function returned: the outcome a real manager would
// commit or roll back.
type txResult struct {
	err error
}

// expectTx expects one transaction and runs its function with a tagged context, recording
// the result.
func (f *fixture) expectTx() *txResult {
	result := &txResult{}
	f.tx.EXPECT().WithinTx(afterTx, gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error {
			result.err = fn(context.WithValue(ctx, txMarker{}, true))
			return result.err
		})
	return result
}

// expectRole answers the membership lookup of userID in the test team, once, with role; an
// empty role means the user is not a member.
func (f *fixture) expectRole(ctx gomock.Matcher, userID int64, role domain.TeamRole) {
	call := f.teams.EXPECT().GetMember(ctx, testTeamID, userID)
	if role == "" {
		call.Return(nil, apperror.NotFound("team member", userID))
		return
	}
	call.Return(&domain.TeamMember{TeamID: testTeamID, UserID: userID, Role: role}, nil)
}

// expectEvent expects one event of type typ to be added to the outbox inside the transaction
// and checks that its payload is the JSON document payload.
func (f *fixture) expectEvent(t *testing.T, typ domain.EventType, payload string) *gomock.Call {
	t.Helper()
	return f.outbox.EXPECT().Add(inTx, gomock.Any()).DoAndReturn(
		func(_ context.Context, events ...domain.NewEvent) error {
			require.Len(t, events, 1)
			assert.Equal(t, typ, events[0].Type)
			assert.JSONEq(t, payload, string(events[0].Payload))
			return nil
		})
}

func fixedClock() time.Time {
	return testNow
}
