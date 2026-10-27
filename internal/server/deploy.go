package server

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

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
	body := map[string]any{"host_id": host, "cwd": cwd, "source": "projects", "ref": is.Identifier, "branch": in.Branch}
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
