package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// password is the password of every account the tests register.
const password = "correct-horse-battery"

// apiClient sends JSON requests to a booted application.
type apiClient struct {
	baseURL string
	http    *http.Client
}

// newClient returns a client of the shared application.
func newClient(t *testing.T) *apiClient {
	t.Helper()
	return clientFor(env(t).server)
}

func clientFor(server *httptest.Server) *apiClient {
	return &apiClient{baseURL: server.URL, http: server.Client()}
}

// rawBody is a request body sent verbatim instead of being encoded as JSON.
type rawBody string

type envelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Meta    *metaBody       `json:"meta"`
	Error   *errorBody      `json:"error"`
}

type errorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

type metaBody struct {
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

// apiResponse is one answer of the application: status, headers, raw body and, when the body
// is JSON, the decoded envelope.
type apiResponse struct {
	Status int
	Header http.Header
	Body   []byte
	envelope
}

// do sends method path with body encoded as JSON (nil for no body, rawBody to send bytes as
// they are) and token as the bearer token (empty for none).
func (c *apiClient) do(t *testing.T, method, path string, body any, token string) apiResponse {
	t.Helper()
	return c.send(t, c.newRequest(t, method, path, body, token))
}

func (c *apiClient) newRequest(t *testing.T, method, path string, body any, token string) *http.Request {
	t.Helper()
	req, err := c.buildRequest(t.Context(), method, path, body, token)
	require.NoError(t, err)
	return req
}

func (c *apiClient) buildRequest(ctx context.Context, method, path string, body any, token string) (*http.Request, error) {
	var reader io.Reader = http.NoBody
	switch b := body.(type) {
	case nil:
	case rawBody:
		reader = strings.NewReader(string(b))
	default:
		encoded, err := json.Marshal(b)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, nil
}

func (c *apiClient) send(t *testing.T, req *http.Request) apiResponse {
	t.Helper()
	resp, err := c.exchange(req)
	require.NoErrorf(t, err, "%s %s", req.Method, req.URL.Path)
	return resp
}

// exchange sends req and reads the answer. Unlike send it reports failures as errors, for use
// outside the goroutine of the test.
func (c *apiClient) exchange(req *http.Request) (apiResponse, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return apiResponse{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return apiResponse{}, err
	}

	out := apiResponse{Status: resp.StatusCode, Header: resp.Header, Body: raw}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		if err = json.Unmarshal(raw, &out.envelope); err != nil {
			return apiResponse{}, fmt.Errorf("decode %s %s: %w: body %q", req.Method, req.URL.Path, err, raw)
		}
	}
	return out, nil
}

// expect requires resp to be a success with the given status and returns its data as T.
func expect[T any](t *testing.T, resp apiResponse, status int) T {
	t.Helper()
	requireStatus(t, resp, status)
	require.Truef(t, resp.Success, "not a success envelope: %s", resp.Body)
	var data T
	require.NoErrorf(t, json.Unmarshal(resp.Data, &data), "decode data %s", resp.Data)
	return data
}

func requireStatus(t *testing.T, resp apiResponse, status int) {
	t.Helper()
	require.Equalf(t, status, resp.Status, "unexpected status, body: %s", resp.Body)
}

// requireError requires resp to be an error envelope with the given status and code.
func requireError(t *testing.T, resp apiResponse, status int, code string) errorBody {
	t.Helper()
	requireStatus(t, resp, status)
	require.NotNilf(t, resp.Error, "no error in body: %s", resp.Body)
	assert.False(t, resp.Success)
	assert.Equal(t, code, resp.Error.Code)
	assert.Empty(t, resp.Data)
	return *resp.Error
}

// fieldProblem returns what a validation error says about field.
func fieldProblem(e errorBody, field string) string {
	fields, _ := e.Details["fields"].(map[string]any)
	problem, _ := fields[field].(string)
	return problem
}

// The data of the API's answers.
type (
	authData struct {
		AccessToken string   `json:"access_token"`
		TokenType   string   `json:"token_type"`
		ExpiresIn   int64    `json:"expires_in"`
		User        userData `json:"user"`
	}

	userData struct {
		ID        int64     `json:"id"`
		Email     string    `json:"email"`
		FullName  string    `json:"full_name"`
		CreatedAt time.Time `json:"created_at"`
	}

	teamData struct {
		ID          int64     `json:"id"`
		Name        string    `json:"name"`
		Description string    `json:"description"`
		OwnerID     int64     `json:"owner_id"`
		CreatedAt   time.Time `json:"created_at"`
		UpdatedAt   time.Time `json:"updated_at"`
	}

	teamListItem struct {
		teamData
		Role string `json:"role"`
	}

	memberData struct {
		UserID   int64  `json:"user_id"`
		Email    string `json:"email"`
		FullName string `json:"full_name"`
		Role     string `json:"role"`
	}

	taskData struct {
		ID          int64      `json:"id"`
		TeamID      int64      `json:"team_id"`
		Title       string     `json:"title"`
		Description string     `json:"description"`
		Status      string     `json:"status"`
		Priority    int        `json:"priority"`
		CreatorID   int64      `json:"creator_id"`
		AssigneeID  *int64     `json:"assignee_id"`
		DueDate     *string    `json:"due_date"`
		CompletedAt *time.Time `json:"completed_at"`
		CreatedAt   time.Time  `json:"created_at"`
		UpdatedAt   time.Time  `json:"updated_at"`
	}

	changeData struct {
		ID        int64     `json:"id"`
		TaskID    int64     `json:"task_id"`
		UserID    int64     `json:"user_id"`
		Field     string    `json:"field"`
		OldValue  string    `json:"old_value"`
		NewValue  string    `json:"new_value"`
		ChangedAt time.Time `json:"changed_at"`
	}

	commentData struct {
		ID        int64     `json:"id"`
		TaskID    int64     `json:"task_id"`
		UserID    int64     `json:"user_id"`
		Content   string    `json:"content"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}

	statsData struct {
		TeamID        int64  `json:"team_id"`
		Name          string `json:"name"`
		MemberCount   int    `json:"member_count"`
		OpenTasks     int    `json:"open_tasks"`
		DoneLast7Days int    `json:"done_last_7_days"`
	}

	contributorData struct {
		UserID         int64  `json:"user_id"`
		FullName       string `json:"full_name"`
		TasksCompleted int    `json:"tasks_completed"`
		Rank           int    `json:"rank"`
	}

	orphanData struct {
		TaskID       int64  `json:"task_id"`
		TaskTitle    string `json:"task_title"`
		AssigneeID   int64  `json:"assignee_id"`
		AssigneeName string `json:"assignee_name"`
	}
)

// account is a registered user with the token it was issued.
type account struct {
	ID       int64
	Email    string
	FullName string
	Token    string
}

func (c *apiClient) register(t *testing.T, email, pass, fullName string) apiResponse {
	t.Helper()
	body := map[string]string{"email": email, "password": pass, "full_name": fullName}
	return c.do(t, http.MethodPost, "/api/v1/register", body, "")
}

func (c *apiClient) login(t *testing.T, email, pass string) apiResponse {
	t.Helper()
	return c.do(t, http.MethodPost, "/api/v1/login", map[string]string{"email": email, "password": pass}, "")
}

// mustRegister registers a user with a unique email and the suite's password.
func (c *apiClient) mustRegister(t *testing.T, fullName string) account {
	t.Helper()
	auth := expect[authData](t, c.register(t, uniqueEmail(t), password, fullName), http.StatusCreated)
	require.NotEmpty(t, auth.AccessToken)
	return account{ID: auth.User.ID, Email: auth.User.Email, FullName: auth.User.FullName, Token: auth.AccessToken}
}

func (c *apiClient) mustCreateTeam(t *testing.T, owner account, name string) teamData {
	t.Helper()
	body := map[string]string{"name": name}
	return expect[teamData](t, c.do(t, http.MethodPost, "/api/v1/teams", body, owner.Token), http.StatusCreated)
}

func (c *apiClient) addMember(t *testing.T, manager account, teamID int64, email, role string) apiResponse {
	t.Helper()
	body := map[string]string{"email": email}
	if role != "" {
		body["role"] = role
	}
	return c.do(t, http.MethodPost, teamPath(teamID, "/members"), body, manager.Token)
}

// mustJoin registers a user and has manager add it to the team with role; an empty role
// leaves the default to the API.
func (c *apiClient) mustJoin(t *testing.T, manager account, teamID int64, fullName, role string) account {
	t.Helper()
	acc := c.mustRegister(t, fullName)
	member := expect[memberData](t, c.addMember(t, manager, teamID, acc.Email, role), http.StatusCreated)
	require.Equal(t, acc.ID, member.UserID)
	return acc
}

func (c *apiClient) removeMember(t *testing.T, caller account, teamID, userID int64) apiResponse {
	t.Helper()
	return c.do(t, http.MethodDelete, teamPath(teamID, "/members/"+strconv.FormatInt(userID, 10)), nil, caller.Token)
}

// mustCreateTask creates a task in the team with the given title and the optional fields in
// extra, such as assignee_id or due_date.
func (c *apiClient) mustCreateTask(t *testing.T, creator account, teamID int64, title string, extra map[string]any) taskData {
	t.Helper()
	body := map[string]any{"team_id": teamID, "title": title}
	maps.Copy(body, extra)
	return expect[taskData](t, c.do(t, http.MethodPost, "/api/v1/tasks", body, creator.Token), http.StatusCreated)
}

func (c *apiClient) patchTask(t *testing.T, caller account, taskID int64, body any) apiResponse {
	t.Helper()
	return c.do(t, http.MethodPatch, taskPath(taskID, ""), body, caller.Token)
}

func (c *apiClient) mustPatchTask(t *testing.T, caller account, taskID int64, body any) taskData {
	t.Helper()
	return expect[taskData](t, c.patchTask(t, caller, taskID, body), http.StatusOK)
}

func (c *apiClient) mustGetTask(t *testing.T, caller account, taskID int64) taskData {
	t.Helper()
	return expect[taskData](t, c.do(t, http.MethodGet, taskPath(taskID, ""), nil, caller.Token), http.StatusOK)
}

func (c *apiClient) history(t *testing.T, caller account, taskID int64) []changeData {
	t.Helper()
	return expect[[]changeData](t, c.do(t, http.MethodGet, taskPath(taskID, "/history"), nil, caller.Token), http.StatusOK)
}

// taskIDs returns the ids of the tasks the caller gets from path, a task list, in the order
// they are listed.
func taskIDs(t *testing.T, c *apiClient, caller account, path string) []int64 {
	t.Helper()
	return idsOf(expect[[]taskData](t, c.do(t, http.MethodGet, path, nil, caller.Token), http.StatusOK))
}

func idsOf(tasks []taskData) []int64 {
	ids := make([]int64, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	return ids
}

func teamPath(id int64, suffix string) string {
	return "/api/v1/teams/" + strconv.FormatInt(id, 10) + suffix
}

func taskPath(id int64, suffix string) string {
	return "/api/v1/tasks/" + strconv.FormatInt(id, 10) + suffix
}

// uniqueName returns a name no other test, and no earlier run against the same database, has
// used: the test name reduced to lower-case letters, digits and dashes, and a random suffix.
func uniqueName(t *testing.T) string {
	t.Helper()
	prefix := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, strings.ToLower(t.Name()))
	if len(prefix) > 40 {
		prefix = prefix[:40]
	}
	return prefix + "-" + strings.ToLower(rand.Text()[:8])
}

func uniqueEmail(t *testing.T) string {
	t.Helper()
	return uniqueName(t) + "@example.com"
}
