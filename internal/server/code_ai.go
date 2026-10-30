package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// "Code with Pocket AI": an issue opens a Claude Code / Codex conversation in
// Pocket AI, in the team's code folder, with the issue as the (editable)
// first message and the team's instructions and memory as hidden system
// context. Pocket AI keeps it a draft until the person presses send; it is
// reached as the person through the "ai" app link (threads:write).

func (s *Server) codeRoutes(r chi.Router) {
	r.Get("/ai/agent-options", s.handleAgentOptions)
	r.Get("/ai/dirs", s.handleAIDirs)
	r.Get("/ai/active", s.handleAIActive)
	r.Get("/teams/{ref}/ai", s.handleGetTeamAI)
	r.Put("/teams/{ref}/ai", s.handlePutTeamAI)
	r.Get("/teams/{ref}/ai/mine", s.handleGetMyAI)
	r.Put("/teams/{ref}/ai/mine", s.handlePutMyAI)
	// where the team's attachments are kept: files.go
	r.Get("/teams/{ref}/files", s.handleTeamFiles)
	r.Put("/teams/{ref}/files", s.handlePutTeamFiles)
	r.Post("/teams/{ref}/files/move", s.handleMoveTeamFiles)
	r.Get("/teams/{ref}/memory", s.handleGetTeamMemory)
	r.Put("/teams/{ref}/memory", s.handlePutTeamMemory)
	r.Post("/teams/{ref}/memory", s.handleRememberTeam)
	r.Get("/projects/{id}/ai", s.handleGetProjectAI)
	r.Put("/projects/{id}/ai", s.handlePutProjectAI)
	r.Get("/issues/{ref}/code", s.handleIssueCode)
	r.Post("/issues/{ref}/code", s.handleStartCode)
	r.Post("/issues/{ref}/code/{draft}/unlink", s.handleCodeUnlink)
	r.Delete("/issues/{ref}/code/{draft}", s.handleCodeDelete)
}

var aiApp, _ = pocketApp("ai")

// agentOptions is the part of Pocket AI's /link/agent-options Projects reads.
type agentOptions struct {
	Providers []struct {
		ID           string `json:"id"`
		Available    bool   `json:"available"`
		ReadonlyMode string `json:"readonly_mode"`
	} `json:"providers"`
	DefaultProvider string `json:"default_provider"`
}

// readonlyMode is the provider's read-only mode ("" when it has none).
func (o *agentOptions) readonlyMode(provider string) string {
	if provider == "" {
		provider = o.DefaultProvider
	}
	for _, p := range o.Providers {
		if p.ID == provider {
			return p.ReadonlyMode
		}
	}
	return ""
}

func (s *Server) aiSibling(ctx context.Context) (*sibling, error) {
	sb, err := s.siblingFor(ctx, userFrom(ctx), aiApp)
	if err != nil {
		return nil, fmt.Errorf("Pocket AI is not connected: %w", err)
	}
	return sb, nil
}

func (s *Server) handleAgentOptions(w http.ResponseWriter, r *http.Request) {
	sb, err := s.aiSibling(r.Context())
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	var raw json.RawMessage
	if err := sb.call(r.Context(), http.MethodGet, "/api/v1/link/agent-options", nil, &raw); err != nil {
		writeErr(w, http.StatusBadGateway, "Pocket AI: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, raw)
}

// handleAIActive lists the person's Pocket AI conversations that are working
// or waiting, by issue identifier, for the live marks in lists and boards.
// Quiet when Pocket AI is not connected: lists simply show no marks.
func (s *Server) handleAIActive(w http.ResponseWriter, r *http.Request) {
	out := []json.RawMessage{}
	sb, err := s.aiSibling(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"active": out, "connected": false})
		return
	}
	if err := sb.call(r.Context(), http.MethodGet, "/api/v1/link/active?source=projects", nil, &out); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"active": []json.RawMessage{}, "connected": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"active": out, "connected": true})
}

// handleAIDirs lists server folders through Pocket AI (Projects itself runs
// in a container and cannot see them) for the code-folder fields.
func (s *Server) handleAIDirs(w http.ResponseWriter, r *http.Request) {
	sb, err := s.aiSibling(r.Context())
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	var raw json.RawMessage
	q := url.Values{"path": {r.URL.Query().Get("path")}}
	if h := r.URL.Query().Get("host"); h != "" && h != "0" {
		q.Set("host", h)
	}
	if err := sb.call(r.Context(), http.MethodGet, "/api/v1/link/dirs?"+q.Encode(), nil, &raw); err != nil {
		writeErr(w, http.StatusBadGateway, "Pocket AI: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, raw)
}

// ---------- team settings, memory, project folder ----------

func (s *Server) handleGetTeamAI(w http.ResponseWriter, r *http.Request) {
	t, err := s.loadTeam(r, s.accessOf(r), chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	ai, err := s.store.GetTeamAI(r.Context(), t.ID)
	if err != nil {
		s.fail(w, err, "team ai")
		return
	}
	writeJSON(w, http.StatusOK, ai)
}

// cleanCwd accepts an absolute or ~ path ("" clears it).
func cleanCwd(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", nil
	}
	if len(p) > 1024 || strings.ContainsAny(p, "\x00\n\r") || !(strings.HasPrefix(p, "/") || p == "~" || strings.HasPrefix(p, "~/")) {
		return "", badRequest("the code folder must be a full path on the server, like ~/Projects/app or /srv/app")
	}
	if strings.HasPrefix(p, "~") {
		return "~" + strings.TrimSuffix(path.Clean("/"+strings.TrimPrefix(p, "~")), "/"), nil
	}
	return path.Clean(p), nil
}

func (s *Server) handlePutTeamAI(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Cwd          string `json:"cwd"`
		Provider     string `json:"provider"`
		AccountID    *int64 `json:"account_id"`
		HostID       int64  `json:"host_id"`
		Model        string `json:"model"`
		Effort       string `json:"effort"`
		Mode         string `json:"mode"`
		Instructions string `json:"instructions"`
		SendNow      bool   `json:"send_now"`
		LinkRefs     bool   `json:"link_refs"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	t, err := s.loadTeam(r, s.accessOf(r), chi.URLParam(r, "ref"), needAdmin)
	if err != nil {
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
	if len(in.Instructions) > store.MaxAIText {
		writeErr(w, http.StatusBadRequest, "the instructions are longer than 32 KB")
		return
	}
	if in.AccountID != nil && *in.AccountID < 0 {
		in.AccountID = nil
	}
	if in.HostID < 0 {
		in.HostID = 0
	}
	if in.HostID != 0 {
		in.AccountID = nil // another machine signs in with its own login
	}
	ai, err := s.store.GetTeamAI(r.Context(), t.ID)
	if err != nil {
		s.fail(w, err, "team ai")
		return
	}
	ai.Cwd, ai.Provider, ai.AccountID, ai.Model, ai.Effort, ai.Mode = cwd, in.Provider, in.AccountID, strings.TrimSpace(in.Model), strings.TrimSpace(in.Effort), strings.TrimSpace(in.Mode)
	ai.Instructions, ai.SendNow, ai.LinkRefs, ai.HostID = strings.TrimSpace(in.Instructions), in.SendNow, in.LinkRefs, in.HostID
	if err := s.store.SaveTeamAI(r.Context(), ai); err != nil {
		s.fail(w, err, "team ai")
		return
	}
	ai, _ = s.store.GetTeamAI(r.Context(), t.ID)
	writeJSON(w, http.StatusOK, ai)
}

// aiFor is the setup a person's code conversations and deploys use in a
// team: the team's, with the person's own (their server, folder, login,
// agent) on top. own: the folder is the person's, not the team's.
func (s *Server) aiFor(ctx context.Context, teamID, uid int64) (*store.TeamAI, bool, error) {
	tai, err := s.store.GetTeamAI(ctx, teamID)
	if err != nil {
		return nil, false, err
	}
	p, err := s.store.GetPersonAI(ctx, teamID, uid)
	if err != nil {
		return nil, false, err
	}
	out, own := tai.With(p)
	return out, own, nil
}

// codeCwd is the folder an issue's conversation works in: the person's own,
// else the project's, else the team's.
func (s *Server) codeCwd(ctx context.Context, tai *store.TeamAI, own bool, projectID *int64) string {
	if !own && projectID != nil {
		if pc := s.store.ProjectAICwd(ctx, *projectID); pc != "" {
			return pc
		}
	}
	return tai.Cwd
}

// myAIView is a person's own setup and what it comes to.
func (s *Server) myAIView(ctx context.Context, teamID, uid int64) (map[string]any, error) {
	p, err := s.store.GetPersonAI(ctx, teamID, uid)
	if err != nil {
		return nil, err
	}
	tai, own, err := s.aiFor(ctx, teamID, uid)
	if err != nil {
		return nil, err
	}
	uses := map[string]any{"host_id": tai.HostID, "cwd": tai.Cwd, "provider": tai.Provider, "account_id": tai.AccountID,
		"model": tai.Model, "effort": tai.Effort, "mode": tai.Mode, "own_folder": own}
	return map[string]any{"mine": p, "uses": uses}, nil
}

func (s *Server) handleGetMyAI(w http.ResponseWriter, r *http.Request) {
	t, err := s.loadTeam(r, s.accessOf(r), chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	out, err := s.myAIView(r.Context(), t.ID, me(r))
	if err != nil {
		s.fail(w, err, "team ai")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handlePutMyAI saves the caller's own setup (any member: it is only theirs).
func (s *Server) handlePutMyAI(w http.ResponseWriter, r *http.Request) {
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
	t, err := s.loadTeam(r, s.accessOf(r), chi.URLParam(r, "ref"), needRead)
	if err != nil {
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
	tai, err := s.store.GetTeamAI(r.Context(), t.ID)
	if err != nil {
		s.fail(w, err, "team ai")
		return
	}
	if in.HostID != nil && *in.HostID == tai.HostID {
		in.HostID = nil // the team's machine anyway
	}
	if in.HostID != nil && cwd == "" {
		writeErr(w, http.StatusBadRequest, "pick your code folder on that machine")
		return
	}
	p := &store.PersonAI{TeamID: t.ID, UserID: me(r), HostID: in.HostID, Cwd: cwd, Provider: in.Provider, AccountID: in.AccountID,
		Model: strings.TrimSpace(in.Model), Effort: strings.TrimSpace(in.Effort), Mode: strings.TrimSpace(in.Mode)}
	if err := s.store.SavePersonAI(r.Context(), p); err != nil {
		s.fail(w, err, "team ai")
		return
	}
	deployLiveByUser.Delete(me(r))
	out, err := s.myAIView(r.Context(), t.ID, me(r))
	if err != nil {
		s.fail(w, err, "team ai")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetTeamMemory(w http.ResponseWriter, r *http.Request) {
	t, err := s.loadTeam(r, s.accessOf(r), chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	ai, err := s.store.GetTeamAI(r.Context(), t.ID)
	if err != nil {
		s.fail(w, err, "team ai")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"team": t.Key, "memory": ai.Memory})
}

func (s *Server) handlePutTeamMemory(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Memory string `json:"memory"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	t, err := s.loadTeam(r, s.accessOf(r), chi.URLParam(r, "ref"), needAdmin)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	mem := strings.TrimSpace(in.Memory)
	if len(mem) > store.MaxAIText {
		writeErr(w, http.StatusBadRequest, "the memory is longer than 32 KB")
		return
	}
	if err := s.store.SetTeamMemory(r.Context(), t.ID, mem); err != nil {
		s.fail(w, err, "team memory")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"team": t.Key, "memory": mem})
}

// handleRememberTeam appends one fact; any member may (Pocket AI's
// pocket_projects_remember tool calls it, after the person approved).
func (s *Server) handleRememberTeam(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	t, err := s.loadTeam(r, s.accessOf(r), chi.URLParam(r, "ref"), needWrite)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	text := strings.Join(strings.Fields(in.Text), " ")
	if text == "" {
		writeErr(w, http.StatusBadRequest, "write the fact to remember")
		return
	}
	if len(text) > 600 {
		writeErr(w, http.StatusBadRequest, "keep one fact under 600 characters")
		return
	}
	u := userFrom(r.Context())
	line := fmt.Sprintf("- %s (%s, by %s)", text, s.today(r.Context(), u.ID), firstNonEmpty(u.Name, u.Email))
	mem, err := s.store.AppendTeamMemory(r.Context(), t.ID, line)
	if errors.Is(err, store.ErrMemoryFull) {
		writeErr(w, http.StatusBadRequest, "the team's AI memory is full (32 KB); tidy it in Team settings → Pocket AI first")
		return
	}
	if err != nil {
		s.fail(w, err, "team memory")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"team": t.Key, "saved": line, "memory": mem})
}

func (s *Server) handleGetProjectAI(w http.ResponseWriter, r *http.Request) {
	p, err := s.loadProject(r, s.accessOf(r), idParam(r, "id"), needRead)
	if err != nil {
		s.fail(w, err, "project")
		return
	}
	// host_id: the machine of the project's (first) team, whose folders the picker lists
	host := int64(0)
	for _, id := range p.TeamIDs {
		if tai, err := s.store.GetTeamAI(r.Context(), id); err == nil && tai.HostID != 0 {
			host = tai.HostID
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"cwd": s.store.ProjectAICwd(r.Context(), p.ID), "host_id": host})
}

func (s *Server) handlePutProjectAI(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Cwd string `json:"cwd"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	p, err := s.loadProject(r, s.accessOf(r), idParam(r, "id"), needWrite)
	if err != nil {
		s.fail(w, err, "project")
		return
	}
	cwd, err := cleanCwd(in.Cwd)
	if err != nil {
		s.fail(w, err, "project ai")
		return
	}
	if err := s.store.SetProjectAICwd(r.Context(), p.ID, cwd); err != nil {
		s.fail(w, err, "project ai")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"cwd": cwd})
}

// ---------- conversations of an issue ----------

type codeThread struct {
	DraftID  string `json:"draft_id"`
	ThreadID string `json:"thread_id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Action   string `json:"action"` // Investigate, Fix it, Chat (from the issue's attachment)
	// PullRequests the conversation opened (reported by Pocket AI)
	PullRequests []string `json:"pull_requests"`
	URL          string   `json:"url"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
	// Worktree: the branch of the git worktree it works in ("" = the code folder itself)
	Worktree   string `json:"worktree,omitempty"`
	WorktreeID int64  `json:"worktree_id,omitempty"`
}

// codeWorktree is a git worktree of the issue in Pocket AI: one its
// conversations work in, or a branch named after it.
type codeWorktree struct {
	ID        int64    `json:"id"`
	Branch    string   `json:"branch"`
	Base      string   `json:"base"`
	Repo      string   `json:"repo"`
	Host      string   `json:"host,omitempty"`
	Status    string   `json:"status"` // active | archived
	Setup     string   `json:"setup"`
	MergedAt  string   `json:"merged_at"`
	PRURL     string   `json:"pr_url"`
	CreatedAt string   `json:"created_at"`
	Threads   []string `json:"thread_ids"`
	Live      bool     `json:"live"`
	Ahead     int      `json:"ahead"`
	Dirty     int      `json:"dirty"`
	Unpushed  int      `json:"unpushed"`
	Merged    bool     `json:"merged"`
	PRState   string   `json:"pr_state,omitempty"`
	URL       string   `json:"url"`
}

func (s *Server) handleIssueCode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	out := map[string]any{"threads": []codeThread{}, "worktrees": []codeWorktree{}, "connected": false, "detail": ""}
	sb, err := s.aiSibling(ctx)
	if err != nil {
		out["detail"] = err.Error()
		writeJSON(w, http.StatusOK, out)
		return
	}
	var list []codeThread
	q := url.Values{"source": {"projects"}, "ref": {is.Identifier}}
	if err := sb.call(ctx, http.MethodGet, "/api/v1/link/threads?"+q.Encode(), nil, &list); err != nil {
		out["detail"] = "Pocket AI: " + err.Error()
		writeJSON(w, http.StatusOK, out)
		return
	}
	if list == nil {
		list = []codeThread{}
	}
	// keep the person's own records' state and address in step
	for i, t := range list {
		th, err := s.store.FindThread(ctx, is.ID, me(r), t.DraftID)
		if err != nil {
			continue
		}
		list[i].Action = th.Action
		if th.State != t.Status || (t.URL != "" && th.URL != t.URL) {
			th.State = t.Status
			if t.URL != "" {
				th.URL = t.URL
			}
			_ = s.store.SaveThread(ctx, th)
		}
	}
	// PRs the agent opened are attached right away (people who may edit the issue)
	if _, err := s.loadIssue(r, a, chi.URLParam(r, "ref"), needWrite); err == nil {
		for _, t := range list {
			for _, u := range t.PullRequests {
				s.adoptPR(ctx, userFrom(ctx), is.Identifier, u)
			}
		}
	}
	// the issue's worktrees (an older Pocket AI has no such list: none shown)
	wts := []codeWorktree{}
	wq := url.Values{"source": {"projects"}, "ref": {is.Identifier}, "branch": {s.branchName(ctx, me(r), is)}}
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

// codeFile is a file attached to the issue; it goes to Pocket AI with the
// draft, so the agent sees the screenshots and reads the documents.
type codeFile struct {
	Name, Mime string
	Att        store.Attachment // read through openAttachment: disk or Pocket Files
	Size       int64
	Sent       bool // within the limits below
}

// What goes along: Pocket AI takes 8 files per draft and 20 MB per file.
const (
	codeMaxFiles     = 8
	codeMaxFileBytes = 20 << 20
	codeMaxAllBytes  = 60 << 20
)

var codeActions = map[string]string{"investigate": "Investigate", "fix": "Fix it", "chat": "Chat"}

// codeIssue is everything the prompt says about an issue.
type codeIssue struct {
	Identifier, Title, URL, Status, Priority, Project, Description, Branch string
	Labels                                                                 []string
	SubIssues                                                              []string
	Comments                                                               []string // "Name (date): body"
	Git                                                                    []string // "commit abc123 on branch x: title"
	Links                                                                  []string // other attachments: "Title — URL"
	SharedNotes                                                            []store.IssueDoc
	Remote                                                                 string // the connected instance it lives on ("" = this one)
	RemoteID                                                               int64  // the person's connection to it: the tools' workspace
	NotesFolder                                                            string // the team's folder in Pocket Notes
	Files                                                                  []codeFile
	Team, Cwd                                                              string
	ID                                                                     int64
}

func (s *Server) handleStartCode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		Action string `json:"action"`
		Note   string `json:"note"`
		// Worktree: work in a git worktree of the issue's own (fix, chat):
		// Pocket AI makes it on send and reuses it for the issue's next ones.
		Worktree bool `json:"worktree"`
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
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	team, err := s.store.GetTeam(ctx, is.TeamID)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	tai, own, err := s.aiFor(ctx, team.ID, me(r))
	if err != nil {
		s.fail(w, err, "team ai")
		return
	}
	cwd := s.codeCwd(ctx, tai, own, is.ProjectID)
	if cwd == "" {
		writeErr(w, http.StatusBadRequest, "Set the code folder in Team settings → Pocket AI (the team's, or your own)")
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
	ci := s.codeIssueOf(r, a, is, team.Name, cwd)
	if in.Worktree && !tai.LinkRefs {
		ci.Branch = plainBranch(ci.Branch, is.Identifier)
	}
	title, text, system := buildCodePrompt(in.Action, in.Note, ci, tai, in.Worktree)
	body := map[string]any{"cwd": cwd, "title": title, "text": text, "system": system, "source": "projects",
		"ref": is.Identifier, "ref_url": ci.URL, "ref_label": is.Identifier + " " + is.Title}
	if in.Worktree {
		body["worktree"] = map[string]any{"name": ci.Branch}
	}
	// the issue's files: screenshots the agent looks at, documents it reads
	files := []map[string]any{}
	for _, f := range ci.Files {
		if !f.Sent {
			continue
		}
		data, err := s.readAttachment(ctx, &f.Att)
		if err != nil {
			s.log.Warn("code: attachment unreadable", "issue", is.Identifier, "file", f.Name, "err", err)
			continue
		}
		files = append(files, map[string]any{"name": f.Name, "data": data})
	}
	if len(files) > 0 {
		body["files"] = files
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
		ID    string `json:"id"`
		URL   string `json:"url"`
		Files []struct {
			Name  string `json:"name"`
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		} `json:"files"`
	}
	if err := sb.call(ctx, http.MethodPost, "/api/v1/link/drafts", body, &draft); err != nil {
		writeErr(w, http.StatusBadGateway, "Pocket AI: "+err.Error())
		return
	}
	if draft.URL != "" && !strings.HasPrefix(draft.URL, "http") {
		draft.URL = sb.public + draft.URL
	}
	// the person's own record: not on the issue's attachments or activity, nobody else sees it
	label := codeActions[in.Action]
	if in.Worktree {
		label += " in a worktree"
	}
	_ = s.store.SaveThread(ctx, &store.IssueThread{IssueID: is.ID, UserID: me(r), DraftID: draft.ID, Action: label, Cwd: shortCwd(cwd), URL: draft.URL, State: "draft"})
	sent, skipped := 0, []string{} // skipped: files Pocket AI could not read (an archive, a scanned PDF…) or too large
	for _, f := range draft.Files {
		if f.OK {
			sent++
		} else {
			skipped = append(skipped, f.Name)
		}
	}
	for _, f := range ci.Files {
		if !f.Sent {
			skipped = append(skipped, f.Name)
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"url": draft.URL, "draft_id": draft.ID, "send": tai.SendNow, "files": sent, "skipped": skipped})
}

func shortCwd(p string) string {
	if i := strings.LastIndex(strings.TrimSuffix(p, "/"), "/"); i >= 0 && i < len(p)-1 {
		return p[i+1:]
	}
	return p
}

// codeIssueOf gathers the issue's context the person can see.
func (s *Server) codeIssueOf(r *http.Request, a *access, is *store.Issue, team, cwd string) codeIssue {
	ctx := r.Context()
	ci := codeIssue{ID: is.ID, Identifier: is.Identifier, Title: is.Title, URL: s.issueURL(r, is.Identifier), Priority: priorityName(is.Priority),
		Description: is.Description, Branch: s.branchName(ctx, a.uid, is), Team: team, Cwd: cwd, NotesFolder: s.teamNotesFolder(ctx, is.TeamID)}
	if st, err := s.store.GetState(ctx, is.StateID); err == nil {
		ci.Status = st.Name
	}
	if is.ProjectID != nil {
		if p, err := s.store.GetProject(ctx, *is.ProjectID); err == nil {
			ci.Project = p.Name
		}
	}
	if len(is.LabelIDs) > 0 {
		labels, _ := s.store.TeamLabels(ctx, is.TeamID)
		names := map[int64]string{}
		for _, l := range labels {
			names[l.ID] = l.Name
		}
		for _, id := range is.LabelIDs {
			if n := names[id]; n != "" {
				ci.Labels = append(ci.Labels, n)
			}
		}
	}
	subs, _, _ := s.store.ListIssues(ctx, store.IssueFilter{ParentID: is.ID, AllowedTeamIDs: a.ids(), ViewerID: a.uid, Archived: "include", Sort: "manual"})
	for _, sub := range subs {
		state := sub.StateType
		if st, err := s.store.GetState(ctx, sub.StateID); err == nil {
			state = st.Name
		}
		ci.SubIssues = append(ci.SubIssues, fmt.Sprintf("%s %s (%s)", sub.Identifier, sub.Title, state))
	}
	comments, _ := s.store.Comments(ctx, is.ID, 0)
	if len(comments) > 10 {
		comments = comments[len(comments)-10:]
	}
	names := map[int64]string{}
	for _, c := range comments {
		who := firstNonEmpty(c.SourceApp, "someone")
		if c.UserID != nil {
			if n, ok := names[*c.UserID]; ok {
				who = n
			} else if u, err := s.store.GetUser(ctx, *c.UserID); err == nil {
				who = firstNonEmpty(u.Name, u.Email)
				names[*c.UserID] = who
			}
		}
		ci.Comments = append(ci.Comments, fmt.Sprintf("%s (%s): %s", who, strings.SplitN(c.CreatedAt, "T", 2)[0], clip(strings.TrimSpace(c.Body), 3000)))
	}
	ci.SharedNotes, _ = s.store.IssueDocs(ctx, is.ID, true)
	atts, _ := s.store.Attachments(ctx, is.ID, 0)
	total := int64(0)
	for _, att := range atts {
		switch att.Kind {
		case "file":
			f := codeFile{Name: firstNonEmpty(att.FileName, att.Title), Mime: att.Mime, Att: att, Size: att.Size}
			if len(ci.Files) < codeMaxFiles && f.Size <= codeMaxFileBytes && total+f.Size <= codeMaxAllBytes {
				f.Sent, total = true, total+f.Size
			}
			ci.Files = append(ci.Files, f)
			continue
		case "commit", "pull_request":
		default:
			if att.URL != "" {
				ci.Links = append(ci.Links, strings.TrimSpace(firstNonEmpty(att.Title, att.Kind)+" — "+att.URL))
			}
			continue
		}
		var meta struct {
			Branch string `json:"branch"`
			Repo   string `json:"repo"`
		}
		_ = json.Unmarshal(att.Meta, &meta)
		line := strings.ReplaceAll(att.Kind, "_", " ") + ": " + att.Title
		if att.Subtitle != "" {
			line += " — " + att.Subtitle
		}
		if meta.Branch != "" {
			line += " (branch " + meta.Branch + ")"
		}
		if att.State != "" {
			line += " [" + att.State + "]"
		}
		ci.Git = append(ci.Git, line)
	}
	return ci
}

var codeAsk = map[string]string{
	"investigate": `Investigate this issue in the code. Find the code that is relevant to it, explain how it works today and what causes the problem (or what is missing), and propose a concrete plan with the files to change. Do not edit any files yet.
When you are done, offer to post a short summary as a comment on %[1]s.`,
	"fix": `Fix this issue. First switch to a branch for it (create it if needed): %[2]s
Implement the change, run the project's tests or build, and commit. Do not push unless I ask.
Finish with a short summary of what changed and offer to post it as a comment on %[1]s.`,
	// fix, when the team keeps Pocket Projects out of the repository
	"fix-plain": `Fix this issue. First switch to a new branch with a short descriptive name.
Implement the change, run the project's tests or build, and commit. Do not push unless I ask.
Finish with a short summary of what changed and offer to post it as a comment on %[1]s.`,
	"chat": `Let's talk about this issue. Read the relevant code when it helps, but do not change anything unless I ask.`,
	// in a worktree Pocket AI made for the issue: already on its branch
	"fix-worktree": `Fix this issue. You are in a git worktree of its own, on the branch %[2]s: stay on it, do not switch branches.
Implement the change, run the project's tests or build, and commit. Do not push unless I ask.
Finish with a short summary of what changed and offer to post it as a comment on %[1]s.`,
	"chat-worktree": `Let's talk about this issue. You are in a git worktree of its own, on the branch %[2]s. Read the relevant code when it helps, but do not change anything unless I ask.`,
}

// plainBranch is the issue's branch without its identifier, for teams that
// keep Pocket Projects out of the repository: "me/as-12-login" → "me/login".
func plainBranch(branch, ident string) string {
	if p := strings.Replace(branch, strings.ToLower(ident)+"-", "", 1); p != branch && !strings.HasSuffix(p, "/") && p != "" {
		return p
	}
	return strings.Replace(branch, strings.ToLower(ident), "fix", 1)
}

// buildCodePrompt returns the conversation title, the visible first message
// and the hidden system context.
func buildCodePrompt(action, note string, ci codeIssue, tai *store.TeamAI, worktree bool) (title, text, system string) {
	title = ci.Identifier + " · " + ci.Title
	if r := []rune(title); len(r) > 80 {
		title = string(r[:79]) + "…"
	}
	var b strings.Builder
	ask := codeAsk[action]
	if worktree && codeAsk[action+"-worktree"] != "" {
		ask = codeAsk[action+"-worktree"]
	} else if action == "fix" && !tai.LinkRefs {
		ask = codeAsk["fix-plain"]
	}
	fmt.Fprintf(&b, ask, ci.Identifier, ci.Branch)
	b.WriteString("\n\n")
	if n := strings.TrimSpace(note); n != "" {
		b.WriteString(n + "\n\n")
	}
	fmt.Fprintf(&b, "## %s: %s\n%s\n\n", ci.Identifier, ci.Title, ci.URL)
	meta := []string{}
	for _, kv := range [][2]string{{"Status", ci.Status}, {"Priority", ci.Priority}, {"Project", ci.Project}, {"Labels", strings.Join(ci.Labels, ", ")}} {
		if kv[1] != "" {
			meta = append(meta, kv[0]+": "+kv[1])
		}
	}
	b.WriteString(strings.Join(meta, " · ") + "\n\n")
	if d := strings.TrimSpace(ci.Description); d != "" {
		b.WriteString("### Description\n" + clip(d, 20000) + "\n\n")
	}
	section := func(h string, lines []string) {
		if len(lines) == 0 {
			return
		}
		b.WriteString("### " + h + "\n")
		for _, l := range lines {
			b.WriteString("- " + l + "\n")
		}
		b.WriteString("\n")
	}
	section("Sub-issues", ci.SubIssues)
	section("Linked commits and pull requests", ci.Git)
	var files, left []string
	for _, f := range ci.Files {
		if f.Sent {
			files = append(files, f.Name)
		} else {
			left = append(left, f.Name+" (too large to send; open it on the issue)")
		}
	}
	section("Attached files (sent with this message)", files)
	section("Attached files not sent", left)
	section("Links", ci.Links)
	section("Latest comments", ci.Comments)
	if len(ci.SharedNotes) > 0 {
		b.WriteString("### Shared notes (the team's)\n")
		for _, n := range ci.SharedNotes {
			b.WriteString("#### " + n.Title + "\n" + clip(strings.TrimSpace(n.Body), 6000) + "\n\n")
		}
	}
	text = strings.TrimSpace(b.String())

	var sys strings.Builder
	fmt.Fprintf(&sys, "This conversation is about issue %s (%q) in Pocket Projects (%s), team %s, working in %s.\n", ci.Identifier, ci.Title, ci.URL, ci.Team, ci.Cwd)
	// a connected instance's ticket: the same tools, with the person's connection as workspace
	ws := ""
	if ci.Remote != "" {
		ws = fmt.Sprintf(", workspace %d", ci.RemoteID)
		fmt.Fprintf(&sys, "The ticket lives on another Pocket Projects instance (%s) that the person connected as workspace %d. Pass workspace %d to every pocket_projects_* tool for this ticket and its team (without it they reach the person's own instance, where the ids mean other things); changes are made there as the person.\n", ci.Remote, ci.RemoteID, ci.RemoteID)
	}
	fmt.Fprintf(&sys, "Read or update the ticket with the Pocket Projects tools when they are available: pocket_projects_get_issue, pocket_projects_comment and pocket_projects_update_issue with id %d%s; writes wait for the person's approval. ", ci.ID, ws)
	sys.WriteString("When you learn something durable about this team's code, conventions or environment that would help future conversations, save it with pocket_projects_remember (team " + teamKeyOf(ci.Identifier) + ws + "): one short factual line, never secrets or credentials.\n")
	link := "[" + strings.NewReplacer("[", "(", "]", ")").Replace(ci.Identifier+" · "+ci.Title) + "](" + ci.URL + ")"
	fmt.Fprintf(&sys, "\n## Notes\nWhen the person asks to put a plan, write-up or findings in a note (Pocket Notes), create it with pocket_notes_create (category %[2]q, the team's folder) and put this link in its content: %[1]s. Every note that links to the issue is listed under Notes on the issue page, so that is all linking takes. To link an existing note, add the same link to it with pocket_notes_append.\n", link, firstNonEmpty(ci.NotesFolder, defaultNotesFolder))
	if tai.LinkRefs {
		fmt.Fprintf(&sys, "\n## Commits and pull requests\nPut %[1]s in commit messages so Pocket Projects links them to the issue. End pull request descriptions with \"Resolves %[1]s (%[2]s)\".\n", ci.Identifier, ci.URL)
	} else {
		fmt.Fprintf(&sys, "\n## Commits and pull requests\nKeep Pocket Projects out of the repository: do not mention %s, its URL, Pocket Projects, any internal id or who asked in branch names, commit messages, pull request titles or descriptions, or code comments. Write them for a reviewer who has never seen the ticket.\n", ci.Identifier)
	}
	// About the ticket, not the repository: always, whatever link_refs says
	fmt.Fprintf(&sys, "\n## After you open a pull request\nThen update the ticket with the Pocket Projects tools (search for pocket_projects_ tools if they are not loaded yet), each write small (they wait for the person's approval):\n"+
		"1. Link it: pocket_projects_link_pr with id %[1]d%[3]s and the pull request's url (plus title, branch, repo, number). Pocket Projects also picks up PRs opened in this conversation by itself, so if that tool is missing just carry on; it follows the PR and completes the issue when it is merged.\n"+
		"2. Post one comment with pocket_projects_comment (id %[1]d%[3]s): what changed, how it was tested, and what is NOT done.\n"+
		"3. Turn concrete follow-ups into sub-issues with pocket_projects_create_issue (parent_id %[1]d, team %[2]s%[3]s); for small items append a \"- [ ] …\" checklist to the description with pocket_projects_update_issue instead (if that is refused, put the checklist in the comment).\n", ci.ID, teamKeyOf(ci.Identifier), ws)
	if ci.Remote != "" {
		sys.WriteString("If the Pocket Projects tools are missing or refuse the workspace, give the person the link and that comment to post on the ticket themselves.\n")
	}
	if ins := strings.TrimSpace(tai.Instructions); ins != "" {
		sys.WriteString("\n## Team instructions\n" + ins + "\n")
	}
	if mem := strings.TrimSpace(tai.Memory); mem != "" {
		sys.WriteString("\n## Team memory — durable facts saved earlier\n" + mem + "\n")
	}
	return title, text, strings.TrimSpace(sys.String())
}

func teamKeyOf(ident string) string {
	if i := strings.LastIndex(ident, "-"); i > 0 {
		return ident[:i]
	}
	return ident
}

// handleCodeUnlink takes one of the person's conversations off the issue
// ("Remove from issue"); it stays in Pocket AI.
func (s *Server) handleCodeUnlink(w http.ResponseWriter, r *http.Request) {
	s.codeConversationAction(w, r, http.MethodPost, "/unlink")
}

// handleCodeDelete deletes one of the person's conversations in Pocket AI
// (its agent stops if it is working).
func (s *Server) handleCodeDelete(w http.ResponseWriter, r *http.Request) {
	s.codeConversationAction(w, r, http.MethodDelete, "")
}

func (s *Server) codeConversationAction(w http.ResponseWriter, r *http.Request, method, suffix string) {
	ctx := r.Context()
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	sb, err := s.aiSibling(ctx)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	draft := chi.URLParam(r, "draft")
	// Pocket AI answers only for the person's own conversations from Projects
	if err := sb.call(ctx, method, "/api/v1/link/threads/"+url.PathEscape(draft)+suffix, nil, nil); err != nil {
		writeErr(w, http.StatusBadGateway, "Pocket AI: "+err.Error())
		return
	}
	if th, err := s.store.FindThread(ctx, is.ID, me(r), draft); err == nil {
		_ = s.store.DeleteThread(ctx, th.ID)
	}
	w.WriteHeader(http.StatusNoContent)
}
