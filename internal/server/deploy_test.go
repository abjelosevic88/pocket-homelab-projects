package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

func TestDeployIssue(t *testing.T) {
	s, h := apiServer(t)
	alex := person(t, s, h, "alex@example.com", "Alex", "admin")
	var team store.Team
	alex.do("POST", "/api/v1/teams", map[string]any{"name": "Web", "key": "web", "preset": "software"}, 201, &team)
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": team.ID, "title": "Fix login"}, 201, nil)

	var mu sync.Mutex
	var started []map[string]any
	var saved map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		q := r.URL.Query()
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/link/deploy":
			if q.Get("cwd") != "/work/web" || q.Get("host_id") != "3" {
				w.WriteHeader(400)
				w.Write([]byte(`{"error":"wrong folder"}`))
				return
			}
			w.Write([]byte(`{"configured":true,"project":"github.com/acme/web","host_name":"staging","config":{"host_id":3,"dir":"","run":["deploy \"$POCKET_BRANCH\""],"url":"https://staging.example"},
				"branches":[{"branch":"web-1-fix-login","sha":"abc","ahead":2,"dirty":0,"subject":"Fix"}],"latest":null,"current":null,"live":{"live":false}}`))
		case r.Method == "POST" && r.URL.Path == "/api/v1/link/deploys":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if len(started) > 0 {
				w.WriteHeader(409)
				w.Write([]byte(`{"error":"WEB-1 (web-1-fix-login) is being deployed right now; try again when it is done"}`))
				return
			}
			started = append(started, body)
			w.Write([]byte(`{"id":7,"branch":"web-1-fix-login","status":"running","host_name":"staging","mine":true}`))
		case r.Method == "GET" && r.URL.Path == "/api/v1/link/deploys/7":
			w.Write([]byte(`{"id":7,"branch":"web-1-fix-login","status":"ok","log":"done\n","mine":true}`))
		case r.Method == "PUT" && r.URL.Path == "/api/v1/link/deploy/config":
			json.NewDecoder(r.Body).Decode(&saved)
			w.Write([]byte(`{"configured":true}`))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)

	// no code folder: no section, no error
	var info map[string]any
	alex.do("GET", "/api/v1/issues/WEB-1/deploy", nil, 200, &info)
	if info["configured"] != false {
		t.Fatalf("no folder: %v", info)
	}
	connectAI(t, s, alex.user.ID, srv.URL)
	alex.do("PUT", "/api/v1/teams/WEB/ai", map[string]any{"cwd": "/work/web", "host_id": 3}, 200, nil)

	alex.do("GET", "/api/v1/issues/WEB-1/deploy", nil, 200, &info)
	if info["configured"] != true || info["can_deploy"] != true || len(info["branches"].([]any)) != 1 {
		t.Fatalf("info: %v", info)
	}
	alex.do("POST", "/api/v1/issues/WEB-1/deploy", map[string]any{"branch": "web-1-fix-login"}, 201, nil)
	if len(started) != 1 || started[0]["ref"] != "WEB-1" || started[0]["cwd"] != "/work/web" || started[0]["host_id"] != float64(3) || started[0]["source"] != "projects" {
		t.Fatalf("started: %v", started)
	}
	// one slot: Pocket AI's 409 and its words come through
	var e struct{ Error string }
	alex.do("POST", "/api/v1/issues/WEB-1/deploy", map[string]any{"branch": "web-1-fix-login"}, 409, &e)
	if !strings.HasPrefix(e.Error, "WEB-1 (web-1-fix-login) is being deployed") {
		t.Fatalf("conflict: %q", e.Error)
	}
	var run map[string]any
	alex.do("GET", "/api/v1/issues/WEB-1/deploy/7", nil, 200, &run)
	if run["status"] != "ok" {
		t.Fatalf("run: %v", run)
	}
	var hist []store.History
	alex.do("GET", "/api/v1/issues/WEB-1/history", nil, 200, &hist)
	found := false
	for _, x := range hist {
		if x.Field == "deploy" && x.ToLabel == "web-1-fix-login to staging" {
			found = true
		}
	}
	if !found {
		t.Fatalf("history: %+v", hist)
	}

	alex.do("PUT", "/api/v1/teams/WEB/deploy", map[string]any{"config": map[string]any{"host_id": 3, "run": []string{"deploy"}, "url": "https://staging.example"}}, 200, nil)
	if saved["cwd"] != "/work/web" || saved["config"].(map[string]any)["url"] != "https://staging.example" {
		t.Fatalf("saved: %v", saved)
	}
}

func TestAIFail(t *testing.T) {
	w := httptest.NewRecorder()
	aiFail(w, errors.New("/api/v1/link/deploys answered 400 this project has no deploy set up yet"))
	if w.Code != 400 || !strings.Contains(w.Body.String(), `"this project has no deploy set up yet"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	aiFail(w, errors.New("dial tcp: refused"))
	if w.Code != 502 {
		t.Fatal(w.Code)
	}
}
