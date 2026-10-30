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

	"github.com/go-chi/chi/v5"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// Pocket Time (docs/worklog.md in pocket-homelab-time): with the person's
// connection on, the issue timer IS their one timer in Pocket Time. Start
// here, it runs there; stop it in either place, both show it stopped. Time
// writes the finished time back through the worklog offer (worklog.go), so
// a shared timer never makes an entry here itself. Without the connection
// (off, no Core, link switched off) the local timer works as it always did.

var timeApp = PocketApp{ID: "time", Name: "Pocket Time", Slug: "time", Scopes: []string{"time:read", "time:write"},
	Reason:      "Your issue timer is the same timer as in Pocket Time",
	Description: "The timer on an issue is your one timer in Pocket Time: start it here and it runs there, stop it in either place. Pocket Time writes the finished time back onto the issue."}

func init() {
	pocketApps = append(pocketApps, timeApp)
	extraRoutes = append(extraRoutes, func(s *Server, r chi.Router) {
		r.Post("/integrations/{id}/test", s.handleTestIntegration)
	})
}

// timeLink reaches Pocket Time for the person. nil sibling and nil error =
// not shared (off, no Core, the link not switched on: reason says why, ""
// when the person chose off). An error = it should be shared but Core did
// not answer.
func (s *Server) timeLink(ctx context.Context, u *store.User) (*sibling, string, error) {
	if u == nil {
		return nil, "", nil
	}
	st, _ := s.store.GetSettings(ctx, u.ID)
	switch s.appMode(st, timeApp) {
	case modeOff:
		return nil, "", nil
	case modeDirect:
		sb, err := s.siblingFor(ctx, u, timeApp)
		if err != nil {
			return nil, err.Error(), nil
		}
		return sb, "", nil
	}
	if !s.core.Configured() {
		return nil, "there is no Pocket Core on this box", nil
	}
	tok, err := s.core.ActAs(ctx, u.Email, timeApp.Slug, timeApp.Scopes...)
	if err != nil {
		var le *core.LinkError
		if errors.As(err, &le) {
			return nil, firstNonEmpty(linkReason(timeApp.Name, le.Reason), le.Reason), nil
		}
		return nil, "", fmt.Errorf("Pocket Core did not answer: %w", err)
	}
	return &sibling{base: strings.TrimRight(tok.URL, "/"), token: tok.AccessToken, public: strings.TrimRight(firstNonEmpty(tok.PublicURL, tok.URL), "/")}, "", nil
}

// timeEntry is what Pocket Time says about a timer.
type timeEntry struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	StartedAt   string `json:"started_at"`
	DurationS   int    `json:"duration_s"`
	Running     bool   `json:"running"`
	Source      string `json:"source"`
	App         string `json:"app"`
	AppRef      string `json:"app_ref"`
	AppLabel    string `json:"app_label"`
	AppURL      string `json:"app_url"`
	ProjectName string `json:"project_name"`
}

// timeAnswer is Pocket Time's answer to a timer call.
type timeAnswer struct {
	Running *timeEntry `json:"running"`
	Stopped *timeEntry `json:"stopped"`
	Error   string     `json:"error"`
}

// timeCall calls Pocket Time; a non-2xx answer is returned with its status, not as an error.
func (s *Server) timeCall(ctx context.Context, sb *sibling, method, path string, body any) (int, *timeAnswer, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, sb.base+path, rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+sb.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "PocketProjects/"+Version)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := siblingHTTP.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("Pocket Time did not answer")
	}
	defer res.Body.Close()
	var out timeAnswer
	_ = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out)
	return res.StatusCode, &out, nil
}

// timePlain says in plain words what went wrong with a Time call.
func timePlain(status int, ans *timeAnswer) string {
	switch {
	case status == http.StatusForbidden:
		return "Pocket Time did not allow it: switch the Projects → Pocket Time link on in Pocket Core → Links"
	case status == http.StatusNotFound:
		return "this Pocket Time version has no shared timer yet (update it)"
	case status == http.StatusUnauthorized:
		return "Pocket Time did not accept the connection (check the token in Settings → Connected apps)"
	case ans != nil && ans.Error != "":
		return "Pocket Time: " + ans.Error
	}
	return fmt.Sprintf("Pocket Time answered %d", status)
}

// appTitle names the Pocket app a timer was started in.
func appTitle(slug string) string {
	switch slug {
	case "", "time", "manual", "import":
		return "Pocket Time"
	case "projects":
		return "Pocket Projects"
	case "ai":
		return "Pocket AI"
	}
	return "Pocket " + strings.ToUpper(slug[:1]) + slug[1:]
}

// sharedTimer is Pocket Time's running timer as the UI shows it.
type sharedTimer struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	StartedAt string          `json:"started_at"`
	DurationS int             `json:"duration_s"`
	StartedIn string          `json:"started_in"` // the app it was started in ("Pocket Guitar")
	Here      bool            `json:"here"`       // for an issue here
	Issue     *store.IssueRef `json:"issue,omitempty"`
	URL       string          `json:"url,omitempty"` // where to look at it
}

func (s *Server) sharedView(ctx context.Context, uid int64, e *timeEntry, timeURL string) *sharedTimer {
	if e == nil {
		return nil
	}
	v := &sharedTimer{ID: e.ID, Name: firstNonEmpty(e.Title, e.AppLabel, e.ProjectName, "a timer"), StartedAt: e.StartedAt, DurationS: e.DurationS,
		StartedIn: appTitle(e.Source), URL: timeURL}
	if e.App == "projects" {
		if key, ok := strings.CutPrefix(e.AppRef, "issue:"); ok {
			if id, err := s.store.IssueIDByIdentifier(ctx, key); err == nil {
				a := s.accessFor(ctx, uid)
				if is, err := s.store.GetIssue(ctx, id, uid); err == nil && a.can(is.TeamID, needRead) && is.DeletedAt == nil {
					refs, _ := s.store.IssueRefs(ctx, []int64{is.ID})
					ref := refs[is.ID]
					v.Here, v.Issue, v.URL = true, &ref, "/issue/"+is.Identifier
				}
			}
		}
	}
	return v
}

// localTimerView is the local timer as GET /timer has always answered.
func (s *Server) localTimerView(ctx context.Context, uid int64) map[string]any {
	t := s.store.RunningTimer(ctx, uid)
	if t == nil {
		return map[string]any{"running": false}
	}
	refs, _ := s.store.IssueRefs(ctx, []int64{t.IssueID})
	return map[string]any{"running": true, "entry": t, "issue": refs[t.IssueID]}
}

// sharedTimerView answers GET /timer while the timer is shared.
func (s *Server) sharedTimerView(ctx context.Context, uid int64, sb *sibling) map[string]any {
	view := s.localTimerView(ctx, uid) // a timer started here before connecting still shows (and stops)
	view["shared"], view["time_url"] = true, sb.public
	st, ans, err := s.timeCall(ctx, sb, http.MethodGet, "/api/v1/timer", nil)
	switch {
	case err != nil:
		view["time_error"] = err.Error()
	case st >= 300:
		view["time_error"] = timePlain(st, ans)
	case ans.Running != nil:
		view["time"], view["running"] = s.sharedView(ctx, uid, ans.Running, sb.public), true
	}
	return view
}

// timerShared handles GET /timer for a person whose timer is shared;
// false = not shared, answer locally.
func (s *Server) timerShared(w http.ResponseWriter, r *http.Request) bool {
	ctx := r.Context()
	u := userFrom(ctx)
	sb, reason, err := s.timeLink(ctx, u)
	if err != nil {
		view := s.localTimerView(ctx, u.ID)
		view["shared"], view["time_error"] = false, err.Error()
		writeJSON(w, http.StatusOK, view)
		return true
	}
	if sb == nil {
		view := s.localTimerView(ctx, u.ID)
		view["shared"] = false
		if reason != "" {
			view["time_note"] = reason
		}
		writeJSON(w, http.StatusOK, view)
		return true
	}
	writeJSON(w, http.StatusOK, s.sharedTimerView(ctx, u.ID, sb))
	return true
}

// startShared starts the issue's timer in Pocket Time. false = not shared.
func (s *Server) startShared(w http.ResponseWriter, r *http.Request, is *store.Issue) bool {
	ctx := r.Context()
	u := userFrom(ctx)
	sb, _, err := s.timeLink(ctx, u)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return true
	}
	if sb == nil {
		return false
	}
	var in struct {
		Switch bool `json:"switch"`
	}
	_ = decode(r, &in)
	// "app" and "source" matter only on a direct connection (Time then sees
	// the person, not this app); through a Core link Time takes both from the token.
	body := map[string]any{"app": "projects", "source": "projects", "app_ref": "issue:" + is.Identifier, "app_label": is.Identifier + " " + is.Title, "app_url": "/issue/" + is.Identifier,
		"deliver": true, "switch": in.Switch}
	if is.ProjectID != nil {
		if p, err := s.store.GetProject(ctx, *is.ProjectID); err == nil && p.DeletedAt == nil {
			body["project"] = p.Name
		}
	}
	st, ans, err := s.timeCall(ctx, sb, http.MethodPost, "/api/v1/timer/start", body)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return true
	}
	if st == http.StatusConflict && ans.Running != nil {
		cur := s.sharedView(ctx, u.ID, ans.Running, sb.public)
		writeJSON(w, http.StatusConflict, map[string]any{"error": "A timer is already running: " + cur.Name + " (started in " + cur.StartedIn + ")", "running": cur, "shared": true})
		return true
	}
	if st >= 300 {
		writeErr(w, http.StatusBadGateway, timePlain(st, ans))
		return true
	}
	// a local timer from before connecting stops now, as starting another always did
	if cur := s.store.RunningTimer(ctx, u.ID); cur != nil {
		s.stopTimer(r, cur)
	}
	s.send(ctx, change{Kind: "timer", users: []int64{u.ID}})
	view := s.localTimerView(ctx, u.ID)
	view["shared"], view["time_url"], view["running"] = true, sb.public, ans.Running != nil
	view["time"] = s.sharedView(ctx, u.ID, ans.Running, sb.public)
	view["stopped"] = s.sharedView(ctx, u.ID, ans.Stopped, sb.public)
	writeJSON(w, http.StatusOK, view)
	return true
}

// stopShared stops the person's timer in Pocket Time (and a local one left
// from before connecting). false = not shared.
func (s *Server) stopShared(w http.ResponseWriter, r *http.Request) bool {
	ctx := r.Context()
	u := userFrom(ctx)
	sb, _, err := s.timeLink(ctx, u)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return true
	}
	if sb == nil {
		return false
	}
	var in struct {
		Note   string `json:"note"`
		OnlyIf int64  `json:"only_if"`
	}
	_ = decode(r, &in)
	out := map[string]any{"running": false, "shared": true}
	if cur := s.store.RunningTimer(ctx, u.ID); cur != nil {
		if in.Note != "" {
			cur.Note = in.Note
		}
		s.stopTimer(r, cur)
		out["entry"] = cur
	}
	body := map[string]any{}
	if in.OnlyIf > 0 {
		body["only_if"] = in.OnlyIf
	}
	if in.Note != "" {
		body["note"] = in.Note
	}
	st, ans, err := s.timeCall(ctx, sb, http.MethodPost, "/api/v1/timer/stop", body)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return true
	}
	if st == http.StatusConflict && ans.Running != nil {
		cur := s.sharedView(ctx, u.ID, ans.Running, sb.public)
		writeJSON(w, http.StatusConflict, map[string]any{"error": "A different timer is running now: " + cur.Name + " (started in " + cur.StartedIn + "); it was left running", "running": cur, "shared": true})
		return true
	}
	if st >= 300 {
		writeErr(w, http.StatusBadGateway, timePlain(st, ans))
		return true
	}
	out["stopped"] = s.sharedView(ctx, u.ID, ans.Stopped, sb.public)
	s.send(ctx, change{Kind: "timer", users: []int64{u.ID}})
	writeJSON(w, http.StatusOK, out)
	return true
}

// handleTestIntegration tries a connection to another Pocket app as the person.
func (s *Server) handleTestIntegration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	app, ok := pocketApp(chi.URLParam(r, "id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "no such app")
		return
	}
	fail := func(msg string) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "detail": strings.ToUpper(msg[:1]) + msg[1:]})
	}
	st, _ := s.store.GetSettings(ctx, u.ID)
	if s.appMode(st, app) == modeOff {
		fail("the connection is switched off")
		return
	}
	sb, err := s.siblingFor(ctx, u, app)
	if err != nil {
		fail(err.Error())
		return
	}
	c, cancel := context.WithTimeout(ctx, siblingTimeout)
	defer cancel()
	switch app.ID {
	case "time":
		code, ans, err := s.timeCall(c, sb, http.MethodGet, "/api/v1/timer", nil)
		if err != nil {
			fail(err.Error())
			return
		}
		if code >= 300 {
			fail(timePlain(code, ans))
			return
		}
		detail := "Working. No timer is running."
		if ans.Running != nil {
			v := s.sharedView(ctx, u.ID, ans.Running, sb.public)
			detail = "Working. Running now: " + v.Name + " (started in " + v.StartedIn + ")."
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "detail": detail})
		return
	case "ai":
		// Pocket AI answers only its own tools; a token from Core is the test
	default:
		if err := sb.call(c, http.MethodGet, "/api/v1/auth/me", nil, nil); err != nil {
			fail(app.Name + " did not accept the connection (" + err.Error() + ")")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "detail": "Working."})
}
