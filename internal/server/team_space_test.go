package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// sceneDraw is a person's Pocket Draw with one sketch (7) whose picture
// changes with rev, and creating drawings (copies).
type sceneDraw struct {
	mu      sync.Mutex
	rev     int
	title   string
	gone    bool
	created []map[string]string
}

func (f *sceneDraw) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gone && strings.HasPrefix(r.URL.Path, "/api/v1/drawings/7") {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	switch {
	case r.Method == "GET" && r.URL.Path == "/api/v1/drawings/7/preview.svg":
		etag := fmt.Sprintf(`"7-%d"`, f.rev)
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg"><text>rev %d</text></svg>`, f.rev)
	case r.Method == "GET" && r.URL.Path == "/api/v1/drawings/7":
		json.NewEncoder(w).Encode(map[string]any{"id": 7, "kind": "sketch", "title": f.title,
			"data": fmt.Sprintf(`{"elements":[{"type":"text","text":"Checkout rev %d"},{"type":"text","text":"old","isDeleted":true}]}`, f.rev)})
	case r.Method == "POST" && r.URL.Path == "/api/v1/drawings":
		var in map[string]string
		json.NewDecoder(r.Body).Decode(&in)
		f.created = append(f.created, in)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"id": 30 + len(f.created)})
	default:
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}
}

func useDraw(t *testing.T, s *Server, c *apiClient, h http.Handler) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	for k, v := range map[string]string{"app.mode.draw": modeDirect, "app.url.draw": srv.URL, "app.token.draw": "x"} {
		if err := s.store.SetUserSetting(context.Background(), c.user.ID, k, v); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTeamSpace(t *testing.T) {
	s, h := apiServer(t)
	ctx := context.Background()
	alex := person(t, s, h, "alex@example.com", "Alex", "admin")
	heidi := person(t, s, h, "heidi@example.com", "Heidi", "member")
	marie := person(t, s, h, "marie@example.com", "Marie", "member")
	carl := person(t, s, h, "carl@example.com", "Carl", "member") // not on the team
	var team store.Team
	alex.do("POST", "/api/v1/teams", map[string]any{"name": "Athena", "key": "ath", "preset": "software"}, 201, &team)
	for _, p := range []*apiClient{heidi, marie} {
		alex.do("POST", "/api/v1/teams/ATH/members", map[string]any{"user_id": p.user.ID, "role": "member"}, 200, nil)
	}
	heidiDraw := &sceneDraw{rev: 1, title: "Checkout flow"}
	useDraw(t, s, heidi, heidiDraw)
	marieDraw := &sceneDraw{rev: 1}
	useDraw(t, s, marie, marieDraw)

	// a note written in the team space
	var note store.TeamItem
	marie.do("POST", "/api/v1/teams/ATH/shared", map[string]any{"title": " Ideas ", "body": "# Big\nWe draw **everything** first."}, 201, &note)
	carl.do("GET", "/api/v1/teams/ATH/shared", nil, 404, nil)

	// Heidi shares her drawing: Marie sees it, picture and all
	var shared struct {
		Item     store.TeamItem `json:"item"`
		Replaced bool           `json:"replaced"`
	}
	heidi.do("POST", "/api/v1/teams/ATH/shared/drawings", map[string]any{"drawing": 7}, 200, &shared)
	d := shared.Item
	if shared.Replaced || d.Kind != "drawing" || d.Title != "Checkout flow" || d.DrawKind != "sketch" || !d.Follow || d.PictureRev != 1 {
		t.Fatalf("shared: %+v", shared)
	}
	var list struct {
		Items   []store.TeamItem `json:"items"`
		CanEdit bool             `json:"can_edit"`
	}
	marie.do("GET", "/api/v1/teams/ATH/shared", nil, 200, &list)
	if len(list.Items) != 2 || !list.CanEdit || list.Items[0].ID != d.ID || list.Items[1].Snippet != "Big We draw everything first." {
		t.Fatalf("list: %+v", list)
	}
	code, body, hdr := pictureOf(marie, fmt.Sprintf("/api/v1/teams/ATH/shared/%d/picture.svg", d.ID))
	if code != 200 || !strings.Contains(body, "rev 1") || hdr.Get("Content-Security-Policy") != drawPictureCSP {
		t.Fatalf("picture: %d %s %v", code, body, hdr)
	}
	var got struct {
		store.TeamItem
		Mine    bool `json:"mine"`
		CanEdit bool `json:"can_edit"`
	}
	marie.do("GET", fmt.Sprintf("/api/v1/teams/ATH/shared/%d", d.ID), nil, 200, &got)
	if got.Mine || got.SourceURL != "" || !got.CanEdit {
		t.Fatalf("marie's view: %+v", got)
	}
	heidi.do("GET", fmt.Sprintf("/api/v1/teams/ATH/shared/%d", d.ID), nil, 200, &got)
	if !got.Mine || !strings.HasSuffix(got.SourceURL, "/d/7") {
		t.Fatalf("heidi's view: %+v", got)
	}
	// searchable by the words in it
	var found struct {
		Docs []store.SearchHit `json:"docs"`
	}
	marie.do("GET", "/api/v1/search?q=checkout", nil, 200, &found)
	if len(found.Docs) != 1 || found.Docs[0].Kind != "team_drawing" || found.Docs[0].Ident != "ATH" {
		t.Fatalf("search: %+v", found.Docs)
	}

	// following: unchanged = nothing; Heidi edits = the copy is renewed
	it, _ := s.store.GetTeamItem(ctx, d.ID)
	if err := s.followDrawing(ctx, it); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.store.GetTeamItem(ctx, d.ID); again.PictureRev != 1 || again.UpdatedAt != it.UpdatedAt {
		t.Fatalf("unchanged but renewed: %+v", again)
	}
	heidiDraw.mu.Lock()
	heidiDraw.rev, heidiDraw.title = 2, "Checkout flow v2"
	heidiDraw.mu.Unlock()
	followed, _ := s.store.FollowedDrawings(ctx)
	if len(followed) != 1 {
		t.Fatalf("followed: %+v", followed)
	}
	if err := s.followDrawing(ctx, &followed[0]); err != nil {
		t.Fatal(err)
	}
	it, _ = s.store.GetTeamItem(ctx, d.ID)
	if it.PictureRev != 2 || it.Title != "Checkout flow v2" || !strings.Contains(it.Picture, "rev 2") || !strings.Contains(it.Body, "Checkout rev 2") || strings.Contains(it.Body, "old") {
		t.Fatalf("renewed: %+v", it)
	}

	// a followed drawing is renamed in Draw, not here
	marie.do("PATCH", fmt.Sprintf("/api/v1/teams/ATH/shared/%d", d.ID), map[string]any{"title": "Mine now"}, 400, nil)
	// Marie makes her own copy in her Pocket Draw
	var cp map[string]string
	marie.do("POST", fmt.Sprintf("/api/v1/teams/ATH/shared/%d/copy", d.ID), nil, 201, &cp)
	if cp["id"] != "31" || len(marieDraw.created) != 1 || marieDraw.created[0]["title"] != "Checkout flow v2" || !strings.Contains(marieDraw.created[0]["data"], "rev 2") ||
		!strings.Contains(marieDraw.created[0]["preview"], "rev 2") {
		t.Fatalf("copy: %v %+v", cp, marieDraw.created)
	}

	// sharing again renews the same item
	heidi.do("POST", "/api/v1/teams/ATH/shared/drawings", map[string]any{"drawing": "7"}, 200, &shared)
	if !shared.Replaced || shared.Item.ID != d.ID {
		t.Fatalf("again: %+v", shared)
	}

	// Marie stops following; only Heidi can start again
	marie.do("PATCH", fmt.Sprintf("/api/v1/teams/ATH/shared/%d", d.ID), map[string]any{"follow": false}, 200, nil)
	if f, _ := s.store.FollowedDrawings(ctx); len(f) != 0 {
		t.Fatalf("still followed: %+v", f)
	}
	marie.do("PATCH", fmt.Sprintf("/api/v1/teams/ATH/shared/%d", d.ID), map[string]any{"title": "Checkout (frozen)"}, 200, nil)
	marie.do("PATCH", fmt.Sprintf("/api/v1/teams/ATH/shared/%d", d.ID), map[string]any{"follow": true}, 403, nil)
	heidi.do("PATCH", fmt.Sprintf("/api/v1/teams/ATH/shared/%d", d.ID), map[string]any{"follow": true}, 200, nil)

	// the original is deleted: the copy stays, with a note why it no longer changes
	heidiDraw.mu.Lock()
	heidiDraw.gone = true
	heidiDraw.mu.Unlock()
	it, _ = s.store.GetTeamItem(ctx, d.ID)
	if err := s.followDrawing(ctx, it); err == nil {
		t.Fatal("deleted original followed")
	}
	if it, _ = s.store.GetTeamItem(ctx, d.ID); !strings.Contains(it.SyncError, "deleted") || it.Picture == "" {
		t.Fatalf("gone: %+v", it)
	}

	// notes: base guards against overwriting a teammate
	var n store.TeamItem
	heidi.do("PATCH", fmt.Sprintf("/api/v1/teams/ATH/shared/%d", note.ID), map[string]any{"body": "Heidi was here", "base": note.UpdatedAt}, 200, &n)
	code, resp := marie.raw("PATCH", fmt.Sprintf("/api/v1/teams/ATH/shared/%d", note.ID), "application/json", []byte(`{"body":"stale","base":"`+note.UpdatedAt+`"}`))
	if code != 409 || !strings.Contains(string(resp), "Heidi changed this meanwhile") {
		t.Fatalf("conflict: %d %s", code, resp)
	}

	// import (what a connected instance sends) is a note, whatever it says
	var imp struct {
		Item store.TeamItem `json:"item"`
	}
	marie.do("POST", "/api/v1/teams/ATH/shared/import", map[string]any{"kind": "drawing", "source_app": "draw", "source_id": "9", "title": "Flow", "body": "x"}, 200, &imp)
	if imp.Item.Kind != "note" || imp.Item.SourceApp != "notes" {
		t.Fatalf("import: %+v", imp)
	}

	// delete
	marie.do("DELETE", fmt.Sprintf("/api/v1/teams/ATH/shared/%d", d.ID), nil, 204, nil)
	marie.do("GET", fmt.Sprintf("/api/v1/teams/ATH/shared/%d", d.ID), nil, 404, nil)
}

// On a connected instance's team: drawings come from that instance's Pocket
// Draw (all passed through), a note from the person's own Notes here.
func TestTeamSpaceConnected(t *testing.T) {
	ctx := context.Background()
	sB, hB := apiServer(t)
	srvB := httptest.NewServer(hB)
	t.Cleanup(srvB.Close)
	sB.cfg.BaseURL = srvB.URL
	bob := person(t, sB, hB, "bob@athena.example", "Bob", "admin")
	alexThere := person(t, sB, hB, "alex@example.com", "Alex", "member")
	bob.do("POST", "/api/v1/teams", map[string]any{"name": "Athena", "key": "ath", "preset": "software"}, 201, nil)
	bob.do("POST", "/api/v1/teams/ATH/members", map[string]any{"user_id": alexThere.user.ID, "role": "member"}, 200, nil)
	drawThere := &sceneDraw{rev: 1, title: "Flow"}
	useDraw(t, sB, alexThere, drawThere)

	sA, hA := apiServer(t)
	alex := person(t, sA, hA, "alex@example.com", "Alex", "admin")
	var rem remoteOut
	alex.do("POST", "/api/v1/remotes", map[string]any{"name": "Athena", "url": srvB.URL, "token": alexThere.token}, 201, &rem)
	base := fmt.Sprintf("/r/%d/api/v1", rem.ID)
	notes := &fakeNotes{notes: map[string]string{"401": "# Plan\n\nsteps"}}
	nsrv := httptest.NewServer(notes)
	t.Cleanup(nsrv.Close)
	for k, v := range map[string]string{"app.mode.notes": modeDirect, "app.url.notes": nsrv.URL, "app.token.notes": "pn_test"} {
		if err := sA.store.SetUserSetting(ctx, alex.user.ID, k, v); err != nil {
			t.Fatal(err)
		}
	}

	var shared struct {
		Item store.TeamItem `json:"item"`
	}
	alex.do("POST", base+"/teams/ATH/shared/drawings", map[string]any{"drawing": 7}, 200, &shared)
	alex.do("POST", base+"/teams/ATH/shared/notes", map[string]any{"note": "401"}, 200, nil)
	var list struct {
		Items []store.TeamItem `json:"items"`
	}
	bob.do("GET", "/api/v1/teams/ATH/shared", nil, 200, &list)
	if len(list.Items) != 2 {
		t.Fatalf("over there: %+v", list.Items)
	}
	for _, it := range list.Items {
		if it.CreatedBy == nil || *it.CreatedBy != alexThere.user.ID {
			t.Fatalf("made by: %+v", it)
		}
	}
	n, _ := sB.store.GetTeamItem(ctx, list.Items[0].ID)
	if n.Kind != "note" || n.Title != "Plan" || n.Body != "steps" {
		t.Fatalf("note there: %+v", n)
	}
	// the picture through the connection
	if code, body, _ := pictureOf(alex, fmt.Sprintf("%s/teams/ATH/shared/%d/picture.svg", base, shared.Item.ID)); code != 200 || !strings.Contains(body, "rev 1") {
		t.Fatalf("picture through the connection: %d %s", code, body)
	}
	// the instance over there follows it
	drawThere.mu.Lock()
	drawThere.rev = 2
	drawThere.mu.Unlock()
	followed, _ := sB.store.FollowedDrawings(ctx)
	if len(followed) != 1 {
		t.Fatalf("followed there: %+v", followed)
	}
	if err := sB.followDrawing(ctx, &followed[0]); err != nil {
		t.Fatal(err)
	}
	if it, _ := sB.store.GetTeamItem(ctx, shared.Item.ID); !strings.Contains(it.Picture, "rev 2") {
		t.Fatalf("not renewed: %+v", it)
	}
}
