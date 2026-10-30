package server

// Code with Pocket AI on a connected instance's issue: the person's own
// Pocket AI (this instance's link), their own setup for that team (stored
// here), the issue and the team's instructions read from over there. The
// conversations are recorded here only (remote_threads).

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// remoteTeamAI is the connected team's setup (its instructions, memory, how
// it links commits) with the person's own on top. The machine and folder
// are always the person's: the team's name the other instance's machines.
func (s *Server) remoteTeamAI(r *http.Request, rem *store.Remote, teamKey string) (*store.TeamAI, *store.PersonAI, error) {
	var team store.TeamAI
	if err := s.remoteCall(r.Context(), rem, http.MethodGet, "/api/v1/teams/"+url.PathEscape(teamKey)+"/ai", nil, &team); err != nil {
		return nil, nil, err
	}
	team.HostID, team.Cwd, team.AccountID = 0, "", nil
	p, err := s.store.RemotePersonAI(r.Context(), rem.ID, strings.ToUpper(teamKey))
	if err != nil {
		return nil, nil, err
	}
	out, _ := team.With(p)
	if p.HostID == nil {
		out.HostID = 0
	}
	return out, p, nil
}

func (s *Server) handleRemoteGetMyAI(w http.ResponseWriter, r *http.Request) {
	rem := remoteFrom(r.Context())
	tai, p, err := s.remoteTeamAI(r, rem, chi.URLParam(r, "ref"))
	if err != nil {
		s.fail(w, err, "team ai")
		return
	}
	writeJSON(w, http.StatusOK, remoteMyAIView(tai, p))
}

func remoteMyAIView(tai *store.TeamAI, p *store.PersonAI) map[string]any {
	uses := map[string]any{"host_id": tai.HostID, "cwd": tai.Cwd, "provider": tai.Provider, "account_id": tai.AccountID,
		"model": tai.Model, "effort": tai.Effort, "mode": tai.Mode, "own_folder": true}
	return map[string]any{"mine": p, "uses": uses, "connected": true}
}

// handleRemotePutMyAI saves the person's own setup for a connected team.
func (s *Server) handleRemotePutMyAI(w http.ResponseWriter, r *http.Request) {
	var in struct {
		HostID    *int64 `json:"host_id"`
		Cwd       string `json:"cwd"`
		Provider  string `json:"provider"`
		AccountID *int64 `json:"account_id"`
		Model     string `json:"model"`
		Effort    string `json:"effort"`
		Mode      string `json:"mode"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	rem := remoteFrom(r.Context())
	key := strings.ToUpper(chi.URLParam(r, "ref"))
	// the team must be one the person sees over there
	if _, _, err := s.remoteTeamAI(r, rem, key); err != nil {
		s.fail(w, err, "team")
		return
	}
	cwd, err := cleanCwd(in.Cwd)
	if err != nil {
		s.fail(w, err, "team ai")
		return
	}
	if in.Provider != "" && in.Provider != "claude" && in.Provider != "codex" {
		writeErr(w, http.StatusBadRequest, "the agent is claude or codex")
		return
	}
	for _, v := range []string{in.Model, in.Effort, in.Mode} {
		if len(v) > 100 {
			writeErr(w, http.StatusBadRequest, "model, effort and mode are short names")
			return
		}
	}
	if in.HostID != nil && *in.HostID < 0 {
		in.HostID = nil
	}
	if in.AccountID != nil && *in.AccountID < -1 {
		in.AccountID = nil
	}
	p := &store.PersonAI{HostID: in.HostID, Cwd: cwd, Provider: in.Provider, AccountID: in.AccountID,
		Model: strings.TrimSpace(in.Model), Effort: strings.TrimSpace(in.Effort), Mode: strings.TrimSpace(in.Mode)}
	if err := s.store.SaveRemotePersonAI(r.Context(), rem.ID, key, p); err != nil {
		s.fail(w, err, "team ai")
		return
	}
	tai, p, err := s.remoteTeamAI(r, rem, key)
	if err != nil {
		s.fail(w, err, "team ai")
		return
	}
	writeJSON(w, http.StatusOK, remoteMyAIView(tai, p))
}

// handleRemoteAIActive: the person's working conversations on this
// connected instance's issues, by their identifier there.
func (s *Server) handleRemoteAIActive(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	sb, err := s.aiSibling(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"active": out, "connected": false})
		return
	}
	var all []map[string]any
	if err := sb.call(r.Context(), http.MethodGet, "/api/v1/link/active?source=projects", nil, &all); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"active": out, "connected": false})
		return
	}
	prefix := remoteFrom(r.Context()).Host() + "/"
	for _, a := range all {
		if ref, _ := a["ref"].(string); strings.HasPrefix(ref, prefix) {
			a["ref"] = strings.TrimPrefix(ref, prefix)
			out = append(out, a)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"active": out, "connected": true})
}

// handleRemoteIssueCode lists the person's conversations about the issue.
func (s *Server) handleRemoteIssueCode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rem := remoteFrom(ctx)
	ident := strings.ToUpper(chi.URLParam(r, "ref"))
	out := map[string]any{"threads": []codeThread{}, "worktrees": []codeWorktree{}, "connected": false, "detail": ""}
	sb, err := s.aiSibling(ctx)
	if err != nil {
		out["detail"] = err.Error()
		writeJSON(w, http.StatusOK, out)
		return
	}
	var list []codeThread
	q := url.Values{"source": {"projects"}, "ref": {remoteRef(rem, ident)}}
	if err := sb.call(ctx, http.MethodGet, "/api/v1/link/threads?"+q.Encode(), nil, &list); err != nil {
		out["detail"] = "Pocket AI: " + err.Error()
		writeJSON(w, http.StatusOK, out)
		return
	}
	if list == nil {
		list = []codeThread{}
	}
	for i, t := range list {
		th, err := s.store.FindRemoteThread(ctx, rem.ID, ident, t.DraftID)
		if err != nil {
			continue
		}
		list[i].Action = th.Action
		if th.State != t.Status || (t.URL != "" && th.URL != t.URL) {
			th.State = t.Status
			if t.URL != "" {
				th.URL = t.URL
			}
			_ = s.store.SaveRemoteThread(ctx, th)
		}
	}
	// the worktrees its conversations work in (by the reference; the branch name is the other instance's to say)
	wts := []codeWorktree{}
	wq := url.Values{"source": {"projects"}, "ref": {remoteRef(rem, ident)}}
	if err := sb.call(ctx, http.MethodGet, "/api/v1/link/worktrees?"+wq.Encode(), nil, &wts); err != nil || wts == nil {
		wts = []codeWorktree{}
	}
	for i := range wts {
		if wts[i].URL != "" && !strings.HasPrefix(wts[i].URL, "http") {
			wts[i].URL = sb.public + wts[i].URL
		}
	}
	out["threads"], out["worktrees"], out["connected"] = list, wts, true
	writeJSON(w, http.StatusOK, out)
}

// handleRemoteStartCode makes a Pocket AI draft about a connected instance's
// issue, in the person's own folder, with the team's instructions from there.
func (s *Server) handleRemoteStartCode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rem := remoteFrom(ctx)
	if !s.codeHere(w, r, rem.Host()) {
		return
	}
	var in struct {
		Action   string `json:"action"`
		Note     string `json:"note"`
		Worktree bool   `json:"worktree"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if codeActions[in.Action] == "" {
		writeErr(w, http.StatusBadRequest, "action is investigate, fix or chat")
		return
	}
	if in.Worktree && in.Action == "investigate" {
		writeErr(w, http.StatusBadRequest, "investigating reads the code as it is; a worktree is for fixing or chatting")
		return
	}
	if len(in.Note) > 8000 {
		writeErr(w, http.StatusBadRequest, "keep the note under 8,000 characters")
		return
	}
	is, err := s.issueInfoOf(r, needRead)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	ri := is.Remote
	tai, _, err := s.remoteTeamAI(r, rem, ri.Team.Key)
	if err != nil {
		s.fail(w, err, "team ai")
		return
	}
	if tai.Cwd == "" {
		writeErr(w, http.StatusBadRequest, "Set your own code folder for "+ri.Team.Name+" in Team settings → Pocket AI → Just for you (the team's folder there is on "+rem.Host()+"'s machines)")
		return
	}
	sb, err := s.aiSibling(ctx)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	mode := tai.Mode
	if in.Action == "investigate" {
		var opts agentOptions
		if err := sb.call(ctx, http.MethodGet, "/api/v1/link/agent-options", nil, &opts); err != nil {
			writeErr(w, http.StatusBadGateway, "Pocket AI: "+err.Error())
			return
		}
		if ro := opts.readonlyMode(tai.Provider); ro != "" {
			mode = ro
		}
	}
	ci := s.remoteCodeIssue(r, rem, ri, tai.Cwd)
	if in.Worktree && !tai.LinkRefs {
		ci.Branch = plainBranch(ci.Branch, ri.Identifier)
	}
	own, _ := s.store.ActionTemplates(ctx, me(r), store.RemoteTemplateScope(rem.ID, teamKeyOf(ri.Identifier)))
	title, text, system := buildCodePrompt(in.Action, in.Note, ci, tai, in.Worktree, own)
	body := map[string]any{"cwd": tai.Cwd, "title": title, "text": text, "system": system, "source": "projects",
		"ref": remoteRef(rem, ri.Identifier), "ref_url": ci.URL, "ref_label": ri.Identifier + " " + ri.Title}
	if in.Worktree {
		body["worktree"] = map[string]any{"name": ci.Branch}
	}
	for k, v := range map[string]string{"provider": tai.Provider, "model": tai.Model, "effort": tai.Effort, "mode": mode} {
		if v != "" {
			body[k] = v
		}
	}
	if tai.HostID != 0 {
		body["host_id"] = tai.HostID
	} else if tai.AccountID != nil {
		body["account_id"] = *tai.AccountID
	}
	var draft struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := sb.call(ctx, http.MethodPost, "/api/v1/link/drafts", body, &draft); err != nil {
		writeErr(w, http.StatusBadGateway, "Pocket AI: "+err.Error())
		return
	}
	if draft.URL != "" && !strings.HasPrefix(draft.URL, "http") {
		draft.URL = sb.public + draft.URL
	}
	label := codeActions[in.Action]
	if in.Worktree {
		label += " in a worktree"
	}
	_ = s.store.SaveRemoteThread(ctx, &store.RemoteThread{RemoteID: rem.ID, Issue: ri.Identifier, DraftID: draft.ID, Action: label, Cwd: shortCwd(tai.Cwd), URL: draft.URL, State: "draft"})
	skipped := []string{}
	for _, f := range ci.Files {
		skipped = append(skipped, f.Name)
	}
	writeJSON(w, http.StatusCreated, map[string]any{"url": draft.URL, "draft_id": draft.ID, "send": tai.SendNow, "files": 0, "skipped": skipped})
}

// remoteCodeIssue is the issue's context from the other instance.
func (s *Server) remoteCodeIssue(r *http.Request, rem *store.Remote, ri *remoteIssue, cwd string) codeIssue {
	ci := codeIssue{Identifier: ri.Identifier, Title: ri.Title, URL: firstNonEmpty(ri.URL, rem.URL+"/issue/"+ri.Identifier), Priority: priorityName(ri.Priority),
		Description: ri.Description, Branch: ri.BranchName, Status: ri.StateType, Team: ri.Team.Name, Cwd: cwd,
		NotesFolder: firstNonEmpty(ri.Team.NotesFolder, defaultNotesFolder), Remote: rem.Host(), ID: ri.ID, RemoteID: rem.ID, Workspace: strings.ToLower(rem.Host())}
	if ci.Branch == "" {
		ci.Branch = strings.ToLower(ri.Identifier)
	}
	comments := ri.Comments
	if len(comments) > 10 {
		comments = comments[len(comments)-10:]
	}
	for _, c := range comments {
		ci.Comments = append(ci.Comments, fmt.Sprintf("someone (%s): %s", strings.SplitN(c.CreatedAt, "T", 2)[0], clip(strings.TrimSpace(c.Body), 3000)))
	}
	for _, a := range ri.Attachments {
		switch a.Kind {
		case "ai_thread":
		case "file":
			ci.Files = append(ci.Files, codeFile{Name: a.Title}) // on the other instance: not sent
		case "commit", "pull_request":
			line := strings.ReplaceAll(a.Kind, "_", " ") + ": " + a.Title
			if a.Subtitle != "" {
				line += " — " + a.Subtitle
			}
			if a.State != "" {
				line += " [" + a.State + "]"
			}
			ci.Git = append(ci.Git, line)
		default:
			if a.URL != "" {
				ci.Links = append(ci.Links, strings.TrimSpace(firstNonEmpty(a.Title, a.Kind)+" — "+a.URL))
			}
		}
	}
	var shared []store.IssueDoc
	if err := s.remoteCall(r.Context(), rem, http.MethodGet, "/api/v1/issues/"+url.PathEscape(ri.Identifier)+"/shared-notes?body=1", nil, &shared); err == nil {
		ci.SharedNotes = shared
	}
	return ci
}

func (s *Server) handleRemoteCodeUnlink(w http.ResponseWriter, r *http.Request) {
	s.remoteConversationAction(w, r, http.MethodPost, "/unlink")
}

func (s *Server) handleRemoteCodeDelete(w http.ResponseWriter, r *http.Request) {
	s.remoteConversationAction(w, r, http.MethodDelete, "")
}

func (s *Server) remoteConversationAction(w http.ResponseWriter, r *http.Request, method, suffix string) {
	ctx := r.Context()
	rem := remoteFrom(ctx)
	ident := strings.ToUpper(chi.URLParam(r, "ref"))
	sb, err := s.aiSibling(ctx)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	draft := chi.URLParam(r, "draft")
	if err := sb.call(ctx, method, "/api/v1/link/threads/"+url.PathEscape(draft)+suffix, nil, nil); err != nil {
		writeErr(w, http.StatusBadGateway, "Pocket AI: "+err.Error())
		return
	}
	if th, err := s.store.FindRemoteThread(ctx, rem.ID, ident, draft); err == nil {
		_ = s.store.DeleteRemoteThread(ctx, th.ID)
	}
	w.WriteHeader(http.StatusNoContent)
}
