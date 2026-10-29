package server

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

func TestSharedNotes(t *testing.T) {
	s, h := apiServer(t)
	alex := person(t, s, h, "alex@example.com", "Alex", "admin")
	ben := person(t, s, h, "ben@example.com", "Ben", "member")
	carl := person(t, s, h, "carl@example.com", "Carl", "member") // not on the team
	var team store.Team
	alex.do("POST", "/api/v1/teams", map[string]any{"name": "Athena", "key": "as", "preset": "software"}, 201, &team)
	alex.do("POST", "/api/v1/teams/AS/members", map[string]any{"user_id": ben.user.ID, "role": "member"}, 200, nil)
	var is, other store.Issue
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": team.ID, "title": "Pilot plan"}, 201, &is)
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": team.ID, "title": "Something else"}, 201, &other)

	// written on the issue: everyone on the team sees it, nobody else
	var n store.IssueDoc
	alex.do("POST", "/api/v1/issues/AS-1/shared-notes", map[string]any{"title": " Decisions ", "body": "We ship on Monday."}, 201, &n)
	if n.Title != "Decisions" || n.SourceNote != "" {
		t.Fatalf("created: %+v", n)
	}
	var detail struct {
		Shared  []store.IssueDoc `json:"shared_notes"`
		History []store.History  `json:"history"`
	}
	ben.do("GET", "/api/v1/issues/AS-1", nil, 200, &detail)
	if len(detail.Shared) != 1 || detail.Shared[0].Body != "" || detail.Shared[0].Title != "Decisions" {
		t.Fatalf("on the issue: %+v", detail.Shared)
	}
	var full store.IssueDoc
	ben.do("GET", "/api/v1/issues/AS-1/shared-notes/"+itoa64(n.ID), nil, 200, &full)
	if full.Body != "We ship on Monday." {
		t.Fatalf("full: %+v", full)
	}
	if carl.status("GET", "/api/v1/issues/AS-1/shared-notes/"+itoa64(n.ID), nil) != 404 || carl.status("GET", "/api/v1/issues/AS-1/shared-notes", nil) != 404 {
		t.Fatal("someone off the team read a shared note")
	}
	if alex.status("GET", "/api/v1/issues/AS-2/shared-notes/"+itoa64(n.ID), nil) != 404 {
		t.Fatal("a shared note opened through another issue")
	}

	// edits: the version you started from must still be the newest one
	var saved store.IssueDoc
	ben.do("PATCH", "/api/v1/issues/AS-1/shared-notes/"+itoa64(n.ID), map[string]any{"body": "We ship on Tuesday.", "base": full.UpdatedAt}, 200, &saved)
	code, body := alex.raw("PATCH", "/api/v1/issues/AS-1/shared-notes/"+itoa64(n.ID), "application/json", []byte(`{"body":"Monday!","base":"`+full.UpdatedAt+`"}`))
	if code != 409 || !strings.Contains(string(body), "Ben changed this note meanwhile") || !strings.Contains(string(body), "Tuesday") {
		t.Fatalf("stale edit: %d %s", code, body)
	}
	alex.do("PATCH", "/api/v1/issues/AS-1/shared-notes/"+itoa64(n.ID), map[string]any{"body": "Monday!", "base": full.UpdatedAt, "force": true}, 200, &saved)
	alex.do("PATCH", "/api/v1/issues/AS-1/shared-notes/"+itoa64(n.ID), map[string]any{"title": "Ship day", "base": saved.UpdatedAt}, 200, &saved)
	if saved.Body != "Monday!" || saved.Title != "Ship day" {
		t.Fatalf("saved: %+v", saved)
	}

	// shared from a private note: a copy without the link back, the note stays as it was
	notes := &fakeNotes{notes: map[string]string{"401": "# Plan\n\nsteps"}}
	srv := httptest.NewServer(notes)
	t.Cleanup(srv.Close)
	for k, v := range map[string]string{"app.mode.notes": modeDirect, "app.url.notes": srv.URL, "app.token.notes": "pn_test"} {
		if err := s.store.SetUserSetting(context.Background(), alex.user.ID, k, v); err != nil {
			t.Fatal(err)
		}
	}
	alex.do("POST", "/api/v1/issues/AS-1/notes", map[string]any{"note_id": 401}, 200, nil)
	private := notes.notes["401"]
	var shared struct {
		Note     store.IssueDoc `json:"note"`
		Replaced bool           `json:"replaced"`
	}
	alex.do("POST", "/api/v1/issues/AS-1/notes/401/share", nil, 200, &shared)
	if shared.Replaced || shared.Note.Title != "Plan" || shared.Note.Body != "steps" || shared.Note.SourceNote != "401" || shared.Note.SharedAt == "" {
		t.Fatalf("shared: %+v", shared)
	}
	if notes.notes["401"] != private {
		t.Fatalf("the private note changed: %q", notes.notes["401"])
	}
	// share again: the same copy, with the note's text of now
	notes.notes["401"] = strings.Replace(notes.notes["401"], "steps", "steps\n- call the customer", 1)
	first := shared.Note.ID
	alex.do("POST", "/api/v1/issues/AS-1/notes/401/share", nil, 200, &shared)
	if !shared.Replaced || shared.Note.ID != first || !strings.Contains(shared.Note.Body, "call the customer") {
		t.Fatalf("shared again: %+v", shared)
	}
	if code := ben.status("POST", "/api/v1/issues/AS-1/notes/401/share", nil); code < 400 {
		t.Fatalf("ben shared a note without a Pocket Notes of his own: %d", code)
	}
	ben.do("GET", "/api/v1/issues/AS-1", nil, 200, &detail)
	if len(detail.Shared) != 2 {
		t.Fatalf("shared notes: %+v", detail.Shared)
	}
	fields := map[string]bool{}
	for _, hi := range detail.History {
		fields[hi.Field] = true
	}
	if !fields["shared_note_added"] || !fields["shared_note_updated"] {
		t.Fatalf("activity: %v", fields)
	}

	// found by search, for the team only
	var res struct {
		Docs []store.SearchHit `json:"docs"`
	}
	ben.do("GET", "/api/v1/search?q=customer", nil, 200, &res)
	if len(res.Docs) != 1 || res.Docs[0].Kind != "shared_note" || res.Docs[0].Ident != "AS-1" || res.Docs[0].ID != first {
		t.Fatalf("search: %+v", res.Docs)
	}
	carl.do("GET", "/api/v1/search?q=customer", nil, 200, &res)
	if len(res.Docs) != 0 {
		t.Fatalf("search off the team: %+v", res.Docs)
	}

	// Pocket AI gets the team's shared notes with the issue
	ci := s.codeIssueOf(httptest.NewRequest("GET", "/", nil), s.accessFor(context.Background(), alex.user.ID), &is, "AS", "~/x")
	if len(ci.SharedNotes) != 2 {
		t.Fatalf("code issue: %+v", ci.SharedNotes)
	}

	ben.do("DELETE", "/api/v1/issues/AS-1/shared-notes/"+itoa64(first), nil, 204, nil)
	if alex.status("GET", "/api/v1/issues/AS-1/shared-notes/"+itoa64(first), nil) != 404 {
		t.Fatal("deleted shared note still there")
	}
	res.Docs = nil
	ben.do("GET", "/api/v1/search?q=customer", nil, 200, &res)
	if len(res.Docs) != 0 {
		t.Fatalf("deleted note still found: %+v", res.Docs)
	}
}
