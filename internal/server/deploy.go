package server

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// Deploy: one click on an issue puts its branch on the project's deploy
// target (a staging box: one slot, the newest deploy is what runs there).
// Pocket AI does the work — it knows the issue's branches, pushes them, runs
// the project's deploy commands on the target machine and follows them — so
// this is a thin layer: which repository (the team's code folder or the
// project's), the Activity entry, and team settings for the commands.

func (s *Server) deployRoutes(r chi.Router) {
	r.Get("/issues/{ref}/deploy", s.handleIssueDeploy)
	r.Post("/issues/{ref}/deploy", s.handleStartDeploy)
	r.Get("/issues/{ref}/deploy/{id}", s.handleGetDeploy)
	r.Get("/deploys/live", s.handleDeploysLive)
	r.Get("/teams/{ref}/deploy", s.handleTeamDeploy)
	r.Put("/teams/{ref}/deploy", s.handlePutTeamDeploy)
}

// deployWhere is the machine + folder whose repository an issue deploys from.
func (s *Server) deployWhere(ctx context.Context, is *store.Issue) (int64, string, error) {
	tai, err := s.store.GetTeamAI(ctx, is.TeamID)
	if err != nil {
		return 0, "", err
	}
	cwd := tai.Cwd
	if is.ProjectID != nil {
		if pc := s.store.ProjectAICwd(ctx, *is.ProjectID); pc != "" {
			cwd = pc
		}
	}
	return tai.HostID, cwd, nil
}

var aiAnswered = regexp.MustCompile(`^\S+ answered (\d{3}) ?`)

// aiFail passes Pocket AI's own words (and its 4xx) on.
func aiFail(w http.ResponseWriter, err error) {
	m := aiAnswered.FindStringSubmatch(err.Error())
	if m == nil {
		writeErr(w, http.StatusBadGateway, "Pocket AI: "+err.Error())
		return
	}
	code, _ := strconv.Atoi(m[1])
	msg := err.Error()[len(m[0]):]
	if code < 400 || code >= 500 {
		code = http.StatusBadGateway
	}
	if msg == "" {
		msg = "Pocket AI answered " + m[1]
	}
	writeErr(w, code, msg)
}

func (s *Server) handleIssueDeploy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	is, err := s.loadIssue(r, s.accessOf(r), chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	host, cwd, err := s.deployWhere(ctx, is)
	if err != nil {
		s.fail(w, err, "team ai")
		return
	}
	if cwd == "" {
		writeJSON(w, http.StatusOK, map[string]any{"configured": false, "detail": "no code folder"})
		return
	}
	sb, err := s.aiSibling(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"configured": false, "detail": err.Error()})
		return
	}
	q := url.Values{"host_id": {strconv.FormatInt(host, 10)}, "cwd": {cwd}, "source": {"projects"}, "ref": {is.Identifier},
		"branch": {s.branchName(ctx, me(r), is)}}
	var out map[string]any
	if err := sb.call(ctx, http.MethodGet, "/api/v1/link/deploy?"+q.Encode(), nil, &out); err != nil {
		// an older Pocket AI has no deploys, or it is restarting: no section
		writeJSON(w, http.StatusOK, map[string]any{"configured": false, "detail": err.Error()})
		return
	}
	out["can_deploy"] = s.accessOf(r).check(is.TeamID, needWrite) == nil
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleStartDeploy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		Branch string `json:"branch"`
	}
	if err := decode(r, &in); err != nil || in.Branch == "" {
		writeErr(w, http.StatusBadRequest, "pick the branch to deploy")
		return
	}
	is, err := s.loadIssue(r, s.accessOf(r), chi.URLParam(r, "ref"), needWrite)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	host, cwd, err := s.deployWhere(ctx, is)
	if err != nil {
		s.fail(w, err, "team ai")
		return
	}
	if cwd == "" {
		writeErr(w, http.StatusBadRequest, "Set the team's code folder in Team settings → Pocket AI")
		return
	}
	sb, err := s.aiSibling(ctx)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	var d struct {
		ID       int64  `json:"id"`
		Branch   string `json:"branch"`
		HostName string `json:"host_name"`
	}
	body := map[string]any{"host_id": host, "cwd": cwd, "source": "projects", "ref": is.Identifier, "branch": in.Branch,
		"ref_url": s.cfg.BaseURL + "/issue/" + is.Identifier, "ref_label": is.Title}
	var raw map[string]any
	if err := sb.call(ctx, http.MethodPost, "/api/v1/link/deploys", body, &raw); err != nil {
		aiFail(w, err)
		return
	}
	if v, ok := raw["id"].(float64); ok {
		d.ID = int64(v)
	}
	d.Branch, _ = raw["branch"].(string)
	d.HostName, _ = raw["host_name"].(string)
	label := d.Branch
	if d.HostName != "" {
		label += " to " + d.HostName
	}
	deployLiveByUser.Range(func(k, _ any) bool { deployLiveByUser.Delete(k); return true }) // the slot is everyone's
	_ = s.store.AddHistory(ctx, store.History{IssueID: &is.ID, ActorID: ptr(me(r)), Field: "deploy", ToValue: strconv.FormatInt(d.ID, 10), ToLabel: label})
	s.send(ctx, change{Kind: "issue", TeamIDs: []int64{is.TeamID}, IDs: []int64{is.ID}})
	writeJSON(w, http.StatusCreated, raw)
}

func (s *Server) handleGetDeploy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if _, err := s.loadIssue(r, s.accessOf(r), chi.URLParam(r, "ref"), needRead); err != nil {
		s.fail(w, err, "issue")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad deploy id")
		return
	}
	sb, err := s.aiSibling(ctx)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	var out map[string]any
	if err := sb.call(ctx, http.MethodGet, "/api/v1/link/deploys/"+strconv.FormatInt(id, 10), nil, &out); err != nil {
		aiFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// The team's deploy (Team settings → Pocket AI): the commands of the team
// code folder's repository, kept in Pocket AI for the project.
func (s *Server) teamDeploySibling(w http.ResponseWriter, r *http.Request, need int) (*sibling, *store.TeamAI, bool) {
	t, err := s.loadTeam(r, s.accessOf(r), chi.URLParam(r, "ref"), need)
	if err != nil {
		s.fail(w, err, "team")
		return nil, nil, false
	}
	tai, err := s.store.GetTeamAI(r.Context(), t.ID)
	if err != nil {
		s.fail(w, err, "team ai")
		return nil, nil, false
	}
	if tai.Cwd == "" {
		writeErr(w, http.StatusBadRequest, "Set the team's code folder first")
		return nil, nil, false
	}
	sb, err := s.aiSibling(r.Context())
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return nil, nil, false
	}
	return sb, tai, true
}

func (s *Server) handleTeamDeploy(w http.ResponseWriter, r *http.Request) {
	sb, tai, ok := s.teamDeploySibling(w, r, needRead)
	if !ok {
		return
	}
	q := url.Values{"host_id": {strconv.FormatInt(tai.HostID, 10)}, "cwd": {tai.Cwd}}
	var out map[string]any
	if err := sb.call(r.Context(), http.MethodGet, "/api/v1/link/deploy?"+q.Encode(), nil, &out); err != nil {
		aiFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePutTeamDeploy(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Config *struct {
			HostID  int64    `json:"host_id"`
			Dir     string   `json:"dir"`
			Run     []string `json:"run"`
			URL     string   `json:"url"`
			Current string   `json:"current"`
		} `json:"config"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	sb, tai, ok := s.teamDeploySibling(w, r, needAdmin)
	if !ok {
		return
	}
	var out map[string]any
	err := sb.call(r.Context(), http.MethodPut, "/api/v1/link/deploy/config", map[string]any{"host_id": tai.HostID, "cwd": tai.Cwd, "config": in.Config}, &out)
	if err != nil {
		aiFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// ---------- what runs on each team's deploy server ----------

// deployLive is one team's deploy server for lists and the sidebar.
type deployLive struct {
	TeamKey  string `json:"team_key"`
	HostName string `json:"host_name"`
	URL      string `json:"url"`
	State    string `json:"state"` // live | running | replaced | none
	Ref      string `json:"ref"`   // the issue on it (live) or being deployed (running)
	Title    string `json:"title,omitempty"` // that issue's title (the feature)
	Branch   string `json:"branch"`
	SHA      string `json:"sha"`
	Now      string `json:"now,omitempty"` // replaced: what the server says runs
	Since    string `json:"since,omitempty"`
}

type deployLiveCache struct {
	at   time.Time
	list []deployLive
}

var deployLiveByUser sync.Map // user id → deployLiveCache

// handleDeploysLive: for every team of the person's with a deploy set up,
// what its deploy server runs now (asked of Pocket AI, kept 20 s).
func (s *Server) handleDeploysLive(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := me(r)
	if c, ok := deployLiveByUser.Load(uid); ok && time.Since(c.(deployLiveCache).at) < 20*time.Second {
		writeJSON(w, http.StatusOK, map[string]any{"teams": c.(deployLiveCache).list, "connected": true})
		return
	}
	sb, err := s.aiSibling(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"teams": []deployLive{}, "connected": false})
		return
	}
	type answer struct {
		Configured bool   `json:"configured"`
		HostName   string `json:"host_name"`
		Config     struct {
			URL string `json:"url"`
		} `json:"config"`
		Latest *struct {
			Ref, Branch, SHA, Status string
			StartedAt                string `json:"started_at"`
		} `json:"latest"`
		Current *struct {
			Ref, Branch, SHA, Status string
			FinishedAt               string `json:"finished_at"`
		} `json:"current"`
		Live struct {
			Live bool   `json:"live"`
			Now  string `json:"now"`
		} `json:"live"`
	}
	asked := map[string]*answer{} // one question per machine + folder
	out := []deployLive{}
	for _, id := range s.accessOf(r).ids() {
		team, err := s.store.GetTeam(ctx, id)
		if err != nil || team.ArchivedAt != nil {
			continue
		}
		tai, err := s.store.GetTeamAI(ctx, id)
		if err != nil || tai.Cwd == "" {
			continue
		}
		key := strconv.FormatInt(tai.HostID, 10) + "|" + tai.Cwd
		a, done := asked[key]
		if !done {
			a = &answer{}
			q := url.Values{"host_id": {strconv.FormatInt(tai.HostID, 10)}, "cwd": {tai.Cwd}}
			if err := sb.call(ctx, http.MethodGet, "/api/v1/link/deploy?"+q.Encode(), nil, a); err != nil {
				a = nil
			}
			asked[key] = a
		}
		if a == nil || !a.Configured {
			continue
		}
		d := deployLive{TeamKey: team.Key, HostName: a.HostName, URL: a.Config.URL, State: "none"}
		switch {
		case a.Latest != nil && a.Latest.Status == "running":
			d.State, d.Ref, d.Branch, d.SHA, d.Since = "running", a.Latest.Ref, a.Latest.Branch, a.Latest.SHA, a.Latest.StartedAt
		case a.Current != nil && a.Live.Live:
			d.State, d.Ref, d.Branch, d.SHA, d.Since = "live", a.Current.Ref, a.Current.Branch, a.Current.SHA, a.Current.FinishedAt
		case a.Current != nil:
			d.State, d.Branch, d.SHA, d.Now, d.Since = "replaced", a.Current.Branch, a.Current.SHA, a.Live.Now, a.Current.FinishedAt
		}
		if d.Ref != "" {
			if iid, err := s.store.IssueIDByIdentifier(ctx, d.Ref); err == nil {
				if is, err := s.store.GetIssue(ctx, iid, uid); err == nil && s.accessOf(r).can(is.TeamID, needRead) {
					d.Title = is.Title
				}
			}
		}
		out = append(out, d)
	}
	deployLiveByUser.Store(uid, deployLiveCache{at: time.Now(), list: out})
	writeJSON(w, http.StatusOK, map[string]any{"teams": out, "connected": true})
}
