package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// fakeAI answers Pocket AI's link API and records the drafts it got.
type fakeAI struct {
	mu      sync.Mutex
	drafts  []map[string]any
	actions []string // unlink / delete calls
	auth    string
}

func (f *fakeAI) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/link/agent-options":
			w.Write([]byte(`{"providers":[{"id":"claude","name":"Claude Code","available":true,"models":[{"id":"opus","name":"Opus"}],"efforts":["low","high"],"modes":[{"id":"default","name":"Ask"},{"id":"plan","name":"Plan"}],"readonly_mode":"plan","default_model":"opus"},{"id":"codex","name":"Codex","available":true,"efforts":[{"id":"high","name":"High"}],"modes":[{"id":"auto","name":"Auto"},{"id":"read-only","name":"Read only"}],"readonly_mode":"read-only"}],"accounts":[],"workspaces":[{"name":"Athena","path":"/home/a/Projects/athena"}],"default_provider":"claude","home":"/home/a"}`))
		case r.Method == "GET" && r.URL.Path == "/api/v1/link/prs":
			w.Write([]byte(`[{"ref":"WEB-1","url":"https://github.com/acme/web/pull/100","thread_id":"t1"},{"ref":"WEB-1","url":"https://github.com/acme/web/pull/99","thread_id":"t1"}]`))
		case r.Method == "GET" && r.URL.Path == "/api/v1/link/active":
			w.Write([]byte(`[{"ref":"WEB-1","status":"running","thread_id":"t1","url":"https://ai.example/t/t1"}]`))
		case r.Method == "GET" && r.URL.Path == "/api/v1/link/dirs":
			json.NewEncoder(w).Encode(map[string]any{"path": r.URL.Query().Get("path"), "dirs": []map[string]any{{"name": "athena", "path": "/home/a/Projects/athena", "git": true}}})
		case r.Method == "POST" && r.URL.Path == "/api/v1/link/drafts":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			f.drafts = append(f.drafts, body)
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(map[string]string{"id": "d1", "url": "/new?draft=d1"})
		case strings.HasPrefix(r.URL.Path, "/api/v1/link/threads/d"):
			f.actions = append(f.actions, r.Method+" "+r.URL.Path)
			w.WriteHeader(204)
		case r.Method == "GET" && r.URL.Path == "/api/v1/link/worktrees":
			if r.URL.Query().Get("ref") != "WEB-1" || !strings.HasSuffix(r.URL.Query().Get("branch"), "web-1-fix-login") {
				w.Write([]byte(`[]`))
				return
			}
			w.Write([]byte(`[{"id":4,"branch":"alex/web-1-fix-login","status":"active","thread_ids":["t1"],"ahead":2,"url":"/worktrees?open=4"}]`))
		case r.Method == "GET" && r.URL.Path == "/api/v1/link/threads":
			if r.URL.Query().Get("ref") != "WEB-1" || r.URL.Query().Get("source") != "projects" {
				w.Write([]byte(`[]`))
				return
			}
			w.Write([]byte(`[{"draft_id":"d1","thread_id":"t1","title":"WEB-1 · Fix login","status":"running","worktree":"alex/web-1-fix-login","worktree_id":4,"url":"https://ai.example/t/t1","pull_requests":["https://github.com/acme/web/pull/99"]}]`))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func connectAI(t *testing.T, s *Server, uid int64, url string) {
	ctx := context.Background()
	for k, v := range map[string]string{"app.mode.ai": modeDirect, "app.url.ai": url, "app.token.ai": "pai_test"} {
		if err := s.store.SetUserSetting(ctx, uid, k, v); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCodeWithPocketAI(t *testing.T) {
	s, h := apiServer(t)
	alex := person(t, s, h, "alex@example.com", "Alex", "admin")
	ben := person(t, s, h, "ben@example.com", "Ben", "member")
	var team store.Team
	alex.do("POST", "/api/v1/teams", map[string]any{"name": "Web", "key": "web", "preset": "software"}, 201, &team)
	alex.do("POST", "/api/v1/teams/WEB/members", map[string]any{"user_id": ben.user.ID, "role": "member"}, 200, nil)
	var is store.Issue
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": team.ID, "title": "Fix login", "description": "Login fails on Safari.", "labels": []string{"Bug"}}, 201, &is)
	alex.do("POST", "/api/v1/issues/WEB-1/comments", map[string]any{"body": "Only since the last deploy."}, 201, nil)

	// not connected: a clear answer, nothing breaks
	var codes struct {
		Threads   []codeThread   `json:"threads"`
		Worktrees []codeWorktree `json:"worktrees"`
		Connected bool           `json:"connected"`
		Detail    string         `json:"detail"`
	}
	alex.do("GET", "/api/v1/issues/WEB-1/code", nil, 200, &codes)
	if codes.Connected || codes.Detail == "" {
		t.Fatalf("unconnected: %+v", codes)
	}

	ai := &fakeAI{}
	srv := ai.server(t)
	connectAI(t, s, alex.user.ID, srv.URL)

	// live marks for lists: the person's working conversations by identifier
	var act struct {
		Active    []struct{ Ref, Status string } `json:"active"`
		Connected bool                           `json:"connected"`
	}
	alex.do("GET", "/api/v1/ai/active", nil, 200, &act)
	if !act.Connected || len(act.Active) != 1 || act.Active[0].Ref != "WEB-1" || act.Active[0].Status != "running" {
		t.Fatalf("active: %+v", act)
	}
	ben.do("GET", "/api/v1/ai/active", nil, 200, &act) // not connected: quiet, no marks
	if act.Connected || len(act.Active) != 0 {
		t.Fatalf("unconnected active: %+v", act)
	}

	// folder suggestions come from Pocket AI, path passed through
	var dirs struct {
		Path string                        `json:"path"`
		Dirs []struct{ Name, Path string } `json:"dirs"`
	}
	alex.do("GET", "/api/v1/ai/dirs?path="+url.QueryEscape("~/Projects"), nil, 200, &dirs)
	if dirs.Path != "~/Projects" || len(dirs.Dirs) != 1 || dirs.Dirs[0].Name != "athena" {
		t.Fatalf("dirs: %+v", dirs)
	}

	// no code folder yet
	if code, body := alex.raw("POST", "/api/v1/issues/WEB-1/code", "application/json", []byte(`{"action":"investigate"}`)); code != 400 || !strings.Contains(string(body), "code folder") {
		t.Fatalf("no folder: %d %s", code, body)
	}
	// only team admins change the settings; folders are full paths
	if ben.status("PUT", "/api/v1/teams/WEB/ai", map[string]any{"cwd": "~/x"}) != 403 {
		t.Fatal("member changed team AI settings")
	}
	if alex.status("PUT", "/api/v1/teams/WEB/ai", map[string]any{"cwd": "relative/dir"}) != 400 {
		t.Fatal("relative folder accepted")
	}
	if alex.status("PUT", "/api/v1/teams/WEB/ai", map[string]any{"cwd": "~/x", "provider": "gpt"}) != 400 {
		t.Fatal("unknown provider accepted")
	}
	var tai store.TeamAI
	alex.do("PUT", "/api/v1/teams/WEB/ai", map[string]any{"cwd": "~/Projects/web/", "provider": "claude", "model": "opus", "effort": "high", "mode": "default",
		"instructions": "Use pnpm, never npm."}, 200, &tai)
	if tai.Cwd != "~/Projects/web" || tai.Provider != "claude" {
		t.Fatalf("saved: %+v", tai)
	}
	alex.do("POST", "/api/v1/teams/WEB/memory", map[string]any{"text": "Tests run with  `make test`\nin CI"}, 201, nil)

	var started struct {
		URL     string `json:"url"`
		DraftID string `json:"draft_id"`
		Send    bool   `json:"send"`
	}
	alex.do("POST", "/api/v1/issues/WEB-1/code", map[string]any{"action": "investigate", "note": "Look at the session cookie first."}, 201, &started)
	if started.URL != srv.URL+"/new?draft=d1" || started.DraftID != "d1" || started.Send {
		t.Fatalf("started: %+v", started)
	}
	d := ai.drafts[0]
	text, system := d["text"].(string), d["system"].(string)
	if d["mode"] != "plan" || d["provider"] != "claude" || d["model"] != "opus" || d["effort"] != "high" || d["cwd"] != "~/Projects/web" ||
		d["ref"] != "WEB-1" || d["source"] != "projects" || d["ref_url"] != "https://projects.example/issue/WEB-1" || ai.auth != "Bearer pai_test" {
		t.Fatalf("draft: %+v", d)
	}
	for _, want := range []string{"Investigate this issue", "Do not edit", "Look at the session cookie first.", "WEB-1: Fix login", "Login fails on Safari.", "Bug", "Only since the last deploy.", "Alex ("} {
		if !strings.Contains(text, want) {
			t.Fatalf("text misses %q:\n%s", want, text)
		}
	}
	for _, want := range []string{"issue WEB-1", "with id 1;", "~/Projects/web", "Use pnpm, never npm.", "Team memory", "- Tests run with `make test` in CI (", "by Alex)", "pocket_projects_remember (team WEB)"} {
		if !strings.Contains(system, want) {
			t.Fatalf("system misses %q:\n%s", want, system)
		}
	}
	if strings.Contains(text, "Use pnpm") {
		t.Fatal("instructions leaked into the visible prompt")
	}
	if d["title"] != "WEB-1 · Fix login" {
		t.Fatalf("title %v", d["title"])
	}

	// fix keeps the team's mode and names the branch; a project folder wins
	var proj struct {
		ID int64 `json:"id"`
	}
	alex.do("POST", "/api/v1/projects", map[string]any{"name": "Relaunch", "team_ids": []int64{team.ID}}, 201, &proj)
	alex.do("PUT", "/api/v1/projects/"+strconv.FormatInt(proj.ID, 10)+"/ai", map[string]any{"cwd": "/srv/relaunch"}, 200, nil)
	alex.do("PATCH", "/api/v1/issues/WEB-1", map[string]any{"project_id": proj.ID}, 200, nil)
	// the team sends prompts right away: the start says so
	alex.do("PUT", "/api/v1/teams/WEB/ai", map[string]any{"cwd": "~/Projects/web", "provider": "claude", "model": "opus", "effort": "high", "mode": "default",
		"instructions": "Use pnpm, never npm.", "send_now": true}, 200, &tai)
	alex.do("POST", "/api/v1/issues/WEB-1/code", map[string]any{"action": "fix"}, 201, &started)
	if !started.Send {
		t.Fatalf("team send_now not passed on: %+v", started)
	}
	d = ai.drafts[1]
	// link_refs off (the default): nothing about Pocket Projects goes into the repository
	if d["mode"] != "default" || d["cwd"] != "/srv/relaunch" || strings.Contains(d["text"].(string), "web-1-fix-login") ||
		!strings.Contains(d["system"].(string), "Keep Pocket Projects out of the repository") || strings.Contains(d["system"].(string), "Resolves WEB-1") {
		t.Fatalf("fix draft (refs off): %+v", d)
	}
	// link_refs on: issue branch, identifier in commits, "Resolves WEB-1 (url)" in PRs
	alex.do("PUT", "/api/v1/teams/WEB/ai", map[string]any{"cwd": "~/Projects/web", "provider": "claude", "model": "opus", "effort": "high", "mode": "default",
		"instructions": "Use pnpm, never npm.", "send_now": true, "link_refs": true}, 200, &tai)
	alex.do("POST", "/api/v1/issues/WEB-1/code", map[string]any{"action": "fix"}, 201, nil)
	d = ai.drafts[2]
	if !strings.Contains(d["text"].(string), "web-1-fix-login") || !strings.Contains(d["system"].(string), "Resolves WEB-1 (") || strings.Contains(d["system"].(string), "Keep Pocket Projects out") {
		t.Fatalf("fix draft (refs on): %+v", d)
	}
	// in a worktree of the issue's own: Pocket AI makes it on the issue's branch, the prompt stays on it
	alex.do("POST", "/api/v1/issues/WEB-1/code", map[string]any{"action": "fix", "worktree": true}, 201, nil)
	d = ai.drafts[len(ai.drafts)-1]
	if wt, _ := d["worktree"].(map[string]any); wt == nil || !strings.HasSuffix(wt["name"].(string), "web-1-fix-login") ||
		!strings.Contains(d["text"].(string), "git worktree of its own, on the branch "+wt["name"].(string)) || strings.Contains(d["text"].(string), "switch to a branch") {
		t.Fatalf("worktree draft: %+v", d)
	}
	if alex.status("POST", "/api/v1/issues/WEB-1/code", map[string]any{"action": "investigate", "worktree": true}) != 400 {
		t.Fatal("read-only investigation in a worktree accepted")
	}
	if got := plainBranch("alex/web-1-fix-login", "WEB-1"); got != "alex/fix-login" {
		t.Fatalf("plain branch %q", got)
	}
	// another machine (Pocket AI → Settings → Servers): its own login, the host goes with the draft
	alex.do("PUT", "/api/v1/teams/WEB/ai", map[string]any{"cwd": "/work/web", "host_id": 3, "account_id": 2, "provider": "claude", "link_refs": true}, 200, &tai)
	if tai.HostID != 3 || tai.AccountID != nil {
		t.Fatalf("host saved: %+v", tai)
	}
	alex.do("POST", "/api/v1/issues/WEB-1/code", map[string]any{"action": "fix"}, 201, nil)
	if d = ai.drafts[len(ai.drafts)-1]; d["host_id"] != float64(3) || d["account_id"] != nil || d["cwd"] != "/srv/relaunch" { // the project's folder, on the team's machine
		t.Fatalf("draft on another machine: %+v", d)
	}
	alex.do("PUT", "/api/v1/teams/WEB/ai", map[string]any{"cwd": "~/Projects/web", "provider": "claude", "model": "opus", "effort": "high", "mode": "default",
		"instructions": "Use pnpm, never npm.", "send_now": true, "link_refs": true}, 200, &tai)
	// the issue's files go along (the agent sees screenshots, reads documents); links are listed
	var mp bytes.Buffer
	mw := multipart.NewWriter(&mp)
	fw, _ := mw.CreateFormFile("file", "CleanShot 15.04.43@2x.png")
	fw.Write([]byte("\x89PNG\r\n\x1a\nfake"))
	mw.Close()
	if code, b := alex.raw("POST", "/api/v1/issues/WEB-1/attachments", mw.FormDataContentType(), mp.Bytes()); code != 201 {
		t.Fatalf("upload: %d %s", code, b)
	}
	alex.do("POST", "/api/v1/issues/WEB-1/attachments", map[string]any{"url": "https://example.com/spec"}, 201, nil)
	alex.do("POST", "/api/v1/issues/WEB-1/code", map[string]any{"action": "chat"}, 201, nil)
	d = ai.drafts[len(ai.drafts)-1]
	files, _ := d["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("files not sent: %+v", d["files"])
	}
	f0 := files[0].(map[string]any)
	if raw, _ := base64.StdEncoding.DecodeString(f0["data"].(string)); f0["name"] != "CleanShot 15.04.43@2x.png" || !bytes.HasPrefix(raw, []byte("\x89PNG")) {
		t.Fatalf("file: %v", f0["name"])
	}
	if txt := d["text"].(string); !strings.Contains(txt, "### Attached files (sent with this message)\n- CleanShot 15.04.43@2x.png") || !strings.Contains(txt, "https://example.com/spec") {
		t.Fatalf("prompt does not list the attachments:\n%s", txt)
	}
	// right-click on a conversation: remove it from the issue, or delete it in Pocket AI
	alex.do("POST", "/api/v1/issues/WEB-1/code/d1/unlink", nil, 204, nil)
	alex.do("DELETE", "/api/v1/issues/WEB-1/code/d1", nil, 204, nil)
	if len(ai.actions) != 2 || ai.actions[0] != "POST /api/v1/link/threads/d1/unlink" || ai.actions[1] != "DELETE /api/v1/link/threads/d1" {
		t.Fatalf("pocket ai calls: %v", ai.actions)
	}
	// after a pull request: link it, comment, follow-ups — with the issue's numeric id, either way
	for _, i := range []int{1, 2} {
		sys := ai.drafts[i]["system"].(string)
		if !strings.Contains(sys, "After you open a pull request") || !strings.Contains(sys, "pocket_projects_link_pr with id "+strconv.FormatInt(is.ID, 10)) ||
			!strings.Contains(sys, "parent_id "+strconv.FormatInt(is.ID, 10)) {
			t.Fatalf("after-PR steps missing (draft %d): %s", i, sys)
		}
	}

	// the conversation is attached and its state follows Pocket AI
	alex.do("GET", "/api/v1/issues/WEB-1/code", nil, 200, &codes)
	if !codes.Connected || len(codes.Threads) != 1 || codes.Threads[0].Status != "running" {
		t.Fatalf("threads: %+v", codes)
	}
	// its worktrees come along, with Pocket AI's address made absolute
	if len(codes.Worktrees) != 1 || codes.Worktrees[0].Branch != "alex/web-1-fix-login" || codes.Worktrees[0].URL != srv.URL+"/worktrees?open=4" ||
		codes.Threads[0].Worktree != "alex/web-1-fix-login" || codes.Threads[0].WorktreeID != 4 {
		t.Fatalf("worktrees: %+v / %+v", codes.Worktrees, codes.Threads)
	}
	// the PR that conversation opened was attached when the list was read
	if pr, err := s.store.FindAttachment(context.Background(), is.ID, "git", "https://github.com/acme/web/pull/99"); err != nil || pr.Kind != "pull_request" || pr.State != "open" {
		t.Fatalf("PR from the conversation: %+v %v", pr, err)
	}
	// the background sweep attaches PRs from conversations nobody is looking at (once)
	s.agentPRJob(context.Background())
	s.agentPRJob(context.Background())
	if pr, err := s.store.FindAttachment(context.Background(), is.ID, "git", "https://github.com/acme/web/pull/100"); err != nil || pr.Kind != "pull_request" {
		t.Fatalf("PR from the sweep: %+v %v", pr, err)
	}
	att, err := s.store.FindAttachment(context.Background(), is.ID, "ai", "d1")
	if err != nil || att.Kind != "ai_thread" || att.State != "running" || att.URL != "https://ai.example/t/t1" {
		t.Fatalf("attachment: %+v %v", att, err)
	}
	if alex.status("POST", "/api/v1/issues/WEB-1/code", map[string]any{"action": "deploy"}) != 400 {
		t.Fatal("unknown action accepted")
	}
}

func TestTeamMemory(t *testing.T) {
	s, h := apiServer(t)
	alex := person(t, s, h, "alex@example.com", "Alex", "admin")
	ben := person(t, s, h, "ben@example.com", "Ben", "member")
	eve := person(t, s, h, "eve@example.com", "Eve", "member")
	alex.do("POST", "/api/v1/teams", map[string]any{"name": "Web", "key": "web"}, 201, nil)
	alex.do("POST", "/api/v1/teams/WEB/members", map[string]any{"user_id": ben.user.ID, "role": "member"}, 200, nil)

	ben.do("POST", "/api/v1/teams/WEB/memory", map[string]any{"text": "Deploys go through Komodo."}, 201, nil)
	if eve.status("POST", "/api/v1/teams/WEB/memory", map[string]any{"text": "x"}) != 404 {
		t.Fatal("outsider wrote team memory")
	}
	if ben.status("PUT", "/api/v1/teams/WEB/memory", map[string]any{"memory": ""}) != 403 {
		t.Fatal("member replaced team memory")
	}
	if ben.status("POST", "/api/v1/teams/WEB/memory", map[string]any{"text": "  "}) != 400 {
		t.Fatal("empty fact accepted")
	}
	var m struct {
		Memory string `json:"memory"`
	}
	ben.do("GET", "/api/v1/teams/WEB/memory", nil, 200, &m)
	if !strings.HasPrefix(m.Memory, "- Deploys go through Komodo. (") || !strings.HasSuffix(m.Memory, "by Ben)") {
		t.Fatalf("memory: %q", m.Memory)
	}
	// full memory: a clear refusal, nothing lost
	alex.do("PUT", "/api/v1/teams/WEB/memory", map[string]any{"memory": strings.Repeat("x", store.MaxAIText-20)}, 200, nil)
	if code, body := ben.raw("POST", "/api/v1/teams/WEB/memory", "application/json", []byte(`{"text":"one more fact that does not fit"}`)); code != 400 || !strings.Contains(string(body), "full") {
		t.Fatalf("full: %d %s", code, body)
	}
	if alex.status("PUT", "/api/v1/teams/WEB/memory", map[string]any{"memory": strings.Repeat("x", store.MaxAIText+1)}) != 400 {
		t.Fatal("oversized memory accepted")
	}
}

func TestReadonlyModeAndCwd(t *testing.T) {
	var o agentOptions
	json.Unmarshal([]byte(`{"providers":[{"id":"claude","readonly_mode":"plan"},{"id":"codex","readonly_mode":"read-only"}],"default_provider":"codex"}`), &o)
	if o.readonlyMode("") != "read-only" || o.readonlyMode("claude") != "plan" || o.readonlyMode("other") != "" {
		t.Fatal("readonly mode")
	}
	for in, want := range map[string]string{"~/a/b/": "~/a/b", "~": "~", "/srv/../srv/app": "/srv/app", "": ""} {
		if got, err := cleanCwd(in); err != nil || got != want {
			t.Fatalf("cleanCwd(%q) = %q %v", in, got, err)
		}
	}
	if _, err := cleanCwd("~user/x"); err == nil {
		t.Fatal("~user accepted")
	}
}
