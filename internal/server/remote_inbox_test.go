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
	tool := func(method, path, workspace string, body any) int {
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
		return w.Code
	}
	num := "/api/v1/issues/" + strconv.FormatInt(here.ID, 10)
	if c := tool("POST", num+"/comments", "", map[string]any{"body": "one"}); c != 201 {
		t.Fatalf("no connections yet: %d", c)
	}

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

	// now a bare number is ambiguous for an agent; people and explicit calls are not
	if c := tool("POST", num+"/comments", "", map[string]any{"body": "two"}); c != 409 {
		t.Fatalf("ambiguous write went through: %d", c)
	}
	if c := tool("PATCH", num, "", map[string]any{"title": "Changed"}); c != 409 {
		t.Fatalf("ambiguous update went through: %d", c)
	}
	if c := tool("POST", num+"/comments", "0", map[string]any{"body": "three"}); c != 201 {
		t.Fatalf("workspace 0 refused: %d", c)
	}
	// strict: an identifier, a new issue, a project, team memory: all need the workspace said
	for _, c := range []struct{ method, path string }{{"POST", "/api/v1/issues/WEB-1/comments"}, {"POST", "/api/v1/issues"}, {"POST", "/api/v1/projects"}, {"POST", "/api/v1/teams/WEB/memory"}} {
		if code := tool(c.method, c.path, "", map[string]any{"body": "x", "title": "x", "name": "x", "text": "x", "team": "WEB"}); code != 409 {
			t.Fatalf("%s %s without workspace: %d", c.method, c.path, code)
		}
	}
	if c := tool("POST", "/api/v1/issues", "0", map[string]any{"title": "Made here", "team": "WEB"}); c != 201 {
		t.Fatalf("create with workspace 0: %d", c)
	}
	if c := tool("POST", "/api/v1/issues/WEB-1/comments", "0", map[string]any{"body": "four"}); c != 201 {
		t.Fatalf("identifier with workspace 0: %d", c)
	}
	if c := tool("GET", num, "", nil); c != 200 {
		t.Fatalf("read refused: %d", c)
	}
	alex.do("POST", num+"/comments", map[string]any{"body": "by hand"}, 201, nil)
	// through the workspace the call is explicit, also for the instance over there
	base := "/r/" + strconv.FormatInt(rem.ID, 10)
	if c := tool("POST", base+"/api/v1/issues/"+strconv.FormatInt(there.ID, 10)+"/comments", "", map[string]any{"body": "there"}); c != 201 {
		t.Fatalf("workspace call: %d", c)
	}

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
