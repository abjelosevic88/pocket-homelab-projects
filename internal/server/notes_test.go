package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// fakeNotes is enough of Pocket Notes: GET /api/v1/mentions?url= and
// POST /api/v1/notes/{id}/append, with notes kept as plain text.
type fakeNotes struct {
	mu    sync.Mutex
	notes map[string]string
}

func (f *fakeNotes) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer pn_test" {
		http.Error(w, "no", http.StatusUnauthorized)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == "GET" && r.URL.Path == "/api/v1/mentions":
		out := []map[string]any{}
		for id, text := range f.notes {
			if strings.Contains(text, "("+r.URL.Query().Get("url")+")") {
				out = append(out, map[string]any{"id": json.Number(id), "title": "Plan", "category": "Projects", "excerpt": "", "modified": 1792500000})
			}
		}
		json.NewEncoder(w).Encode(out)
	case r.Method == "GET" && r.URL.Path == "/api/v1/notes":
		out := []map[string]any{}
		if r.URL.Query().Get("category") == "Athena/Plans" && r.URL.Query().Get("recursive") == "1" {
			out = append(out, map[string]any{"id": 401, "title": "Plan", "category": "Athena/Plans", "modified": 1792500000})
		}
		json.NewEncoder(w).Encode(out)
	case r.Method == "GET" && r.URL.Path == "/api/v1/folders":
		json.NewEncoder(w).Encode([]map[string]any{{"path": "Athena", "count": 0}, {"path": "Athena/Plans", "count": 1}})
	case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/append"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/notes/"), "/append")
		if _, ok := f.notes[id]; !ok {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		var in struct{ Text string }
		json.NewDecoder(r.Body).Decode(&in)
		f.notes[id] += "\n" + in.Text
		json.NewEncoder(w).Encode(map[string]any{"id": json.Number(id), "title": "Plan"})
	default:
		http.Error(w, "nf", http.StatusNotFound)
	}
}

func TestIssueNotes(t *testing.T) {
	s, h := apiServer(t)
	alex := person(t, s, h, "alex@example.com", "Alex", "admin")
	var team store.Team
	alex.do("POST", "/api/v1/teams", map[string]any{"name": "Athena", "key": "as", "preset": "software"}, 201, &team)
	var is store.Issue
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": team.ID, "title": "Audit [what] Athena learns"}, 201, &is)

	notes := &fakeNotes{notes: map[string]string{"401": "# Plan\n\nsteps"}}
	srv := httptest.NewServer(notes)
	t.Cleanup(srv.Close)
	for k, v := range map[string]string{"app.mode.notes": modeDirect, "app.url.notes": srv.URL, "app.token.notes": "pn_test"} {
		if err := s.store.SetUserSetting(context.Background(), alex.user.ID, k, v); err != nil {
			t.Fatal(err)
		}
	}

	var sec RelatedSection
	alex.do("GET", "/api/v1/issues/AS-1/notes", nil, 200, &sec)
	if sec.Status != "ok" || len(sec.Items) != 0 || !strings.Contains(sec.NewURL, "/new?") || !strings.Contains(sec.NewURL, "issue%2FAS-1") {
		t.Fatalf("before: %+v", sec)
	}
	if alex.status("POST", "/api/v1/issues/AS-1/notes", map[string]any{"note_id": "../x"}) != 400 {
		t.Fatal("a note id that is not a number went through")
	}
	alex.do("POST", "/api/v1/issues/AS-1/notes", map[string]any{"note_id": 401}, 200, nil)
	if text := notes.notes["401"]; !strings.Contains(text, "\n[AS-1 · Audit (what) Athena learns](") || !strings.HasSuffix(text, "/issue/AS-1)") {
		t.Fatalf("note: %q", text)
	}
	alex.do("GET", "/api/v1/issues/AS-1/notes", nil, 200, &sec)
	if len(sec.Items) != 1 || sec.Items[0]["id"] != "401" || sec.Items[0]["folder"] != "Projects" {
		t.Fatalf("after: %+v", sec)
	}
	if alex.status("POST", "/api/v1/issues/AS-1/notes", map[string]any{"note_id": 999}) != 502 {
		t.Fatal("a missing note did not fail")
	}

	// the team's folder: issue notes start there, the team page lists it
	ben := person(t, s, h, "ben@example.com", "Ben", "member")
	alex.do("POST", "/api/v1/teams/AS/members", map[string]any{"user_id": ben.user.ID, "role": "member"}, 200, nil)
	if ben.status("PUT", "/api/v1/teams/AS/notes", map[string]any{"folder": "Mine"}) != 403 {
		t.Fatal("a member chose the team's folder")
	}
	var tn struct {
		Folder    string         `json:"folder"`
		FolderURL string         `json:"folder_url"`
		Notes     RelatedSection `json:"notes"`
	}
	alex.do("PUT", "/api/v1/teams/AS/notes", map[string]any{"folder": " /Athena//Plans/ "}, 200, &tn)
	if tn.Folder != "Athena/Plans" || len(tn.Notes.Items) != 1 || !strings.HasSuffix(tn.FolderURL, "/?in=Athena%2FPlans") {
		t.Fatalf("team notes: %+v", tn)
	}
	alex.do("GET", "/api/v1/issues/AS-1/notes", nil, 200, &sec)
	if !strings.Contains(sec.NewURL, "folder=Athena%2FPlans") {
		t.Fatalf("new note folder: %s", sec.NewURL)
	}
	var team2 store.Team
	alex.do("GET", "/api/v1/teams/AS", nil, 200, &team2)
	if team2.NotesFolder != "Athena/Plans" {
		t.Fatalf("team json: %q", team2.NotesFolder)
	}
	var folders []map[string]any
	alex.do("GET", "/api/v1/notes/folders", nil, 200, &folders)
	if len(folders) != 2 {
		t.Fatalf("folders: %v", folders)
	}
}
