package server

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// The team's deploy server as a page (sidebar → athena-box): what runs there,
// every branch of the team's repository, the slot's recent deploys, and a
// deploy of any branch — not only an issue's. Pocket AI does the work
// (GET /link/deploy/server, POST /link/deploys); branches are matched to the
// team's issues by pull request or by the identifier in their name.

func (s *Server) deployServerRoutes(r chi.Router) {
	r.Get("/teams/{ref}/server", s.handleTeamServer)
	r.Get("/teams/{ref}/server/check", s.handleTeamServerCheck)
	r.Post("/teams/{ref}/server/deploy", s.handleTeamServerDeploy)
	r.Get("/teams/{ref}/server/deploy/{id}", s.handleTeamServerGetDeploy)
}

// branchIssues finds the team's issue behind each branch name.
type branchIssues struct {
	s     *Server
	ctx   context.Context
	team  *store.Team
	uid   int64
	prs   map[string]store.BranchIssue
	ident *regexp.Regexp
	seen  map[string]*store.BranchIssue
}

func (s *Server) newBranchIssues(ctx context.Context, team *store.Team, uid int64) *branchIssues {
	prs, _ := s.store.TeamPRBranches(ctx, team.ID)
	return &branchIssues{s: s, ctx: ctx, team: team, uid: uid, prs: prs, seen: map[string]*store.BranchIssue{},
		ident: regexp.MustCompile(`(?i)(?:^|[/_.-])(` + regexp.QuoteMeta(team.Key) + `-[0-9]{1,9})(?:$|[/_.-])`)}
}

// byIdent: an identifier of the team's → its issue (nil = none of the team's).
func (b *branchIssues) byIdent(ident string) *store.BranchIssue {
	ident = strings.ToUpper(ident)
	if v, ok := b.seen[ident]; ok {
		return v
	}
	var out *store.BranchIssue
	if id, err := b.s.store.IssueIDByIdentifier(b.ctx, ident); err == nil {
		if is, err := b.s.store.GetIssue(b.ctx, id, b.uid); err == nil && is.TeamID == b.team.ID && is.DeletedAt == nil {
			out = &store.BranchIssue{Identifier: is.Identifier, Title: is.Title}
		}
	}
	b.seen[ident] = out
	return out
}

func (b *branchIssues) of(branch string) *store.BranchIssue {
	branch = strings.TrimPrefix(branch, "origin/")
	if v, ok := b.prs[branch]; ok {
		return &v
	}
	if m := b.ident.FindStringSubmatch(branch); m != nil {
		return b.byIdent(m[1])
	}
	return nil
}

func (s *Server) handleTeamServer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t, err := s.loadTeam(r, s.accessOf(r), chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	tai, err := s.store.GetTeamAI(ctx, t.ID)
	if err != nil {
		s.fail(w, err, "team ai")
		return
	}
	if tai.Cwd == "" {
		writeJSON(w, http.StatusOK, map[string]any{"configured": false, "detail": "no code folder"})
		return
	}
	sb, err := s.aiSibling(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"configured": false, "detail": err.Error()})
		return
	}
	q := url.Values{"host_id": {strconv.FormatInt(tai.HostID, 10)}, "cwd": {tai.Cwd}}
	if r.URL.Query().Get("fetch") == "1" {
		q.Set("fetch", "1")
	}
	var out map[string]any
	if err := sb.call(ctx, http.MethodGet, "/api/v1/link/deploy/server?"+q.Encode(), nil, &out); err != nil {
		if m := aiAnswered.FindStringSubmatch(err.Error()); m != nil && (m[1] == "404" || m[1] == "405") {
			writeJSON(w, http.StatusOK, map[string]any{"configured": false, "detail": "Pocket AI is too old for the server page (needs 0.26.1)"})
			return
		}
		aiFail(w, err)
		return
	}
	bi := s.newBranchIssues(ctx, t, me(r))
	if list, ok := out["branches"].([]any); ok {
		for _, x := range list {
			if b, ok := x.(map[string]any); ok {
				if name, _ := b["branch"].(string); name != "" {
					if is := bi.of(name); is != nil {
						b["issue"] = is
					}
				}
			}
		}
	}
	// deploys say which issue they came from: give it its title
	tag := func(x any) {
		d, ok := x.(map[string]any)
		if !ok {
			return
		}
		var is *store.BranchIssue
		if ref, _ := d["ref"].(string); ref != "" && store.IdentifierRe.MatchString(ref) {
			is = bi.byIdent(ref)
		}
		if is == nil {
			if br, _ := d["branch"].(string); br != "" {
				is = bi.of(br)
			}
		}
		if is != nil {
			d["issue"] = is
		}
	}
	tag(out["latest"])
	tag(out["current"])
	if list, ok := out["history"].([]any); ok {
		for _, x := range list {
			tag(x)
		}
	}
	if now, ok := out["now"].(map[string]any); ok {
		if br, _ := now["branch"].(string); br != "" {
			if is := bi.of(br); is != nil {
				now["issue"] = is
			}
		}
	}
	out["can_deploy"] = s.accessOf(r).check(t.ID, needWrite) == nil
	out["team_key"] = t.Key
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleTeamServerCheck(w http.ResponseWriter, r *http.Request) {
	branch := r.URL.Query().Get("branch")
	if branch == "" {
		writeErr(w, http.StatusBadRequest, "branch is required")
		return
	}
	sb, tai, ok := s.teamDeploySibling(w, r, needRead)
	if !ok {
		return
	}
	q := url.Values{"host_id": {strconv.FormatInt(tai.HostID, 10)}, "cwd": {tai.Cwd}, "check": {branch}}
	var out map[string]any
	if err := sb.call(r.Context(), http.MethodGet, "/api/v1/link/deploy?"+q.Encode(), nil, &out); err != nil {
		aiFail(w, err)
		return
	}
	if out["lines"] == nil {
		out["lines"] = []any{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleTeamServerDeploy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		Branch string `json:"branch"`
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Branch) == "" {
		writeErr(w, http.StatusBadRequest, "pick the branch to deploy")
		return
	}
	sb, tai, ok := s.teamDeploySibling(w, r, needWrite)
	if !ok {
		return
	}
	t, _ := s.loadTeam(r, s.accessOf(r), chi.URLParam(r, "ref"), needWrite)
	body := map[string]any{"host_id": tai.HostID, "cwd": tai.Cwd, "source": "projects", "branch": in.Branch}
	// a branch of one of the team's issues deploys as that issue's (the badge
	// on the deployed app links back, the issue's Activity says so)
	var is *store.Issue
	if b := s.newBranchIssues(ctx, t, me(r)).of(in.Branch); b != nil {
		if id, err := s.store.IssueIDByIdentifier(ctx, b.Identifier); err == nil {
			if x, err := s.store.GetIssue(ctx, id, me(r)); err == nil {
				is = x
				body["ref"], body["ref_url"], body["ref_label"] = x.Identifier, s.cfg.BaseURL+"/issue/"+x.Identifier, x.Title
			}
		}
	}
	var raw map[string]any
	if err := sb.call(ctx, http.MethodPost, "/api/v1/link/deploys", body, &raw); err != nil {
		aiFail(w, err)
		return
	}
	deployLiveByUser.Range(func(k, _ any) bool { deployLiveByUser.Delete(k); return true }) // the slot is everyone's
	if is != nil {
		label, _ := raw["branch"].(string)
		if h, _ := raw["host_name"].(string); h != "" {
			label += " to " + h
		}
		id, _ := raw["id"].(float64)
		_ = s.store.AddHistory(ctx, store.History{IssueID: &is.ID, ActorID: ptr(me(r)), Field: "deploy", ToValue: strconv.FormatInt(int64(id), 10), ToLabel: label})
		s.send(ctx, change{Kind: "issue", TeamIDs: []int64{is.TeamID}, IDs: []int64{is.ID}})
	}
	writeJSON(w, http.StatusCreated, raw)
}

func (s *Server) handleTeamServerGetDeploy(w http.ResponseWriter, r *http.Request) {
	if _, err := s.loadTeam(r, s.accessOf(r), chi.URLParam(r, "ref"), needRead); err != nil {
		s.fail(w, err, "team")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad deploy id")
		return
	}
	sb, err := s.aiSibling(r.Context())
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	var out map[string]any
	if err := sb.call(r.Context(), http.MethodGet, "/api/v1/link/deploys/"+strconv.FormatInt(id, 10), nil, &out); err != nil {
		aiFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
