package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

const drawSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect width="10" height="10"/></svg>`

// fakeDraw is enough of Pocket Draw: the drawing list, a drawing's picture
// (12 has one, 13 not yet) and creating a drawing.
type fakeDraw struct {
	mu      sync.Mutex
	token   string
	query   string
	created []map[string]string
}

func (f *fakeDraw) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+f.token {
		http.Error(w, "no", http.StatusUnauthorized)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == "GET" && r.URL.Path == "/api/v1/drawings":
		f.query = r.URL.RawQuery
		json.NewEncoder(w).Encode(map[string]any{"drawings": []map[string]any{
			{"id": 12, "title": "Login (v2)\nflow", "kind": "sketch", "folder_name": "Athena", "edited_at": "2026-10-28T10:00:00Z", "has_preview": true, "preview_rev": 3, "rev": 3},
			{"id": 13, "title": "", "kind": "mermaid", "folder_name": "", "edited_at": "2026-10-27T10:00:00Z", "has_preview": false, "preview_rev": 0, "rev": 1},
		}, "total": 2})
	case r.Method == "GET" && r.URL.Path == "/api/v1/drawings/12/preview.svg":
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write([]byte(drawSVG))
	case r.Method == "POST" && r.URL.Path == "/api/v1/drawings":
		var in map[string]string
		json.NewDecoder(r.Body).Decode(&in)
		f.created = append(f.created, in)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"id": 21, "title": in["title"], "kind": in["kind"]})
	default:
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}
}

func TestDrawPictures(t *testing.T) {
	s, h := apiServer(t)
	alex := person(t, s, h, "alex@example.com", "Alex", "admin")

	// Not connected (no Pocket Core here, nothing direct): a placeholder, never cached.
	code, body, hdr := pictureOf(alex, "/api/v1/draw/12/picture.svg")
	if code != 200 || !strings.Contains(body, "Drawing not available here") || hdr.Get("Cache-Control") != "no-store" || hdr.Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("not connected: %d %q %v", code, body, hdr)
	}
	var list struct {
		Items     []map[string]any `json:"items"`
		Connected bool             `json:"connected"`
		Error     string           `json:"error"`
	}
	alex.do("GET", "/api/v1/draw/drawings", nil, 200, &list)
	if list.Connected || list.Error == "" || len(list.Items) != 0 {
		t.Fatalf("picker without Draw: %+v", list)
	}

	draw := &fakeDraw{token: "pd_test"}
	srv := httptest.NewServer(draw)
	t.Cleanup(srv.Close)
	for k, v := range map[string]string{"app.mode.draw": modeDirect, "app.url.draw": srv.URL, "app.token.draw": "pd_test"} {
		if err := s.store.SetUserSetting(context.Background(), alex.user.ID, k, v); err != nil {
			t.Fatal(err)
		}
	}

	// The picture comes from Draw as the person, inert and briefly cached.
	code, body, hdr = pictureOf(alex, "/api/v1/draw/12/picture.svg?r=3")
	if code != 200 || body != drawSVG || hdr.Get("Content-Type") != "image/svg+xml" || hdr.Get("Cache-Control") != "private, max-age=30" ||
		hdr.Get("Content-Security-Policy") != drawPictureCSP || hdr.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("picture: %d %q %v", code, body, hdr)
	}
	if !strings.Contains(drawPictureCSP, "default-src 'none'") || strings.Contains(drawPictureCSP, "script") {
		t.Fatalf("csp: %s", drawPictureCSP)
	}
	// No picture yet, or not a drawing id: a placeholder.
	if code, body, hdr = pictureOf(alex, "/api/v1/draw/13/picture.svg"); code != 200 || !strings.Contains(body, "No picture yet") || hdr.Get("Cache-Control") != "no-store" {
		t.Fatalf("no picture: %d %q %v", code, body, hdr)
	}
	if code, body, _ = pictureOf(alex, "/api/v1/draw/x1/picture.svg"); code != 200 || !strings.Contains(body, "not available") {
		t.Fatalf("bad id: %d %q", code, body)
	}

	// The picker: the embed Markdown points at Draw, the thumbnail at the proxy.
	list.Error = ""
	alex.do("GET", "/api/v1/draw/drawings?q=login", nil, 200, &list)
	if !list.Connected || list.Error != "" || len(list.Items) != 2 {
		t.Fatalf("picker: %+v", list)
	}
	if !strings.Contains(draw.query, "q=login") || !strings.Contains(draw.query, "limit=24") || !strings.Contains(draw.query, "sort=edited") {
		t.Fatalf("query to Draw: %s", draw.query)
	}
	it := list.Items[0]
	if it["markdown"] != "![Login v2 flow]("+srv.URL+"/d/12/picture.svg)" || it["url"] != srv.URL+"/d/12" ||
		it["picture"] != "/api/v1/draw/12/picture.svg?r=3" || it["detail"] != "Sketch · Athena" || it["id"] != "12" {
		t.Fatalf("item: %+v", it)
	}
	if list.Items[1]["markdown"] != "![Drawing 13]("+srv.URL+"/d/13/picture.svg)" || list.Items[1]["detail"] != "Text diagram" {
		t.Fatalf("untitled item: %+v", list.Items[1])
	}

	// New sketch / text diagram.
	var made map[string]any
	alex.do("POST", "/api/v1/draw/drawings", map[string]any{"kind": "mermaid", "title": " Deploy  flow "}, 201, &made)
	if len(draw.created) != 1 || draw.created[0]["kind"] != "mermaid" || draw.created[0]["title"] != "Deploy flow" {
		t.Fatalf("created in Draw: %+v", draw.created)
	}
	if made["markdown"] != "![Deploy flow]("+srv.URL+"/d/21/picture.svg)" || made["url"] != srv.URL+"/d/21" {
		t.Fatalf("made: %+v", made)
	}
	alex.do("POST", "/api/v1/draw/drawings", map[string]any{}, 201, &made)
	if draw.created[1]["kind"] != "sketch" || draw.created[1]["title"] != "Untitled sketch" {
		t.Fatalf("default sketch: %+v", draw.created[1])
	}
	if alex.status("POST", "/api/v1/draw/drawings", map[string]any{"kind": "drawio"}) != 400 {
		t.Fatal("a kind Projects can't start went through")
	}

	// Switched off: the placeholder again.
	_ = s.store.SetUserSetting(context.Background(), alex.user.ID, "app.mode.draw", modeOff)
	if _, body, _ = pictureOf(alex, "/api/v1/draw/12/picture.svg"); !strings.Contains(body, "Drawing not available here") {
		t.Fatalf("off: %q", body)
	}
}

func pictureOf(c *apiClient, path string) (int, string, http.Header) {
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, req)
	return w.Code, w.Body.String(), w.Header()
}

// fakeCore issues link tokens to Draw (recording the scopes asked for) and
// introspects a link token from Draw acting for Alex with projects:read.
type fakeCore struct {
	mu     sync.Mutex
	draw   string
	scopes [][]string
}

func (f *fakeCore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/api/v1/links/token":
		var in struct {
			User   string   `json:"user"`
			App    string   `json:"app"`
			Scopes []string `json:"scopes"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		f.scopes = append(f.scopes, in.Scopes)
		json.NewEncoder(w).Encode(map[string]any{"access_token": "pd_test", "expires_in": 300, "scope": strings.Join(in.Scopes, " "), "app": in.App,
			"url": f.draw, "public_url": "https://Draw.example/"})
	case "/api/v1/auth/introspect":
		var in struct{ Token string }
		json.NewDecoder(r.Body).Decode(&in)
		if in.Token != "pcx_fromdraw" {
			json.NewEncoder(w).Encode(map[string]any{"active": false})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"active": true, "kind": "delegated", "scope": "projects:read", "audience": "projects", "iss": "https://core.example",
			"user": map[string]any{"id": 1, "sub": "u-alex", "email": "alex@example.com", "name": "Alex"}, "app": map[string]any{"id": 9, "slug": "draw"}})
	default:
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}
}

func TestDrawThroughCoreAndMentions(t *testing.T) {
	draw := &fakeDraw{token: "pd_test"}
	dsrv := httptest.NewServer(draw)
	t.Cleanup(dsrv.Close)
	fc := &fakeCore{draw: dsrv.URL}
	csrv := httptest.NewServer(fc)
	t.Cleanup(csrv.Close)
	s, h := apiServerCore(t, csrv.URL)
	alex := person(t, s, h, "alex@example.com", "Alex", "admin")
	eve := person(t, s, h, "eve@example.com", "Eve", "member")

	// Through Core: the picker reads with drawings:read, creating asks drawings:write;
	// the Markdown carries Draw's public address, the calls go to its server address.
	var list struct {
		Items     []map[string]any `json:"items"`
		Connected bool             `json:"connected"`
	}
	alex.do("GET", "/api/v1/draw/drawings", nil, 200, &list)
	if !list.Connected || len(list.Items) != 2 || list.Items[0]["markdown"] != "![Login v2 flow](https://Draw.example/d/12/picture.svg)" {
		t.Fatalf("picker through Core: %+v", list)
	}
	var made map[string]any
	alex.do("POST", "/api/v1/draw/drawings", map[string]any{"kind": "sketch", "title": "Wireframe"}, 201, &made)
	if made["url"] != "https://Draw.example/d/21" || len(draw.created) != 1 {
		t.Fatalf("made through Core: %+v %+v", made, draw.created)
	}
	if len(fc.scopes) != 2 || strings.Join(fc.scopes[0], " ") != "drawings:read" || strings.Join(fc.scopes[1], " ") != "drawings:write" {
		t.Fatalf("scopes asked of Core: %v", fc.scopes)
	}

	// What uses drawing 12: an issue whose comment shows its picture, a
	// project whose description links to it. Not drawing 120, not Eve's team.
	var web, ops store.Team
	alex.do("POST", "/api/v1/teams", map[string]any{"name": "Web", "key": "web", "preset": "software"}, 201, &web)
	eve.do("POST", "/api/v1/teams", map[string]any{"name": "Ops", "key": "ops", "preset": "software"}, 201, &ops)
	var is1, is2, is3, other store.Issue
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": web.ID, "title": "Login page"}, 201, &is1)
	alex.do("POST", "/api/v1/issues/"+is1.Identifier+"/comments", map[string]any{"body": "Like this:\n\n![Login flow](https://draw.example/d/12/picture.svg)"}, 201, nil)
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": web.ID, "title": "Another drawing", "description": "![Other](https://draw.example/d/120/picture.svg)"}, 201, &is2)
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": web.ID, "title": "Linked", "description": "See https://DRAW.example/d/12#top."}, 201, &is3)
	eve.do("POST", "/api/v1/issues", map[string]any{"team_id": ops.ID, "title": "Ops copy", "description": "![Login flow](https://draw.example/d/12/picture.svg)"}, 201, &other)
	var p struct {
		ID int64 `json:"id"`
	}
	alex.do("POST", "/api/v1/projects", map[string]any{"name": "Relaunch", "team_ids": []int64{web.ID}, "content": "The plan: [flow](http://draw.example/d/12/)"}, 201, &p)

	var hits []map[string]any
	alex.do("GET", "/api/v1/mentions?url="+urlQ("https://draw.example/d/12"), nil, 200, &hits)
	got := map[string]bool{}
	for _, x := range hits {
		got[x["kind"].(string)+":"+firstNonEmpty(str(x["identifier"]), str(x["title"]))] = true
	}
	if len(hits) != 3 || !got["issue:"+is1.Identifier] || !got["issue:"+is3.Identifier] || !got["project:Relaunch"] {
		t.Fatalf("mentions: %+v", hits)
	}
	for _, x := range hits {
		if x["kind"] == "issue" && x["identifier"] == is1.Identifier && (x["url"] != "https://projects.example/issue/"+is1.Identifier || x["team"] != "Web" || x["title"] != "Login page" || str(x["updated_at"]) == "") {
			t.Fatalf("issue hit: %+v", x)
		}
		if x["kind"] == "project" && x["url"] != "https://projects.example/project/"+str(x["id"]) {
			t.Fatalf("project hit: %+v", x)
		}
	}
	// Eve sees only her own team's issue.
	eve.do("GET", "/api/v1/mentions?url="+urlQ("https://draw.example/d/12"), nil, 200, &hits)
	if len(hits) != 1 || hits[0]["identifier"] != other.Identifier {
		t.Fatalf("eve's mentions: %+v", hits)
	}
	if alex.status("GET", "/api/v1/mentions", nil) != 400 || alex.status("GET", "/api/v1/mentions?url=notaurl", nil) != 400 {
		t.Fatal("mentions without an address went through")
	}

	// Pocket Draw asks as Alex with a projects:read link token.
	draw2 := &apiClient{t: t, h: h, token: "pcx_fromdraw"}
	draw2.do("GET", "/api/v1/mentions?url="+urlQ("https://draw.example/d/12")+"&url="+urlQ("https://draw.example/d/99"), nil, 200, &hits)
	if len(hits) != 3 {
		t.Fatalf("mentions for Draw: %+v", hits)
	}
	if draw2.status("GET", "/api/v1/draw/drawings", nil) != 403 {
		t.Fatal("a link token reached the drawing picker")
	}
}

func urlQ(v string) string { return url.QueryEscape(v) }

// str reads a JSON string or number as text.
func str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatInt(int64(x), 10)
	}
	return ""
}

func TestMentionMatching(t *testing.T) {
	keys := []string{itemKey("https://Draw.example/d/12/")}
	for text, want := range map[string]bool{
		"![x](https://draw.example/d/12/picture.svg)": true,
		"http://DRAW.example/d/12#top":                true,
		"see https://draw.example/d/12.":              true,
		"https://draw.example/d/12?t=1":               true,
		"https://draw.example/d/120/picture.svg":      false,
		"https://other.example/d/12":                  false,
		"draw.example/d/12":                           false,
	} {
		if got := linksTo(text, keys); got != want {
			t.Errorf("%q: got %v", text, got)
		}
	}
}
