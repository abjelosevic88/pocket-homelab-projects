package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

func TestPRDetails(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := map[string]string{
			"/repos/acme/web/pulls/12":               `{"title":"Fix","state":"open","head":{"sha":"abc","ref":"fix-login"}}`,
			"/repos/acme/web/commits/abc/check-runs": `{"check_runs":[{"name":"test","status":"completed","conclusion":"success","html_url":"https://ci/1"},{"name":"lint","status":"completed","conclusion":"failure","output":{"title":"3 problems"}},{"name":"e2e","status":"in_progress"}]}`,
			"/repos/acme/web/commits/abc/status":     `{"statuses":[{"context":"vercel","state":"success","target_url":"https://v"}]}`,
			"/repos/acme/web/issues/12/comments":     `[{"user":{"login":"ben","type":"User"},"body":"Looks good","html_url":"https://c/1","created_at":"2026-10-28T10:00:00Z"},{"user":{"login":"codecov[bot]","type":"Bot"},"body":"<!-- x -->cov","html_url":"https://c/2","created_at":"2026-10-28T09:00:00Z"}]`,
			"/repos/acme/web/pulls/12/reviews":       `[{"user":{"login":"cara","type":"User"},"state":"APPROVED","body":"","html_url":"https://r/1","submitted_at":"2026-10-28T11:00:00Z"},{"user":{"login":"cara","type":"User"},"state":"COMMENTED","body":"","html_url":"https://r/0","submitted_at":"2026-10-28T08:00:00Z"}]`,
			"/repos/acme/web/pulls/12/comments":      `[{"id":7,"user":{"login":"cara","type":"User"},"body":"rename this","path":"a.go","line":4,"html_url":"https://i/1","created_at":"2026-10-28T08:00:00Z"}]`,
			"/graphql":                               `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[{"isResolved":true,"isOutdated":false,"comments":{"nodes":[{"databaseId":7}]}}]}}}}}`,
		}[r.URL.Path]
		if !ok {
			w.WriteHeader(404)
			return
		}
		w.Write([]byte(body))
	}))
	defer gh.Close()
	old := githubAPI
	githubAPI = gh.URL
	defer func() { githubAPI = old }()

	s, h := apiServer(t)
	alex := person(t, s, h, "alex@example.com", "Alex", "admin")
	var team store.Team
	alex.do("POST", "/api/v1/teams", map[string]any{"name": "Web", "key": "web", "preset": "software"}, 201, &team)
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": team.ID, "title": "Fix login"}, 201, nil)
	alex.do("POST", "/api/v1/issues/WEB-1/pull-requests", map[string]any{"url": "https://github.com/acme/web/pull/12"}, 201, nil)

	var list []prDetails
	alex.do("GET", "/api/v1/issues/WEB-1/pull-requests", nil, 200, &list)
	if len(list) != 1 || list[0].Error == "" || len(list[0].Checks) != 0 {
		t.Fatalf("without a token: %+v", list)
	}
	alex.do("PUT", "/api/v1/teams/WEB/git-token", map[string]any{"github_token": "ghp_secret1234"}, 200, nil)
	list = nil
	alex.do("GET", "/api/v1/issues/WEB-1/pull-requests", nil, 200, &list)
	d := list[0]
	if d.Error != "" || d.ChecksState != "failed" || len(d.Checks) != 4 || d.Checks[0].Name != "lint" || d.Checks[1].State != "pending" {
		t.Fatalf("checks: %+v", d)
	}
	if d.Branch != "fix-login" || d.Checks[0].Summary != "3 problems" {
		t.Fatalf("branch/summary: %+v", d)
	}
	// empty COMMENTED review shells are dropped; oldest first
	if len(d.Comments) != 4 || d.Comments[0].Kind != "inline" || !d.Comments[1].Bot || d.Comments[3].Review != "approved" || !d.Comments[0].Resolved || d.Comments[0].Line != 4 {
		t.Fatalf("comments: %+v", d.Comments)
	}
	// the status check fills in the branch the link came without
	s.checkPRs(t.Context(), 0, 0)
	if a, _ := s.store.FindAttachment(t.Context(), 1, "git", "https://github.com/acme/web/pull/12"); a == nil || string(a.Meta) == "" || !strings.Contains(string(a.Meta), `"branch":"fix-login"`) {
		t.Fatalf("branch not filled: %+v", a)
	}
}
