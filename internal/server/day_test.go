package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// A person's day for Pocket Day: their work grouped per issue with a
// summary, commits landing on their issues, project updates — and nothing
// of someone else's.
func TestDayWork(t *testing.T) {
	s, h := apiServer(t)
	alex := person(t, s, h, "alex@example.com", "Alex", "admin")
	ben := person(t, s, h, "ben@example.com", "Ben", "member")
	var team store.Team
	alex.do("POST", "/api/v1/teams", map[string]any{"name": "Athena", "key": "as", "preset": "software"}, 201, &team)
	alex.do("POST", "/api/v1/teams/AS/members", map[string]any{"user_id": ben.user.ID, "role": "member"}, 200, nil)
	var proj store.Project
	alex.do("POST", "/api/v1/projects", map[string]any{"name": "Relaunch", "team_ids": []int64{team.ID}}, 201, &proj)
	var is store.Issue
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": team.ID, "title": "Rules attach to a workflow", "project_id": proj.ID}, 201, &is)
	alex.do("POST", "/api/v1/issues/AS-1/comments", map[string]any{"body": "Found it\nin the scoping code"}, 201, nil)
	today := time.Now().In(s.loc(context.Background(), alex.user.ID)).Format("2006-01-02")
	alex.do("POST", "/api/v1/issues/AS-1/time", map[string]any{"minutes": 80, "spent_on": today, "note": "debugging"}, 201, nil)
	alex.do("PATCH", "/api/v1/issues/AS-1", map[string]any{"state": "completed"}, 200, nil)
	// a commit lands on it (git authors are names, not people)
	if err := s.store.SaveAttachment(context.Background(), &store.Attachment{IssueID: &is.ID, Kind: "commit", Title: "Scope rules to the brand", URL: "https://git.example/c/abc"}); err != nil {
		t.Fatal(err)
	}
	// Ben's issue: not in Alex's day, and Alex's in his only as nothing
	ben.do("POST", "/api/v1/issues", map[string]any{"team_id": team.ID, "title": "Ben's own"}, 201, nil)
	alex.do("POST", "/api/v1/projects/"+itoa64(proj.ID)+"/updates", map[string]any{"health": "on_track", "body": "Rules fixed"}, 201, nil)

	var d dayWork
	alex.do("GET", "/api/v1/day/"+today, nil, 200, &d)
	if len(d.Issues) != 1 {
		t.Fatalf("issues: %+v", d.Issues)
	}
	got := d.Issues[0]
	if got.Identifier != "AS-1" || got.Project != "Relaunch" || !got.Done || !got.Created || got.Minutes != 80 || !strings.HasSuffix(got.URL, "/issue/AS-1") {
		t.Fatalf("issue: %+v", got)
	}
	if !strings.HasPrefix(got.Summary, "created · done (") || !strings.Contains(got.Summary, "1 comment") || !strings.Contains(got.Summary, "1 commit") || !strings.HasSuffix(got.Summary, "1 h 20 min tracked") {
		t.Fatalf("summary: %q", got.Summary)
	}
	kinds := []string{}
	for _, e := range got.Events {
		kinds = append(kinds, e.Kind)
		if e.Kind == "comment" && e.Text != "Commented: Found it …" {
			t.Fatalf("comment text: %q", e.Text)
		}
	}
	if j := strings.Join(kinds, ","); !strings.Contains(j, "created") || !strings.Contains(j, "state") || !strings.Contains(j, "commit") || !strings.HasSuffix(j, "time") {
		t.Fatalf("events: %s", j)
	}
	if d.Minutes != 80 || len(d.Projects) != 1 || !strings.Contains(d.Projects[0].Events[0].Text, "Rules fixed") {
		t.Fatalf("day: minutes %d projects %+v", d.Minutes, d.Projects)
	}

	var bd dayWork
	ben.do("GET", "/api/v1/day/"+today, nil, 200, &bd)
	if len(bd.Issues) != 1 || bd.Issues[0].Identifier != "AS-2" || len(bd.Projects) != 0 {
		t.Fatalf("ben's day: %+v", bd.Issues)
	}
	// another day is empty; a bad date is refused
	alex.do("GET", "/api/v1/day/2001-01-01", nil, 200, &d)
	if len(d.Issues) != 0 {
		t.Fatalf("old day: %+v", d.Issues)
	}
	if alex.status("GET", "/api/v1/day/yesterday", nil) == 200 {
		t.Fatal("bad date accepted")
	}
}

func itoa64(n int64) string {
	b := []byte{}
	if n == 0 {
		return "0"
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
