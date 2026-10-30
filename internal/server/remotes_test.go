package server

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

func TestConnectedInstance(t *testing.T) {
	// over there: the team's own instance
	sB, hB := apiServer(t)
	srvB := httptest.NewServer(hB)
	t.Cleanup(srvB.Close)
	sB.cfg.BaseURL = srvB.URL
	bob := person(t, sB, hB, "bob@athena.example", "Bob", "admin")
	alexThere := person(t, sB, hB, "alex@example.com", "Alex", "member")
	var team store.Team
	bob.do("POST", "/api/v1/teams", map[string]any{"name": "Athena", "key": "ath", "preset": "software"}, 201, &team)
	bob.do("POST", "/api/v1/teams/ATH/members", map[string]any{"user_id": alexThere.user.ID, "role": "member"}, 200, nil)
	bob.do("PUT", "/api/v1/teams/ATH/ai", map[string]any{"instructions": "ATHENA-RULES: small PRs", "cwd": "/srv/athena"}, 200, nil)
	bob.do("POST", "/api/v1/issues", map[string]any{"team_id": team.ID, "title": "Pilot setup", "description": "Screenshot: ![s](/api/v1/attachments/7/file)"}, 201, nil)

	// here: the person's own instance
	sA, hA := apiServer(t)
	alex := person(t, sA, hA, "alex@example.com", "Alex", "admin")
	eve := person(t, sA, hA, "eve@example.com", "Eve", "member")

	if code := alex.status("POST", "/api/v1/remotes", map[string]any{"url": srvB.URL, "token": "ppt_wrong"}); code != 400 {
		t.Fatalf("a wrong token was accepted: %d", code)
	}
	var rem remoteOut
	alex.do("POST", "/api/v1/remotes", map[string]any{"name": "Athena", "url": srvB.URL + "/api/v1/", "token": alexThere.token}, 201, &rem)
	if rem.URL != srvB.URL || !strings.Contains(rem.RemoteUser, "alex@example.com") || rem.Token != "" {
		t.Fatalf("connection: %+v", rem)
	}
	base := "/r/" + strconv.FormatInt(rem.ID, 10) + "/api/v1"

	// everything else passes through, as the person over there
	var boot struct {
		Teams []store.Team `json:"teams"`
	}
	alex.do("GET", base+"/bootstrap", nil, 200, &boot)
	if len(boot.Teams) != 1 || boot.Teams[0].Key != "ATH" {
		t.Fatalf("bootstrap there: %+v", boot.Teams)
	}
	if eve.status("GET", base+"/bootstrap", nil) != 404 {
		t.Fatal("someone else used alex's connection")
	}
	// file addresses: through here when read, the other instance's own when saved
	var is struct {
		ID          int64  `json:"id"`
		Description string `json:"description"`
	}
	alex.do("GET", base+"/issues/ATH-1", nil, 200, &is)
	if !strings.Contains(is.Description, base+"/attachments/7/file") {
		t.Fatalf("read: %q", is.Description)
	}
	alex.do("PATCH", base+"/issues/ATH-1", map[string]any{"description": is.Description + " (seen)"}, 200, nil)
	bob.do("GET", "/api/v1/issues/ATH-1", nil, 200, &is)
	if is.Description != "Screenshot: ![s](/api/v1/attachments/7/file) (seen)" {
		t.Fatalf("saved there: %q", is.Description)
	}

	// the person's own notes, here, about the issue over there
	notes := &fakeNotes{notes: map[string]string{"401": "# Plan\n\nsteps"}}
	nsrv := httptest.NewServer(notes)
	t.Cleanup(nsrv.Close)
	for k, v := range map[string]string{"app.mode.notes": modeDirect, "app.url.notes": nsrv.URL, "app.token.notes": "pn_test"} {
		if err := sA.store.SetUserSetting(context.Background(), alex.user.ID, k, v); err != nil {
			t.Fatal(err)
		}
	}
	alex.do("POST", base+"/issues/ATH-1/notes", map[string]any{"note_id": 401}, 200, nil)
	if !strings.Contains(notes.notes["401"], "("+srvB.URL+"/issue/ATH-1)") {
		t.Fatalf("note link: %q", notes.notes["401"])
	}
	var sec RelatedSection
	alex.do("GET", base+"/issues/ATH-1/notes", nil, 200, &sec)
	if sec.Status != "ok" || len(sec.Items) != 1 {
		t.Fatalf("notes about it: %+v", sec)
	}
	// shared: the copy lands over there, made by the person's account there
	alex.do("POST", base+"/issues/ATH-1/notes/401/share", nil, 200, nil)
	var detail struct {
		Shared []store.IssueDoc `json:"shared_notes"`
	}
	bob.do("GET", "/api/v1/issues/ATH-1", nil, 200, &detail)
	if len(detail.Shared) != 1 || detail.Shared[0].SourceNote != "401" || detail.Shared[0].CreatedBy == nil || *detail.Shared[0].CreatedBy != alexThere.user.ID {
		t.Fatalf("shared over there: %+v", detail.Shared)
	}

	// the person's own Pocket AI, with their own folder for that team
	ai := &fakeAI{}
	asrv := ai.server(t)
	connectAI(t, sA, alex.user.ID, asrv.URL)
	if code, b := alex.raw("POST", base+"/issues/ATH-1/code", "application/json", []byte(`{"action":"chat"}`)); code != 400 || !strings.Contains(string(b), "Just for you") {
		t.Fatalf("no folder yet: %d %s", code, b)
	}
	if alex.status("PUT", base+"/teams/ATH/ai", map[string]any{"cwd": "~/x"}) != 403 {
		t.Fatal("the team's own setup was changed from here")
	}
	alex.do("PUT", base+"/teams/ATH/ai/mine", map[string]any{"cwd": "~/Projects/athena"}, 200, nil)
	alex.do("POST", base+"/issues/ATH-1/code", map[string]any{"action": "investigate"}, 201, nil)
	d := ai.drafts[len(ai.drafts)-1]
	host := strings.TrimPrefix(srvB.URL, "http://")
	sys, _ := d["system"].(string)
	text, _ := d["text"].(string)
	if d["ref"] != host+"/ATH-1" || d["cwd"] != "~/Projects/athena" || d["mode"] != "plan" || !strings.Contains(sys, "another Pocket Projects instance") ||
		!strings.Contains(sys, "ATHENA-RULES") || !strings.Contains(sys, fmt.Sprintf("pocket_projects_link_pr with id %d, workspace %d", is.ID, rem.ID)) ||
		!strings.Contains(sys, fmt.Sprintf("Pass workspace %d to every pocket_projects_* tool", rem.ID)) || !strings.Contains(text, "Pilot setup") || !strings.Contains(text, "### Shared notes") {
		t.Fatalf("draft: %+v", d)
	}
	if _, err := sA.store.FindRemoteThread(context.Background(), rem.ID, "ATH-1", "d1"); err != nil {
		t.Fatalf("conversation record here: %v", err)
	}
	var atts []store.Attachment
	bob.do("GET", "/api/v1/issues/ATH-1/attachments", nil, 200, &atts)
	if len(atts) != 0 {
		t.Fatalf("the conversation left a trace over there: %+v", atts)
	}

	// Pocket Day here tells the work there too
	date := time.Now().In(sA.loc(context.Background(), alex.user.ID)).Format("2006-01-02")
	var day dayWork
	alex.do("GET", "/api/v1/day/"+date, nil, 200, &day)
	found := false
	for _, di := range day.Issues {
		found = found || di.Identifier == "ATH-1" && strings.HasPrefix(di.URL, srvB.URL)
	}
	if !found {
		t.Fatalf("day: %+v", day.Issues)
	}

	// a token they took back there: a clear answer, never "signed out here"
	var toks []struct {
		ID int64 `json:"id"`
	}
	alexThere.do("GET", "/api/v1/tokens", nil, 200, &toks)
	for _, tk := range toks {
		alexThere.do("DELETE", "/api/v1/tokens/"+strconv.FormatInt(tk.ID, 10), nil, 204, nil)
	}
	if code, b := alex.raw("GET", base+"/issues/ATH-1", "", nil); code != 502 || !strings.Contains(string(b), "no longer accepts your token") {
		t.Fatalf("revoked token: %d %s", code, b)
	}
	alex.do("DELETE", "/api/v1/remotes/"+strconv.FormatInt(rem.ID, 10), nil, 204, nil)
	if alex.status("GET", base+"/bootstrap", nil) != 404 {
		t.Fatal("a removed connection still answered")
	}
}
