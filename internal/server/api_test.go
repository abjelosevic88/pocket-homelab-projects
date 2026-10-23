package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/config"
	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

func apiServer(t *testing.T) (*Server, http.Handler) {
	return apiServerCore(t, "")
}

// apiServerCore runs with a Pocket Core address (nothing answers there): every
// change then also queues its Core event inside the saving transaction.
func apiServerCore(t *testing.T, coreURL string) (*Server, http.Handler) {
	t.Helper()
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("AUTH_MODE", "local")
	t.Setenv("TZ", "Europe/Sarajevo")
	t.Setenv("CORE_URL", coreURL)
	t.Setenv("BASE_URL", "https://projects.example")
	t.Setenv("DEMO_DATA", "")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(cfg.DBPath, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := New(cfg, st, log, nil)
	if err := s.core.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s, s.Router()
}

type apiClient struct {
	t     *testing.T
	h     http.Handler
	token string
	user  *store.User
}

func person(t *testing.T, s *Server, h http.Handler, email, name, role string) *apiClient {
	t.Helper()
	ctx := context.Background()
	u := &store.User{Email: email, Name: name, Role: role}
	if err := s.store.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	tok, _, err := s.store.CreateAPIToken(ctx, u.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	return &apiClient{t: t, h: h, token: tok, user: u}
}

func (c *apiClient) raw(method, path, ctype string, body []byte) (int, []byte) {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

func (c *apiClient) do(method, path string, body any, want int, out any) {
	c.t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	code, resp := c.raw(method, path, "application/json", b)
	if code != want {
		c.t.Fatalf("%s %s: want %d, got %d: %s", method, path, want, code, resp)
	}
	if out != nil {
		if err := json.Unmarshal(resp, out); err != nil {
			c.t.Fatalf("%s %s: decode: %v (%s)", method, path, err, resp)
		}
	}
}

func (c *apiClient) status(method, path string, body any) int {
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	code, _ := c.raw(method, path, "application/json", b)
	return code
}

func TestTeamsIssuesAndAccess(t *testing.T) {
	s, h := apiServer(t)
	alex := person(t, s, h, "alex@example.com", "Alex", "admin")
	ben := person(t, s, h, "ben@example.com", "Ben", "member")
	eve := person(t, s, h, "eve@example.com", "Eve", "member")

	var team store.Team
	alex.do("POST", "/api/v1/teams", map[string]any{"name": "Web", "key": "web", "preset": "software"}, 201, &team)
	if team.Key != "WEB" || len(team.States) < 5 || team.Role != "admin" || len(team.Labels) == 0 {
		t.Fatalf("team: %+v", team)
	}
	if alex.status("POST", "/api/v1/teams", map[string]any{"name": "Other", "key": "WEB"}) != 409 {
		t.Fatal("duplicate key accepted")
	}
	alex.do("POST", "/api/v1/teams/WEB/members", map[string]any{"user_id": ben.user.ID, "role": "member"}, 200, nil)

	var is store.Issue
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": team.ID, "title": "Fix login", "priority": 2, "assignee": "ben@example.com", "labels": []string{"Bug", "Brand new"}}, 201, &is)
	if is.Identifier != "WEB-1" || is.AssigneeID == nil || *is.AssigneeID != ben.user.ID || len(is.LabelIDs) != 2 || is.StateType != "unstarted" {
		t.Fatalf("issue: %+v", is)
	}
	// Ben was assigned: one notification.
	var inbox struct {
		Notifications []store.Notification `json:"notifications"`
		Unread        int                  `json:"unread"`
	}
	ben.do("GET", "/api/v1/notifications", nil, 200, &inbox)
	if inbox.Unread != 2 || inbox.Notifications[0].Kind != "assigned" || inbox.Notifications[0].Issue == nil || inbox.Notifications[1].Kind != "added" {
		t.Fatalf("inbox: %+v", inbox)
	}

	// Eve is on no team: everything answers 404, lists are empty.
	if c := eve.status("GET", "/api/v1/issues/WEB-1", nil); c != 404 {
		t.Fatalf("eve sees the issue: %d", c)
	}
	if c := eve.status("GET", "/api/v1/teams/WEB", nil); c != 404 {
		t.Fatalf("eve sees the team: %d", c)
	}
	if c := eve.status("PATCH", fmt.Sprintf("/api/v1/issues/%d", is.ID), map[string]any{"title": "x"}); c != 404 {
		t.Fatalf("eve changes the issue: %d", c)
	}
	var list struct {
		Issues []store.Issue `json:"issues"`
		Total  int           `json:"total"`
	}
	eve.do("GET", "/api/v1/issues", nil, 200, &list)
	if list.Total != 0 {
		t.Fatal("eve lists issues")
	}

	// Move to In Progress, then Done: stamps and history.
	var states []store.State
	alex.do("GET", "/api/v1/teams/WEB/states", nil, 200, &states)
	byName := map[string]int64{}
	for _, st := range states {
		byName[st.Name] = st.ID
	}
	ben.do("PATCH", "/api/v1/issues/WEB-1", map[string]any{"state_id": byName["In Progress"]}, 200, &is)
	if is.StartedAt == nil || is.StateType != "started" {
		t.Fatalf("not started: %+v", is)
	}
	ben.do("PATCH", "/api/v1/issues/WEB-1", map[string]any{"state": "done"}, 200, &is)
	if is.CompletedAt == nil || is.StateType != "completed" {
		t.Fatalf("not completed: %+v", is)
	}
	var hist []store.History
	alex.do("GET", "/api/v1/issues/WEB-1/history", nil, 200, &hist)
	if len(hist) != 3 || hist[1].Field != "state" || hist[2].ToLabel != "Done" {
		t.Fatalf("history: %+v", hist)
	}
	// Alex created the issue and follows it: the status change reached the inbox.
	alex.do("GET", "/api/v1/notifications", nil, 200, &inbox)
	if inbox.Unread == 0 {
		t.Fatal("no status notification for the creator")
	}

	// A guest reads and comments but cannot change issues.
	alex.do("POST", "/api/v1/teams/WEB/members", map[string]any{"user_id": eve.user.ID, "role": "guest"}, 200, nil)
	if c := eve.status("PATCH", "/api/v1/issues/WEB-1", map[string]any{"title": "x"}); c != 403 {
		t.Fatalf("guest changed an issue: %d", c)
	}
	var c store.Comment
	eve.do("POST", "/api/v1/issues/WEB-1/comments", map[string]any{"body": "Looks good @ben"}, 201, &c)
	ben.do("GET", "/api/v1/notifications?filter=unread", nil, 200, &inbox)
	found := false
	for _, n := range inbox.Notifications {
		found = found || n.Kind == "mentioned"
	}
	if !found {
		t.Fatalf("no mention: %+v", inbox.Notifications)
	}
	if c := eve.status("POST", "/api/v1/issues", map[string]any{"team_id": team.ID, "title": "x"}); c != 403 {
		t.Fatalf("guest created an issue: %d", c)
	}
}

func TestLabelGroupsSubIssuesRelationsAndMove(t *testing.T) {
	s, h := apiServer(t)
	alex := person(t, s, h, "alex@example.com", "Alex", "admin")
	var web, home store.Team
	alex.do("POST", "/api/v1/teams", map[string]any{"name": "Web", "key": "WEB"}, 201, &web)
	alex.do("POST", "/api/v1/teams", map[string]any{"name": "Home", "key": "HOME", "preset": "household"}, 201, &home)

	var group, small, large store.Label
	alex.do("POST", "/api/v1/teams/WEB/labels", map[string]any{"name": "Size", "is_group": true}, 201, &group)
	alex.do("POST", "/api/v1/teams/WEB/labels", map[string]any{"name": "Small", "parent_id": group.ID}, 201, &small)
	alex.do("POST", "/api/v1/teams/WEB/labels", map[string]any{"name": "Large", "parent_id": group.ID}, 201, &large)
	var is store.Issue
	alex.do("POST", "/api/v1/issues", map[string]any{"team": "WEB", "title": "Parent", "label_ids": []int64{small.ID, large.ID, group.ID}, "sub_issues": []string{"One", "Two"}}, 201, &is)
	if len(is.LabelIDs) != 1 || is.LabelIDs[0] != large.ID {
		t.Fatalf("group not exclusive: %v", is.LabelIDs)
	}
	if is.SubTotal != 2 {
		t.Fatalf("sub-issues: %d", is.SubTotal)
	}
	var b store.Issue
	alex.do("POST", "/api/v1/issues", map[string]any{"team": "WEB", "title": "Blocker"}, 201, &b)
	alex.do("POST", "/api/v1/issues/WEB-1/relations", map[string]any{"type": "blocked_by", "issue": b.Identifier}, 201, nil)
	alex.do("GET", "/api/v1/issues/WEB-1", nil, 200, &is)
	if is.BlockedBy != 1 {
		t.Fatalf("blocked_by: %d", is.BlockedBy)
	}
	// A parent cannot go inside its own sub-issue.
	if c := alex.status("PATCH", "/api/v1/issues/WEB-1", map[string]any{"parent_id": "WEB-2"}); c != 400 {
		t.Fatalf("loop accepted: %d", c)
	}
	// Move to HOME: a new identifier, the old one still resolves.
	alex.do("PATCH", "/api/v1/issues/WEB-4", map[string]any{"team_id": home.ID}, 200, &b)
	if b.Identifier != "HOME-1" || b.TeamID != home.ID {
		t.Fatalf("moved: %+v", b)
	}
	alex.do("GET", "/api/v1/issues/WEB-4", nil, 200, &b)
	if b.Identifier != "HOME-1" {
		t.Fatal("old identifier lost")
	}
	// Duplicate closes the duplicate.
	var d store.Issue
	alex.do("POST", "/api/v1/issues", map[string]any{"team": "WEB", "title": "Same as parent"}, 201, &d)
	alex.do("POST", "/api/v1/issues/"+d.Identifier+"/relations", map[string]any{"type": "duplicate", "issue": "WEB-1"}, 201, nil)
	alex.do("GET", "/api/v1/issues/"+d.Identifier, nil, 200, &d)
	if d.StateType != "canceled" {
		t.Fatalf("duplicate not closed: %s", d.StateType)
	}
	// Trash and restore with sub-issues.
	alex.do("DELETE", "/api/v1/issues/WEB-1", nil, 204, nil)
	var list struct {
		Total int `json:"total"`
	}
	alex.do("GET", "/api/v1/issues?team=WEB", nil, 200, &list)
	if list.Total != 1 {
		t.Fatalf("after trash: %d", list.Total)
	}
	alex.do("POST", "/api/v1/issues/WEB-1/restore", nil, 200, nil)
	alex.do("GET", "/api/v1/issues?team=WEB", nil, 200, &list)
	if list.Total != 4 {
		t.Fatalf("after restore: %d", list.Total)
	}
}

func TestProjectsCyclesAndCalendar(t *testing.T) {
	s, h := apiServer(t)
	alex := person(t, s, h, "alex@example.com", "Alex", "admin")
	eve := person(t, s, h, "eve@example.com", "Eve", "member")
	var team store.Team
	alex.do("POST", "/api/v1/teams", map[string]any{"name": "Web", "key": "WEB", "cycles_enabled": true}, 201, &team)
	var p store.Project
	today := s.boxToday()
	alex.do("POST", "/api/v1/projects", map[string]any{"name": "Relaunch", "team_ids": []int64{team.ID}, "status": "started",
		"target_date": today.AddDate(0, 0, 10).Format("2006-01-02"), "milestones": []map[string]string{{"name": "Beta", "target_date": today.AddDate(0, 0, 5).Format("2006-01-02")}}}, 201, &p)
	if p.LeadID == nil || *p.LeadID != alex.user.ID || p.StartedAt == nil {
		t.Fatalf("project: %+v", p)
	}
	if c := eve.status("GET", fmt.Sprintf("/api/v1/projects/%d", p.ID), nil); c != 404 {
		t.Fatalf("eve sees the project: %d", c)
	}
	for i := 0; i < 4; i++ {
		alex.do("POST", "/api/v1/issues", map[string]any{"team": "WEB", "title": fmt.Sprintf("Task %d", i), "project_id": p.ID, "cycle": "current",
			"due_date": today.AddDate(0, 0, i).Format("2006-01-02"), "assignee": "me", "estimate": 2}, 201, nil)
	}
	alex.do("PATCH", "/api/v1/issues/WEB-1", map[string]any{"state": "completed"}, 200, nil)
	alex.do("GET", fmt.Sprintf("/api/v1/projects/%d", p.ID), nil, 200, &p)
	if p.Progress.Total != 4 || p.Progress.Completed != 1 || p.Progress.Percent != 25 || len(p.Milestones) != 1 {
		t.Fatalf("progress: %+v", p.Progress)
	}
	var upd store.ProjectUpdate
	alex.do("POST", fmt.Sprintf("/api/v1/projects/%d/updates", p.ID), map[string]any{"health": "at_risk", "body": "Slipping"}, 201, &upd)
	alex.do("GET", fmt.Sprintf("/api/v1/projects/%d", p.ID), nil, 200, &p)
	if p.Health != "at_risk" {
		t.Fatalf("health: %s", p.Health)
	}

	var cycles []store.Cycle
	alex.do("GET", "/api/v1/teams/WEB/cycles", nil, 200, &cycles)
	if len(cycles) < 3 || cycles[0].Status != "current" || cycles[0].Progress.Total != 4 {
		t.Fatalf("cycles: %+v", cycles)
	}
	// Ending the cycle moves the three unfinished issues to the next one.
	var res struct {
		Moved int `json:"moved"`
	}
	alex.do("POST", fmt.Sprintf("/api/v1/cycles/%d/complete", cycles[0].ID), map[string]any{}, 200, &res)
	if res.Moved != 3 {
		t.Fatalf("moved %d", res.Moved)
	}
	var is store.Issue
	alex.do("GET", "/api/v1/issues/WEB-2", nil, 200, &is)
	if is.CycleID == nil || *is.CycleID != cycles[1].ID {
		t.Fatalf("not rolled over: %v", is.CycleID)
	}

	var cal struct {
		Items []CalendarItem `json:"items"`
	}
	alex.do("GET", "/api/v1/calendar?from="+today.Format("2006-01-02")+"&to="+today.AddDate(0, 0, 20).Format("2006-01-02"), nil, 200, &cal)
	kinds := map[string]int{}
	for _, it := range cal.Items {
		kinds[it.Kind]++
	}
	if kinds["issue"] != 4 || kinds["milestone"] != 1 || kinds["project_target"] != 1 || kinds["cycle"] == 0 {
		t.Fatalf("calendar: %v", kinds)
	}
	// The secret feed works without a login.
	u, _ := s.store.GetUser(context.Background(), alex.user.ID)
	anon := &apiClient{t: t, h: h}
	code, body := anon.raw("GET", "/feed/"+u.FeedToken+".ics", "", nil)
	if code != 200 || !strings.Contains(string(body), "BEGIN:VEVENT") || !strings.Contains(string(body), "WEB-2") {
		t.Fatalf("feed %d: %s", code, body)
	}
	if code, _ := anon.raw("GET", "/feed/nope.ics", "", nil); code != 404 {
		t.Fatal("feed without a valid token")
	}

	var dash map[string]any
	alex.do("GET", "/api/v1/dashboard", nil, 200, &dash)
	if dash["assigned_open"].(float64) != 3 {
		t.Fatalf("dashboard: %v", dash)
	}
	var ins store.Insights
	alex.do("GET", "/api/v1/insights?team_id="+fmt.Sprint(team.ID), nil, 200, &ins)
	if ins.Totals.Open != 3 || ins.Totals.Completed != 1 {
		t.Fatalf("insights: %+v", ins.Totals)
	}
}

func TestGitHookTemplatesAndImport(t *testing.T) {
	s, h := apiServer(t)
	alex := person(t, s, h, "alex@example.com", "Alex", "admin")
	var team store.Team
	alex.do("POST", "/api/v1/teams", map[string]any{"name": "Web", "key": "WEB"}, 201, &team)
	alex.do("POST", "/api/v1/issues", map[string]any{"team": "WEB", "title": "Login bug"}, 201, nil)
	var git struct {
		URL string `json:"url"`
	}
	alex.do("GET", "/api/v1/teams/WEB/git", nil, 200, &git)
	path := git.URL[strings.Index(git.URL, "/hooks/"):]
	payload := `{"ref":"refs/heads/main","repository":{"full_name":"me/site","default_branch":"main"},"commits":[{"id":"abcdef1234","message":"Fixes WEB-1: handle cookies\n\nmore","url":"https://git.example/c/abcdef1234","author":{"name":"Alex"}}]}`
	req := httptest.NewRequest("POST", path, strings.NewReader(payload))
	req.Header.Set("X-Forgejo-Event", "push")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"linked":1`) {
		t.Fatalf("hook: %d %s", w.Code, w.Body.String())
	}
	var d struct {
		store.Issue
		Attachments []store.Attachment `json:"attachment_list"`
	}
	alex.do("GET", "/api/v1/issues/WEB-1", nil, 200, &d)
	if d.StateType != "completed" || len(d.Attachments) != 1 || d.Attachments[0].Kind != "commit" {
		t.Fatalf("after push: %s %+v", d.StateType, d.Attachments)
	}

	// A repeating template makes its issue when due and moves on.
	var tmpl store.Template
	alex.do("POST", "/api/v1/teams/WEB/templates", map[string]any{"name": "Weekly backup check", "title": "Check backups", "recur": "weekly",
		"data": map[string]any{"priority": 2, "sub_issues": []string{"Restore one file"}}}, 201, &tmpl)
	s.recurringJob(context.Background())
	var list struct {
		Issues []store.Issue `json:"issues"`
	}
	alex.do("GET", "/api/v1/issues?q=backups", nil, 200, &list)
	if len(list.Issues) != 1 || list.Issues[0].Priority != 2 || list.Issues[0].SubTotal != 1 {
		t.Fatalf("repeating: %+v", list.Issues)
	}
	got, _ := s.store.GetTemplate(context.Background(), tmpl.ID)
	if got.NextAt <= s.boxToday().Format("2006-01-02") {
		t.Fatalf("next_at not advanced: %s", got.NextAt)
	}
	s.recurringJob(context.Background())
	alex.do("GET", "/api/v1/issues?q=backups", nil, 200, &list)
	if len(list.Issues) != 1 {
		t.Fatal("repeating issue made twice")
	}

	// CSV import in Linear's shape, with a parent.
	csv := "ID,Title,Description,Status,Priority,Labels,Project,Parent issue,Due Date\n" +
		"ENG-1,Epic thing,,In Progress,High,\"Feature, Docs\",Imported,,2026-12-01\n" +
		"ENG-2,Child,body,Todo,Low,,Imported,ENG-1,\n"
	var imp struct {
		Created int      `json:"created"`
		Failed  []string `json:"failed"`
	}
	alex.do("POST", "/api/v1/teams/WEB/import", map[string]any{"csv": csv}, 200, &imp)
	if imp.Created != 2 || len(imp.Failed) != 0 {
		t.Fatalf("import: %+v", imp)
	}
	var child store.Issue
	alex.do("GET", "/api/v1/issues/ENG-2", nil, 200, &child)
	if child.ParentID == nil || child.ProjectID == nil || child.Priority != 4 {
		t.Fatalf("imported child: %+v", child)
	}
}

func TestLinkRoutes(t *testing.T) {
	read := []string{"projects:read"}
	for _, c := range []struct {
		method, path string
		scopes       []string
		ok           bool
	}{
		{"GET", "/api/v1/issues", read, true},
		{"GET", "/api/v1/issues/WEB-12", read, true},
		{"GET", "/api/v1/related", read, true},
		{"GET", "/api/v1/projects/3/updates", read, true},
		{"POST", "/api/v1/issues", read, false},
		{"POST", "/api/v1/issues", []string{"projects:write"}, true},
		{"DELETE", "/api/v1/issues/3", []string{"projects:write"}, false},
		{"POST", "/api/v1/issues/3/pull-requests", []string{"projects:write"}, true},
		{"POST", "/api/v1/issues/3/pull-requests", read, false},
		{"PUT", "/api/v1/teams/WEB/git-token", []string{"projects:write"}, false},
		{"GET", "/api/v1/tokens", read, false},
		{"GET", "/api/v1/settings", read, false},
	} {
		r := httptest.NewRequest(c.method, c.path, nil)
		if got := linkAllows(c.scopes, r); got != c.ok {
			t.Errorf("%s %s %v: got %v", c.method, c.path, c.scopes, got)
		}
	}
}

func TestDemoSeedsWithCore(t *testing.T) {
	s, h := apiServerCore(t, "http://127.0.0.1:1")
	done := make(chan error, 1)
	go func() { done <- s.seedDemo(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("seeding with Core configured hangs (a query inside a transaction?)")
	}
	var n int
	_ = s.store.DB.QueryRow(`SELECT COUNT(1) FROM pocket_outbox`).Scan(&n)
	if n < 10 {
		t.Fatalf("core events queued: %d", n)
	}
	_ = h
}

func TestDemoSeeds(t *testing.T) {
	s, h := apiServer(t)
	if err := s.seedDemo(context.Background()); err != nil {
		t.Fatal(err)
	}
	u, _ := s.store.GetUserByEmail(context.Background(), "admin@localhost")
	tok, _, _ := s.store.CreateAPIToken(context.Background(), u.ID, "t")
	c := &apiClient{t: t, h: h, token: tok}
	var boot map[string]json.RawMessage
	c.do("GET", "/api/v1/bootstrap", nil, 200, &boot)
	var teams []store.Team
	_ = json.Unmarshal(boot["teams"], &teams)
	if len(teams) != 2 {
		t.Fatalf("teams: %d", len(teams))
	}
	var list struct {
		Total int `json:"total"`
	}
	c.do("GET", "/api/v1/issues", nil, 200, &list)
	if list.Total < 10 {
		t.Fatalf("issues: %d", list.Total)
	}
}

func TestIdempotentCreate(t *testing.T) {
	s, h := apiServer(t)
	alex := person(t, s, h, "alex@example.com", "Alex", "admin")
	alex.do("POST", "/api/v1/teams", map[string]any{"name": "Web", "key": "WEB"}, 201, nil)
	post := func(path string, body any) (int, []byte) {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+alex.token)
		req.Header.Set("Idempotency-Key", "mcp-same-request-123456")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w.Code, w.Body.Bytes()
	}
	c1, b1 := post("/api/v1/issues", map[string]any{"team": "WEB", "title": "Once"})
	c2, b2 := post("/api/v1/issues", map[string]any{"team": "WEB", "title": "Once"})
	var i1, i2 store.Issue
	_ = json.Unmarshal(b1, &i1)
	_ = json.Unmarshal(b2, &i2)
	if c1 != 201 || c2 != 200 || i1.ID != i2.ID {
		t.Fatalf("retry made another issue: %d %d %d %d", c1, c2, i1.ID, i2.ID)
	}
	if c, _ := post("/api/v1/issues", map[string]any{"team": "WEB", "title": "Different"}); c != 409 {
		t.Fatalf("changed request under the same key: %d", c)
	}
	c3, _ := post("/api/v1/issues/WEB-1/comments", map[string]any{"body": "hi"})
	c4, _ := post("/api/v1/issues/WEB-1/comments", map[string]any{"body": "hi"})
	var cs []store.Comment
	alex.do("GET", "/api/v1/issues/WEB-1/comments", nil, 200, &cs)
	if c3 != 201 || c4 != 200 || len(cs) != 1 {
		t.Fatalf("comment retry: %d %d %d", c3, c4, len(cs))
	}
}
