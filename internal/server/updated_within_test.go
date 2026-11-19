package server

import (
	"context"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// "Recently updated": issues created, changed, moved or commented on within the
// rolling window; an issue nobody touched for two days stays out.
func TestUpdatedWithinFilter(t *testing.T) {
	s, h := apiServer(t)
	alex := person(t, s, h, "alex@example.com", "Alex", "admin")
	var team store.Team
	alex.do("POST", "/api/v1/teams", map[string]any{"name": "Web", "key": "web", "preset": "software"}, 201, &team)
	for _, title := range []string{"Old", "Moved", "Commented", "Untouched"} {
		alex.do("POST", "/api/v1/issues", map[string]any{"team_id": team.ID, "title": title}, 201, nil)
	}
	old := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339Nano)
	if _, err := s.store.DB.ExecContext(context.Background(), `UPDATE issues SET created_at = ?, updated_at = ?`, old, old); err != nil {
		t.Fatal(err)
	}
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": team.ID, "title": "New"}, 201, nil)
	done := 0
	for _, st := range team.States {
		if st.Type == "completed" {
			done = int(st.ID)
		}
	}
	alex.do("PATCH", "/api/v1/issues/WEB-2", map[string]any{"state_id": done}, 200, nil)
	alex.do("POST", "/api/v1/issues/WEB-3/comments", map[string]any{"body": "Looked at it"}, 201, nil)

	titles := func(filter string) string {
		var list struct {
			Issues []store.Issue `json:"issues"`
		}
		alex.do("GET", "/api/v1/issues?filter="+url.QueryEscape(filter), nil, 200, &list)
		var n []string
		for _, is := range list.Issues {
			n = append(n, is.Title)
		}
		sort.Strings(n)
		return strings.Join(n, ",")
	}
	if got := titles(`{"updated_within":"24h"}`); got != "Commented,Moved,New" {
		t.Fatalf("24h: %s", got)
	}
	if got := titles(`{"updated_within":"3d"}`); got != "Commented,Moved,New,Old,Untouched" {
		t.Fatalf("3d: %s", got)
	}
	if got := titles(`{"updated_within":"bogus"}`); got != "Commented,Moved,New,Old,Untouched" {
		t.Fatalf("unknown window filters nothing: %s", got)
	}
}
