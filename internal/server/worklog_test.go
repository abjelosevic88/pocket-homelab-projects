package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// fakeTimeCore is a Pocket Core that hands out link tokens to Pocket Time
// (or refuses them while timeOff) and introspects the link tokens Pocket
// Time and others use here.
type fakeTimeCore struct {
	mu      sync.Mutex
	timeURL string
	timeOff bool
	links   map[string][3]string // token → scope, app slug, email
}

func (f *fakeTimeCore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/api/v1/links/token":
		var in struct {
			App    string   `json:"app"`
			Scopes []string `json:"scopes"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		if in.App != "time" || f.timeOff {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]any{"error": "switched off", "reason": "off_admin"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"access_token": "pcx_tofake", "expires_in": 300, "scope": strings.Join(in.Scopes, " "), "app": in.App,
			"url": f.timeURL, "public_url": "https://time.example"})
	case "/api/v1/auth/introspect":
		var in struct{ Token string }
		json.NewDecoder(r.Body).Decode(&in)
		l, ok := f.links[in.Token]
		if !ok {
			json.NewEncoder(w).Encode(map[string]any{"active": false})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"active": true, "kind": "delegated", "scope": l[0], "audience": "projects", "iss": "https://core.example",
			"user": map[string]any{"id": 1, "sub": "u-" + l[2], "email": l[2], "name": l[2]}, "app": map[string]any{"id": 9, "slug": l[1]}})
	default:
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}
}

// fakeTime is Pocket Time's shared timer: one running entry, 409 on a
// second start unless switch, only_if on stop.
type fakeTime struct {
	mu      sync.Mutex
	next    int64
	running *timeEntry
	starts  []map[string]any
	stops   []map[string]any
}

func (f *fakeTime) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("Authorization") != "Bearer pcx_tofake" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var in map[string]any
	_ = json.NewDecoder(r.Body).Decode(&in)
	switch r.Method + " " + r.URL.Path {
	case "GET /api/v1/timer":
		json.NewEncoder(w).Encode(map[string]any{"running": f.running, "now": time.Now().Format(time.RFC3339)})
	case "POST /api/v1/timer/start":
		f.starts = append(f.starts, in)
		var stopped *timeEntry
		if f.running != nil {
			if in["switch"] != true {
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]any{"error": "a timer is already running", "running": f.running})
				return
			}
			stopped = f.running
		}
		f.next++
		f.running = &timeEntry{ID: f.next, Running: true, Source: "projects", App: "projects", AppRef: in["app_ref"].(string), AppLabel: in["app_label"].(string),
			AppURL: in["app_url"].(string), Title: in["app_label"].(string), StartedAt: time.Now().Format(time.RFC3339)}
		json.NewEncoder(w).Encode(map[string]any{"running": f.running, "stopped": stopped})
	case "POST /api/v1/timer/stop":
		f.stops = append(f.stops, in)
		if id, ok := in["only_if"].(float64); ok && f.running != nil && int64(id) != f.running.ID {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]any{"error": "a different timer is running now", "running": f.running})
			return
		}
		stopped := f.running
		f.running = nil
		json.NewEncoder(w).Encode(map[string]any{"running": nil, "stopped": stopped})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func timeSetup(t *testing.T) (*Server, http.Handler, *fakeTimeCore, *fakeTime) {
	t.Helper()
	ft := &fakeTime{}
	tsrv := httptest.NewServer(ft)
	t.Cleanup(tsrv.Close)
	fc := &fakeTimeCore{timeURL: tsrv.URL, links: map[string][3]string{
		"pcx_time_alex":  {"worklog:read worklog:write", "time", "alex@example.com"},
		"pcx_time_eve":   {"worklog:read worklog:write", "time", "eve@example.com"},
		"pcx_timeread":   {"worklog:read", "time", "alex@example.com"},
		"pcx_inv_alex":   {"worklog:read worklog:write", "invoicing", "alex@example.com"},
		"pcx_projs_alex": {"projects:read", "draw", "alex@example.com"},
	}}
	csrv := httptest.NewServer(fc)
	t.Cleanup(csrv.Close)
	s, h := apiServerCore(t, csrv.URL)
	return s, h, fc, ft
}

type worklogRow struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	StartedAt  string `json:"started_at"`
	EndedAt    string `json:"ended_at"`
	Date       string `json:"date"`
	DurationS  int    `json:"duration_s"`
	Ref        string `json:"ref"`
	Label      string `json:"label"`
	URL        string `json:"url"`
	Source     string `json:"source"`
	ExternalID string `json:"external_id"`
}

func worklogOf(t *testing.T, c *apiClient) []worklogRow {
	t.Helper()
	var res struct {
		Entries []worklogRow `json:"entries"`
	}
	c.do("GET", "/api/v1/worklog?from=2000-01-01&to=2100-01-01", nil, 200, &res)
	return res.Entries
}

func TestWorklogContract(t *testing.T) {
	s, h, fc, _ := timeSetup(t)
	fc.timeOff = true // the Projects → Time link is off: the timer here stays local
	alex := person(t, s, h, "alex@example.com", "Alex", "admin")
	eve := person(t, s, h, "eve@example.com", "Eve", "member")
	var web, ops store.Team
	alex.do("POST", "/api/v1/teams", map[string]any{"name": "Web", "key": "web", "preset": "software"}, 201, &web)
	eve.do("POST", "/api/v1/teams", map[string]any{"name": "Ops", "key": "ops", "preset": "software"}, 201, &ops)
	var login, other, opsIs store.Issue
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": web.ID, "title": "Fix the login redirect", "assignee_id": alex.user.ID}, 201, &login)
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": web.ID, "title": "Footer links"}, 201, &other)
	eve.do("POST", "/api/v1/issues", map[string]any{"team_id": ops.ID, "title": "Rotate keys"}, 201, &opsIs)

	alex.do("POST", "/api/v1/issues/"+login.Identifier+"/time", map[string]any{"duration": "1h30m", "note": "private thoughts"}, 201, nil)
	var tv map[string]any
	alex.do("POST", "/api/v1/issues/"+other.Identifier+"/timer", nil, 200, &tv)
	if tv["running"] != true || tv["shared"] != false {
		t.Fatalf("local timer with the link off: %+v", tv)
	}

	tm := &apiClient{t: t, h: h, token: "pcx_time_alex"}
	// Only finished time, never notes; the running timer is left out.
	got := worklogOf(t, tm)
	if len(got) != 1 || got[0].Ref != "issue:"+login.Identifier || got[0].DurationS != 5400 || got[0].Source != "app" || got[0].URL != "/issue/"+login.Identifier ||
		got[0].Label != login.Identifier+" Fix the login redirect" || got[0].Title != "Fix the login redirect" || got[0].ExternalID != "" {
		t.Fatalf("worklog: %+v", got)
	}
	code, raw := tm.raw("GET", "/api/v1/worklog?from=2000-01-01&to=2100-01-01", "", nil)
	if code != 200 || strings.Contains(string(raw), "private") || strings.Contains(string(raw), `"note"`) {
		t.Fatalf("worklog shows notes: %d %s", code, raw)
	}
	if tm.status("GET", "/api/v1/worklog?from=yesterday", nil) != 400 {
		t.Fatal("a bad date went through")
	}

	// Items: the person's issues (time logged, assigned first), not Eve's team; q searches.
	var items struct {
		Items []map[string]string `json:"items"`
	}
	tm.do("GET", "/api/v1/worklog/items", nil, 200, &items)
	// the timed issue (the running timer's, most recent) first, then the one with logged time
	if len(items.Items) != 2 || items.Items[0]["ref"] != "issue:"+other.Identifier || items.Items[1]["ref"] != "issue:"+login.Identifier ||
		!strings.HasPrefix(items.Items[1]["detail"], "Web · ") || items.Items[1]["url"] != "/issue/"+login.Identifier {
		t.Fatalf("items: %+v", items.Items)
	}
	tm.do("GET", "/api/v1/worklog/items?q=footer", nil, 200, &items)
	if len(items.Items) != 1 || items.Items[0]["ref"] != "issue:"+other.Identifier {
		t.Fatalf("items q=footer: %+v", items.Items)
	}
	tm.do("GET", "/api/v1/worklog/items?q=rotate", nil, 200, &items)
	if len(items.Items) != 0 {
		t.Fatalf("Eve's issue offered to Alex: %+v", items.Items)
	}

	// POST is idempotent on external_id; a new ref moves the record.
	var saved struct{ ID, URL string }
	body := map[string]any{"ref": "issue:" + login.Identifier, "title": "Login", "note": "from Time", "started_at": "2026-10-28T09:10:00+01:00",
		"ended_at": "2026-10-28T09:35:00+01:00", "date": "2026-10-28", "duration_s": 1500, "billable": true, "project": "Harbor", "external_id": "time:812"}
	tm.do("POST", "/api/v1/worklog", body, 200, &saved)
	if saved.ID == "" || saved.URL != "/issue/"+login.Identifier {
		t.Fatalf("saved: %+v", saved)
	}
	body["duration_s"] = 1800
	var again struct{ ID, URL string }
	tm.do("POST", "/api/v1/worklog", body, 200, &again)
	if again.ID != saved.ID {
		t.Fatalf("same external_id made a second record: %s vs %s", again.ID, saved.ID)
	}
	got = worklogOf(t, tm)
	var fromTime *worklogRow
	for i := range got {
		if got[i].ID == saved.ID {
			fromTime = &got[i]
		}
	}
	if len(got) != 2 || fromTime == nil || fromTime.DurationS != 1800 || fromTime.Source != "time" || fromTime.ExternalID != "time:812" ||
		fromTime.Date != "2026-10-28" || fromTime.StartedAt != "2026-10-28T09:10:00+01:00" || fromTime.EndedAt != "2026-10-28T09:40:00+01:00" {
		t.Fatalf("after the update: %+v", got)
	}
	body["ref"] = "issue:" + other.Identifier
	tm.do("POST", "/api/v1/worklog", body, 200, &again)
	if again.ID != saved.ID || again.URL != "/issue/"+other.Identifier {
		t.Fatalf("moved: %+v", again)
	}
	if list, _ := s.store.TimeEntries(context.Background(), other.ID); len(list) != 2 { // the running timer + Time's
		t.Fatalf("time on %s: %+v", other.Identifier, list)
	}

	// Unknown refs, issues of a team the person is not on: 400 in plain words.
	for _, ref := range []string{"issue:" + opsIs.Identifier, "issue:NOPE-9", "", "tab:42"} {
		code, raw := tm.raw("POST", "/api/v1/worklog", "application/json", mustJSON(map[string]any{"ref": ref, "duration_s": 600}))
		if code != 400 || !strings.Contains(string(raw), "issue") {
			t.Fatalf("ref %q: %d %s", ref, code, raw)
		}
	}
	if tm.status("POST", "/api/v1/worklog", map[string]any{"ref": "issue:" + login.Identifier}) != 400 ||
		tm.status("POST", "/api/v1/worklog", map[string]any{"ref": "issue:" + login.Identifier, "duration_s": -5}) != 400 ||
		tm.status("POST", "/api/v1/worklog", map[string]any{"ref": "issue:" + login.Identifier, "duration_s": 8 * 24 * 3600}) != 400 {
		t.Fatal("no length, a negative one or more than a week went through")
	}
	// a timer stopped at once counts a minute, as here
	var quick struct{ ID string }
	tm.do("POST", "/api/v1/worklog", map[string]any{"ref": "issue:" + login.Identifier, "duration_s": 0, "external_id": "time:813"}, 200, &quick)
	if e, err := s.store.GetTimeEntry(context.Background(), idOf(quick.ID)); err != nil || e.Minutes != 1 {
		t.Fatalf("0 s: %+v %v", e, err)
	}
	tm.do("DELETE", "/api/v1/worklog/"+quick.ID, nil, 200, nil)

	// Scopes: read-only can't write; the worklog scopes don't open the rest.
	ro := &apiClient{t: t, h: h, token: "pcx_timeread"}
	if ro.status("GET", "/api/v1/worklog/items", nil) != 200 || ro.status("POST", "/api/v1/worklog", body) != 403 || ro.status("DELETE", "/api/v1/worklog/"+saved.ID, nil) != 403 {
		t.Fatal("worklog:read wrote")
	}
	if tm.status("GET", "/api/v1/issues", nil) != 403 || tm.status("GET", "/api/v1/time", nil) != 403 || tm.status("POST", "/api/v1/issues/"+login.Identifier+"/timer", nil) != 403 {
		t.Fatal("worklog scopes reached other routes")
	}
	pr := &apiClient{t: t, h: h, token: "pcx_projs_alex"}
	if pr.status("GET", "/api/v1/worklog", nil) != 403 {
		t.Fatal("projects:read reached the worklog")
	}

	// Another app only touches what it logged itself; the same external_id is its own record.
	inv := &apiClient{t: t, h: h, token: "pcx_inv_alex"}
	manual := ""
	for _, g := range got {
		if g.Source == "app" {
			manual = g.ID
		}
	}
	if manual == "" || inv.status("DELETE", "/api/v1/worklog/"+saved.ID, nil) != 403 || inv.status("DELETE", "/api/v1/worklog/"+manual, nil) != 403 {
		t.Fatal("another app removed records it did not make")
	}
	var invSaved struct{ ID string }
	inv.do("POST", "/api/v1/worklog", map[string]any{"ref": "issue:" + login.Identifier, "duration_s": 600, "external_id": "time:812"}, 200, &invSaved)
	if invSaved.ID == saved.ID {
		t.Fatal("another app's POST overwrote Time's record")
	}
	if e, err := s.store.GetTimeEntry(context.Background(), idOf(saved.ID)); err != nil || e.Minutes != 30 || e.Source != "time" {
		t.Fatalf("Time's record changed: %+v %v", e, err)
	}

	// Per person: Eve sees none of Alex's time and can't remove it.
	ev := &apiClient{t: t, h: h, token: "pcx_time_eve"}
	if len(worklogOf(t, ev)) != 0 || ev.status("DELETE", "/api/v1/worklog/"+saved.ID, nil) != 404 {
		t.Fatal("Eve reached Alex's time")
	}
	// The running timer is not a record to remove.
	if run := s.store.RunningTimer(context.Background(), alex.user.ID); run == nil || tm.status("DELETE", "/api/v1/worklog/"+i64s(run.ID), nil) != 404 {
		t.Fatal("the running timer could be removed through the worklog")
	}
	if tm.status("DELETE", "/api/v1/worklog/"+saved.ID, nil) != 200 || tm.status("DELETE", "/api/v1/worklog/"+saved.ID, nil) != 404 {
		t.Fatal("Time could not remove its own record once")
	}
}

func TestSharedTimerThroughPocketTime(t *testing.T) {
	s, h, fc, ft := timeSetup(t)
	alex := person(t, s, h, "alex@example.com", "Alex", "admin")
	var web store.Team
	alex.do("POST", "/api/v1/teams", map[string]any{"name": "Web", "key": "web", "preset": "software"}, 201, &web)
	var a, b store.Issue
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": web.ID, "title": "Fix the login redirect"}, 201, &a)
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": web.ID, "title": "Footer links"}, 201, &b)
	ctx := context.Background()

	type view struct {
		Running bool         `json:"running"`
		Shared  bool         `json:"shared"`
		Time    *sharedTimer `json:"time"`
		Stopped *sharedTimer `json:"stopped"`
		Error   string       `json:"error"`
	}
	var v view
	do := func(method, path string, body any, want int) {
		t.Helper()
		v = view{}
		alex.do(method, path, body, want, &v)
	}
	alex.do("POST", "/api/v1/issues/"+a.Identifier+"/timer", nil, 200, &v)
	if !v.Shared || !v.Running || v.Time == nil || !v.Time.Here || v.Time.Issue == nil || v.Time.Issue.Identifier != a.Identifier || v.Time.URL != "/issue/"+a.Identifier {
		t.Fatalf("started through Time: %+v", v)
	}
	st := ft.starts[0]
	if st["app_ref"] != "issue:"+a.Identifier || st["app_label"] != a.Identifier+" Fix the login redirect" || st["app_url"] != "/issue/"+a.Identifier || st["deliver"] != true || st["switch"] != false ||
		st["app"] != "projects" || st["source"] != "projects" {
		t.Fatalf("start sent to Time: %+v", st)
	}
	if s.store.RunningTimer(ctx, alex.user.ID) != nil {
		t.Fatal("a shared timer also ran here")
	}
	do("GET", "/api/v1/timer", nil, 200)
	if !v.Shared || v.Time == nil || v.Time.ID != 1 || v.Time.StartedIn != "Pocket Projects" {
		t.Fatalf("GET /timer: %+v", v)
	}

	// One at a time: another start says what runs, then switches when asked.
	code, raw := alex.raw("POST", "/api/v1/issues/"+b.Identifier+"/timer", "application/json", nil)
	if code != 409 || !strings.Contains(string(raw), a.Identifier) || !strings.Contains(string(raw), "Pocket Projects") {
		t.Fatalf("second start: %d %s", code, raw)
	}
	do("POST", "/api/v1/issues/"+b.Identifier+"/timer", map[string]any{"switch": true}, 200)
	if v.Time == nil || v.Time.Issue == nil || v.Time.Issue.Identifier != b.Identifier || v.Stopped == nil || v.Stopped.ID != 1 {
		t.Fatalf("switched: %+v", v)
	}

	// A timer started elsewhere shows, with where it was started.
	ft.mu.Lock()
	ft.running = &timeEntry{ID: 9, Running: true, Source: "guitar", App: "guitar", AppRef: "tab:42", AppLabel: "Wonderwall", Title: "Wonderwall", StartedAt: time.Now().Format(time.RFC3339)}
	ft.mu.Unlock()
	do("GET", "/api/v1/timer", nil, 200)
	if v.Time == nil || v.Time.Here || v.Time.Name != "Wonderwall" || v.Time.StartedIn != "Pocket Guitar" || v.Time.URL != "https://time.example" {
		t.Fatalf("timer from Guitar: %+v", v)
	}
	// Stop with only_if: another one running → 409, left running.
	if code, _ := alex.raw("POST", "/api/v1/timer/stop", "application/json", mustJSON(map[string]any{"only_if": 2})); code != 409 {
		t.Fatalf("stop of a timer no longer running: %d", code)
	}
	do("POST", "/api/v1/timer/stop", map[string]any{"only_if": 9}, 200)
	if v.Running || v.Stopped == nil || v.Stopped.ID != 9 || ft.running != nil {
		t.Fatalf("stopped: %+v", v)
	}
	// Time writes the time back; nothing was logged here by the timer itself.
	if list, _ := s.store.TimeEntries(ctx, a.ID); len(list) != 0 {
		t.Fatalf("shared timer logged here too: %+v", list)
	}

	// The test button.
	var test struct {
		OK     bool   `json:"ok"`
		Detail string `json:"detail"`
	}
	alex.do("POST", "/api/v1/integrations/time/test", nil, 200, &test)
	if !test.OK || !strings.Contains(test.Detail, "No timer") {
		t.Fatalf("test: %+v", test)
	}

	// The person switches the connection off: the local timer again, as before.
	alex.do("PUT", "/api/v1/integrations/time", map[string]any{"mode": "off"}, 200, nil)
	do("POST", "/api/v1/issues/"+a.Identifier+"/timer", nil, 200)
	if v.Shared || !v.Running || s.store.RunningTimer(ctx, alex.user.ID) == nil || len(ft.starts) != 3 {
		t.Fatalf("off: %+v (starts %d)", v, len(ft.starts))
	}
	alex.do("POST", "/api/v1/integrations/time/test", nil, 200, &test)
	if test.OK {
		t.Fatalf("test while off: %+v", test)
	}
	// Back on: the local timer left from before stops with the shared one.
	alex.do("PUT", "/api/v1/integrations/time", map[string]any{"mode": "core"}, 200, nil)
	do("GET", "/api/v1/timer", nil, 200)
	if !v.Shared || !v.Running || v.Time != nil {
		t.Fatalf("local leftover while shared: %+v", v)
	}
	do("POST", "/api/v1/timer/stop", nil, 200)
	if s.store.RunningTimer(ctx, alex.user.ID) != nil {
		t.Fatal("the local leftover kept running")
	}
	if list, _ := s.store.TimeEntries(ctx, a.ID); len(list) != 1 || list[0].BeganAt == "" {
		t.Fatalf("local timer's record: %+v", list)
	}

	// Link switched off in Core: local again.
	fc.mu.Lock()
	fc.timeOff = true
	fc.mu.Unlock()
	do("POST", "/api/v1/issues/"+b.Identifier+"/timer", nil, 200)
	if v.Shared || s.store.RunningTimer(ctx, alex.user.ID) == nil {
		t.Fatalf("link off: %+v", v)
	}
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func idOf(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }

func i64s(n int64) string { return strconv.FormatInt(n, 10) }
