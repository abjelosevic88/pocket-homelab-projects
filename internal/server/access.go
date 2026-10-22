package server

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// What a person needs on a team to do something.
const (
	needRead  = 1 // guest
	needWrite = 2 // member
	needAdmin = 3 // admin
)

func roleRank(role string) int {
	switch role {
	case "admin":
		return needAdmin
	case "member":
		return needWrite
	case "guest":
		return needRead
	}
	return 0
}

// access is the set of teams a person is on, with their role on each.
type access struct {
	uid   int64
	teams map[int64]string
}

func (s *Server) accessFor(ctx context.Context, uid int64) *access {
	teams, err := s.store.UserTeamIDs(ctx, uid)
	if err != nil {
		teams = map[int64]string{}
	}
	return &access{uid: uid, teams: teams}
}

func (s *Server) accessOf(r *http.Request) *access { return s.accessFor(r.Context(), me(r)) }

func (a *access) ids() []int64 {
	out := make([]int64, 0, len(a.teams))
	for id := range a.teams {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (a *access) can(teamID int64, need int) bool { return roleRank(a.teams[teamID]) >= need }

// canAny reports whether the person has need on at least one of the teams.
func (a *access) canAny(teamIDs []int64, need int) bool {
	for _, t := range teamIDs {
		if a.can(t, need) {
			return true
		}
	}
	return false
}

// check answers not found for a team the person is not on, forbidden for a
// role that is not enough.
func (a *access) check(teamID int64, need int) error {
	role := a.teams[teamID]
	if role == "" {
		return store.ErrNotFound
	}
	if roleRank(role) < need {
		if need == needAdmin {
			return errForbidden("only an admin of the team can change that")
		}
		return errForbidden("guests of a team can read and comment, not change things")
	}
	return nil
}

func (a *access) checkAny(teamIDs []int64, need int) error {
	seen := false
	for _, t := range teamIDs {
		if a.teams[t] != "" {
			seen = true
			if a.can(t, need) {
				return nil
			}
		}
	}
	if !seen {
		return store.ErrNotFound
	}
	return errForbidden("guests of a team can read and comment, not change things")
}

// loadTeam loads a team the person may use at need.
func (s *Server) loadTeam(r *http.Request, a *access, ref string, need int) (*store.Team, error) {
	var t *store.Team
	var err error
	if id, perr := strconv.ParseInt(ref, 10, 64); perr == nil {
		t, err = s.store.GetTeam(r.Context(), id)
	} else {
		t, err = s.store.GetTeamByKey(r.Context(), ref)
	}
	if err != nil {
		return nil, err
	}
	if err := a.check(t.ID, need); err != nil {
		return nil, err
	}
	t.Role = a.teams[t.ID]
	return t, nil
}

// issueID resolves a numeric id or an identifier ("WEB-12").
func (s *Server) issueID(ctx context.Context, ref string) (int64, error) {
	ref = strings.TrimSpace(ref)
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil {
		return id, nil
	}
	return s.store.IssueIDByIdentifier(ctx, ref)
}

// loadIssue loads an issue the person may use at need (trash only when allowTrash).
func (s *Server) loadIssue(r *http.Request, a *access, ref string, need int) (*store.Issue, error) {
	id, err := s.issueID(r.Context(), ref)
	if err != nil {
		return nil, err
	}
	is, err := s.store.GetIssue(r.Context(), id, a.uid)
	if err != nil {
		return nil, err
	}
	if err := a.check(is.TeamID, need); err != nil {
		return nil, err
	}
	if is.DeletedAt != nil && r.Method != http.MethodGet && !strings.HasSuffix(r.URL.Path, "/restore") {
		return nil, badRequest("the issue is in the trash; restore it first")
	}
	// A Pocket app acting through a link changes only what it created.
	if app := s.linkApp(r); app != "" && need >= needWrite && r.Method == http.MethodPatch && is.SourceApp != app {
		return nil, errLinkScope
	}
	return is, nil
}

// loadProject loads a project the person may use at need.
func (s *Server) loadProject(r *http.Request, a *access, id int64, need int) (*store.Project, error) {
	p, err := s.store.GetProject(r.Context(), id)
	if err != nil {
		return nil, err
	}
	if err := a.checkAny(p.TeamIDs, need); err != nil {
		return nil, err
	}
	if p.DeletedAt != nil && r.Method != http.MethodGet && !strings.HasSuffix(r.URL.Path, "/restore") {
		return nil, badRequest("the project is in the trash; restore it first")
	}
	return p, nil
}

// visibleProjectIDs lists the projects of the person's teams.
func (s *Server) visibleProject(ctx context.Context, a *access, id int64) bool {
	for _, t := range s.store.ProjectTeamIDs(ctx, id) {
		if a.teams[t] != "" {
			return true
		}
	}
	return false
}
