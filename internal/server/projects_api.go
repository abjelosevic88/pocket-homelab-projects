package server

import (
	"context"
	"database/sql"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

func (s *Server) projectRoutes(r chi.Router) {
	r.Get("/projects", s.handleListProjects)
	r.Post("/projects", s.handleCreateProject)
	r.Get("/projects/{id}", s.handleGetProject)
	r.Patch("/projects/{id}", s.handlePatchProject)
	r.Put("/projects/{id}", s.handlePatchProject)
	r.Delete("/projects/{id}", s.handleDeleteProject)
	r.Post("/projects/{id}/restore", s.handleRestoreProject)
	r.Get("/projects/{id}/history", s.handleProjectHistory)
	r.Get("/projects/{id}/milestones", s.handleMilestones)
	r.Post("/projects/{id}/milestones", s.handleCreateMilestone)
	r.Get("/milestones/{id}", s.handleGetMilestone)
	r.Patch("/milestones/{id}", s.handlePatchMilestone)
	r.Delete("/milestones/{id}", s.handleDeleteMilestone)
	r.Get("/projects/{id}/updates", s.handleProjectUpdates)
	r.Post("/projects/{id}/updates", s.handlePostUpdate)
	r.Patch("/updates/{id}", s.handleEditUpdate)
	r.Delete("/updates/{id}", s.handleDeleteUpdate)
	r.Get("/updates/{id}/comments", s.handleUpdateComments)
	r.Post("/updates/{id}/comments", s.handleCommentUpdate)
	r.Get("/projects/{id}/docs", s.handleProjectDocs)
	r.Post("/projects/{id}/docs", s.handleCreateDoc)
	r.Get("/docs/{id}", s.handleGetDoc)
	r.Patch("/docs/{id}", s.handlePatchDoc)
	r.Delete("/docs/{id}", s.handleDeleteDoc)
	r.Get("/projects/{id}/links", s.handleProjectLinks)
	r.Post("/projects/{id}/links", s.handleAddProjectLink)
	r.Delete("/project-links/{id}", s.handleDeleteProjectLink)
	r.Get("/projects/{id}/contacts", s.handleProjectContacts)
	r.Post("/projects/{id}/contacts", s.handleAddProjectContact)
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	f := store.ProjectFilter{AllowedTeamIDs: a.ids(), TeamIDs: queryIDs(r, "team_id"), Statuses: queryList(r, "status"), Q: r.URL.Query().Get("q"),
		URL: r.URL.Query().Get("url"), Trash: queryBool(r, "trash"), IDs: queryIDs(r, "ids")}
	if v := r.URL.Query().Get("lead"); v == "me" {
		f.LeadID = a.uid
	} else if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		f.LeadID = n
	}
	if r.URL.Query().Get("member") == "me" {
		f.MemberID = a.uid
	}
	list, err := s.store.ListProjects(r.Context(), f)
	if err != nil {
		s.fail(w, err, "projects")
		return
	}
	if queryBool(r, "milestones") {
		for _, p := range list {
			p.Milestones, _ = s.store.Milestones(r.Context(), p.ID)
		}
	}
	writeJSON(w, http.StatusOK, list)
}

// ProjectDetail is a project with everything its page shows.
type ProjectDetail struct {
	*store.Project
	Docs      []store.Doc           `json:"docs"`
	Links     []store.ProjectLink   `json:"links"`
	Contacts  []store.ContactLink   `json:"contacts"`
	Updates   []store.ProjectUpdate `json:"updates"`
	History   []store.History       `json:"history"`
	CanEdit   bool                  `json:"can_edit"`
	URL       string                `json:"url"`
	UpdateDue bool                  `json:"update_due"`
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := s.accessOf(r)
	p, err := s.loadProject(r, a, idParam(r, "id"), needRead)
	if err != nil {
		s.fail(w, err, "project")
		return
	}
	d := ProjectDetail{Project: p, CanEdit: a.canAny(p.TeamIDs, needWrite), URL: s.projectURL(r, p.ID)}
	d.Docs, _ = s.store.ProjectDocs(ctx, p.ID)
	d.Links, _ = s.store.ProjectLinks(ctx, p.ID)
	d.Contacts, _ = s.store.ContactLinks(ctx, p.ID, 0)
	d.Updates, _ = s.store.ProjectUpdates(ctx, p.ID, 20)
	d.History, _ = s.store.ProjectHistory(ctx, p.ID)
	d.UpdateDue = s.updateDue(p, time.Now())
	writeJSON(w, http.StatusOK, d)
}

// updateDue reports whether a started project's lead owes an update.
func (s *Server) updateDue(p *store.Project, now time.Time) bool {
	if p.Status != "started" || p.UpdateCadenceDays <= 0 {
		return false
	}
	last := p.CreatedAt
	if p.StartedAt != nil {
		last = *p.StartedAt
	}
	if p.LastUpdate != nil {
		last = p.LastUpdate.CreatedAt
	}
	return now.Sub(store.ParseTime(last)) > time.Duration(p.UpdateCadenceDays)*24*time.Hour
}

type projectInput struct {
	Name              *string  `json:"name"`
	Summary           *string  `json:"summary"`
	Content           *string  `json:"content"`
	Icon              *string  `json:"icon"`
	Color             *string  `json:"color"`
	Status            *string  `json:"status"`
	Priority          *int     `json:"priority"`
	LeadID            *int64   `json:"lead_id"`
	StartDate         *string  `json:"start_date"`
	TargetDate        *string  `json:"target_date"`
	UpdateCadenceDays *int     `json:"update_cadence_days"`
	SortOrder         *float64 `json:"sort_order"`
	TeamIDs           *[]int64 `json:"team_ids"`
	MemberIDs         *[]int64 `json:"member_ids"`
	Milestones        []struct {
		Name       string `json:"name"`
		TargetDate string `json:"target_date"`
	} `json:"milestones"`
}

func validDate(v string) bool {
	if v == "" {
		return true
	}
	_, err := time.Parse("2006-01-02", v)
	return err == nil
}

func (s *Server) applyProject(ctx context.Context, a *access, who actor, p *store.Project, in projectInput) ([]store.History, error) {
	var hist []store.History
	rec := func(field, from, to, fl, tl string) {
		if from != to {
			hist = append(hist, store.History{ActorID: who.idPtr(), ActorApp: who.app, Field: field, FromValue: from, ToValue: to, FromLabel: fl, ToLabel: tl})
		}
	}
	names := newNamer(s, ctx)
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if n == "" {
			return nil, badRequest("a project needs a name")
		}
		rec("name", p.Name, n, "", "")
		p.Name = n
	}
	if in.Summary != nil {
		p.Summary = strings.TrimSpace(*in.Summary)
	}
	if in.Content != nil {
		if p.ID > 0 && *in.Content != p.Content {
			rec("content", "", "1", "", "")
		}
		p.Content = *in.Content
	}
	if in.Icon != nil {
		p.Icon = *in.Icon
	}
	if in.Color != nil {
		p.Color = *in.Color
	}
	if in.Status != nil {
		if !store.ValidProjectStatus(*in.Status) {
			return nil, badRequest("status is backlog, planned, started, paused, completed or canceled")
		}
		rec("status", p.Status, *in.Status, "", "")
		if *in.Status != p.Status {
			now := store.Now()
			switch *in.Status {
			case "started":
				if p.StartedAt == nil {
					p.StartedAt = &now
				}
				p.CompletedAt, p.CanceledAt = nil, nil
			case "completed":
				p.CompletedAt, p.CanceledAt = &now, nil
			case "canceled":
				p.CanceledAt, p.CompletedAt = &now, nil
			default:
				p.CompletedAt, p.CanceledAt = nil, nil
			}
		}
		p.Status = *in.Status
	}
	if in.Priority != nil {
		if *in.Priority < 0 || *in.Priority > 4 {
			return nil, badRequest("priority is 0–4")
		}
		rec("priority", strconv.Itoa(p.Priority), strconv.Itoa(*in.Priority), priorityName(p.Priority), priorityName(*in.Priority))
		p.Priority = *in.Priority
	}
	if in.StartDate != nil {
		if !validDate(*in.StartDate) {
			return nil, badRequest("start_date looks like 2026-10-23")
		}
		rec("start_date", p.StartDate, *in.StartDate, "", "")
		p.StartDate = *in.StartDate
	}
	if in.TargetDate != nil {
		if !validDate(*in.TargetDate) {
			return nil, badRequest("target_date looks like 2026-12-31")
		}
		rec("target_date", p.TargetDate, *in.TargetDate, "", "")
		p.TargetDate = *in.TargetDate
	}
	if p.StartDate != "" && p.TargetDate != "" && p.TargetDate < p.StartDate {
		return nil, badRequest("the target date is before the start date")
	}
	if in.UpdateCadenceDays != nil {
		p.UpdateCadenceDays = max(0, min(*in.UpdateCadenceDays, 90))
	}
	if in.SortOrder != nil {
		p.SortOrder = *in.SortOrder
	}
	if in.TeamIDs != nil {
		var teams []int64
		cur := map[int64]bool{}
		for _, t := range p.TeamIDs {
			cur[t] = true
		}
		for _, t := range *in.TeamIDs {
			// Adding a team needs write access there; keeping one only needs that it was there.
			if !cur[t] && !a.can(t, needWrite) {
				return nil, badRequest("you can't add a team you are not a member of")
			}
			teams = append(teams, t)
		}
		// Teams the person cannot see stay attached.
		for _, t := range p.TeamIDs {
			if a.teams[t] == "" {
				teams = append(teams, t)
			}
		}
		if len(teams) == 0 {
			return nil, badRequest("a project works with at least one team")
		}
		p.TeamIDs = uniq(teams)
	}
	if in.LeadID != nil {
		var next *int64
		if *in.LeadID > 0 {
			if !s.onAnyTeam(ctx, *in.LeadID, p.TeamIDs) {
				return nil, badRequest("the lead must be on one of the project's teams")
			}
			next = ptr(*in.LeadID)
		}
		rec("lead", idStr(p.LeadID), idStr(next), names.person(p.LeadID), names.person(next))
		p.LeadID = next
	}
	if in.MemberIDs != nil {
		var members []int64
		for _, uid := range *in.MemberIDs {
			if s.onAnyTeam(ctx, uid, p.TeamIDs) {
				members = append(members, uid)
			}
		}
		p.MemberIDs = uniq(members)
	}
	return hist, nil
}

func uniq(ids []int64) []int64 {
	seen := map[int64]bool{}
	out := []int64{}
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func (s *Server) onAnyTeam(ctx context.Context, uid int64, teams []int64) bool {
	for _, t := range teams {
		if s.store.TeamRole(ctx, t, uid) != "" {
			return true
		}
	}
	return false
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in projectInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a := s.accessOf(r)
	who := s.actorOf(r)
	p := &store.Project{Status: "planned", UpdateCadenceDays: 7, CreatedBy: who.idPtr(), SourceApp: who.app, TeamIDs: []int64{}, MemberIDs: []int64{}}
	if in.TeamIDs == nil || len(*in.TeamIDs) == 0 {
		if st, _ := s.store.GetSettings(ctx, a.uid); st.DefaultTeamID > 0 && a.can(st.DefaultTeamID, needWrite) {
			in.TeamIDs = &[]int64{st.DefaultTeamID}
		} else if ids := a.ids(); len(ids) == 1 {
			in.TeamIDs = &ids
		}
	}
	if in.LeadID == nil {
		in.LeadID = ptr(a.uid)
	}
	if _, err := s.applyProject(ctx, a, who, p, in); err != nil {
		s.fail(w, err, "project")
		return
	}
	if p.Name == "" {
		writeErr(w, http.StatusBadRequest, "a project needs a name")
		return
	}
	if len(p.TeamIDs) == 0 {
		writeErr(w, http.StatusBadRequest, "choose at least one team (team_ids)")
		return
	}
	if p.LeadID != nil && !containsID(p.MemberIDs, *p.LeadID) {
		p.MemberIDs = append(p.MemberIDs, *p.LeadID)
	}
	if p.Status == "started" && p.StartedAt == nil {
		now := store.Now()
		p.StartedAt = &now
	}
	payload := map[string]any{"name": p.Name, "status": p.Status, "audience_emails": s.teamEmails(ctx, p.TeamIDs...)}
	if who.id > 0 {
		payload["user_email"] = s.userEmail(ctx, who.id)
	}
	if err := s.store.SaveProject(ctx, p, nil, func(tx *sql.Tx) error {
		payload["id"], payload["url"] = p.ID, who.base+"/project/"+strconv.FormatInt(p.ID, 10)
		return s.coreEvent(ctx, "projects.project_created", "project:"+strconv.FormatInt(p.ID, 10), payload)(tx)
	}); err != nil {
		s.fail(w, err, "project")
		return
	}
	for _, m := range in.Milestones {
		if strings.TrimSpace(m.Name) != "" && validDate(m.TargetDate) {
			_ = s.store.SaveMilestone(ctx, &store.Milestone{ProjectID: p.ID, Name: strings.TrimSpace(m.Name), TargetDate: m.TargetDate})
		}
	}
	for _, uid := range p.MemberIDs {
		s.notifyPerson(ctx, store.Notification{UserID: uid, Kind: "added", ProjectID: &p.ID, ActorID: who.idPtr(), ActorApp: who.app,
			Title: who.name + " added you to the project " + p.Name, Body: p.Summary}, who.base+"/project/"+strconv.FormatInt(p.ID, 10))
	}
	fresh, _ := s.store.GetProject(ctx, p.ID)
	s.changed(r, "project", p.TeamIDs, p.ID)
	for _, t := range p.TeamIDs {
		s.webhook(t, "project.created", fresh)
	}
	writeJSON(w, http.StatusCreated, fresh)
}

func containsID(ids []int64, id int64) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func (s *Server) handlePatchProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in projectInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a := s.accessOf(r)
	p, err := s.loadProject(r, a, idParam(r, "id"), needWrite)
	if err != nil {
		s.fail(w, err, "project")
		return
	}
	who := s.actorOf(r)
	oldMembers := append([]int64(nil), p.MemberIDs...)
	oldTeams := append([]int64(nil), p.TeamIDs...)
	oldStatus := p.Status
	hist, err := s.applyProject(ctx, a, who, p, in)
	if err != nil {
		s.fail(w, err, "project")
		return
	}
	payload := map[string]any{"id": p.ID, "name": p.Name, "status": p.Status, "changes": fieldsOf(hist), "url": who.base + "/project/" + strconv.FormatInt(p.ID, 10),
		"audience_emails": s.teamEmails(ctx, p.TeamIDs...)}
	if who.id > 0 {
		payload["user_email"] = s.userEmail(ctx, who.id)
	}
	if err := s.store.SaveProject(ctx, p, hist, s.coreEvent(ctx, "projects.project_updated", "project:"+strconv.FormatInt(p.ID, 10), payload)); err != nil {
		s.fail(w, err, "project")
		return
	}
	link := who.base + "/project/" + strconv.FormatInt(p.ID, 10)
	for _, uid := range p.MemberIDs {
		if !containsID(oldMembers, uid) {
			s.notifyPerson(ctx, store.Notification{UserID: uid, Kind: "added", ProjectID: &p.ID, ActorID: who.idPtr(), Title: who.name + " added you to the project " + p.Name, Body: p.Summary}, link)
		}
	}
	if oldStatus != p.Status {
		for _, uid := range s.store.ProjectPeople(ctx, p.ID) {
			s.notifyPerson(ctx, store.Notification{UserID: uid, Kind: "status", ProjectID: &p.ID, ActorID: who.idPtr(), Title: p.Name + " is now " + projectStatusName(p.Status), Body: "by " + who.name}, link)
		}
	}
	fresh, _ := s.store.GetProject(ctx, p.ID)
	s.changed(r, "project", uniq(append(oldTeams, p.TeamIDs...)), p.ID)
	for _, t := range p.TeamIDs {
		s.webhook(t, "project.updated", map[string]any{"project": fresh, "changes": hist})
	}
	writeJSON(w, http.StatusOK, fresh)
}

func projectStatusName(st string) string {
	return map[string]string{"backlog": "in the backlog", "planned": "planned", "started": "in progress", "paused": "paused", "completed": "completed", "canceled": "canceled"}[st]
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	p, err := s.loadProject(r, a, idParam(r, "id"), needWrite)
	if err != nil {
		s.fail(w, err, "project")
		return
	}
	now := store.Now()
	p.DeletedAt = &now
	if err := s.store.SaveProject(r.Context(), p, []store.History{{ActorID: ptr(me(r)), Field: "deleted"}}); err != nil {
		s.fail(w, err, "project")
		return
	}
	// Its issues stay, without the project.
	_, _ = s.store.DB.ExecContext(r.Context(), `UPDATE issues SET project_id = NULL, milestone_id = NULL, updated_at = ? WHERE project_id = ?`, now, p.ID)
	s.changed(r, "project", p.TeamIDs, p.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRestoreProject(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	p, err := s.loadProject(r, a, idParam(r, "id"), needWrite)
	if err != nil {
		s.fail(w, err, "project")
		return
	}
	p.DeletedAt = nil
	if err := s.store.SaveProject(r.Context(), p, []store.History{{ActorID: ptr(me(r)), Field: "restored"}}); err != nil {
		s.fail(w, err, "project")
		return
	}
	s.changed(r, "project", p.TeamIDs, p.ID)
	fresh, _ := s.store.GetProject(r.Context(), p.ID)
	writeJSON(w, http.StatusOK, fresh)
}

func (s *Server) handleProjectHistory(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	p, err := s.loadProject(r, a, idParam(r, "id"), needRead)
	if err != nil {
		s.fail(w, err, "project")
		return
	}
	h, err := s.store.ProjectHistory(r.Context(), p.ID)
	if err != nil {
		s.fail(w, err, "history")
		return
	}
	writeJSON(w, http.StatusOK, h)
}

// ---------- milestones ----------

func (s *Server) handleMilestones(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	p, err := s.loadProject(r, a, idParam(r, "id"), needRead)
	if err != nil {
		s.fail(w, err, "project")
		return
	}
	writeJSON(w, http.StatusOK, p.Milestones)
}

type milestoneInput struct {
	Name        *string  `json:"name"`
	Description *string  `json:"description"`
	TargetDate  *string  `json:"target_date"`
	SortOrder   *float64 `json:"sort_order"`
}

func (in milestoneInput) apply(m *store.Milestone) error {
	if in.Name != nil {
		m.Name = strings.TrimSpace(*in.Name)
	}
	if m.Name == "" {
		return badRequest("a milestone needs a name")
	}
	if in.Description != nil {
		m.Description = *in.Description
	}
	if in.TargetDate != nil {
		if !validDate(*in.TargetDate) {
			return badRequest("target_date looks like 2026-12-01")
		}
		m.TargetDate = *in.TargetDate
	}
	if in.SortOrder != nil {
		m.SortOrder = *in.SortOrder
	}
	return nil
}

func (s *Server) handleCreateMilestone(w http.ResponseWriter, r *http.Request) {
	var in milestoneInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a := s.accessOf(r)
	p, err := s.loadProject(r, a, idParam(r, "id"), needWrite)
	if err != nil {
		s.fail(w, err, "project")
		return
	}
	m := &store.Milestone{ProjectID: p.ID}
	if err := in.apply(m); err != nil {
		s.fail(w, err, "milestone")
		return
	}
	if err := s.store.SaveMilestone(r.Context(), m); err != nil {
		s.fail(w, err, "milestone")
		return
	}
	_ = s.store.AddHistory(r.Context(), store.History{ProjectID: &p.ID, ActorID: ptr(me(r)), Field: "milestone_added", ToLabel: m.Name})
	s.changed(r, "project", p.TeamIDs, p.ID)
	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) milestoneFor(r *http.Request, need int) (*store.Milestone, *store.Project, error) {
	m, err := s.store.GetMilestone(r.Context(), idParam(r, "id"))
	if err != nil {
		return nil, nil, err
	}
	p, err := s.loadProject(r, s.accessOf(r), m.ProjectID, need)
	if err != nil {
		return nil, nil, err
	}
	return m, p, nil
}

func (s *Server) handleGetMilestone(w http.ResponseWriter, r *http.Request) {
	m, p, err := s.milestoneFor(r, needRead)
	if err != nil {
		s.fail(w, err, "milestone")
		return
	}
	for _, x := range p.Milestones {
		if x.ID == m.ID {
			writeJSON(w, http.StatusOK, x)
			return
		}
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handlePatchMilestone(w http.ResponseWriter, r *http.Request) {
	var in milestoneInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	m, p, err := s.milestoneFor(r, needWrite)
	if err != nil {
		s.fail(w, err, "milestone")
		return
	}
	if err := in.apply(m); err != nil {
		s.fail(w, err, "milestone")
		return
	}
	if err := s.store.SaveMilestone(r.Context(), m); err != nil {
		s.fail(w, err, "milestone")
		return
	}
	s.changed(r, "project", p.TeamIDs, p.ID)
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleDeleteMilestone(w http.ResponseWriter, r *http.Request) {
	m, p, err := s.milestoneFor(r, needWrite)
	if err != nil {
		s.fail(w, err, "milestone")
		return
	}
	if err := s.store.DeleteMilestone(r.Context(), m.ID); err != nil {
		s.fail(w, err, "milestone")
		return
	}
	_ = s.store.AddHistory(r.Context(), store.History{ProjectID: &p.ID, ActorID: ptr(me(r)), Field: "milestone_removed", FromLabel: m.Name})
	s.changed(r, "project", p.TeamIDs, p.ID)
	w.WriteHeader(http.StatusNoContent)
}

// ---------- updates ----------

func (s *Server) handleProjectUpdates(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	p, err := s.loadProject(r, a, idParam(r, "id"), needRead)
	if err != nil {
		s.fail(w, err, "project")
		return
	}
	list, err := s.store.ProjectUpdates(r.Context(), p.ID, queryInt(r, "limit"))
	if err != nil {
		s.fail(w, err, "updates")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handlePostUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		Health string `json:"health"`
		Body   string `json:"body"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Health == "" {
		in.Health = "on_track"
	}
	if !store.ValidHealth(in.Health) || strings.TrimSpace(in.Body) == "" {
		writeErr(w, http.StatusBadRequest, "an update needs a body and health on_track, at_risk or off_track")
		return
	}
	a := s.accessOf(r)
	p, err := s.loadProject(r, a, idParam(r, "id"), needWrite)
	if err != nil {
		s.fail(w, err, "project")
		return
	}
	who := s.actorOf(r)
	u := &store.ProjectUpdate{ProjectID: p.ID, UserID: who.idPtr(), Health: in.Health, Body: strings.TrimSpace(in.Body), SourceApp: who.app}
	link := who.base + "/project/" + strconv.FormatInt(p.ID, 10) + "?tab=updates"
	payload := map[string]any{"project_id": p.ID, "name": p.Name, "health": u.Health, "body": excerpt(u.Body, 800), "url": link, "audience_emails": s.teamEmails(ctx, p.TeamIDs...)}
	if who.id > 0 {
		payload["user_email"] = s.userEmail(ctx, who.id)
	}
	if err := s.store.SaveUpdate(ctx, u, s.coreEvent(ctx, "projects.project_update_posted", "project:"+strconv.FormatInt(p.ID, 10), payload)); err != nil {
		s.fail(w, err, "update")
		return
	}
	health := map[string]string{"on_track": "on track", "at_risk": "at risk", "off_track": "off track"}[u.Health]
	for _, uid := range s.store.ProjectPeople(ctx, p.ID) {
		s.notifyPerson(ctx, store.Notification{UserID: uid, Kind: "project_update", ProjectID: &p.ID, ActorID: who.idPtr(), ActorApp: who.app,
			Title: who.name + " posted an update on " + p.Name + " (" + health + ")", Body: excerpt(u.Body, 200)}, link)
	}
	s.changed(r, "project", p.TeamIDs, p.ID)
	for _, t := range p.TeamIDs {
		s.webhook(t, "project.update_posted", map[string]any{"project": map[string]any{"id": p.ID, "name": p.Name}, "update": u})
	}
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) updateFor(r *http.Request, need int) (*store.ProjectUpdate, *store.Project, error) {
	u, err := s.store.GetUpdate(r.Context(), idParam(r, "id"))
	if err != nil {
		return nil, nil, err
	}
	p, err := s.loadProject(r, s.accessOf(r), u.ProjectID, need)
	if err != nil {
		return nil, nil, err
	}
	return u, p, nil
}

func (s *Server) handleEditUpdate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Health *string `json:"health"`
		Body   *string `json:"body"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	u, p, err := s.updateFor(r, needWrite)
	if err != nil {
		s.fail(w, err, "update")
		return
	}
	if u.UserID == nil || *u.UserID != me(r) {
		writeErr(w, http.StatusForbidden, "only the author can edit an update")
		return
	}
	if in.Health != nil {
		if !store.ValidHealth(*in.Health) || *in.Health == "" {
			writeErr(w, http.StatusBadRequest, "health is on_track, at_risk or off_track")
			return
		}
		u.Health = *in.Health
	}
	if in.Body != nil && strings.TrimSpace(*in.Body) != "" {
		u.Body = strings.TrimSpace(*in.Body)
	}
	if err := s.store.SaveUpdate(r.Context(), u); err != nil {
		s.fail(w, err, "update")
		return
	}
	s.changed(r, "project", p.TeamIDs, p.ID)
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleDeleteUpdate(w http.ResponseWriter, r *http.Request) {
	u, p, err := s.updateFor(r, needWrite)
	if err != nil {
		s.fail(w, err, "update")
		return
	}
	a := s.accessOf(r)
	if (u.UserID == nil || *u.UserID != a.uid) && !a.canAny(p.TeamIDs, needAdmin) {
		writeErr(w, http.StatusForbidden, "only the author or a team admin can delete an update")
		return
	}
	if err := s.store.DeleteUpdate(r.Context(), u); err != nil {
		s.fail(w, err, "update")
		return
	}
	s.changed(r, "project", p.TeamIDs, p.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUpdateComments(w http.ResponseWriter, r *http.Request) {
	u, _, err := s.updateFor(r, needRead)
	if err != nil {
		s.fail(w, err, "update")
		return
	}
	list, err := s.store.Comments(r.Context(), 0, u.ID)
	if err != nil {
		s.fail(w, err, "comments")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCommentUpdate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Body string `json:"body"`
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Body) == "" {
		writeErr(w, http.StatusBadRequest, "the comment is empty")
		return
	}
	u, p, err := s.updateFor(r, needRead)
	if err != nil {
		s.fail(w, err, "update")
		return
	}
	who := s.actorOf(r)
	c := &store.Comment{ProjectUpdateID: &u.ID, UserID: who.idPtr(), Body: strings.TrimSpace(in.Body), SourceApp: who.app}
	if err := s.store.SaveComment(r.Context(), c); err != nil {
		s.fail(w, err, "comment")
		return
	}
	if u.UserID != nil {
		s.notifyPerson(r.Context(), store.Notification{UserID: *u.UserID, Kind: "comment", ProjectID: &p.ID, CommentID: &c.ID, ActorID: who.idPtr(),
			Title: who.name + " replied to your update on " + p.Name, Body: excerpt(c.Body, 200)}, who.base+"/project/"+strconv.FormatInt(p.ID, 10)+"?tab=updates")
	}
	s.changed(r, "project", p.TeamIDs, p.ID)
	writeJSON(w, http.StatusCreated, c)
}

// ---------- documents ----------

func (s *Server) handleProjectDocs(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	p, err := s.loadProject(r, a, idParam(r, "id"), needRead)
	if err != nil {
		s.fail(w, err, "project")
		return
	}
	list, err := s.store.ProjectDocs(r.Context(), p.ID)
	if err != nil {
		s.fail(w, err, "docs")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateDoc(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Icon  string `json:"icon"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a := s.accessOf(r)
	p, err := s.loadProject(r, a, idParam(r, "id"), needWrite)
	if err != nil {
		s.fail(w, err, "project")
		return
	}
	d := &store.Doc{ProjectID: p.ID, Title: firstNonEmpty(strings.TrimSpace(in.Title), "Untitled"), Body: in.Body, Icon: in.Icon, CreatedBy: ptr(me(r)), UpdatedBy: ptr(me(r))}
	if err := s.store.SaveDoc(r.Context(), d); err != nil {
		s.fail(w, err, "doc")
		return
	}
	_ = s.store.AddHistory(r.Context(), store.History{ProjectID: &p.ID, ActorID: ptr(me(r)), Field: "doc_added", ToLabel: d.Title})
	s.changed(r, "project", p.TeamIDs, p.ID)
	writeJSON(w, http.StatusCreated, d)
}

func (s *Server) docFor(r *http.Request, need int) (*store.Doc, *store.Project, error) {
	d, err := s.store.GetDoc(r.Context(), idParam(r, "id"))
	if err != nil {
		return nil, nil, err
	}
	p, err := s.loadProject(r, s.accessOf(r), d.ProjectID, need)
	if err != nil {
		return nil, nil, err
	}
	return d, p, nil
}

func (s *Server) handleGetDoc(w http.ResponseWriter, r *http.Request) {
	d, _, err := s.docFor(r, needRead)
	if err != nil {
		s.fail(w, err, "doc")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handlePatchDoc(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title *string `json:"title"`
		Body  *string `json:"body"`
		Icon  *string `json:"icon"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	d, p, err := s.docFor(r, needWrite)
	if err != nil {
		s.fail(w, err, "doc")
		return
	}
	if in.Title != nil {
		d.Title = firstNonEmpty(strings.TrimSpace(*in.Title), "Untitled")
	}
	if in.Body != nil {
		d.Body = *in.Body
	}
	if in.Icon != nil {
		d.Icon = *in.Icon
	}
	d.UpdatedBy = ptr(me(r))
	if err := s.store.SaveDoc(r.Context(), d); err != nil {
		s.fail(w, err, "doc")
		return
	}
	s.changed(r, "project", p.TeamIDs, p.ID)
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleDeleteDoc(w http.ResponseWriter, r *http.Request) {
	d, p, err := s.docFor(r, needWrite)
	if err != nil {
		s.fail(w, err, "doc")
		return
	}
	if err := s.store.DeleteDoc(r.Context(), d.ID); err != nil {
		s.fail(w, err, "doc")
		return
	}
	_ = s.store.AddHistory(r.Context(), store.History{ProjectID: &p.ID, ActorID: ptr(me(r)), Field: "doc_removed", FromLabel: d.Title})
	s.changed(r, "project", p.TeamIDs, p.ID)
	w.WriteHeader(http.StatusNoContent)
}

// ---------- resources ----------

func (s *Server) handleProjectLinks(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	p, err := s.loadProject(r, a, idParam(r, "id"), needRead)
	if err != nil {
		s.fail(w, err, "project")
		return
	}
	list, err := s.store.ProjectLinks(r.Context(), p.ID)
	if err != nil {
		s.fail(w, err, "links")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleAddProjectLink(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title string `json:"title"`
		URL   string `json:"url"`
		App   string `json:"app"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	u, err := url.Parse(strings.TrimSpace(in.URL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		writeErr(w, http.StatusBadRequest, "a resource needs an http(s) address")
		return
	}
	a := s.accessOf(r)
	p, err := s.loadProject(r, a, idParam(r, "id"), needWrite)
	if err != nil {
		s.fail(w, err, "project")
		return
	}
	l := &store.ProjectLink{ProjectID: p.ID, Title: firstNonEmpty(strings.TrimSpace(in.Title), u.Host+u.Path), URL: u.String(), App: firstNonEmpty(in.App, appOfURL(u)), CreatedBy: ptr(me(r))}
	if err := s.store.AddProjectLink(r.Context(), l); err != nil {
		s.fail(w, err, "link")
		return
	}
	s.changed(r, "project", p.TeamIDs, p.ID)
	writeJSON(w, http.StatusCreated, l)
}

func (s *Server) handleDeleteProjectLink(w http.ResponseWriter, r *http.Request) {
	l, err := s.store.GetProjectLink(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "link")
		return
	}
	p, err := s.loadProject(r, s.accessOf(r), l.ProjectID, needWrite)
	if err != nil {
		s.fail(w, err, "link")
		return
	}
	if err := s.store.DeleteProjectLink(r.Context(), l.ID); err != nil {
		s.fail(w, err, "link")
		return
	}
	s.changed(r, "project", p.TeamIDs, p.ID)
	w.WriteHeader(http.StatusNoContent)
}

// ---------- stakeholders ----------

func (s *Server) handleProjectContacts(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	p, err := s.loadProject(r, a, idParam(r, "id"), needRead)
	if err != nil {
		s.fail(w, err, "project")
		return
	}
	list, err := s.store.ContactLinks(r.Context(), p.ID, 0)
	if err != nil {
		s.fail(w, err, "contacts")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleAddProjectContact(w http.ResponseWriter, r *http.Request) {
	var in contactInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a := s.accessOf(r)
	p, err := s.loadProject(r, a, idParam(r, "id"), needWrite)
	if err != nil {
		s.fail(w, err, "project")
		return
	}
	c, err := in.link()
	if err != nil {
		s.fail(w, err, "contact")
		return
	}
	c.ProjectID = &p.ID
	if c.URL == "" && c.ContactID != "" {
		c.URL = s.contactPageURL(r.Context(), me(r), c.ContactID)
	}
	if err := s.store.AddContactLink(r.Context(), c, me(r)); err != nil {
		s.fail(w, err, "contact")
		return
	}
	_ = s.store.AddHistory(r.Context(), store.History{ProjectID: &p.ID, ActorID: ptr(me(r)), Field: "contact_added", ToLabel: c.Name})
	s.changed(r, "project", p.TeamIDs, p.ID)
	writeJSON(w, http.StatusCreated, c)
}
