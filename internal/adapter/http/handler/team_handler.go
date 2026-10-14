package handler

import (
	"net/http"
	"time"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/response"
	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

type createTeamRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type addMemberRequest struct {
	Email string          `json:"email"`
	Role  domain.TeamRole `json:"role"`
}

type teamResponse struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	OwnerID     int64     `json:"owner_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func newTeamResponse(t domain.Team) teamResponse {
	return teamResponse{
		ID:          t.ID,
		Name:        t.Name,
		Description: t.Description,
		OwnerID:     t.OwnerID,
		CreatedAt:   t.CreatedAt.UTC(),
		UpdatedAt:   t.UpdatedAt.UTC(),
	}
}

// teamListItem is a team as listed for the caller: the team and the caller's role in it.
type teamListItem struct {
	teamResponse
	Role string `json:"role"`
}

func newTeamListItem(s domain.TeamSummary) teamListItem {
	return teamListItem{teamResponse: newTeamResponse(s.Team), Role: string(s.Role)}
}

type memberResponse struct {
	UserID   int64  `json:"user_id"`
	Email    string `json:"email"`
	FullName string `json:"full_name"`
	Role     string `json:"role"`
}

func newMemberResponse(m domain.MemberProfile) memberResponse {
	return memberResponse{UserID: m.UserID, Email: m.Email, FullName: m.FullName, Role: string(m.Role)}
}

type teamStatsResponse struct {
	TeamID        int64  `json:"team_id"`
	Name          string `json:"name"`
	MemberCount   int    `json:"member_count"`
	OpenTasks     int    `json:"open_tasks"`
	DoneLast7Days int    `json:"done_last_7_days"`
}

func newTeamStatsResponse(s domain.TeamStats) teamStatsResponse {
	return teamStatsResponse(s)
}

type topContributorResponse struct {
	UserID         int64  `json:"user_id"`
	FullName       string `json:"full_name"`
	TasksCompleted int    `json:"tasks_completed"`
	Rank           int    `json:"rank"`
}

func newTopContributorResponse(c domain.TopContributor) topContributorResponse {
	return topContributorResponse(c)
}

// TeamHandler serves teams, their members and team analytics.
type TeamHandler struct {
	teams port.TeamService
}

// NewTeamHandler returns a TeamHandler backed by teams.
func NewTeamHandler(teams port.TeamService) *TeamHandler {
	return &TeamHandler{teams: teams}
}

// Create creates a team owned by the caller and answers 201 with it.
func (h *TeamHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req createTeamRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}

	team, err := h.teams.Create(r.Context(), userID, port.CreateTeamInput{Name: req.Name, Description: req.Description})
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, http.StatusCreated, newTeamResponse(*team))
}

// List returns the caller's teams with the caller's role in each.
func (h *TeamHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	teams, err := h.teams.List(r.Context(), userID)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, mapAll(teams, newTeamListItem))
}

// Get returns the team in the path.
func (h *TeamHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	teamID, err := pathID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}

	team, err := h.teams.Get(r.Context(), userID, teamID)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, newTeamResponse(*team))
}

// ListMembers returns the members of the team in the path.
func (h *TeamHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	teamID, err := pathID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}

	members, err := h.teams.ListMembers(r.Context(), userID, teamID)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, mapAll(members, newMemberResponse))
}

// AddMember adds a registered user to the team in the path and answers 201 with the member.
func (h *TeamHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	teamID, err := pathID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	var req addMemberRequest
	if err = decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}

	member, err := h.teams.AddMember(r.Context(), userID, teamID, port.AddMemberInput{Email: req.Email, Role: req.Role})
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, http.StatusCreated, newMemberResponse(*member))
}

// RemoveMember removes the user in the path from the team in the path and answers 204.
func (h *TeamHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	teamID, err := pathID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	memberID, err := pathID(r, "user_id")
	if err != nil {
		response.Error(w, r, err)
		return
	}

	if err := h.teams.RemoveMember(r.Context(), userID, teamID, memberID); err != nil {
		response.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Stats returns statistics for the caller's teams.
func (h *TeamHandler) Stats(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	stats, err := h.teams.Stats(r.Context(), userID)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, mapAll(stats, newTeamStatsResponse))
}

// TopContributors returns the most productive members of the team in the path.
func (h *TeamHandler) TopContributors(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	teamID, err := pathID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}

	contributors, err := h.teams.TopContributors(r.Context(), userID, teamID)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, mapAll(contributors, newTopContributorResponse))
}
