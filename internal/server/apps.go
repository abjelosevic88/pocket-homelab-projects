package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// The other Pocket apps Projects connects to (meetings in the calendar,
// stakeholders from contacts, to-dos, notes). They are reached as the
// person, through a Pocket Core app link ("core") or an address and token
// they pasted ("direct"), never with a box-wide key.

const (
	modeCore   = "core"
	modeDirect = "direct"
	modeOff    = "off"
)

// PocketApp describes one app Projects connects to.
type PocketApp struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Slug        string   `json:"-"`
	Scopes      []string `json:"scopes"`
	Reason      string   `json:"reason"`
	Description string   `json:"description"`
	ReadScopes  []string `json:"-"` // enough for looking only (default: Scopes)
}

// reading narrows the app to the scopes for looking only, so lists keep
// working while the link for writing is switched off.
func (a PocketApp) reading() PocketApp {
	if len(a.ReadScopes) > 0 {
		a.Scopes = a.ReadScopes
	}
	return a
}

// only narrows the app to the given scopes.
func (a PocketApp) only(scopes ...string) PocketApp { a.Scopes = scopes; return a }

var pocketApps = []PocketApp{
	{ID: "calendar", Name: "Pocket Calendar", Slug: "calendar", Scopes: []string{"calendar:read", "calendar:write"},
		Reason:      "Schedule meetings and work sessions for issues, projects and milestones, and show your calendar next to your deadlines",
		Description: "\"Schedule\" on an issue, project or milestone makes an event with the right people invited and a link back; the Calendar view shows your events next to due dates."},
	{ID: "contacts", Name: "Pocket Contacts", Slug: "contacts", Scopes: []string{"contacts:read"},
		Reason:      "Pick a project's stakeholders and an issue's client or requester from your contacts",
		Description: "Stakeholders on projects and contacts on issues come from your address book; their page in Pocket Contacts lists the projects they are part of."},
	{ID: "tasks", Name: "Pocket Tasks", Slug: "tasks", Scopes: []string{"tasks:read", "tasks:write"},
		Reason:      "Put an issue on your personal to-do list and tick it off when the issue is done",
		Description: "\"Add to my Tasks\" on an issue makes a to-do linking back to it (with its due date); completing the issue completes the to-do."},
	{ID: "notes", Name: "Pocket Notes", Slug: "notes", Scopes: []string{"notes:read", "notes:write"}, ReadScopes: []string{"notes:read"},
		Reason:      "Show the notes that link to an issue or a project, and link a note to an issue by adding the issue's link to it",
		Description: "Notes that link to an issue or a project appear on its page; \"New note\" opens Pocket Notes with it linked, \"Link a note\" adds the link to one of your notes."},
	{ID: "draw", Name: "Pocket Draw", Slug: "draw", Scopes: []string{"drawings:read", "drawings:write"}, ReadScopes: []string{"drawings:read"},
		Reason:      "Show sketches and diagrams from Pocket Draw in issues and projects, and start a new one from an issue",
		Description: "\"Drawing\" in a description or comment puts one of your drawings in as a picture that stays up to date; clicking it opens it in Pocket Draw. \"New sketch\" starts one there."},
	{ID: "pr-review", Name: "Pocket PR Review", Slug: "pr-review", Scopes: []string{"prs:read", "prs:write"}, ReadScopes: []string{"prs:read"},
		Reason:      "Open an issue's pull request as a guided walkthrough and show how far its review got",
		Description: "\"Walkthrough\" on a pull request linked to an issue opens it in Pocket PR Review, with the changes in the order to read them; the row shows how far your review got."},
	{ID: "ai", Name: "Pocket AI", Slug: "ai", Scopes: []string{"threads:write"},
		Reason:      "Prepare Claude Code / Codex conversations about issues in the team's code folder",
		Description: "\"Code with Pocket AI\" on an issue opens Pocket AI with the issue, the team's instructions and memory ready; you read the prompt and press send."},
}

func pocketApp(id string) (PocketApp, bool) {
	for _, a := range pocketApps {
		if a.ID == id {
			return a, true
		}
	}
	return PocketApp{}, false
}

func appUses() []core.Use {
	var out []core.Use
	for _, a := range pocketApps {
		out = append(out, core.Use{App: a.Slug, Scopes: a.Scopes, Reason: a.Reason})
	}
	return out
}

// appMode is the mode in effect for a person: chosen, else direct when a
// token was saved, else on through Core.
func (s *Server) appMode(st store.Settings, app PocketApp) string {
	switch m := st.Integrations[app.ID]; m {
	case modeCore, modeDirect, modeOff:
		return m
	}
	if st.IntegrationToken[app.ID] {
		return modeDirect
	}
	return modeCore
}

func linkReason(app, reason string) string {
	switch reason {
	case "off_person":
		return "you switched the Projects → " + app + " link off in Pocket Core (Settings → Apps acting for me)"
	case "off_admin":
		return "the Projects → " + app + " link is switched off in Pocket Core (Links); new links start switched off until an admin turns them on"
	case "target_missing":
		return app + " is not installed on this box"
	case "not_offered":
		return "this " + app + " version does not accept app links yet"
	case "not_declared":
		return "Pocket Core has not seen this Projects version yet; it re-registers within a minute of starting"
	case "not_in_audience":
		return "you do not have access to " + app + " in Pocket Core"
	case "unknown_user":
		return "your email is not a person in Pocket Core"
	}
	return ""
}

// sibling is another Pocket app reached for one person.
type sibling struct {
	base   string // API address (may be internal)
	public string // address for links the browser opens
	token  string
}

// siblingFor connects to another Pocket app as the person.
func (s *Server) siblingFor(ctx context.Context, u *store.User, app PocketApp) (*sibling, error) {
	st, _ := s.store.GetSettings(ctx, u.ID)
	switch s.appMode(st, app) {
	case modeOff:
		return nil, errors.New("switched off")
	case modeDirect:
		token := s.store.UserSetting(ctx, u.ID, "app.token."+app.ID)
		base := st.IntegrationURLs[app.ID]
		if token == "" || base == "" {
			return nil, fmt.Errorf("a direct connection needs %s's address and an API token", app.Name)
		}
		return &sibling{base: strings.TrimRight(base, "/"), token: token, public: strings.TrimRight(base, "/")}, nil
	}
	if !s.core.Configured() {
		return nil, errors.New("there is no Pocket Core on this box; use a direct connection")
	}
	tok, err := s.core.ActAs(ctx, u.Email, app.Slug, app.Scopes...)
	if err != nil {
		var le *core.LinkError
		if errors.As(err, &le) {
			if t := linkReason(app.Name, le.Reason); t != "" {
				return nil, errors.New(t)
			}
		}
		return nil, err
	}
	return &sibling{base: strings.TrimRight(tok.URL, "/"), token: tok.AccessToken, public: strings.TrimRight(firstNonEmpty(tok.PublicURL, tok.URL), "/")}, nil
}

var siblingHTTP = &http.Client{Timeout: 15 * time.Second}

func (sb *sibling) call(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, sb.base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+sb.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "PocketProjects/"+Version)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := siblingHTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&e)
		return fmt.Errorf("%s answered %d %s", path, res.StatusCode, e.Error)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(res.Body, 32<<20)).Decode(out)
}

// handleIntegrations lists the Pocket apps Projects connects to, with the
// person's mode and whether the connection works right now.
func (s *Server) handleIntegrations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	st, _ := s.store.GetSettings(ctx, u.ID)
	out := []map[string]any{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, app := range pocketApps {
		mode := s.appMode(st, app)
		row := map[string]any{"id": app.ID, "name": app.Name, "description": app.Description, "scopes": app.Scopes, "mode": mode,
			"url": st.IntegrationURLs[app.ID], "has_token": st.IntegrationToken[app.ID], "status": "", "detail": "", "public_url": ""}
		out = append(out, row)
		wg.Add(1)
		go func(app PocketApp, row map[string]any, mode string) {
			defer wg.Done()
			status, detail, public := "off", "", ""
			// The app's address is known even while the link is off, so pages can
			// still recognise its links (a drawing's picture shows a placeholder).
			resolve := func() {
				if res, err := s.core.Resolve(ctx, app.Slug); err == nil {
					public = firstNonEmpty(res.PublicURL, res.URL)
				}
			}
			switch mode {
			case modeDirect:
				status, public = "ok", st.IntegrationURLs[app.ID]
				if st.IntegrationURLs[app.ID] == "" || !st.IntegrationToken[app.ID] {
					status, detail = "error", "needs the app's address and an API token"
				}
			case modeCore:
				if !s.core.Configured() {
					status, detail = "error", "there is no Pocket Core on this box"
				} else if on, reason := s.core.LinkOn(ctx, u.Email, app.Slug, app.Scopes...); on {
					status = "ok"
					resolve()
				} else {
					status, detail = "error", firstNonEmpty(linkReason(app.Name, reason), reason)
					resolve()
				}
			default:
				if public = st.IntegrationURLs[app.ID]; public == "" && s.core.Configured() {
					resolve()
				}
			}
			mu.Lock()
			row["status"], row["detail"], row["public_url"] = status, detail, strings.TrimRight(public, "/")
			mu.Unlock()
		}(app, row, mode)
	}
	wg.Wait()
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSaveIntegration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	app, ok := pocketApp(chi.URLParam(r, "id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "no such app")
		return
	}
	var in struct {
		Mode  *string `json:"mode"`
		URL   *string `json:"url"`
		Token *string `json:"token"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Mode != nil {
		switch *in.Mode {
		case modeCore, modeDirect, modeOff:
		default:
			writeErr(w, http.StatusBadRequest, "mode must be core, direct or off")
			return
		}
		_ = s.store.SetUserSetting(ctx, u.ID, "app.mode."+app.ID, *in.Mode)
	}
	if in.URL != nil {
		_ = s.store.SetUserSetting(ctx, u.ID, "app.url."+app.ID, strings.TrimRight(strings.TrimSpace(*in.URL), "/"))
	}
	if in.Token != nil {
		_ = s.store.SetUserSetting(ctx, u.ID, "app.token."+app.ID, strings.TrimSpace(*in.Token))
	}
	go s.reportModes(context.WithoutCancel(ctx))
	s.handleIntegrations(w, r)
}
