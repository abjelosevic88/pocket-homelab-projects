package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// The person's inbox over there reaches them here; agent writes by bare
// number need the workspace said; the token there is sealed in the database.
func TestConnectedInboxGuardAndSealedToken(t *testing.T) {
	sB, hB := apiServer(t)
	srvB := httptest.NewServer(hB)
	t.Cleanup(srvB.Close)
	sB.cfg.BaseURL = srvB.URL
	bob := person(t, sB, hB, "bob@athena.example", "Bob", "admin")
	alexThere := person(t, sB, hB, "alex@example.com", "Alex", "member")
	var team store.Team
	bob.do("POST", "/api/v1/teams", map[string]any{"name": "Athena", "key": "ath", "preset": "software"}, 201, &team)
	bob.do("POST", "/api/v1/teams/ATH/members", map[string]any{"user_id": alexThere.user.ID, "role": "member"}, 200, nil)
	var there store.Issue
	bob.do("POST", "/api/v1/issues", map[string]any{"team_id": team.ID, "title": "Pilot setup"}, 201, &there)
	// a team over there with the key of one here: said when connecting
	bob.do("POST", "/api/v1/teams", map[string]any{"name": "Their web", "key": "web", "preset": "software"}, 201, nil)
	bob.do("POST", "/api/v1/teams/WEB/members", map[string]any{"user_id": alexThere.user.ID, "role": "member"}, 200, nil)

	sA, hA := apiServer(t)
	sA.cfg.BaseURL = "https://projects.home.example"
	alex := person(t, sA, hA, "alex@example.com", "Alex", "admin")
	var pushes []map[string]string
	var mu sync.Mutex
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]string
		_ = json.NewDecoder(r.Body).Decode(&m)
		mu.Lock()
		pushes = append(pushes, m)
		mu.Unlock()
	}))
	t.Cleanup(hook.Close)
	alex.do("PUT", "/api/v1/settings", map[string]any{"notify_kind": "webhook", "notify_url": hook.URL}, 200, nil)
	var mine store.Team
	alex.do("POST", "/api/v1/teams", map[string]any{"name": "Web", "key": "web", "preset": "software"}, 201, &mine)
	var here store.Issue
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": mine.ID, "title": "Home thing"}, 201, &here)

	// an agent's write by bare number: fine while there is only this instance
	call := func(method, path, workspace string, body any) (int, string) {
		t.Helper()
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer "+alex.token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Pocket-Source", "mcp")
		if workspace != "" {
			req.Header.Set("X-Pocket-Workspace", workspace)
		}
		w := httptest.NewRecorder()
		hA.ServeHTTP(w, req)
		return w.Code, w.Body.String()
	}
	tool := func(method, path, workspace string, body any) int {
		t.Helper()
		c, _ := call(method, path, workspace, body)
		return c
	}
	num := "/api/v1/issues/" + strconv.FormatInt(here.ID, 10)
	if c := tool("POST", num+"/comments", "", map[string]any{"body": "one"}); c != 201 {
		t.Fatalf("no connections yet: %d", c)
	}
	var only store.Team
	alex.do("POST", "/api/v1/teams", map[string]any{"name": "Mine", "key": "mine", "preset": "software"}, 201, &only)
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": only.ID, "title": "Only here"}, 201, nil)

	var rem struct {
		remoteOut
		SameKeys []string `json:"same_keys"`
	}
	alex.do("POST", "/api/v1/remotes", map[string]any{"name": "Athena", "url": srvB.URL, "token": alexThere.token}, 201, &rem)
	if strings.Join(rem.SameKeys, ",") != "WEB" {
		t.Fatalf("same keys: %v", rem.SameKeys)
	}

	// the token is sealed in the database, and still works
	var raw string
	if err := sA.store.DB.QueryRow(`SELECT token FROM remotes WHERE id = ?`, rem.ID).Scan(&raw); err != nil || !strings.HasPrefix(raw, "enc1:") || strings.Contains(raw, alexThere.token) {
		t.Fatalf("token stored as %q (%v)", raw, err)
	}
	got, err := sA.store.GetRemote(context.Background(), rem.ID, alex.user.ID)
	if err != nil || got.Token != alexThere.token {
		t.Fatalf("token read back: %v", err)
	}

	own, addr := "projects.home.example", strings.ToLower(strings.TrimPrefix(srvB.URL, "http://"))
	expect := func(code int, method, path, ws string, body any, why string) {
		t.Helper()
		if c, b := call(method, path, ws, body); c != code {
			t.Fatalf("%s: %s %s (%s) = %d %s", why, method, path, ws, c, b)
		}
	}
	// every change says its workspace, and names its target so a wrong one finds nothing
	expect(409, "POST", "/api/v1/issues/MINE-1/comments", "", map[string]any{"body": "x"}, "no workspace")
	expect(409, "POST", num+"/comments", own, map[string]any{"body": "x"}, "bare number")
	expect(201, "POST", "/api/v1/issues/MINE-1/comments", own, map[string]any{"body": "by name"}, "identifier here")
	expect(409, "POST", "/api/v1/issues/WEB-1/comments", own, map[string]any{"body": "x"}, "key in both workspaces")
	expect(409, "POST", "/api/v1/issues/ATH-1/comments", own, map[string]any{"body": "x"}, "team only over addr")
	expect(409, "POST", "/api/v1/issues", own, map[string]any{"title": "x"}, "new issue without team")
	expect(201, "POST", "/api/v1/issues", own, map[string]any{"title": "New here", "team": "MINE"}, "new issue by key")
	expect(409, "POST", "/api/v1/issues", own, map[string]any{"title": "x", "team": "MINE", "parent_id": "1"}, "parent by number")
	expect(409, "POST", "/api/v1/projects", own, map[string]any{"name": "P", "team_ids": []int64{only.ID}}, "project by team ids")
	var proj struct {
		ID int64 `json:"id"`
	}
	c, b := call("POST", "/api/v1/projects", own, map[string]any{"name": "Pilot", "teams": []string{"MINE"}})
	if c != 201 || json.Unmarshal([]byte(b), &proj) != nil {
		t.Fatalf("project by key: %d %s", c, b)
	}
	pu := "/api/v1/projects/" + strconv.FormatInt(proj.ID, 10) + "/updates"
	expect(409, "POST", pu, own, map[string]any{"body": "x", "health": "on_track"}, "update without the project's name")
	expect(409, "POST", pu, own, map[string]any{"body": "x", "health": "on_track", "project": "Other"}, "update with another name")
	expect(201, "POST", pu, own, map[string]any{"body": "Fine", "health": "on_track", "project": "pilot"}, "update by number and name")
	expect(404, "GET", "/api/v1/issues/MINE-1", "nowhere.example", nil, "unknown workspace")
	expect(200, "GET", "/api/v1/issues/MINE-1", "", nil, "reads default to the open workspace")

	// one workspace at a time: only the one open in Projects
	expect(409, "GET", "/api/v1/issues/ATH-1", addr, nil, "closed workspace, reads too")
	expect(409, "POST", "/r/"+strconv.FormatInt(rem.ID, 10)+"/api/v1/issues/ATH-1/comments", "", map[string]any{"body": "x"}, "an agent straight to /r")
	expect(403, "PUT", "/api/v1/workspaces/agent", own, map[string]any{"id": rem.ID}, "an agent picking its workspace")
	expect(403, "PUT", "/api/v1/workspaces/rule", own, map[string]any{"one_workspace": false}, "an agent switching the rule off")
	if c := alex.status("PUT", "/api/v1/workspaces/agent", map[string]any{"id": rem.ID}); c != 200 {
		t.Fatalf("opening the workspace: %d", c)
	}
	expect(201, "POST", "/api/v1/issues/ATH-1/comments", addr, map[string]any{"body": "addr"}, "the open workspace")
	expect(409, "POST", "/api/v1/issues/MINE-1/comments", own, map[string]any{"body": "x"}, "own instance closed now")
	if c := alex.status("POST", "/api/v1/issues/MINE-1/code", map[string]any{"action": "chat"}); c != 409 {
		t.Fatalf("Code on a closed workspace: %d", c)
	}
	var ws []workspaceOut
	alex.do("GET", "/api/v1/workspaces", nil, 200, &ws)
	if len(ws) != 2 || ws[0].Active || !ws[1].Active || !ws[1].Open || ws[1].Address != addr {
		t.Fatalf("workspaces: %+v", ws)
	}
	// the rule off: every workspace, still naming which
	alex.do("PUT", "/api/v1/workspaces/rule", map[string]any{"one_workspace": false}, 200, nil)
	expect(201, "POST", "/api/v1/issues/MINE-1/comments", own, map[string]any{"body": "rule off"}, "rule off")
	expect(409, "POST", "/api/v1/issues/MINE-1/comments", "", map[string]any{"body": "x"}, "rule off, still says the workspace")
	alex.do("GET", "/api/v1/workspaces", nil, 200, &ws)
	if !ws[0].Active || !ws[1].Active || ws[0].Open {
		t.Fatalf("workspaces with the rule off: %+v", ws)
	}
	alex.do("PUT", "/api/v1/workspaces/rule", map[string]any{"one_workspace": true}, 200, nil)
	// links Core builds by address open the workspace here
	if c, _ := alex.raw("GET", "/r/"+addr+"/issue/ATH-1", "", nil); c != 302 {
		t.Fatalf("address link: %d", c)
	}
	base := "/r/" + strconv.FormatInt(rem.ID, 10)

	// the inbox over there: the first poll only takes note
	bob.do("PATCH", "/api/v1/issues/ATH-1", map[string]any{"assignee": "alex@example.com"}, 200, nil)
	ctx := context.Background()
	sA.remoteInboxJob(ctx)
	if len(pushes) != 0 {
		t.Fatalf("old entries pushed: %+v", pushes)
	}
	var list []remoteOut
	alex.do("GET", "/api/v1/remotes", nil, 200, &list)
	if len(list) != 1 || list[0].Unread == 0 {
		t.Fatalf("unread over there: %+v", list)
	}
	// new ones are pushed once, linking to the workspace here
	bob.do("POST", "/api/v1/issues/ATH-1/comments", map[string]any{"body": "@Alex can you look?"}, 201, nil)
	sA.remoteInboxJob(ctx)
	sA.remoteInboxJob(ctx)
	mu.Lock()
	defer mu.Unlock()
	if len(pushes) != 1 || !strings.HasPrefix(pushes[0]["title"], "Athena · ") || pushes[0]["url"] != "https://projects.home.example"+base+"/issue/ATH-1" {
		t.Fatalf("pushes: %+v", pushes)
	}
}
