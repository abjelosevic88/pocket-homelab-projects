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
	r.Get("/teams/{ref}/ai", s.handleGetTeamAI)
	r.Put("/teams/{ref}/ai", s.handlePutTeamAI)
	r.Get("/teams/{ref}/memory", s.handleGetTeamMemory)
	r.Put("/teams/{ref}/memory", s.handlePutTeamMemory)
	r.Post("/teams/{ref}/memory", s.handleRememberTeam)
	r.Get("/projects/{id}/ai", s.handleGetProjectAI)
	r.Put("/projects/{id}/ai", s.handlePutProjectAI)
	r.Get("/issues/{ref}/code", s.handleIssueCode)
	r.Post("/issues/{ref}/code", s.handleStartCode)
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
		Model        string `json:"model"`
		Effort       string `json:"effort"`
		Mode         string `json:"mode"`
		Instructions string `json:"instructions"`
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
	ai, err := s.store.GetTeamAI(r.Context(), t.ID)
	if err != nil {
		s.fail(w, err, "team ai")
		return
	}
	ai.Cwd, ai.Provider, ai.AccountID, ai.Model, ai.Effort, ai.Mode = cwd, in.Provider, in.AccountID, strings.TrimSpace(in.Model), strings.TrimSpace(in.Effort), strings.TrimSpace(in.Mode)
	ai.Instructions = strings.TrimSpace(in.Instructions)
	if err := s.store.SaveTeamAI(r.Context(), ai); err != nil {
		s.fail(w, err, "team ai")
		return
	}
	ai, _ = s.store.GetTeamAI(r.Context(), t.ID)
	writeJSON(w, http.StatusOK, ai)
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
	writeJSON(w, http.StatusOK, map[string]string{"cwd": s.store.ProjectAICwd(r.Context(), p.ID)})
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
	DraftID   string `json:"draft_id"`
	ThreadID  string `json:"thread_id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	URL       string `json:"url"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func (s *Server) handleIssueCode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	out := map[string]any{"threads": []codeThread{}, "connected": false, "detail": ""}
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
	// keep the attachments' state and address in step
	for _, t := range list {
		if att, err := s.store.FindAttachment(ctx, is.ID, "ai", t.DraftID); err == nil && (att.State != t.Status || (t.URL != "" && att.URL != t.URL)) {
			att.State = t.Status
			if t.URL != "" {
				att.URL = t.URL
			}
			_ = s.store.SaveAttachment(ctx, att)
		}
	}
	out["threads"], out["connected"] = list, true
	writeJSON(w, http.StatusOK, out)
}

var codeActions = map[string]string{"investigate": "Investigate", "fix": "Fix it", "chat": "Chat"}

// codeIssue is everything the prompt says about an issue.
type codeIssue struct {
	Identifier, Title, URL, Status, Priority, Project, Description, Branch string
	Labels                                                                 []string
	SubIssues                                                              []string
	Comments                                                               []string // "Name (date): body"
	Git                                                                    []string // "commit abc123 on branch x: title"
	Team, Cwd                                                              string
}

func (s *Server) handleStartCode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		Action string `json:"action"`
		Note   string `json:"note"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if codeActions[in.Action] == "" {
		writeErr(w, http.StatusBadRequest, "action is investigate, fix or chat")
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
	tai, err := s.store.GetTeamAI(ctx, team.ID)
	if err != nil {
		s.fail(w, err, "team ai")
		return
	}
	cwd := tai.Cwd
	if is.ProjectID != nil {
		if pc := s.store.ProjectAICwd(ctx, *is.ProjectID); pc != "" {
			cwd = pc
		}
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
	title, text, system := buildCodePrompt(in.Action, in.Note, ci, tai)
	body := map[string]any{"cwd": cwd, "title": title, "text": text, "system": system, "source": "projects",
		"ref": is.Identifier, "ref_url": ci.URL, "ref_label": is.Identifier + " " + is.Title}
	for k, v := range map[string]string{"provider": tai.Provider, "model": tai.Model, "effort": tai.Effort, "mode": mode} {
		if v != "" {
			body[k] = v
		}
	}
	if tai.AccountID != nil {
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
	if a.check(is.TeamID, needWrite) == nil {
		uid := me(r)
		att := &store.Attachment{IssueID: &is.ID, Kind: "ai_thread", Title: "Pocket AI · " + codeActions[in.Action], Subtitle: shortCwd(cwd),
			URL: draft.URL, ExtApp: "ai", ExtID: draft.ID, State: "draft", CreatedBy: &uid}
		if err := s.store.SaveAttachment(ctx, att); err == nil {
			_ = s.store.AddHistory(ctx, store.History{IssueID: &is.ID, ActorID: &uid, Field: "ai_thread", ToValue: in.Action, ToLabel: att.Title})
			s.send(ctx, change{Kind: "issue", TeamIDs: []int64{is.TeamID}, IDs: []int64{is.ID}})
		}
	}
	writeJSON(w, http.StatusCreated, map[string]string{"url": draft.URL, "draft_id": draft.ID})
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
	ci := codeIssue{Identifier: is.Identifier, Title: is.Title, URL: s.issueURL(r, is.Identifier), Priority: priorityName(is.Priority),
		Description: is.Description, Branch: s.branchName(ctx, a.uid, is), Team: team, Cwd: cwd}
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
	atts, _ := s.store.Attachments(ctx, is.ID, 0)
	for _, att := range atts {
		if att.Kind != "commit" && att.Kind != "pull_request" {
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
Implement the change, run the project's tests or build, and commit with %[1]s in the commit message so Pocket Projects links the commit to the issue. Do not push unless I ask.
Finish with a short summary of what changed and offer to post it as a comment on %[1]s.`,
	"chat": `Let's talk about this issue. Read the relevant code when it helps, but do not change anything unless I ask.`,
}

// buildCodePrompt returns the conversation title, the visible first message
// and the hidden system context.
func buildCodePrompt(action, note string, ci codeIssue, tai *store.TeamAI) (title, text, system string) {
	title = ci.Identifier + " · " + ci.Title
	if r := []rune(title); len(r) > 80 {
		title = string(r[:79]) + "…"
	}
	var b strings.Builder
	fmt.Fprintf(&b, codeAsk[action], ci.Identifier, ci.Branch)
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
	section("Latest comments", ci.Comments)
	text = strings.TrimSpace(b.String())

	var sys strings.Builder
	fmt.Fprintf(&sys, "This conversation is about issue %s (%q) in Pocket Projects (%s), team %s, working in %s.\n", ci.Identifier, ci.Title, ci.URL, ci.Team, ci.Cwd)
	sys.WriteString("Read or update the ticket with the Pocket Projects tools when they are available: pocket_projects_get_issue (use the identifier), pocket_projects_comment and pocket_projects_update_issue; writes wait for the person's approval. ")
	sys.WriteString("When you learn something durable about this team's code, conventions or environment that would help future conversations, save it with pocket_projects_remember (team " + teamKeyOf(ci.Identifier) + "): one short factual line, never secrets or credentials.\n")
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
