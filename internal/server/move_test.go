package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// Pocket Move: move:export reaches the description and the exports,
// move:import the description and the import; each person moves only what
// is theirs (issues they wrote or are assigned to, their own comments), and
// a second run of the same file adds nothing.
func TestMovePerPerson(t *testing.T) {
	fc := &fakeTimeCore{links: map[string][3]string{
		"pcx_alex_out":  {core.ScopeMoveExport, "move", "alex@example.com"},
		"pcx_alex_in":   {core.ScopeMoveImport, "move", "alex@example.com"},
		"pcx_eve_out":   {core.ScopeMoveExport, "move", "eve@example.com"},
		"pcx_eve_in":    {core.ScopeMoveImport, "move", "eve@example.com"},
		"pcx_alex_read": {"projects:read", "draw", "alex@example.com"},
	}}
	csrv := httptest.NewServer(fc)
	t.Cleanup(csrv.Close)
	s, h := apiServerCore(t, csrv.URL)
	alex := person(t, s, h, "alex@example.com", "Alex", "admin")
	eve := person(t, s, h, "eve@example.com", "Eve", "member")
	var web, ops store.Team
	alex.do("POST", "/api/v1/teams", map[string]any{"name": "Web", "key": "web", "preset": "software"}, 201, &web)
	eve.do("POST", "/api/v1/teams", map[string]any{"name": "Ops", "key": "ops", "preset": "software"}, 201, &ops)
	var login, footer, keys store.Issue
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": web.ID, "title": "Fix the login redirect", "assignee_id": alex.user.ID, "priority": 2, "labels": []string{"bug"}}, 201, &login)
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": web.ID, "title": "Footer links", "description": "the links in the footer"}, 201, &footer)
	eve.do("POST", "/api/v1/issues", map[string]any{"team_id": ops.ID, "title": "Rotate keys"}, 201, &keys)
	alex.do("POST", "/api/v1/issues/"+login.Identifier+"/comments", map[string]any{"body": "seen on Safari only"}, 201, nil)

	out := &apiClient{t: t, h: h, token: "pcx_alex_out"}
	in := &apiClient{t: t, h: h, token: "pcx_alex_in"}
	read := &apiClient{t: t, h: h, token: "pcx_alex_read"}
	for _, c := range []struct {
		c            *apiClient
		method, path string
		want         int
	}{
		{out, "GET", core.PortablePath, 200},
		{out, "GET", "/api/v1/export.json", 200},
		{out, "GET", "/api/v1/export/my-issues.csv", 200},
		{out, "POST", "/api/v1/import", 403},
		{out, "GET", "/api/v1/issues", 403},
		{out, "GET", "/api/v1/export/issues.csv", 403},
		{out, "GET", "/api/v1/teams/web/export.json", 403},
		{out, "GET", "/api/v1/settings", 403},
		{out, "GET", "/api/v1/backup", 403},
		{in, "GET", core.PortablePath, 200},
		{in, "GET", "/api/v1/export.json", 403},
		{in, "POST", "/api/v1/issues", 403},
		{in, "POST", "/api/v1/teams/web/import", 403},
		{in, "PUT", "/api/v1/settings", 403},
		{read, "GET", core.PortablePath, 403},
		{read, "GET", "/api/v1/export.json", 403},
	} {
		if code, body := c.c.raw(c.method, c.path, "application/json", []byte("{}")); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%s)", c.c.token, c.method, c.path, code, c.want, body)
		}
	}
	var p core.Portable
	out.do("GET", core.PortablePath, nil, 200, &p)
	if len(p.Exports) != 2 || len(p.Imports) != 1 || p.Exports[0].Kind != core.KindPocket {
		t.Fatalf("portable: %+v", p)
	}

	code, dump := out.raw("GET", "/api/v1/export.json", "", nil)
	if code != 200 || !strings.Contains(string(dump), "Fix the login redirect") || !strings.Contains(string(dump), "seen on Safari only") ||
		!strings.Contains(string(dump), "the links in the footer") || strings.Contains(string(dump), "Rotate keys") {
		t.Fatalf("alex's export: %d %s", code, dump)
	}
	if _, csv := out.raw("GET", "/api/v1/export/my-issues.csv", "", nil); !strings.Contains(string(csv), "Footer links") || strings.Contains(string(csv), "Rotate keys") {
		t.Fatalf("alex's csv: %s", csv)
	}
	if _, e := (&apiClient{t: t, h: h, token: "pcx_eve_out"}).raw("GET", "/api/v1/export.json", "", nil); strings.Contains(string(e), "login") {
		t.Fatalf("eve's export holds alex's issues: %s", e)
	}

	// Alex's file into Eve's account: she is not in Web, so the issues go to her Ops, as hers
	eveIn := &apiClient{t: t, h: h, token: "pcx_eve_in"}
	for run, want := range []float64{2, 0} {
		var res map[string]any
		eveIn.do("POST", "/api/v1/import", json.RawMessage(dump), 200, &res)
		if res["created"] != want || res["skipped"] != 2-want || (run == 0 && res["comments"] != float64(1)) {
			t.Fatalf("import run %d: %v", run+1, res)
		}
	}
	// Alex's own file back into his own account: all there already
	var own map[string]any
	in.do("POST", "/api/v1/import", json.RawMessage(dump), 200, &own)
	if own["created"] != float64(0) || own["skipped"] != float64(2) {
		t.Fatalf("own import: %v", own)
	}
	var evs struct {
		Issues []store.Issue `json:"issues"`
	}
	eve.do("GET", "/api/v1/issues?team=ops&show_completed=all", nil, 200, &evs)
	if len(evs.Issues) != 3 {
		t.Fatalf("eve's ops issues: %+v", evs.Issues)
	}
	for _, is := range evs.Issues {
		if is.Title == "Fix the login redirect" && (is.Priority != 2 || is.CreatorID == nil || *is.CreatorID != eve.user.ID) {
			t.Fatalf("imported issue: %+v", is)
		}
	}
	var als struct {
		Issues []store.Issue `json:"issues"`
	}
	alex.do("GET", "/api/v1/issues?team=web", nil, 200, &als)
	if len(als.Issues) != 2 {
		t.Fatalf("alex's web issues changed: %+v", als.Issues)
	}
}
