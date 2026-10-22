package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// Pocket AI writes, splits and summarises through the person's Core link
// (inference:write). Nothing is sent anywhere else; without the link the
// buttons say how to switch it on.

func (s *Server) aiRoutes(r chi.Router) {
	r.Get("/ai/status", s.handleAIStatus)
	r.Get("/ai/models", s.handleAIModels)
	r.Post("/ai/describe", s.handleAIDescribe)
	r.Post("/ai/breakdown", s.handleAIBreakdown)
	r.Post("/ai/summarize", s.handleAISummarize)
	r.Post("/ai/project-update", s.handleAIProjectUpdate)
	r.Post("/ai/triage", s.handleAITriage)
	r.Post("/ai/extract", s.handleAIExtract)
	r.Post("/ai/rewrite", s.handleAIRewrite)
}

const aiSystem = `You are the assistant inside Pocket Projects, a Linear-style project tracker used by a household or a small team.
Write plainly and concretely, in the language of the input. Never invent facts, names, dates or numbers that are not in the input.
Use Markdown sparingly: short paragraphs, "- " bullet lists, "- [ ] " checklists where useful. No headings above level 3.`

func (s *Server) aiClient(ctx context.Context, uid int64) (*core.InferenceClient, core.AIStatus) {
	email := s.userEmail(ctx, uid)
	c, st := s.core.SelectAI(ctx, email, "pocket", false)
	if c != nil && st.Ready {
		if m := s.store.UserSetting(ctx, uid, "ai_model"); m != "" && core.ValidInferenceModel(m) {
			c = c.WithModel(m)
		}
	}
	return c, st
}

func (s *Server) handleAIStatus(w http.ResponseWriter, r *http.Request) {
	_, st := s.aiClient(r.Context(), me(r))
	writeJSON(w, http.StatusOK, map[string]any{"ready": st.Ready, "detail": st.Detail, "model": s.store.UserSetting(r.Context(), me(r), "ai_model")})
}

func (s *Server) handleAIModels(w http.ResponseWriter, r *http.Request) {
	c, st := s.aiClient(r.Context(), me(r))
	if c == nil || !st.Ready {
		writeErr(w, http.StatusServiceUnavailable, st.Detail)
		return
	}
	models, err := c.Models(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models)
}

// ask sends one prompt; with jsonOut the answer is decoded into out.
func (s *Server) ask(w http.ResponseWriter, r *http.Request, prompt string, out any) (string, bool) {
	c, st := s.aiClient(r.Context(), me(r))
	if c == nil || !st.Ready {
		writeErr(w, http.StatusServiceUnavailable, "Pocket AI is not available: "+st.Detail)
		return "", false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	reply, err := c.Complete(ctx, aiSystem, prompt, out != nil)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return "", false
	}
	text := strings.TrimSpace(reply.Text)
	if out != nil {
		if err := json.Unmarshal([]byte(jsonPart(text)), out); err != nil {
			writeErr(w, http.StatusBadGateway, "Pocket AI answered something that is not the JSON asked for; try again")
			return "", false
		}
	}
	return text, true
}

// jsonPart cuts the JSON object out of an answer that may be fenced or chatty.
func jsonPart(t string) string {
	t = strings.TrimSpace(t)
	t = strings.TrimPrefix(t, "```json")
	t = strings.TrimPrefix(t, "```")
	t = strings.TrimSuffix(t, "```")
	if i, j := strings.Index(t, "{"), strings.LastIndex(t, "}"); i >= 0 && j > i {
		return t[i : j+1]
	}
	return t
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n…(cut)"
}

func (s *Server) handleAIDescribe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		TeamID      int64  `json:"team_id"`
		ProjectID   int64  `json:"project_id"`
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Title) == "" {
		writeErr(w, http.StatusBadRequest, "give the issue a title first")
		return
	}
	a := s.accessOf(r)
	ctxText := ""
	if in.TeamID > 0 && a.teams[in.TeamID] != "" {
		if t, err := s.store.GetTeam(r.Context(), in.TeamID); err == nil {
			ctxText += "Team: " + t.Name + ". " + t.Description + "\n"
		}
	}
	if in.ProjectID > 0 && s.visibleProject(r.Context(), a, in.ProjectID) {
		if p, err := s.store.GetProject(r.Context(), in.ProjectID); err == nil {
			ctxText += "Project: " + p.Name + " — " + p.Summary + "\n"
		}
	}
	prompt := fmt.Sprintf(`%sWrite the description of this issue: a two-sentence summary of the goal, then "**Acceptance criteria**" as a short checklist of 3–6 verifiable items. Keep it under 150 words.
If notes are given, keep every fact from them.

Title: %s
Notes: %s`, ctxText, in.Title, clip(in.Description, 4000))
	text, ok := s.ask(w, r, prompt, nil)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"description": text})
}

func (s *Server) handleAIBreakdown(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Issue string `json:"issue"`
		Count int    `json:"count"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, in.Issue, needRead)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	n := in.Count
	if n <= 0 || n > 12 {
		n = 5
	}
	var existing []string
	subs, _, _ := s.store.ListIssues(r.Context(), store.IssueFilter{ParentID: is.ID, AllowedTeamIDs: a.ids(), ViewerID: a.uid})
	for _, x := range subs {
		existing = append(existing, "- "+x.Title)
	}
	prompt := fmt.Sprintf(`Split this issue into at most %d sub-issues that together complete it. Each is one concrete, independently finishable piece of work, ordered as they should be done.
Skip anything already covered by the existing sub-issues.
Answer only JSON: {"sub_issues": [{"title": "short imperative title", "description": "one or two sentences"}]}

Issue %s: %s
Description:
%s

Existing sub-issues:
%s`, n, is.Identifier, is.Title, clip(is.Description, 6000), strings.Join(existing, "\n"))
	var out struct {
		SubIssues []struct {
			Title       string `json:"title"`
			Description string `json:"description"`
		} `json:"sub_issues"`
	}
	if _, ok := s.ask(w, r, prompt, &out); !ok {
		return
	}
	if len(out.SubIssues) > n {
		out.SubIssues = out.SubIssues[:n]
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleAISummarize(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Issue string `json:"issue"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ctx := r.Context()
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, in.Issue, needRead)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	comments, _ := s.store.Comments(ctx, is.ID, 0)
	var ids []int64
	for _, c := range comments {
		if c.UserID != nil {
			ids = append(ids, *c.UserID)
		}
	}
	people, _ := s.store.PeopleByID(ctx, ids)
	var b strings.Builder
	for _, c := range comments {
		who := "someone"
		if c.UserID != nil {
			who = firstNonEmpty(people[*c.UserID].Name, people[*c.UserID].Email)
		}
		fmt.Fprintf(&b, "[%s] %s: %s\n\n", c.CreatedAt[:10], who, c.Body)
	}
	prompt := fmt.Sprintf(`Summarise this issue and its discussion for someone who has not read it: where it stands, what was decided, what is still open (with who owns it if said), in at most 6 bullets.

Issue %s: %s
Description:
%s

Comments (oldest first):
%s`, is.Identifier, is.Title, clip(is.Description, 4000), clip(b.String(), 12000))
	text, ok := s.ask(w, r, prompt, nil)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"summary": text})
}

func (s *Server) handleAIProjectUpdate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ProjectID int64 `json:"project_id"`
		Days      int   `json:"days"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ctx := r.Context()
	a := s.accessOf(r)
	p, err := s.loadProject(r, a, in.ProjectID, needRead)
	if err != nil {
		s.fail(w, err, "project")
		return
	}
	days := in.Days
	if days <= 0 || days > 60 {
		days = max(p.UpdateCadenceDays, 7)
	}
	since := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339)
	all, _, _ := s.store.ListIssues(ctx, store.IssueFilter{AllowedTeamIDs: a.ids(), ViewerID: a.uid, ProjectIDs: []int64{p.ID}, Archived: "include", Limit: 500, Snoozed: "include"})
	var done, started, added, blocked, overdue []string
	today := s.today(ctx, a.uid)
	for _, is := range all {
		line := is.Identifier + " " + is.Title
		switch {
		case is.CompletedAt != nil && *is.CompletedAt >= since:
			done = append(done, line)
		case is.StateType == "started":
			started = append(started, line)
		}
		if is.CreatedAt >= since {
			added = append(added, line)
		}
		if is.BlockedBy > 0 && is.StateType != "completed" && is.StateType != "canceled" {
			blocked = append(blocked, line)
		}
		if is.DueDate != "" && is.DueDate < today && is.StateType != "completed" && is.StateType != "canceled" {
			overdue = append(overdue, line+" (due "+is.DueDate+")")
		}
	}
	var ms []string
	for _, m := range p.Milestones {
		ms = append(ms, fmt.Sprintf("%s — target %s — %d/%d done", m.Name, firstNonEmpty(m.TargetDate, "none"), m.Progress.Completed, m.Progress.Total))
	}
	last := "none"
	if p.LastUpdate != nil {
		last = p.LastUpdate.Body
	}
	list := func(v []string) string {
		if len(v) == 0 {
			return "(none)"
		}
		if len(v) > 40 {
			v = v[:40]
		}
		return "- " + strings.Join(v, "\n- ")
	}
	prompt := fmt.Sprintf(`Draft this week's status update for the project below, written by its lead to the team.
Structure: one opening sentence on overall progress; "**Done**", "**In progress**", "**Risks & blockers**" and "**Next**" as short bullet lists (omit a section that would be empty). Under 220 words.
Then judge the health honestly from the facts: "on_track", "at_risk" or "off_track" (at risk when overdue/blocked work or the target date is threatened).
Answer only JSON: {"health": "on_track|at_risk|off_track", "body": "the Markdown update"}

Project: %s (status %s, target %s, %d of %d issues done, %.0f%%)
Summary: %s
Milestones:
%s
Completed in the last %d days:
%s
In progress:
%s
New in the last %d days:
%s
Blocked:
%s
Overdue:
%s
Previous update:
%s`, p.Name, p.Status, firstNonEmpty(p.TargetDate, "not set"), p.Progress.Completed, p.Progress.Total, p.Progress.Percent, p.Summary,
		list(ms), days, list(done), list(started), days, list(added), list(blocked), list(overdue), clip(last, 2000))
	var out struct {
		Health string `json:"health"`
		Body   string `json:"body"`
	}
	if _, ok := s.ask(w, r, prompt, &out); !ok {
		return
	}
	if !store.ValidHealth(out.Health) || out.Health == "" {
		out.Health = "on_track"
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleAITriage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Issue string `json:"issue"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ctx := r.Context()
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, in.Issue, needRead)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	labels, _ := s.store.TeamLabels(ctx, is.TeamID)
	members, _ := s.store.TeamMembers(ctx, is.TeamID)
	projects, _ := s.store.ListProjects(ctx, store.ProjectFilter{AllowedTeamIDs: []int64{is.TeamID}, Statuses: []string{"planned", "started", "paused", "backlog"}})
	var lb, mb, pb []string
	for _, l := range labels {
		if !l.IsGroup {
			lb = append(lb, fmt.Sprintf("%d=%s", l.ID, l.Name))
		}
	}
	for _, m := range members {
		mb = append(mb, fmt.Sprintf("%d=%s", m.ID, firstNonEmpty(m.Name, m.Email)))
	}
	for _, p := range projects {
		pb = append(pb, fmt.Sprintf("%d=%s (%s)", p.ID, p.Name, p.Summary))
	}
	recent, _, _ := s.store.ListIssues(ctx, store.IssueFilter{AllowedTeamIDs: []int64{is.TeamID}, ViewerID: a.uid, Sort: "updated", Limit: 25})
	var ex []string
	for _, x := range recent {
		if x.ID == is.ID {
			continue
		}
		var names []string
		for _, lid := range x.LabelIDs {
			for _, l := range labels {
				if l.ID == lid {
					names = append(names, l.Name)
				}
			}
		}
		ex = append(ex, fmt.Sprintf("%s | priority %d | labels %s | assignee %v", x.Title, x.Priority, strings.Join(names, ","), idStr(x.AssigneeID)))
	}
	prompt := fmt.Sprintf(`Suggest how to file this new issue, using only the ids listed. Priority: 1 urgent, 2 high, 3 medium, 4 low, 0 none.
Answer only JSON: {"priority": 0, "label_ids": [], "project_id": null, "assignee_id": null, "reason": "one sentence"}

Issue: %s
%s

Labels (id=name): %s
People (id=name): %s
Projects (id=name): %s
Recent issues of the team, for how things are usually filed:
%s`, is.Title, clip(is.Description, 3000), strings.Join(lb, "; "), strings.Join(mb, "; "), strings.Join(pb, "; "), strings.Join(ex, "\n"))
	var out struct {
		Priority   int     `json:"priority"`
		LabelIDs   []int64 `json:"label_ids"`
		ProjectID  *int64  `json:"project_id"`
		AssigneeID *int64  `json:"assignee_id"`
		Reason     string  `json:"reason"`
	}
	if _, ok := s.ask(w, r, prompt, &out); !ok {
		return
	}
	// Keep only ids that exist.
	clean, _ := s.cleanLabels(ctx, is.TeamID, out.LabelIDs)
	out.LabelIDs = clean
	if out.Priority < 0 || out.Priority > 4 {
		out.Priority = 0
	}
	if out.AssigneeID != nil && s.store.TeamRole(ctx, is.TeamID, *out.AssigneeID) == "" {
		out.AssigneeID = nil
	}
	if out.ProjectID != nil && !s.visibleProject(ctx, a, *out.ProjectID) {
		out.ProjectID = nil
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleAIExtract(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text   string `json:"text"`
		TeamID int64  `json:"team_id"`
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Text) == "" {
		writeErr(w, http.StatusBadRequest, "paste some notes first")
		return
	}
	a := s.accessOf(r)
	people := ""
	if in.TeamID > 0 && a.teams[in.TeamID] != "" {
		members, _ := s.store.TeamMembers(r.Context(), in.TeamID)
		var mb []string
		for _, m := range members {
			mb = append(mb, fmt.Sprintf("%d=%s", m.ID, firstNonEmpty(m.Name, m.Email)))
		}
		people = strings.Join(mb, "; ")
	}
	prompt := fmt.Sprintf(`Turn these notes (meeting notes, an email, a brain dump) into separate actionable issues. Only real to-dos; merge duplicates; at most 20.
Today is %s. Convert relative dates ("next Friday") into YYYY-MM-DD; leave due_date empty when none is said.
Priority: 1 urgent, 2 high, 3 medium, 4 low, 0 not said. assignee_id only when the notes clearly name one of these people (id=name): %s
Answer only JSON: {"issues": [{"title": "short imperative title", "description": "context from the notes", "priority": 0, "due_date": "", "assignee_id": null}]}

Notes:
%s`, s.today(r.Context(), a.uid), firstNonEmpty(people, "(none)"), clip(in.Text, 16000))
	var out struct {
		Issues []struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			Priority    int    `json:"priority"`
			DueDate     string `json:"due_date"`
			AssigneeID  *int64 `json:"assignee_id"`
		} `json:"issues"`
	}
	if _, ok := s.ask(w, r, prompt, &out); !ok {
		return
	}
	for i := range out.Issues {
		if !validDate(out.Issues[i].DueDate) {
			out.Issues[i].DueDate = ""
		}
		if out.Issues[i].Priority < 0 || out.Issues[i].Priority > 4 {
			out.Issues[i].Priority = 0
		}
		if out.Issues[i].AssigneeID != nil && (in.TeamID == 0 || s.store.TeamRole(r.Context(), in.TeamID, *out.Issues[i].AssigneeID) == "") {
			out.Issues[i].AssigneeID = nil
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleAIRewrite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text        string `json:"text"`
		Instruction string `json:"instruction"` // clearer | shorter | friendlier | checklist | fix
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Text) == "" {
		writeErr(w, http.StatusBadRequest, "nothing to rewrite")
		return
	}
	how := map[string]string{
		"clearer":    "Rewrite it to be clearer and better structured, keeping every fact.",
		"shorter":    "Make it about half as long, keeping every decision and fact.",
		"friendlier": "Make the tone warmer and more polite, keeping the content.",
		"checklist":  "Turn it into a Markdown checklist (- [ ] …) of the concrete steps it contains.",
		"fix":        "Fix spelling and grammar only; change nothing else.",
	}[in.Instruction]
	if how == "" {
		how = strings.TrimSpace(in.Instruction)
	}
	if how == "" {
		how = "Rewrite it to be clearer, keeping every fact."
	}
	text, ok := s.ask(w, r, how+" Answer with the rewritten text only.\n\nText:\n"+clip(in.Text, 12000), nil)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": text})
}
