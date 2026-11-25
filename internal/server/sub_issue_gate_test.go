package server

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// A merged pull request completes its issue only once every sub-issue is done
// or canceled; until then the issue waits and completes with the last one.
func TestMergedPRWaitsForSubIssues(t *testing.T) {
	s, h := apiServer(t)
	alex := person(t, s, h, "alex@example.com", "Alex", "admin")
	var team store.Team
	alex.do("POST", "/api/v1/teams", map[string]any{"name": "Web", "key": "WEB", "preset": "software"}, 201, &team)
	var parent store.Issue
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": team.ID, "title": "Checkout"}, 201, &parent)
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": team.ID, "title": "Card form", "parent_id": parent.ID}, 201, nil)
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": team.ID, "title": "Receipt mail", "parent_id": parent.ID}, 201, nil)
	var states []store.State
	alex.do("GET", "/api/v1/teams/WEB/states", nil, 200, &states)
	byType := map[string]int64{}
	for _, st := range states {
		if byType[st.Type] == 0 {
			byType[st.Type] = st.ID
		}
	}
	var git struct {
		URL string `json:"url"`
	}
	alex.do("GET", "/api/v1/teams/WEB/git", nil, 200, &git)
	path := git.URL[strings.Index(git.URL, "/hooks/"):]
	hook := func(event, payload string) {
		req := httptest.NewRequest("POST", path, strings.NewReader(payload))
		req.Header.Set("X-GitHub-Event", event)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("hook: %d %s", w.Code, w.Body.String())
		}
	}
	stateOf := func(id int64) string {
		is, _ := s.store.GetIssue(context.Background(), id, 0)
		return is.StateType
	}

	hook("pull_request", `{"action":"closed","number":5,"repository":{"full_name":"me/site"},"pull_request":{"title":"Checkout","body":"Fixes WEB-1","html_url":"https://github.com/me/site/pull/5","merged":true,"state":"closed","head":{"ref":"web-1-checkout"}}}`)
	if st := stateOf(parent.ID); st == "completed" {
		t.Fatal("parent completed with open sub-issues")
	}
	// the same merge again does not add a second note
	hook("pull_request", `{"action":"closed","number":5,"repository":{"full_name":"me/site"},"pull_request":{"title":"Checkout","body":"Fixes WEB-1","html_url":"https://github.com/me/site/pull/5","merged":true,"state":"closed","head":{"ref":"web-1-checkout"}}}`)
	hist, _ := s.store.IssueHistory(context.Background(), parent.ID)
	notes := 0
	for _, h := range hist {
		if h.Field == "waits_for_sub_issues" {
			notes++
		}
	}
	if notes != 1 {
		t.Fatalf("notes: %d", notes)
	}

	alex.do("PATCH", "/api/v1/issues/WEB-2", map[string]any{"state_id": byType["completed"]}, 200, nil)
	if st := stateOf(parent.ID); st == "completed" {
		t.Fatal("parent completed with one sub-issue open")
	}
	alex.do("PATCH", "/api/v1/issues/WEB-3", map[string]any{"state_id": byType["canceled"]}, 200, nil)
	if st := stateOf(parent.ID); st != "completed" {
		t.Fatalf("parent not completed after its last sub-issue: %s", st)
	}
	att, _ := s.store.FindAttachment(context.Background(), parent.ID, "git", "https://github.com/me/site/pull/5")
	if strings.Contains(string(att.Meta), waitsForSubIssues) {
		t.Fatalf("flag left: %s", att.Meta)
	}

	// without a waiting pull request, finishing sub-issues leaves the parent alone
	var other store.Issue
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": team.ID, "title": "Search"}, 201, &other)
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": team.ID, "title": "Filters", "parent_id": other.ID}, 201, nil)
	alex.do("PATCH", "/api/v1/issues/WEB-5", map[string]any{"state_id": byType["completed"]}, 200, nil)
	if st := stateOf(other.ID); st == "completed" {
		t.Fatal("parent completed without a merged pull request")
	}
	// a parent without sub-issues completes right away
	hook("pull_request", `{"action":"closed","number":6,"repository":{"full_name":"me/site"},"pull_request":{"title":"Search","body":"Fixes WEB-4","html_url":"https://github.com/me/site/pull/6","merged":true,"state":"closed","head":{"ref":"x"}}}`)
	if st := stateOf(other.ID); st != "completed" {
		t.Fatalf("all sub-issues done, merge did not complete: %s", st)
	}
}
