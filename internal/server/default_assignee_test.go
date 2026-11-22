package server

import (
	"strconv"
	"strings"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

func TestTeamDefaultAssignee(t *testing.T) {
	s, h := apiServer(t)
	alex := person(t, s, h, "alex@example.com", "Alex", "admin")
	ben := person(t, s, h, "ben@example.com", "Ben", "member")
	var team store.Team
	alex.do("POST", "/api/v1/teams", map[string]any{"name": "Web", "key": "web", "preset": "software"}, 201, &team)
	alex.do("POST", "/api/v1/teams/WEB/members", map[string]any{"user_id": ben.user.ID, "role": "member"}, 200, nil)
	if team.DefaultAssignee != "creator" {
		t.Fatalf("new team default: %q", team.DefaultAssignee)
	}
	assignee := func(c *apiClient, body map[string]any) *int64 {
		t.Helper()
		var is store.Issue
		body["team_id"] = team.ID
		c.do("POST", "/api/v1/issues", body, 201, &is)
		return is.AssigneeID
	}
	// default: the creator; an explicit 0 stays unassigned; a named person wins
	if a := assignee(ben, map[string]any{"title": "a"}); a == nil || *a != ben.user.ID {
		t.Fatalf("creator default: %v", a)
	}
	if a := assignee(ben, map[string]any{"title": "b", "assignee_id": 0}); a != nil {
		t.Fatalf("explicit none: %v", *a)
	}
	if a := assignee(ben, map[string]any{"title": "c", "assignee_id": alex.user.ID}); a == nil || *a != alex.user.ID {
		t.Fatalf("named: %v", a)
	}
	// a chosen member
	alex.do("PATCH", "/api/v1/teams/WEB", map[string]any{"default_assignee": strconv.FormatInt(alex.user.ID, 10)}, 200, nil)
	if a := assignee(ben, map[string]any{"title": "d"}); a == nil || *a != alex.user.ID {
		t.Fatalf("member default: %v", a)
	}
	// no one
	alex.do("PATCH", "/api/v1/teams/WEB", map[string]any{"default_assignee": "none"}, 200, nil)
	if a := assignee(ben, map[string]any{"title": "e"}); a != nil {
		t.Fatalf("none default: %v", *a)
	}
	// only members may be the default
	if c := alex.status("PATCH", "/api/v1/teams/WEB", map[string]any{"default_assignee": "999"}); c != 400 {
		t.Fatalf("non-member default accepted: %d", c)
	}
	// the project's lead, else the creator
	alex.do("PATCH", "/api/v1/teams/WEB", map[string]any{"default_assignee": "project_lead"}, 200, nil)
	var proj store.Project
	alex.do("POST", "/api/v1/projects", map[string]any{"name": "Site", "team_ids": []int64{team.ID}, "lead_id": alex.user.ID}, 201, &proj)
	if a := assignee(ben, map[string]any{"title": "f", "project_id": proj.ID}); a == nil || *a != alex.user.ID {
		t.Fatalf("project lead default: %v", a)
	}
	if a := assignee(ben, map[string]any{"title": "g"}); a == nil || *a != ben.user.ID {
		t.Fatalf("no project → creator: %v", a)
	}
}

func TestTeamHiddenFields(t *testing.T) {
	s, h := apiServer(t)
	alex := person(t, s, h, "alex@example.com", "Alex", "admin")
	var team store.Team
	alex.do("POST", "/api/v1/teams", map[string]any{"name": "Web", "key": "web", "preset": "software"}, 201, &team)
	if len(team.HiddenFields) != 0 {
		t.Fatalf("new team hides %v", team.HiddenFields)
	}
	alex.do("PATCH", "/api/v1/teams/WEB", map[string]any{"hidden_fields": []string{"due", "priority", "labels"}}, 200, &team)
	if got := strings.Join(team.HiddenFields, ","); got != "priority,labels,due" {
		t.Fatalf("hidden: %q", got)
	}
	if team.FieldOn("priority") || !team.FieldOn("milestone") {
		t.Fatalf("FieldOn wrong: %v", team.HiddenFields)
	}
	if c := alex.status("PATCH", "/api/v1/teams/WEB", map[string]any{"hidden_fields": []string{"status"}}); c != 400 {
		t.Fatalf("unknown field accepted: %d", c)
	}
	// hidden fields keep their values
	var is store.Issue
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": team.ID, "title": "x", "priority": 2}, 201, &is)
	if is.Priority != 2 {
		t.Fatalf("priority dropped: %d", is.Priority)
	}
	var again store.Team
	alex.do("GET", "/api/v1/teams/WEB", nil, 200, &again)
	if len(again.HiddenFields) != 3 {
		t.Fatalf("not saved: %v", again.HiddenFields)
	}
	alex.do("PATCH", "/api/v1/teams/WEB", map[string]any{"hidden_fields": []string{}}, 200, &team)
	if len(team.HiddenFields) != 0 {
		t.Fatalf("not cleared: %v", team.HiddenFields)
	}
}
