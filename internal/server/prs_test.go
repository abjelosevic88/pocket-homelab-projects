package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

func TestParsePRURL(t *testing.T) {
	for u, want := range map[string]prRef{
		"https://github.com/athenastudioAI/athena/pull/3990":   {"github.com", "athenastudioAI", "athena", 3990},
		"https://git.bjelke.org/abjelosevic88/homelab/pulls/7": {"git.bjelke.org", "abjelosevic88", "homelab", 7},
		"https://github.com/o/r/pull/12/files":                 {},
	} {
		got, ok := parsePRURL(u)
		if ok != (want.Number > 0) || (ok && got != want) {
			t.Fatalf("%s: %+v %v", u, got, ok)
		}
	}
}

func TestPullRequestLinkAndStatus(t *testing.T) {
	var mu sync.Mutex
	prs := map[string]string{"/repos/acme/web/pulls/12": `{"title":"Fix login on Safari","state":"open","merged":false,"draft":false}`}
	auth := ""
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		auth = r.Header.Get("Authorization")
		body, ok := prs[r.URL.Path]
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
	ben := person(t, s, h, "ben@example.com", "Ben", "member")
	var team store.Team
	alex.do("POST", "/api/v1/teams", map[string]any{"name": "Web", "key": "web", "preset": "software"}, 201, &team)
	alex.do("POST", "/api/v1/teams/WEB/members", map[string]any{"user_id": ben.user.ID, "role": "member"}, 200, nil)
	var is store.Issue
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": team.ID, "title": "Fix login"}, 201, &is)

	// links: http(s) only; the PR attaches open and the issue moves to review
	if c := alex.status("POST", "/api/v1/issues/WEB-1/pull-requests", map[string]any{"url": "javascript:alert(1)"}); c != 400 {
		t.Fatalf("bad url accepted: %d", c)
	}
	var att store.Attachment
	alex.do("POST", "/api/v1/issues/WEB-1/pull-requests", map[string]any{"url": "https://github.com/acme/web/pull/12", "branch": "fix-login"}, 201, &att)
	if att.Kind != "pull_request" || att.State != "open" || att.ExtApp != "git" || att.ExtID != "https://github.com/acme/web/pull/12" || !strings.Contains(att.Subtitle, "acme/web #12") {
		t.Fatalf("attachment: %+v", att)
	}
	fresh, _ := s.store.GetIssue(context.Background(), is.ID, 0)
	if fresh.StateType != "started" {
		t.Fatalf("not moved to review: %s", fresh.StateType)
	}
	// linking again updates the same row
	alex.do("POST", "/api/v1/issues/WEB-1/pull-requests", map[string]any{"url": "https://github.com/acme/web/pull/12", "title": "Fix login"}, 201, nil)
	if list, _ := s.store.Attachments(context.Background(), is.ID, 0); len(list) != 1 {
		t.Fatalf("duplicate rows: %d", len(list))
	}

	// tokens: admins only, never shown back in full
	if c := ben.status("PUT", "/api/v1/teams/WEB/git-token", map[string]any{"github_token": "ghp_x"}); c != 403 {
		t.Fatalf("member set token: %d", c)
	}
	if c := alex.status("POST", "/api/v1/teams/WEB/pr-status/refresh", nil); c != 400 {
		t.Fatalf("refresh without token: %d", c)
	}
	var view map[string]any
	alex.do("PUT", "/api/v1/teams/WEB/git-token", map[string]any{"github_token": "ghp_secret1234"}, 200, &view)
	if view["has_token"] != true || view["last4"] != "1234" || tokenLeaks(alex) {
		t.Fatalf("token view: %+v", view)
	}

	// still open: nothing changes; merged: state follows and the linked issue completes
	var res map[string]int
	alex.do("POST", "/api/v1/teams/WEB/pr-status/refresh", nil, 200, &res)
	if res["checked"] != 1 || res["changed"] != 0 || auth != "Bearer ghp_secret1234" {
		t.Fatalf("open check: %+v %q", res, auth)
	}
	mu.Lock()
	prs["/repos/acme/web/pulls/12"] = `{"title":"Fix login on Safari","state":"closed","merged":true}`
	mu.Unlock()
	s.prJob(context.Background())
	got, _ := s.store.FindAttachment(context.Background(), is.ID, "git", "https://github.com/acme/web/pull/12")
	if got.State != "merged" || got.Title != "Fix login on Safari" {
		t.Fatalf("after merge: %+v", got)
	}
	fresh, _ = s.store.GetIssue(context.Background(), is.ID, 0)
	if fresh.StateType != "completed" {
		t.Fatalf("merged PR did not complete the issue: %s", fresh.StateType)
	}
	// merged rows are no longer polled; unreadable ones are marked, not fatal
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": team.ID, "title": "Other"}, 201, nil)
	alex.do("POST", "/api/v1/issues/WEB-2/pull-requests", map[string]any{"url": "https://github.com/acme/private/pull/1"}, 201, nil)
	alex.do("POST", "/api/v1/teams/WEB/pr-status/refresh", nil, 200, &res)
	if res["checked"] != 1 {
		t.Fatalf("second check: %+v", res)
	}
	is2, _ := s.store.IssueIDByIdentifier(context.Background(), "WEB-2")
	bad, _ := s.store.FindAttachment(context.Background(), is2, "git", "https://github.com/acme/private/pull/1")
	if bad.State != "open" || !strings.Contains(string(bad.Meta), "status_error") {
		t.Fatalf("unreadable PR: %+v %s", bad, bad.Meta)
	}
}

func tokenLeaks(c *apiClient) bool {
	_, b := c.raw("GET", "/api/v1/teams/WEB/git-token", "", nil)
	return strings.Contains(string(b), "secret")
}

// A pull request address pasted with Attachments → Link is tracked like a linked PR;
// other addresses stay plain links.
func TestPastedPRLinkIsTracked(t *testing.T) {
	s, h := apiServer(t)
	alex := person(t, s, h, "alex@example.com", "Alex", "admin")
	var team store.Team
	alex.do("POST", "/api/v1/teams", map[string]any{"name": "Web", "key": "web", "preset": "software"}, 201, &team)
	var is store.Issue
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": team.ID, "title": "Fix login"}, 201, &is)

	var att store.Attachment
	alex.do("POST", "/api/v1/issues/WEB-1/attachments", map[string]any{"url": "https://github.com/acme/web/pull/7"}, 201, &att)
	if att.Kind != "pull_request" || att.State != "open" || att.ExtApp != "git" {
		t.Fatalf("pasted PR: %+v", att)
	}
	alex.do("POST", "/api/v1/issues/WEB-1/attachments", map[string]any{"url": "https://example.com/spec"}, 201, &att)
	if att.Kind != "link" {
		t.Fatalf("plain link: %+v", att)
	}
}
