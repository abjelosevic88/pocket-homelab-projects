package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// What Pocket Search, Pocket Day and Pocket Hub ask (capabilities.go). The
// cast is the same everywhere: Alex and Ben share the team Web, Eve has a
// team of her own that neither of them is on.

type capCast struct {
	s              *Server
	h              http.Handler
	alex, ben, eve *apiClient
	web, sec       store.Team
}

func capSetup(t *testing.T, coreURL string) *capCast {
	t.Helper()
	c := &capCast{}
	c.s, c.h = apiServerCore(t, coreURL)
	c.alex = person(t, c.s, c.h, "alex@example.com", "Alex", "admin")
	c.ben = person(t, c.s, c.h, "ben@example.com", "Ben", "member")
	c.eve = person(t, c.s, c.h, "eve@example.com", "Eve", "member")
	c.alex.do("POST", "/api/v1/teams", map[string]any{"name": "Web", "key": "web", "preset": "software"}, 201, &c.web)
	c.alex.do("POST", "/api/v1/teams/WEB/members", map[string]any{"user_id": c.ben.user.ID, "role": "member"}, 200, nil)
	c.eve.do("POST", "/api/v1/teams", map[string]any{"name": "Security", "key": "sec", "preset": "software"}, 201, &c.sec)
	return c
}

// newIssue makes an issue through the API and returns it.
func newIssue(t *testing.T, c *apiClient, body map[string]any) store.Issue {
	t.Helper()
	var is store.Issue
	c.do("POST", "/api/v1/issues", body, 201, &is)
	return is
}

// stateName is the name of a status of the team.
func stateName(team store.Team, id int64) string {
	for _, st := range team.States {
		if st.ID == id {
			return st.Name
		}
	}
	return ""
}

// ask calls a route and reads its JSON answer into out (a pointer, emptied
// first so nothing of an earlier answer stays).
func ask(t *testing.T, c *apiClient, method, path string, body, out any) int {
	t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	code, resp := c.raw(method, path, "application/json", b)
	if out != nil {
		reflect.ValueOf(out).Elem().SetZero()
		if err := json.Unmarshal(resp, out); err != nil {
			t.Fatalf("%s %s: %d %s", method, path, code, resp)
		}
	}
	return code
}

func titles[T any](items []T, title func(T) string) string {
	out := []string{}
	for _, it := range items {
		out = append(out, title(it))
	}
	return strings.Join(out, " | ")
}

func TestPocketSearch(t *testing.T) {
	c := capSetup(t, "")
	var proj store.Project
	c.alex.do("POST", "/api/v1/projects", map[string]any{"name": "Relaunch", "team_ids": []int64{c.web.ID}}, 201, &proj)
	redirect := newIssue(t, c.alex, map[string]any{"team_id": c.web.ID, "title": "Fix the login redirect", "project_id": proj.ID})
	newIssue(t, c.ben, map[string]any{"team_id": c.web.ID, "title": "Footer links", "description": "next to the login button"})
	newIssue(t, c.alex, map[string]any{"team_id": c.web.ID, "title": "Darker header"})
	gone := newIssue(t, c.alex, map[string]any{"team_id": c.web.ID, "title": "Old login page"})
	c.alex.do("DELETE", "/api/v1/issues/"+gone.Identifier, nil, 204, nil)
	newIssue(t, c.eve, map[string]any{"team_id": c.sec.ID, "title": "Login audit"})

	var got core.SearchResults
	if code := ask(t, c.alex, "GET", "/api/v1/pocket/search?q=login", nil, &got); code != 200 || len(got.Items) != 2 {
		t.Fatalf("search: %d %+v", code, got)
	}
	by := map[string]core.SearchItem{}
	for _, it := range got.Items {
		by[it.ID] = it
		if it.Kind != "issue" {
			t.Errorf("%s: kind %q", it.ID, it.Kind)
		}
		if _, err := time.Parse(time.RFC3339, it.Date); err != nil {
			t.Errorf("%s: date %q is not when it last changed", it.ID, it.Date)
		}
	}
	// in a project: the project; otherwise the team (the title is the issue's own, a teammate's too)
	state := stateName(c.web, redirect.StateID)
	if it := by["WEB-1"]; state == "" || it.Title != "Fix the login redirect" || it.Snippet != "WEB-1 · "+state+" · Relaunch" || it.URL != "/issue/WEB-1" {
		t.Errorf("issue in a project: %+v", it)
	}
	if it := by["WEB-2"]; it.Title != "Footer links" || it.Snippet != "WEB-2 · "+state+" · Web" || it.URL != "/issue/WEB-2" {
		t.Errorf("issue found by its description: %+v", it)
	}
	// by identifier
	if ask(t, c.alex, "GET", "/api/v1/pocket/search?q=web-3", nil, &got); len(got.Items) != 1 || got.Items[0].Title != "Darker header" {
		t.Errorf("by identifier: %+v", got.Items)
	}
	if ask(t, c.alex, "GET", "/api/v1/pocket/search?q=login&limit=1", nil, &got); len(got.Items) != 1 {
		t.Errorf("limit=1: %d items", len(got.Items))
	}
	// no words, or nothing to look for: nothing found (never "every issue")
	for _, q := range []string{"", "%3F%3F%3F", "-", "%25"} {
		if code := ask(t, c.alex, "GET", "/api/v1/pocket/search?q="+q, nil, &got); code != 200 || got.Items == nil || len(got.Items) != 0 {
			t.Errorf("q=%q: %d %+v", q, code, got)
		}
	}

	// a team only Eve is on: found by her, by nobody else, not even by its identifier
	for _, q := range []string{"audit", "login", "SEC-1", "security"} {
		ask(t, c.alex, "GET", "/api/v1/pocket/search?q="+q, nil, &got)
		for _, it := range got.Items {
			if strings.HasPrefix(it.ID, "SEC-") || strings.Contains(it.Title+it.Snippet, "audit") {
				t.Errorf("alex finds Eve's issue with %q: %+v", q, it)
			}
		}
	}
	if ask(t, c.eve, "GET", "/api/v1/pocket/search?q=login", nil, &got); len(got.Items) != 1 || got.Items[0].ID != "SEC-1" || got.Items[0].Snippet != "SEC-1 · "+stateName(c.sec, c.sec.States[1].ID)+" · Security" {
		t.Errorf("eve finds: %+v", got.Items)
	}
	if code := ask(t, &apiClient{t: t, h: c.h}, "GET", "/api/v1/pocket/search?q=login", nil, nil); code != 401 {
		t.Errorf("signed out: %d", code)
	}
}

// stamp moves when something happened (nothing in the API can).
func stamp(t *testing.T, s *Server, query string, args ...any) {
	t.Helper()
	res, err := s.store.DB.Exec(query, args...)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		t.Fatalf("nothing stamped by %s %v", query, args)
	}
}

func dayItems(d core.Day) map[string]core.DayItem {
	out := map[string]core.DayItem{}
	for _, it := range d.Items {
		out[it.ID] = it
	}
	return out
}

func TestPocketDay(t *testing.T) {
	c := capSetup(t, "")
	var proj store.Project
	c.alex.do("POST", "/api/v1/projects", map[string]any{"name": "Relaunch", "team_ids": []int64{c.web.ID}}, 201, &proj)
	// Honolulu is ten hours behind UTC: 23:30 there on the 20th is 09:30 UTC on
	// the 21st (and 10:30 on the 21st in the box's zone, Europe/Sarajevo).
	created := func(is store.Issue, at string) {
		stamp(t, c.s, `UPDATE history SET created_at = ? WHERE issue_id = ? AND field = 'created'`, at, is.ID)
	}
	const before = "2026-11-01T08:00:00.000Z"
	done := newIssue(t, c.alex, map[string]any{"team_id": c.web.ID, "title": "Fix the login", "project_id": proj.ID})
	created(done, "2026-11-20T20:00:00.000Z") // 10:00 in Honolulu
	c.alex.do("PATCH", "/api/v1/issues/WEB-1", map[string]any{"state": "completed"}, 200, nil)
	stamp(t, c.s, `UPDATE history SET created_at = ? WHERE issue_id = ? AND field = 'state'`, "2026-11-21T09:30:00.000Z", done.ID)
	talked := newIssue(t, c.ben, map[string]any{"team_id": c.web.ID, "title": "Footer links"})
	created(talked, before)
	c.alex.do("POST", "/api/v1/issues/WEB-2/comments", map[string]any{"body": "Looks fine to me"}, 201, nil)
	stamp(t, c.s, `UPDATE comments SET created_at = ? WHERE issue_id = ?`, "2026-11-20T21:00:00.000Z", talked.ID) // 11:00
	fresh := newIssue(t, c.alex, map[string]any{"team_id": c.web.ID, "title": "Darker header"})
	created(fresh, "2026-11-20T22:00:00.000Z") // 12:00
	moved := newIssue(t, c.alex, map[string]any{"team_id": c.web.ID, "title": "Slow search"})
	created(moved, before)
	c.alex.do("PATCH", "/api/v1/issues/WEB-4", map[string]any{"state": "started"}, 200, nil)
	stamp(t, c.s, `UPDATE history SET created_at = ? WHERE issue_id = ? AND field = 'state'`, "2026-11-20T22:30:00.000Z", moved.ID) // 12:30
	timed := newIssue(t, c.alex, map[string]any{"team_id": c.web.ID, "title": "Read the logs"})
	created(timed, before)
	c.alex.do("POST", "/api/v1/issues/WEB-5/time", map[string]any{"minutes": 80, "spent_on": "2026-11-20"}, 201, nil)
	c.alex.do("POST", "/api/v1/projects/"+i64s(proj.ID)+"/updates", map[string]any{"health": "on_track", "body": "Login is fixed"}, 201, nil)
	stamp(t, c.s, `UPDATE project_updates SET created_at = ? WHERE project_id = ?`, "2026-11-20T22:45:00.000Z", proj.ID) // 12:45
	// a teammate's work and another team's are not part of Alex's day
	bens := newIssue(t, c.ben, map[string]any{"team_id": c.web.ID, "title": "Ben's own"})
	created(bens, "2026-11-20T22:00:00.000Z")
	eves := newIssue(t, c.eve, map[string]any{"team_id": c.sec.ID, "title": "Eve's audit"})
	created(eves, "2026-11-20T22:00:00.000Z")

	var day core.Day
	if code := ask(t, c.alex, "GET", "/api/v1/pocket/day/2026-11-20?tz=Pacific/Honolulu", nil, &day); code != 200 {
		t.Fatalf("day: %d", code)
	}
	if day.Date != "2026-11-20" || day.Summary != "1 issue completed, 1 created, 1 updated, 1 commented on, 1 project update, 1 h 20 min tracked" || len(day.Items) != 6 {
		t.Fatalf("the 20th in Honolulu: %+v", day)
	}
	got := dayItems(day)
	if it := got["issue:WEB-1"]; it.Title != "WEB-1 Fix the login" || it.Sub != "Relaunch" || it.Kind != "completed" || !it.Done || it.Time != "23:30" || it.URL != "/issue/WEB-1" {
		t.Errorf("created at 10:00 and completed at 23:30 local shows once, as completed: %+v", it)
	}
	if it := got["issue:WEB-2"]; it.Title != "WEB-2 Footer links" || it.Sub != "Web" || it.Kind != "commented" || it.Done || it.Time != "11:00" {
		t.Errorf("commented on: %+v", it)
	}
	if it := got["issue:WEB-3"]; it.Kind != "created" || it.Done || it.Time != "12:00" || it.Sub != "Web" {
		t.Errorf("created: %+v", it)
	}
	if it := got["issue:WEB-4"]; it.Kind != "updated" || it.Done || it.Time != "12:30" {
		t.Errorf("moved to in progress: %+v", it)
	}
	if it := got["issue:WEB-5"]; it.Kind != "tracked" || it.Time != "" || it.Minutes != 80 {
		t.Errorf("time tracked: %+v", it)
	}
	if it := got["project:"+i64s(proj.ID)]; it.Title != "Relaunch" || it.Kind != "update" || it.Time != "12:45" || !strings.Contains(it.Sub, "Login is fixed") || it.URL != "/project/"+i64s(proj.ID) {
		t.Errorf("project update: %+v", it)
	}
	want := []core.Metric{{Label: "Completed", Value: "1"}, {Label: "Created", Value: "1"}, {Label: "Time tracked", Value: "1 h 20 min"}}
	if !reflect.DeepEqual(day.Metrics, want) {
		t.Errorf("metrics: %+v, want %+v", day.Metrics, want)
	}

	// the next day there: 23:30 the evening before is not part of it
	if ask(t, c.alex, "GET", "/api/v1/pocket/day/2026-11-21?tz=Pacific/Honolulu", nil, &day); day.Summary != "" || day.Items == nil || len(day.Items) != 0 {
		t.Errorf("the 21st in Honolulu: %+v", day)
	}
	// without tz the person's own zone decides: completed at 10:30 on the 21st
	ask(t, c.alex, "GET", "/api/v1/pocket/day/2026-11-21", nil, &day)
	if it := dayItems(day)["issue:WEB-1"]; day.Summary != "1 issue completed" || len(day.Items) != 1 || it.Time != "10:30" || !it.Done {
		t.Errorf("the 21st in the app's zone: %+v", day)
	}
	// the same numbers GET /day gives for that day
	var work dayWork
	c.alex.do("GET", "/api/v1/day/2026-11-20?tz=Pacific/Honolulu", nil, 200, &work)
	if len(work.Issues) != 5 || len(work.Projects) != 1 || work.Minutes != 80 {
		t.Errorf("GET /day: %d issues, %d projects, %d min", len(work.Issues), len(work.Projects), work.Minutes)
	}

	// strictly per person, and per team
	ask(t, c.ben, "GET", "/api/v1/pocket/day/2026-11-20?tz=Pacific/Honolulu", nil, &day)
	if len(day.Items) != 1 || day.Items[0].ID != "issue:WEB-6" || day.Summary != "1 issue created" {
		t.Errorf("ben's 20th: %+v", day)
	}
	ask(t, c.eve, "GET", "/api/v1/pocket/day/2026-11-20?tz=Pacific/Honolulu", nil, &day)
	if len(day.Items) != 1 || day.Items[0].ID != "issue:SEC-1" || day.Items[0].Sub != "Security" {
		t.Errorf("eve's 20th: %+v", day)
	}
	// Alex leaves nothing behind in a team he is taken off: his day there goes with it
	c.eve.do("POST", "/api/v1/teams/SEC/members", map[string]any{"user_id": c.alex.user.ID, "role": "member"}, 200, nil)
	newIssue(t, c.alex, map[string]any{"team_id": c.sec.ID, "title": "Alex in Security"})
	today := c.s.today(context.Background(), c.alex.user.ID)
	if ask(t, c.alex, "GET", "/api/v1/pocket/day/"+today, nil, &day); dayItems(day)["issue:SEC-2"].Kind != "created" {
		t.Errorf("alex's day in a team he joined: %+v", day)
	}
	c.eve.do("DELETE", "/api/v1/teams/SEC/members/"+i64s(c.alex.user.ID), nil, 200, nil)
	if ask(t, c.alex, "GET", "/api/v1/pocket/day/"+today, nil, &day); strings.Contains(titles(day.Items, func(it core.DayItem) string { return it.ID + it.Title }), "SEC") {
		t.Errorf("a team alex is no longer on still shows in his day: %+v", day)
	}

	if code := ask(t, c.alex, "GET", "/api/v1/pocket/day/1999-01-01", nil, &day); code != 200 || day.Summary != "" || day.Items == nil || len(day.Items) != 0 {
		t.Errorf("a day with nothing: %d %+v", code, day)
	}
	if code := ask(t, c.alex, "GET", "/api/v1/pocket/day/yesterday", nil, nil); code != 400 {
		t.Errorf("not a day: %d", code)
	}
}

func TestPocketWidget(t *testing.T) {
	c := capSetup(t, "")
	loner := person(t, c.s, c.h, "nora@example.com", "Nora", "member")
	today := c.s.today(context.Background(), c.alex.user.ID)
	now, _ := time.Parse("2006-01-02", today)
	day := func(n int) string { return now.AddDate(0, 0, n).Format("2006-01-02") }

	var got core.WidgetData
	// on no team: nothing to show; on a team with nothing assigned: said so
	if code := ask(t, loner, "GET", "/api/v1/pocket/widget", nil, &got); code != 200 || got.Line != "" || got.Items == nil || len(got.Items) != 0 || len(got.Metrics) != 0 {
		t.Fatalf("on no team: %d %+v", code, got)
	}
	if code := ask(t, c.alex, "GET", "/api/v1/pocket/widget", nil, &got); code != 200 || got.Line != "Nothing assigned to you" || got.URL != "/my-issues" || len(got.Items) != 0 {
		t.Fatalf("nothing yet: %d %+v", code, got)
	}

	// new issues go to the person creating them (the team's default assignee)
	newIssue(t, c.alex, map[string]any{"team_id": c.web.ID, "title": "Low and going", "state": "started", "priority": 4, "due_date": day(3)})
	newIssue(t, c.alex, map[string]any{"team_id": c.web.ID, "title": "Urgent and going", "state": "started", "priority": 1})
	late := newIssue(t, c.alex, map[string]any{"team_id": c.web.ID, "title": "Late", "due_date": day(-2)})
	newIssue(t, c.alex, map[string]any{"team_id": c.web.ID, "title": "High, tomorrow", "priority": 2, "due_date": day(1)})
	newIssue(t, c.alex, map[string]any{"team_id": c.web.ID, "title": "High, later", "priority": 2, "due_date": day(20)})
	newIssue(t, c.alex, map[string]any{"team_id": c.web.ID, "title": "Finished", "state": "completed", "due_date": day(-1)})
	newIssue(t, c.alex, map[string]any{"team_id": c.web.ID, "title": "Nobody's", "assignee_id": 0, "due_date": day(-1)})
	newIssue(t, c.alex, map[string]any{"team_id": c.web.ID, "title": "Ben's", "assignee_id": c.ben.user.ID, "due_date": day(-1)})
	newIssue(t, c.eve, map[string]any{"team_id": c.sec.ID, "title": "Eve's", "due_date": day(-1)})

	ask(t, c.alex, "GET", "/api/v1/pocket/widget", nil, &got)
	if got.Line != "2 in progress, 3 due this week (1 overdue)" || got.URL != "/my-issues" {
		t.Errorf("line: %+v", got)
	}
	want := []core.Metric{{Label: "Assigned, open", Value: "5"}, {Label: "In progress", Value: "2"}, {Label: "Overdue", Value: "1"}, {Label: "Due this week", Value: "3"}}
	if !reflect.DeepEqual(got.Metrics, want) {
		t.Errorf("metrics: %+v, want %+v", got.Metrics, want)
	}
	// the numbers are the dashboard's
	var dash map[string]any
	c.alex.do("GET", "/api/v1/dashboard", nil, 200, &dash)
	if dash["assigned_open"] != float64(5) || dash["in_progress"] != float64(2) || dash["overdue"] != float64(1) || dash["due_this_week"] != float64(3) {
		t.Errorf("dashboard: %v %v %v %v", dash["assigned_open"], dash["in_progress"], dash["overdue"], dash["due_this_week"])
	}
	// in progress first, then the rest; each by priority, then by due date
	going, todo := "", stateName(c.web, late.StateID)
	for _, st := range c.web.States {
		if st.Type == "started" && going == "" {
			going = st.Name
		}
	}
	rows := titles(got.Items, func(it core.WidgetItem) string { return it.Title + " (" + it.Detail + ")" })
	wantRows := "WEB-2 Urgent and going (" + going + ") | WEB-1 Low and going (" + going + " · " + dueWords(day(3), today) + ") | WEB-4 High, tomorrow (" + todo + " · due tomorrow) | " +
		"WEB-5 High, later (" + todo + " · " + dueWords(day(20), today) + ") | WEB-3 Late (" + todo + " · " + dueWords(day(-2), today) + ")"
	if rows != wantRows || going == "" || todo == "" {
		t.Errorf("rows: %s\n want: %s", rows, wantRows)
	}
	if !strings.HasPrefix(dueWords(day(-2), today), "was due ") || !strings.HasPrefix(dueWords(day(3), today), "due ") || dueWords("", today) != "" || dueWords(today, today) != "due today" {
		t.Errorf("due words: %q %q %q", dueWords(day(-2), today), dueWords(day(3), today), dueWords(today, today))
	}
	if got.Items[0].ID != "WEB-2" || got.Items[0].URL != "/issue/WEB-2" {
		t.Errorf("row address: %+v", got.Items[0])
	}

	// never more than eight rows, however much is open
	for i := 0; i < 6; i++ {
		newIssue(t, c.alex, map[string]any{"team_id": c.web.ID, "title": "More " + strconv.Itoa(i)})
	}
	ask(t, c.alex, "GET", "/api/v1/pocket/widget", nil, &got)
	if len(got.Items) != 8 || got.Items[0].ID != "WEB-2" || got.Metrics[0].Value != "11" {
		t.Errorf("a long list: %d rows, %+v", len(got.Items), got.Metrics)
	}

	// strictly per person, and per team
	ask(t, c.ben, "GET", "/api/v1/pocket/widget", nil, &got)
	if got.Line != "1 overdue" || len(got.Items) != 1 || got.Items[0].Title != "WEB-8 Ben's" {
		t.Errorf("ben's widget: %+v", got)
	}
	ask(t, c.eve, "GET", "/api/v1/pocket/widget", nil, &got)
	if len(got.Items) != 1 || got.Items[0].ID != "SEC-1" {
		t.Errorf("eve's widget: %+v", got)
	}
	// open work that is neither going nor due is still said
	plain := person(t, c.s, c.h, "pat@example.com", "Pat", "member")
	c.alex.do("POST", "/api/v1/teams/WEB/members", map[string]any{"user_id": plain.user.ID, "role": "member"}, 200, nil)
	newIssue(t, plain, map[string]any{"team_id": c.web.ID, "title": "Some day"})
	if ask(t, plain, "GET", "/api/v1/pocket/widget", nil, &got); got.Line != "1 open issue" || len(got.Items) != 1 || got.Items[0].Detail != todo {
		t.Errorf("one open issue: %+v", got)
	}
}

func capture(t *testing.T, c *apiClient, body map[string]any) (int, core.CaptureResult, string) {
	t.Helper()
	b, _ := json.Marshal(body)
	code, resp := c.raw("POST", "/api/v1/pocket/capture", "application/json", b)
	var res core.CaptureResult
	_ = json.Unmarshal(resp, &res)
	return code, res, string(resp)
}

// issuesOf counts the issues of a team, as one of its members sees them.
func issuesOf(t *testing.T, c *apiClient, key string) int {
	t.Helper()
	var list struct {
		Total int `json:"total"`
	}
	c.do("GET", "/api/v1/issues?team="+key+"&show_completed=all", nil, 200, &list)
	return list.Total
}

func TestPocketCapture(t *testing.T) {
	c := capSetup(t, "")
	loner := person(t, c.s, c.h, "nora@example.com", "Nora", "member")

	// one team: the line is the title and is not repeated as the description
	line := map[string]any{"request_id": "hub-0000000000000001", "kind": "issue", "text": "  Call the   vendor  "}
	code, first, body := capture(t, c.alex, line)
	if code != 201 || first.ID != "WEB-1" || first.Title != "WEB-1 Call the vendor" || first.URL != "/issue/WEB-1" {
		t.Fatalf("capture: %d %s", code, body)
	}
	var is store.Issue
	c.alex.do("GET", "/api/v1/issues/WEB-1", nil, 200, &is)
	// made like an issue in the app: the team's status for new issues, its default assignee (the creator), no source
	made := newIssue(t, c.alex, map[string]any{"team_id": c.web.ID, "title": "By hand"})
	if is.Title != "Call the vendor" || is.Description != "" || is.StateID != made.StateID || is.CreatorID == nil || *is.CreatorID != c.alex.user.ID ||
		is.AssigneeID == nil || *is.AssigneeID != c.alex.user.ID || is.SourceApp != "" || is.DueDate != "" {
		t.Errorf("the issue it made: %+v", is)
	}
	var n int
	if _ = c.s.store.DB.QueryRow(`SELECT COUNT(1) FROM history WHERE issue_id = ? AND field = 'created' AND actor_id = ?`, is.ID, c.alex.user.ID).Scan(&n); n != 1 {
		t.Errorf("history entries for the new issue: %d", n)
	}

	// the same request again answers the same issue and adds nothing
	code, again, body := capture(t, c.alex, line)
	if code != 201 || again != first {
		t.Errorf("same request_id: %d %s, want %+v", code, body, first)
	}
	if n := issuesOf(t, c.alex, "web"); n != 2 {
		t.Errorf("same request_id twice: %d issues, want 2", n)
	}
	// …and it is not a way to keep something else
	if code, _, body := capture(t, c.alex, map[string]any{"request_id": "hub-0000000000000001", "text": "Something else"}); code != 400 {
		t.Errorf("same request_id, other words: %d %s", code, body)
	}
	// another person's same request_id is their own issue (Ben's only team is Web)
	code, his, _ := capture(t, c.ben, line)
	if code != 201 || his.ID != "WEB-3" {
		t.Errorf("ben's capture: %d %+v", code, his)
	}
	c.ben.do("GET", "/api/v1/issues/WEB-3", nil, 200, &is)
	if is.CreatorID == nil || *is.CreatorID != c.ben.user.ID {
		t.Errorf("ben's captured issue: %+v", is)
	}

	// a title and a text: the text is the description; several lines: the first is the title
	code, full, body := capture(t, c.alex, map[string]any{"request_id": "hub-0000000000000002", "title": "Order paint", "text": "Two tins of white\nand a brush"})
	c.alex.do("GET", "/api/v1/issues/"+full.ID, nil, 200, &is)
	if code != 201 || full.Title != "WEB-4 Order paint" || is.Description != "Two tins of white\nand a brush" {
		t.Errorf("capture with a title: %d %s %q", code, body, is.Description)
	}
	_, lines, _ := capture(t, c.alex, map[string]any{"request_id": "hub-0000000000000003", "text": "Plan the offsite\nrooms\nfood"})
	c.alex.do("GET", "/api/v1/issues/"+lines.ID, nil, 200, &is)
	if lines.Title != "WEB-5 Plan the offsite" || is.Description != "rooms\nfood" {
		t.Errorf("several lines: %+v %q", lines, is.Description)
	}
	// a long paragraph: its start is the title, all of it the description
	long := strings.Repeat("Шта је ново у апликацији ", 20)
	_, para, _ := capture(t, c.alex, map[string]any{"request_id": "hub-0000000000000004", "text": long})
	c.alex.do("GET", "/api/v1/issues/"+para.ID, nil, 200, &is)
	if len([]rune(is.Title)) > 200 || !strings.HasSuffix(is.Title, "…") || is.Description != strings.TrimSpace(long) || !strings.HasPrefix(long, strings.TrimSuffix(is.Title, "…")) {
		t.Errorf("a long line: title %q (%d), description %d bytes", is.Title, len([]rune(is.Title)), len(is.Description))
	}

	// two teams: the default team (Settings → Preferences; the first team made), unless the line names another by its key
	var ops store.Team
	c.alex.do("POST", "/api/v1/teams", map[string]any{"name": "Operations", "key": "ops", "preset": "software"}, 201, &ops)
	_, home, _ := capture(t, c.alex, map[string]any{"request_id": "hub-0000000000000005", "text": "Goes home"})
	_, named, _ := capture(t, c.alex, map[string]any{"request_id": "hub-0000000000000006", "text": "ops : rotate the keys\nthe old ones expire"})
	c.alex.do("GET", "/api/v1/issues/OPS-1", nil, 200, &is)
	if !strings.HasPrefix(home.ID, "WEB-") || named.ID != "OPS-1" || named.Title != "OPS-1 rotate the keys" || is.Title != "rotate the keys" || is.Description != "the old ones expire" {
		t.Errorf("default team and a named team: %+v %+v %+v", home, named, is)
	}
	// a first word that is no team of theirs is part of the title
	_, note, _ := capture(t, c.alex, map[string]any{"request_id": "hub-0000000000000007", "title": "Note: ask Ben", "text": "about the keys"})
	if !strings.HasPrefix(note.ID, "WEB-") || !strings.HasSuffix(note.Title, " Note: ask Ben") {
		t.Errorf("a line with a colon: %+v", note)
	}
	if code, _, body := capture(t, c.alex, map[string]any{"request_id": "hub-0000000000000008", "text": "OPS:"}); code != 400 || !strings.Contains(body, "OPS:") {
		t.Errorf("a key and nothing else: %d %s", code, body)
	}

	// a team only Eve is on: its key is no way in, and the line does not say whether it exists
	before := issuesOf(t, c.eve, "sec")
	code, peek, body := capture(t, c.alex, map[string]any{"request_id": "hub-0000000000000009", "text": "SEC: have a look"})
	if code != 201 || !strings.HasPrefix(peek.ID, "WEB-") || !strings.HasSuffix(peek.Title, " SEC: have a look") {
		t.Errorf("another team's key: %d %s", code, body)
	}
	if n := issuesOf(t, c.eve, "sec"); n != before {
		t.Errorf("eve's team got %d issues from alex", n-before)
	}
	// a line kept in a team the person has left since: the retry gets no second look at it
	c.eve.do("POST", "/api/v1/teams/SEC/members", map[string]any{"user_id": c.alex.user.ID, "role": "member"}, 200, nil)
	inSec := map[string]any{"request_id": "hub-0000000000000010", "text": "sec: while I am here"}
	if code, res, body := capture(t, c.alex, inSec); code != 201 || !strings.HasPrefix(res.ID, "SEC-") {
		t.Fatalf("a team alex joined: %d %s", code, body)
	}
	c.eve.do("DELETE", "/api/v1/teams/SEC/members/"+i64s(c.alex.user.ID), nil, 200, nil)
	if code, _, body := capture(t, c.alex, inSec); code != 400 || strings.Contains(body, "SEC") || strings.Contains(body, "while I am here") {
		t.Errorf("the same request after leaving the team: %d %s", code, body)
	}
	if n := issuesOf(t, c.eve, "sec"); n != before+1 {
		t.Errorf("eve's team has %d more issues, want 1", n-before)
	}
	// a guest reads a team and adds nothing to it: its key is no way in either
	c.alex.do("POST", "/api/v1/teams/OPS/members", map[string]any{"user_id": c.ben.user.ID, "role": "guest"}, 200, nil)
	code, guest, body := capture(t, c.ben, map[string]any{"request_id": "hub-000000000000000a", "text": "OPS: let me in"})
	if code != 201 || !strings.HasPrefix(guest.ID, "WEB-") {
		t.Errorf("a guest's capture into the team: %d %s", code, body)
	}
	if n := issuesOf(t, c.alex, "ops"); n != 1 {
		t.Errorf("ops has %d issues, want 1", n)
	}

	// no default team and several to choose from: refused, with how to say the team
	c.alex.do("PUT", "/api/v1/settings", map[string]any{"default_team_id": 0}, 200, nil)
	webBefore := issuesOf(t, c.alex, "web")
	code, _, body = capture(t, c.alex, map[string]any{"request_id": "hub-000000000000000b", "text": "Where does this go"})
	if code != 400 || !strings.Contains(body, `OPS: Fix login`) || !strings.Contains(body, "your teams: OPS, WEB") || strings.Contains(body, "SEC") {
		t.Errorf("no default team: %d %s", code, body)
	}
	// the refusal kept nothing, so the same request can be sent again with the team
	code, retry, body := capture(t, c.alex, map[string]any{"request_id": "hub-000000000000000c", "text": "web: Where does this go"})
	if code != 201 || !strings.HasPrefix(retry.ID, "WEB-") || issuesOf(t, c.alex, "web") != webBefore+1 {
		t.Errorf("with the team: %d %s", code, body)
	}
	// an archived team takes no quick lines
	c.alex.do("PATCH", "/api/v1/teams/OPS", map[string]any{"archived": true}, 200, nil)
	_, only, _ := capture(t, c.alex, map[string]any{"request_id": "hub-000000000000000d", "text": "OPS: after the archive"})
	if !strings.HasPrefix(only.ID, "WEB-") || !strings.HasSuffix(only.Title, " OPS: after the archive") {
		t.Errorf("an archived team's key: %+v", only)
	}

	// on no team at all
	if code, _, body := capture(t, loner, map[string]any{"request_id": "hub-000000000000000e", "text": "Anything"}); code != 400 || !strings.Contains(body, "team") {
		t.Errorf("on no team: %d %s", code, body)
	}
	for name, bad := range map[string]map[string]any{
		"nothing to keep": {"request_id": "hub-000000000000000f", "kind": "issue"},
		"only spaces":     {"request_id": "hub-000000000000000f", "text": "   "},
		"no request_id":   {"text": "Call the vendor"},
		"unknown kind":    {"request_id": "hub-000000000000000f", "kind": "project", "text": "Call the vendor"},
	} {
		if code, _, body := capture(t, c.alex, bad); code != 400 {
			t.Errorf("%s: %d %s", name, code, body)
		}
	}
}

// A captured issue is the team's from the start, like one made in the app:
// whoever the team gives new issues to hears about it, and so does someone
// the line mentions.
func TestPocketCaptureTellsTheTeam(t *testing.T) {
	c := capSetup(t, "")
	unread := func(p *apiClient) int {
		var out map[string]int
		p.do("GET", "/api/v1/notifications/count", nil, 200, &out)
		return out["unread"]
	}
	had := unread(c.ben) // being added to the team
	if code, _, body := capture(t, c.alex, map[string]any{"request_id": "hub-0000000000000001", "text": "Mine by default"}); code != 201 || unread(c.ben) != had {
		t.Fatalf("a capture that is the person's own: %d %s, ben has %d unread", code, body, unread(c.ben))
	}
	c.alex.do("PATCH", "/api/v1/teams/WEB", map[string]any{"default_assignee": i64s(c.ben.user.ID)}, 200, nil)
	_, res, _ := capture(t, c.alex, map[string]any{"request_id": "hub-0000000000000002", "text": "Goes to Ben"})
	var is store.Issue
	c.alex.do("GET", "/api/v1/issues/"+res.ID, nil, 200, &is) // (Ben opening it would read his notification)
	if is.AssigneeID == nil || *is.AssigneeID != c.ben.user.ID || unread(c.ben) != had+1 {
		t.Errorf("the team's default assignee: %+v, ben has %d unread", is.AssigneeID, unread(c.ben))
	}
	c.alex.do("PATCH", "/api/v1/teams/WEB", map[string]any{"default_assignee": "none"}, 200, nil)
	capture(t, c.alex, map[string]any{"request_id": "hub-0000000000000003", "text": "Look at this\n@ben what do you think?"})
	if unread(c.ben) != had+2 || unread(c.eve) != 0 {
		t.Errorf("a mention: ben has %d unread, eve %d", unread(c.ben), unread(c.eve))
	}
}

// The same line sent several times at once (a retry racing the first try)
// still makes one issue, and every answer names it.
func TestPocketCaptureAtOnce(t *testing.T) {
	c := capSetup(t, "")
	line := map[string]any{"request_id": "hub-000000000000000a", "text": "Only once"}
	const n = 8
	ids := make(chan string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, _ := json.Marshal(line)
			code, resp := c.alex.raw("POST", "/api/v1/pocket/capture", "application/json", b)
			var res core.CaptureResult
			_ = json.Unmarshal(resp, &res)
			if code != 201 {
				res.ID = "HTTP " + strconv.Itoa(code) + " " + string(resp)
			}
			ids <- res.ID
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[string]int{}
	for id := range ids {
		seen[id]++
	}
	if len(seen) != 1 || seen["WEB-1"] != n {
		t.Errorf("answers: %v", seen)
	}
	if got := issuesOf(t, c.alex, "web"); got != 1 {
		t.Errorf("%d issues, want 1", got)
	}
}

func TestPocketLinkTokens(t *testing.T) {
	fc := &fakeTimeCore{links: map[string][3]string{
		"pcx_search":  {core.ScopeSearch, "search", "alex@example.com"},
		"pcx_day":     {core.ScopeDay, "day", "alex@example.com"},
		"pcx_widget":  {core.ScopeWidget, "hub", "alex@example.com"},
		"pcx_capture": {core.ScopeCapture, "hub", "alex@example.com"},
		"pcx_read":    {"projects:read", "calendar", "alex@example.com"},
		"pcx_rw":      {"projects:read projects:write", "ai", "alex@example.com"},
		"pcx_time":    {"worklog:read worklog:write", "time", "alex@example.com"},
		"pcx_move":    {core.ScopeMoveExport + " " + core.ScopeMoveImport, "move", "alex@example.com"},
	}}
	csrv := httptest.NewServer(fc)
	t.Cleanup(csrv.Close)
	c := capSetup(t, csrv.URL)
	today := c.s.today(context.Background(), c.alex.user.ID)
	newIssue(t, c.alex, map[string]any{"team_id": c.web.ID, "title": "Alex's bread"})
	newIssue(t, c.ben, map[string]any{"team_id": c.web.ID, "title": "Ben's bread"})
	newIssue(t, c.eve, map[string]any{"team_id": c.sec.ID, "title": "Eve's bread"})

	line := map[string]any{"request_id": "hub-0000000000000001", "text": "Kept through a link"}
	routes := []struct {
		scope, method, path string
		body                any
		ok                  int
	}{
		{"search", "GET", "/api/v1/pocket/search?q=bread", nil, 200},
		{"day", "GET", "/api/v1/pocket/day/" + today + "?tz=Europe/Sarajevo", nil, 200},
		{"widget", "GET", "/api/v1/pocket/widget", nil, 200},
		{"capture", "POST", "/api/v1/pocket/capture", line, 201},
	}
	for _, tok := range []string{"search", "day", "widget", "capture", "read", "rw", "time", "move"} {
		lc := &apiClient{t: t, h: c.h, token: "pcx_" + tok}
		for _, rt := range routes {
			want := 403 // the app's own scopes do not open the new routes either
			if rt.scope == tok {
				want = rt.ok
			}
			var b []byte
			if rt.body != nil {
				b, _ = json.Marshal(rt.body)
			}
			code, resp := lc.raw(rt.method, rt.path, "application/json", b)
			if code != want {
				t.Errorf("pcx_%s %s %s: %d, want %d (%s)", tok, rt.method, rt.path, code, want, resp)
			}
			if strings.Contains(string(resp), "Eve") {
				t.Errorf("pcx_%s %s: another team's issue: %s", tok, rt.path, resp)
			}
		}
		if code := lc.status("GET", "/api/v1/auth/me", nil); code != 200 {
			t.Errorf("pcx_%s /auth/me: %d", tok, code)
		}
		if len(tok) <= 4 { // read, rw, time, move: the app's own scopes
			continue
		}
		// a capability opens its one route with its one method, nothing else of the app
		for _, rt := range []struct{ method, path string }{
			{"GET", "/api/v1/issues"}, {"GET", "/api/v1/search?q=bread"}, {"GET", "/api/v1/dashboard"}, {"GET", "/api/v1/day/" + today}, {"POST", "/api/v1/issues"},
			{"GET", "/api/v1/teams"}, {"GET", "/api/v1/bootstrap"}, {"GET", "/api/v1/worklog"}, {"GET", "/api/v1/export.json"}, {"GET", core.PortablePath},
			{"GET", "/api/v1/settings"}, {"GET", "/api/v1/remotes"}, {"GET", "/r/1/api/v1/pocket/search?q=bread"},
		} {
			if code := lc.status(rt.method, rt.path, map[string]any{"title": "nope", "team": "WEB"}); code != 403 {
				t.Errorf("pcx_%s %s %s: %d, want 403", tok, rt.method, rt.path, code)
			}
		}
	}
	// the other method on a capability's own route is no way in either
	for _, rt := range []struct{ tok, method, path string }{{"search", "POST", "/api/v1/pocket/search?q=bread"}, {"capture", "GET", "/api/v1/pocket/capture"}, {"widget", "DELETE", "/api/v1/pocket/widget"}} {
		if code := (&apiClient{t: t, h: c.h, token: "pcx_" + rt.tok}).status(rt.method, rt.path, nil); code != 403 && code != 405 {
			t.Errorf("pcx_%s %s %s: %d", rt.tok, rt.method, rt.path, code)
		}
	}
	// the app's own scopes still open what they opened
	for _, rt := range []struct{ tok, method, path string }{{"read", "GET", "/api/v1/search?q=bread"}, {"read", "GET", "/api/v1/day/" + today}, {"read", "GET", "/api/v1/dashboard"},
		{"time", "GET", "/api/v1/worklog"}, {"move", "GET", core.PortablePath}} {
		if code := (&apiClient{t: t, h: c.h, token: "pcx_" + rt.tok}).status(rt.method, rt.path, nil); code != 200 {
			t.Errorf("pcx_%s %s %s: %d, want 200", rt.tok, rt.method, rt.path, code)
		}
	}

	// what came back is what alex may see: his team's, never Eve's
	var found core.SearchResults
	ask(t, &apiClient{t: t, h: c.h, token: "pcx_search"}, "GET", "/api/v1/pocket/search?q=bread", nil, &found)
	if got := titles(found.Items, func(it core.SearchItem) string { return it.Title }); got != "Ben's bread | Alex's bread" {
		t.Errorf("link search: %s", got)
	}
	var day core.Day
	ask(t, &apiClient{t: t, h: c.h, token: "pcx_day"}, "GET", "/api/v1/pocket/day/"+today, nil, &day)
	if got := titles(day.Items, func(it core.DayItem) string { return it.Title + " " + it.Kind }); got != "WEB-3 Kept through a link created | WEB-1 Alex's bread created" && got != "WEB-1 Alex's bread created | WEB-3 Kept through a link created" {
		t.Errorf("link day: %s", got)
	}
	var wd core.WidgetData
	ask(t, &apiClient{t: t, h: c.h, token: "pcx_widget"}, "GET", "/api/v1/pocket/widget", nil, &wd)
	if wd.Metrics[0].Value != "2" || len(wd.Items) != 2 {
		t.Errorf("link widget: %+v", wd)
	}

	// the same capture again through the link: still one issue; it is alex's and says which app made it
	hub := &apiClient{t: t, h: c.h, token: "pcx_capture"}
	code, first, body := capture(t, hub, line)
	code2, again, _ := capture(t, hub, line)
	if code != 201 || code2 != 201 || first.ID != "WEB-3" || first != again {
		t.Fatalf("link capture twice: %d %d %s", code, code2, body)
	}
	var is store.Issue
	c.alex.do("GET", "/api/v1/issues/WEB-3", nil, 200, &is)
	if is.Title != "Kept through a link" || is.SourceApp != "hub" || is.CreatorID == nil || *is.CreatorID != c.alex.user.ID {
		t.Errorf("the issue the link made: %+v", is)
	}
	var n int
	if _ = c.s.store.DB.QueryRow(`SELECT COUNT(1) FROM history WHERE issue_id = ? AND field = 'created' AND actor_app = 'hub'`, is.ID).Scan(&n); n != 1 {
		t.Errorf("history entries by the app: %d", n)
	}
	// its Core event is queued with the issue, once, naming the app
	rows, err := c.s.store.DB.Query(`SELECT body FROM pocket_outbox`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	events := 0
	for rows.Next() {
		var b []byte
		_ = rows.Scan(&b)
		if bytes.Contains(b, []byte("projects.issue_created")) && bytes.Contains(b, []byte(`"WEB-3"`)) {
			events++
			if !bytes.Contains(b, []byte(`"source":"hub"`)) {
				t.Errorf("the event does not name the app: %s", b)
			}
		}
	}
	if events != 1 {
		t.Errorf("issue_created events for the captured issue: %d", events)
	}
	if got := issuesOf(t, c.alex, "web"); got != 3 {
		t.Errorf("web has %d issues, want 3", got)
	}
	if code, _, body := capture(t, hub, map[string]any{"request_id": "hub-0000000000000009"}); code != 400 {
		t.Errorf("link capture with nothing: %d %s", code, body)
	}
	// a team that sorts what other apps send: the line waits in its triage, as for any app
	c.alex.do("POST", "/api/v1/teams", map[string]any{"name": "Support", "key": "sup", "preset": "software", "triage_enabled": true}, 201, nil)
	_, sorted, _ := capture(t, hub, map[string]any{"request_id": "hub-0000000000000002", "text": "SUP: To be sorted"})
	c.alex.do("GET", "/api/v1/issues/"+sorted.ID, nil, 200, &is)
	_, own, _ := capture(t, c.alex, map[string]any{"request_id": "hub-0000000000000003", "text": "SUP: My own line"})
	var mine store.Issue
	c.alex.do("GET", "/api/v1/issues/"+own.ID, nil, 200, &mine)
	if sorted.ID != "SUP-1" || is.StateType != "triage" || mine.StateType == "triage" {
		t.Errorf("into a team with triage: through a link %q, as the person %q", is.StateType, mine.StateType)
	}
}

// A connected instance is another server's data: nothing Pocket Search, Day
// or Hub ask reaches it, and its /r/{id} address has no such routes.
func TestPocketRoutesStayHere(t *testing.T) {
	// over there
	sB, hB := apiServer(t)
	var asked atomic.Int64
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		hB.ServeHTTP(w, r)
	}))
	t.Cleanup(srvB.Close)
	sB.cfg.BaseURL = srvB.URL
	bob := person(t, sB, hB, "bob@athena.example", "Bob", "admin")
	alexThere := person(t, sB, hB, "alex@example.com", "Alex", "member")
	var ath store.Team
	bob.do("POST", "/api/v1/teams", map[string]any{"name": "Athena", "key": "ath", "preset": "software"}, 201, &ath)
	bob.do("POST", "/api/v1/teams/ATH/members", map[string]any{"user_id": alexThere.user.ID, "role": "member"}, 200, nil)
	newIssue(t, alexThere, map[string]any{"team_id": ath.ID, "title": "Bread over there", "state": "started"})

	// here
	c := capSetup(t, "")
	newIssue(t, c.alex, map[string]any{"team_id": c.web.ID, "title": "Bread here", "state": "started"})
	var rem remoteOut
	c.alex.do("POST", "/api/v1/remotes", map[string]any{"name": "Athena", "url": srvB.URL, "token": alexThere.token}, 201, &rem)
	today := c.s.today(context.Background(), c.alex.user.ID)
	base := "/r/" + i64s(rem.ID) + "/api/v1"
	// the workspace is open in a window, so agents may use it (workspace_guard.go)
	c.alex.do("PUT", "/api/v1/workspaces/agent", map[string]any{"id": rem.ID}, 200, nil)

	asked.Store(0)
	var found core.SearchResults
	ask(t, c.alex, "GET", "/api/v1/pocket/search?q=bread", nil, &found)
	if len(found.Items) != 1 || found.Items[0].ID != "WEB-1" {
		t.Errorf("search: %+v", found.Items)
	}
	var day core.Day
	ask(t, c.alex, "GET", "/api/v1/pocket/day/"+today, nil, &day)
	if len(day.Items) != 1 || day.Items[0].ID != "issue:WEB-1" {
		t.Errorf("day: %+v", day.Items)
	}
	var wd core.WidgetData
	ask(t, c.alex, "GET", "/api/v1/pocket/widget", nil, &wd)
	if len(wd.Items) != 1 || wd.Items[0].ID != "WEB-1" || wd.Line != "1 in progress" {
		t.Errorf("widget: %+v", wd)
	}
	if code, res, body := capture(t, c.alex, map[string]any{"request_id": "hub-0000000000000001", "text": "ATH: over there?"}); code != 201 || !strings.HasPrefix(res.ID, "WEB-") {
		t.Errorf("capture with the other instance's team key: %d %s", code, body)
	}
	// the connection's address has none of the four routes
	for _, rt := range []struct{ method, path string }{{"GET", "/pocket/search?q=bread"}, {"GET", "/pocket/day/" + today}, {"GET", "/pocket/widget"}, {"POST", "/pocket/capture"}} {
		if code, resp := c.alex.raw(rt.method, base+rt.path, "application/json", []byte(`{"request_id":"hub-0000000000000002","text":"Over there"}`)); code != 404 || strings.Contains(string(resp), "over there") {
			t.Errorf("%s %s%s: %d %s", rt.method, base, rt.path, code, resp)
		}
	}
	// a tool call that names the connection as its workspace is not sent there either
	for _, rt := range []struct{ method, path string }{{"GET", "/api/v1/pocket/search?q=bread"}, {"GET", "/api/v1/pocket/widget"}, {"POST", "/api/v1/pocket/capture"}} {
		for _, workspace := range []string{strings.TrimPrefix(srvB.URL, "http://"), ""} {
			req := httptest.NewRequest(rt.method, rt.path, strings.NewReader(`{"request_id":"hub-0000000000000003","text":"From a tool"}`))
			req.Header.Set("Authorization", "Bearer "+c.alex.token)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Pocket-Source", "mcp")
			if workspace != "" {
				req.Header.Set("X-Pocket-Workspace", workspace)
			}
			w := httptest.NewRecorder()
			c.h.ServeHTTP(w, req)
			if w.Code < 400 || strings.Contains(w.Body.String(), "over there") {
				t.Errorf("tool call %s %s (workspace %q): %d %s", rt.method, rt.path, workspace, w.Code, w.Body.String())
			}
		}
	}
	if n := asked.Load(); n != 0 {
		t.Errorf("the connected instance was asked %d times", n)
	}
	if n := issuesOf(t, bob, "ath"); n != 1 {
		t.Errorf("the connected instance has %d issues, want 1", n)
	}
	// the proxy itself still works for everything else
	var boot struct {
		Teams []store.Team `json:"teams"`
	}
	c.alex.do("GET", base+"/bootstrap", nil, 200, &boot)
	if len(boot.Teams) != 1 || boot.Teams[0].Key != "ATH" || asked.Load() == 0 {
		t.Errorf("bootstrap there: %+v", boot.Teams)
	}
}

// The manifest Core gets says what Projects can do, so nobody has to list it.
func TestManifestOffersCapabilities(t *testing.T) {
	s, _ := apiServer(t)
	b, err := json.Marshal(s.manifest())
	if err != nil {
		t.Fatal(err)
	}
	var sent map[string]any
	_ = json.Unmarshal(b, &sent)
	if sent["slug"] != "projects" || sent["category"] != "Work" {
		t.Errorf("manifest: slug %v, category %v", sent["slug"], sent["category"])
	}
	for _, k := range []string{"search", "day", "widget", "capture", "portable"} {
		if sent[k] == nil {
			t.Errorf("manifest has no %q", k)
		}
	}
	bangs, _ := sent["search"].(map[string]any)["bangs"].([]any)
	if len(bangs) != 3 || bangs[0] != "issues" || bangs[1] != "issue" || bangs[2] != "projects" {
		t.Errorf("bangs: %v", bangs)
	}
	kinds, _ := sent["capture"].(map[string]any)["kinds"].([]any)
	if len(kinds) != 1 || kinds[0].(map[string]any)["kind"] != "issue" {
		t.Errorf("capture kinds: %v", sent["capture"])
	}
	if feed, _ := sent["day"].(map[string]any); feed["label"] != "Projects" || feed["one"] != "issue" || feed["many"] != "issues" || feed["unit"] != "count" {
		t.Errorf("day feed: %v", sent["day"])
	}
	// Register adds one offer per capability, next to the app's own
	offered := map[string]bool{}
	for _, o := range capabilities.Offers() {
		offered[o.Scope] = true
	}
	for _, sc := range []string{core.ScopeSearch, core.ScopeDay, core.ScopeWidget, core.ScopeCapture} {
		if !offered[sc] {
			t.Errorf("no offer for %s: %v", sc, offered)
		}
	}
	for _, o := range linkOffers {
		if strings.HasPrefix(o.Scope, "pocket:") {
			t.Errorf("the app's own offers name %s", o.Scope)
		}
	}
}
