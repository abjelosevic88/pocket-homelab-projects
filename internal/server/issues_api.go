package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

func (s *Server) issueRoutes(r chi.Router) {
	r.Get("/issues", s.handleListIssues)
	r.Post("/issues", s.handleCreateIssue)
	r.Post("/issues/bulk", s.handleBulkIssues)
	r.Get("/issues/{ref}", s.handleGetIssue)
	r.Patch("/issues/{ref}", s.handlePatchIssue)
	r.Put("/issues/{ref}", s.handlePatchIssue)
	r.Delete("/issues/{ref}", s.handleDeleteIssue)
	r.Post("/issues/{ref}/restore", s.handleRestoreIssue)
	r.Get("/issues/{ref}/history", s.handleIssueHistory)
	r.Get("/issues/{ref}/sub-issues", s.handleSubIssues)
	r.Get("/issues/{ref}/relations", s.handleIssueRelations)
	r.Post("/issues/{ref}/relations", s.handleAddRelation)
	r.Delete("/relations/{id}", s.handleDeleteRelation)
	r.Put("/issues/{ref}/subscription", s.handleSubscription)
	r.Get("/issues/{ref}/comments", s.handleListComments)
	r.Post("/issues/{ref}/comments", s.handleCreateComment)
	r.Patch("/comments/{id}", s.handleEditComment)
	r.Delete("/comments/{id}", s.handleDeleteComment)
	r.Post("/comments/{id}/reactions", s.handleReaction)
	r.Post("/comments/{id}/resolve", s.handleResolveComment)
	r.Get("/issues/{ref}/attachments", s.handleListAttachments)
	r.Post("/issues/{ref}/attachments", s.handleAddAttachment)
	r.Delete("/attachments/{id}", s.handleDeleteAttachment)
	r.Get("/attachments/{id}/file", s.handleAttachmentFile)
	r.Get("/issues/{ref}/contacts", s.handleIssueContacts)
	r.Post("/issues/{ref}/contacts", s.handleAddIssueContact)
	r.Delete("/contact-links/{id}", s.handleDeleteContactLink)
}

// actor is who makes a change: a person, a Pocket app acting for them, or the box itself.
type actor struct {
	id   int64
	app  string // acting Pocket app, "git", "system", "" for the person
	name string
	base string // the app's public address for links
}

func (s *Server) actorOf(r *http.Request) actor {
	ctx := r.Context()
	a := actor{id: me(r), app: s.linkApp(r), base: s.publicURL(r)}
	a.name = s.actorName(ctx, r)
	return a
}

func (s *Server) systemActor(app string) actor {
	return actor{app: app, name: "Pocket Projects", base: s.cfg.BaseURL}
}

func (a actor) idPtr() *int64 {
	if a.id <= 0 {
		return nil
	}
	return &a.id
}

// ---------- listing ----------

// filterFromQuery reads an IssueFilter from the query string (or a JSON `filter` parameter).
func (s *Server) filterFromQuery(r *http.Request, a *access) store.IssueFilter {
	q := r.URL.Query()
	var f store.IssueFilter
	if raw := q.Get("filter"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &f)
	}
	if v := queryIDs(r, "team_id"); len(v) > 0 {
		f.TeamIDs = v
	}
	if keys := queryList(r, "team"); len(keys) > 0 {
		for _, k := range keys {
			if t, err := s.store.GetTeamByKey(r.Context(), k); err == nil {
				f.TeamIDs = append(f.TeamIDs, t.ID)
			}
		}
	}
	setIDs := func(dst *[]int64, name string) {
		if v := queryIDs(r, name); len(v) > 0 {
			*dst = v
		}
	}
	setIDs(&f.IDs, "ids")
	setIDs(&f.StateIDs, "state_id")
	setIDs(&f.CreatorIDs, "creator_id")
	setIDs(&f.LabelIDs, "label_id")
	setIDs(&f.NotLabelIDs, "not_label_id")
	setIDs(&f.ProjectIDs, "project_id")
	setIDs(&f.MilestoneIDs, "milestone_id")
	setIDs(&f.CycleIDs, "cycle_id")
	if v := q.Get("assignee_id"); v != "" {
		if v == "me" {
			f.AssigneeIDs = []int64{a.uid}
		} else {
			f.AssigneeIDs = queryIDs(r, "assignee_id")
		}
	}
	if v := queryList(r, "state_type"); len(v) > 0 {
		f.StateTypes = v
	}
	if v := queryList(r, "status"); len(v) > 0 { // friendly: open | done | started
		for _, st := range v {
			switch st {
			case "open":
				f.StateTypes = append(f.StateTypes, "triage", "backlog", "unstarted", "started")
			case "done":
				f.StateTypes = append(f.StateTypes, "completed", "canceled")
			default:
				if store.ValidStateType(st) {
					f.StateTypes = append(f.StateTypes, st)
				}
			}
		}
	}
	for _, p := range queryList(r, "priority") {
		if n, err := strconv.Atoi(p); err == nil && n >= 0 && n <= 4 {
			f.Priorities = append(f.Priorities, n)
		}
	}
	str := func(dst *string, name string) {
		if v := q.Get(name); v != "" {
			*dst = v
		}
	}
	str(&f.LabelMode, "label_mode")
	str(&f.Cycle, "cycle")
	str(&f.Due, "due")
	str(&f.DueFrom, "due_from")
	str(&f.DueTo, "due_to")
	str(&f.CreatedSince, "created_since")
	str(&f.UpdatedSince, "updated_since")
	str(&f.ShowCompleted, "show_completed")
	str(&f.Archived, "archived")
	str(&f.Snoozed, "snoozed")
	str(&f.SourceApp, "source_app")
	str(&f.SourceRef, "source_ref")
	str(&f.URL, "url")
	str(&f.Q, "q")
	str(&f.Sort, "sort")
	if v := q.Get("parent"); v != "" {
		if id, err := s.issueID(r.Context(), v); err == nil {
			f.ParentID = id
		} else {
			f.ParentID = -1
		}
	}
	if queryBool(r, "top_level") {
		f.TopLevel = true
	}
	if queryBool(r, "subscribed") {
		f.SubscribedBy = a.uid
	}
	if queryBool(r, "blocked") {
		f.Blocked = true
	}
	if queryBool(r, "trash") {
		f.Trash = true
	}
	if n := queryInt(r, "limit"); n > 0 {
		f.Limit = n
	}
	if n := queryInt(r, "offset"); n > 0 {
		f.Offset = n
	}
	f.ViewerID, f.AllowedTeamIDs, f.Today = a.uid, a.ids(), s.today(r.Context(), a.uid)
	return f
}

func (s *Server) handleListIssues(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	f := s.filterFromQuery(r, a)
	if v := queryInt64(r, "view_id"); v > 0 {
		if view, err := s.store.GetView(r.Context(), v); err == nil && (view.UserID == a.uid || (view.Shared && view.TeamID != nil && a.teams[*view.TeamID] != "")) {
			var vf store.IssueFilter
			if json.Unmarshal(view.Filters, &vf) == nil {
				vf.ViewerID, vf.AllowedTeamIDs, vf.Today, vf.Limit, vf.Offset = f.ViewerID, f.AllowedTeamIDs, f.Today, f.Limit, f.Offset
				if vf.Q == "" {
					vf.Q = f.Q
				}
				f = vf
			}
		}
	}
	if f.ParentID < 0 {
		writeJSON(w, http.StatusOK, map[string]any{"issues": []any{}, "total": 0})
		return
	}
	list, total, err := s.store.ListIssues(r.Context(), f)
	if err != nil {
		s.fail(w, err, "list issues")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"issues": list, "total": total})
}

// IssueDetail is an issue with everything its page shows.
type IssueDetail struct {
	*store.Issue
	Team        *store.Team         `json:"team"`
	Parent      *store.IssueRef     `json:"parent"`
	SubIssues   []*store.Issue      `json:"sub_issues"`
	Relations   []relationOut       `json:"relations"`
	Attachments []store.Attachment  `json:"attachment_list"`
	Contacts    []store.ContactLink `json:"contacts"`
	Comments    []store.Comment     `json:"comment_list"`
	History     []store.History     `json:"history"`
	TimeMinutes int                 `json:"time_minutes"`
	Subscribers []int64             `json:"subscriber_ids"`
	URL         string              `json:"url"`
	Branch      string              `json:"branch_name"`
}

type relationOut struct {
	store.Relation
	Kind  string          `json:"kind"` // blocks | blocked_by | related | duplicate | duplicated_by
	Other *store.IssueRef `json:"issue"`
}

func (s *Server) handleGetIssue(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	d := IssueDetail{Issue: is, URL: s.issueURL(r, is.Identifier)}
	d.Team, _ = s.store.GetTeam(ctx, is.TeamID)
	if d.Team != nil {
		d.Team.Role = a.teams[is.TeamID]
	}
	if is.ParentID != nil {
		if refs, _ := s.store.IssueRefs(ctx, []int64{*is.ParentID}); len(refs) > 0 {
			if ref := refs[*is.ParentID]; a.teams[ref.TeamID] != "" {
				d.Parent = &ref
			}
		}
	}
	d.SubIssues, _, _ = s.store.ListIssues(ctx, store.IssueFilter{ParentID: is.ID, AllowedTeamIDs: a.ids(), ViewerID: a.uid, Archived: "include", Sort: "manual"})
	d.Relations = s.relationsOut(ctx, a, is.ID)
	d.Attachments, _ = s.store.Attachments(ctx, is.ID, 0)
	d.Contacts, _ = s.store.ContactLinks(ctx, 0, is.ID)
	d.Comments, _ = s.store.Comments(ctx, is.ID, 0)
	d.History, _ = s.store.IssueHistory(ctx, is.ID)
	d.TimeMinutes = s.store.TimeTotals(ctx, []int64{is.ID})[is.ID]
	d.Subscribers = s.store.Subscribers(ctx, is.ID)
	d.Branch = s.branchName(ctx, a.uid, is)
	if d.SubIssues == nil {
		d.SubIssues = []*store.Issue{}
	}
	if queryBool(r, "read") || r.URL.Query().Get("read") == "" {
		s.store.MarkIssueRead(ctx, a.uid, is.ID)
	}
	writeJSON(w, http.StatusOK, d)
}

// branchName is the git branch the person would use for an issue ("ab/web-12-fix-login").
func (s *Server) branchName(ctx context.Context, uid int64, is *store.Issue) string {
	prefix := ""
	if u, err := s.store.GetUser(ctx, uid); err == nil && u.Username != "" {
		prefix = u.Username + "/"
	}
	var slug []rune
	dash := false
	for _, r := range strings.ToLower(is.Title) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			slug = append(slug, r)
			dash = false
		} else if !dash && len(slug) > 0 {
			slug = append(slug, '-')
			dash = true
		}
		if len(slug) > 40 {
			break
		}
	}
	return prefix + strings.ToLower(is.Identifier) + "-" + strings.Trim(string(slug), "-")
}

func (s *Server) relationsOut(ctx context.Context, a *access, issueID int64) []relationOut {
	rels, _ := s.store.IssueRelations(ctx, issueID)
	var ids []int64
	for _, rel := range rels {
		ids = append(ids, rel.IssueID, rel.RelatedID)
	}
	refs, _ := s.store.IssueRefs(ctx, ids)
	out := []relationOut{}
	for _, rel := range rels {
		other := rel.RelatedID
		kind := rel.Type
		if rel.RelatedID == issueID {
			other = rel.IssueID
			switch rel.Type {
			case "blocks":
				kind = "blocked_by"
			case "duplicate":
				kind = "duplicated_by"
			}
		}
		ref, ok := refs[other]
		if !ok || a.teams[ref.TeamID] == "" {
			continue
		}
		out = append(out, relationOut{Relation: rel, Kind: kind, Other: &ref})
	}
	return out
}

func (s *Server) handleSubIssues(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	list, _, err := s.store.ListIssues(r.Context(), store.IssueFilter{ParentID: is.ID, AllowedTeamIDs: a.ids(), ViewerID: a.uid, Archived: "include"})
	if err != nil {
		s.fail(w, err, "sub-issues")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"issues": list})
}

// ---------- create ----------

// issueInput is the body of POST /issues and PATCH /issues/{ref}; absent
// fields stay as they are. Ids may be 0 (or null) to clear.
type issueInput struct {
	TeamID         *int64           `json:"team_id"`
	TeamKey        string           `json:"team"`
	Title          *string          `json:"title"`
	Description    *string          `json:"description"`
	StateID        *int64           `json:"state_id"`
	State          string           `json:"state"` // a state name or type ("In Progress", "completed")
	Priority       *int             `json:"priority"`
	Estimate       *json.RawMessage `json:"estimate"`
	AssigneeID     *int64           `json:"assignee_id"`
	Assignee       string           `json:"assignee"` // "me", an email or @name
	ProjectID      *int64           `json:"project_id"`
	MilestoneID    *int64           `json:"milestone_id"`
	CycleID        *int64           `json:"cycle_id"`
	Cycle          string           `json:"cycle"` // current | next | none
	ParentID       *json.RawMessage `json:"parent_id"`
	DueDate        *string          `json:"due_date"`
	LabelIDs       *[]int64         `json:"label_ids"`
	AddLabelIDs    []int64          `json:"add_label_ids"`
	RemoveLabelIDs []int64          `json:"remove_label_ids"`
	Labels         []string         `json:"labels"` // names (created on the team when missing, by members)
	SortOrder      *float64         `json:"sort_order"`
	SnoozedUntil   *string          `json:"snoozed_until"`
	Archived       *bool            `json:"archived"`
	TemplateID     int64            `json:"template_id"`
	SubIssues      []string         `json:"sub_issues"`
	SourceRef      string           `json:"source_ref"`
	SourceURL      string           `json:"source_url"`
	Subscribe      *bool            `json:"subscribe"`
}

func (s *Server) handleCreateIssue(w http.ResponseWriter, r *http.Request) {
	var in issueInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a := s.accessOf(r)
	// A retried request with the same Idempotency-Key (Pocket AI's tools send one) answers the issue it made.
	op, err := core.NewOperation(fmt.Sprintf("%d:%s:issues:create", a.uid, s.linkApp(r)), r.Header.Get("Idempotency-Key"), in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var receipt struct {
		ID int64 `json:"id"`
	}
	if done, err := op.Replay(r.Context(), s.store.DB, &receipt); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	} else if done {
		is, err := s.loadIssue(r, a, strconv.FormatInt(receipt.ID, 10), needRead)
		if err != nil {
			s.fail(w, err, "issue")
			return
		}
		writeJSON(w, http.StatusOK, is)
		return
	}
	is, err := s.createIssue(r.Context(), a, s.actorOf(r), in, opHook(r.Context(), op))
	if err != nil {
		s.fail(w, err, "create issue")
		return
	}
	writeJSON(w, http.StatusCreated, is)
}

// opHook saves an idempotency receipt {"id"} in the creating transaction.
func opHook(ctx context.Context, op *core.Operation) func(id int64) store.TxHook {
	return func(id int64) store.TxHook {
		return func(tx *sql.Tx) error {
			if op == nil {
				return nil
			}
			return op.Save(ctx, tx, map[string]int64{"id": id})
		}
	}
}

// createIssue validates and saves a new issue, then tells everyone concerned.
func (s *Server) createIssue(ctx context.Context, a *access, who actor, in issueInput, receipt ...func(id int64) store.TxHook) (*store.Issue, error) {
	var tmpl *store.Template
	var td store.TemplateData
	if in.TemplateID > 0 {
		t, err := s.store.GetTemplate(ctx, in.TemplateID)
		if err != nil || a.teams[t.TeamID] == "" {
			return nil, badRequest("unknown template")
		}
		tmpl = t
		_ = json.Unmarshal(t.Data, &td)
		if in.TeamID == nil && in.TeamKey == "" {
			in.TeamID = &t.TeamID
		}
	}
	teamID := int64(0)
	if in.TeamID != nil {
		teamID = *in.TeamID
	} else if in.TeamKey != "" {
		if t, err := s.store.GetTeamByKey(ctx, in.TeamKey); err == nil {
			teamID = t.ID
		}
	} else if st, _ := s.store.GetSettings(ctx, a.uid); st.DefaultTeamID > 0 && a.teams[st.DefaultTeamID] != "" {
		teamID = st.DefaultTeamID
	} else if ids := a.ids(); len(ids) == 1 {
		teamID = ids[0]
	}
	if teamID == 0 {
		return nil, badRequest("choose a team (team_id or team)")
	}
	if err := a.check(teamID, needWrite); err != nil {
		return nil, err
	}
	team, err := s.store.GetTeam(ctx, teamID)
	if err != nil {
		return nil, err
	}
	is := &store.Issue{TeamID: teamID, CreatorID: who.idPtr(), SourceApp: who.app, SourceRef: in.SourceRef, SourceURL: in.SourceURL, LabelIDs: []int64{}}
	if who.app == "git" || who.app == "system" {
		is.SourceApp = ""
	}
	if tmpl != nil {
		is.Title, is.Description = tmpl.Title, tmpl.Description
		if td.StateID != nil {
			is.StateID = *td.StateID
		}
		if td.Priority != nil {
			is.Priority = *td.Priority
		}
		is.Estimate, is.AssigneeID, is.ProjectID = td.Estimate, td.AssigneeID, td.ProjectID
		is.LabelIDs = append(is.LabelIDs, td.LabelIDs...)
		if td.DueInDays != nil {
			is.DueDate = time.Now().In(s.loc(ctx, a.uid)).AddDate(0, 0, *td.DueInDays).Format("2006-01-02")
		}
		if len(in.SubIssues) == 0 {
			in.SubIssues = td.SubIssues
		}
	}
	if _, err := s.applyIssueInput(ctx, a, who, team, is, in, true); err != nil {
		return nil, err
	}
	if strings.TrimSpace(is.Title) == "" {
		return nil, badRequest("an issue needs a title")
	}
	if is.StateID == 0 {
		is.StateID = s.defaultState(ctx, team, who, a)
	}
	hooks := []store.TxHook{s.lazyEvent(ctx, "projects.issue_created", is, who, nil)}
	for _, rc := range receipt {
		hooks = append(hooks, func(tx *sql.Tx) error { return rc(is.ID)(tx) })
	}
	if err := s.store.CreateIssue(ctx, is, who.id, who.app, hooks...); err != nil {
		return nil, err
	}
	if in.Subscribe != nil && !*in.Subscribe && who.id > 0 {
		_ = s.store.Subscribe(ctx, is.ID, who.id, false)
	}
	for _, title := range in.SubIssues {
		if title = strings.TrimSpace(title); title == "" {
			continue
		}
		sub := issueInput{TeamID: &is.TeamID, Title: &title, ProjectID: is.ProjectID, CycleID: is.CycleID}
		raw := json.RawMessage(strconv.FormatInt(is.ID, 10))
		sub.ParentID = &raw
		if is.AssigneeID != nil {
			sub.AssigneeID = is.AssigneeID
		}
		if _, err := s.createIssue(ctx, a, who, sub); err != nil {
			s.log.Warn("sub-issue", "parent", is.Identifier, "err", err)
		}
	}
	fresh, err := s.store.GetIssue(ctx, is.ID, a.uid)
	if err != nil {
		return nil, err
	}
	s.afterIssueCreate(ctx, who, fresh)
	return fresh, nil
}

// defaultState is where a new issue starts: triage when the team triages
// work from other apps or guests, else the team's default, else the first
// unstarted (or backlog) state.
func (s *Server) defaultState(ctx context.Context, team *store.Team, who actor, a *access) int64 {
	if team.TriageEnabled && (who.app != "" || a.teams[team.ID] == "guest") {
		if st := s.store.FirstStateOfType(ctx, team.ID, "triage"); st != nil {
			return st.ID
		}
	}
	if team.DefaultStateID != nil {
		return *team.DefaultStateID
	}
	for _, typ := range []string{"unstarted", "backlog", "started"} {
		if st := s.store.FirstStateOfType(ctx, team.ID, typ); st != nil {
			return st.ID
		}
	}
	states, _ := s.store.TeamStates(ctx, team.ID)
	if len(states) > 0 {
		return states[0].ID
	}
	return 0
}

// ---------- apply a change ----------

// applyIssueInput validates in and applies it to is, returning the history
// of what changed (for a new issue the history is not used).
func (s *Server) applyIssueInput(ctx context.Context, a *access, who actor, team *store.Team, is *store.Issue, in issueInput, creating bool) ([]store.History, error) {
	var hist []store.History
	rec := func(field, from, to, fromLabel, toLabel string) {
		if from == to && field != "description" {
			return
		}
		hist = append(hist, store.History{ActorID: who.idPtr(), ActorApp: who.app, Field: field, FromValue: from, ToValue: to, FromLabel: fromLabel, ToLabel: toLabel})
	}
	names := newNamer(s, ctx)

	// Moving to another team first: states, labels and cycles depend on it.
	if !creating && (in.TeamID != nil && *in.TeamID != is.TeamID || in.TeamKey != "" && !strings.EqualFold(in.TeamKey, team.Key)) {
		var target *store.Team
		var err error
		if in.TeamID != nil {
			target, err = s.store.GetTeam(ctx, *in.TeamID)
		} else {
			target, err = s.store.GetTeamByKey(ctx, in.TeamKey)
		}
		if err != nil || a.teams[target.ID] == "" {
			return nil, badRequest("unknown team")
		}
		if err := a.check(target.ID, needWrite); err != nil {
			return nil, err
		}
		old := is.Identifier
		num, key, err := s.store.NextIssueNumber(ctx, target.ID)
		if err != nil {
			return nil, err
		}
		_ = s.store.AddAlias(ctx, old, is.ID)
		cur, _ := s.store.GetState(ctx, is.StateID)
		is.TeamID, is.Number, is.Identifier = target.ID, num, key+"-"+strconv.FormatInt(num, 10)
		if in.StateID == nil && in.State == "" {
			typ := "backlog"
			if cur != nil {
				typ = cur.Type
			}
			if st := s.store.FirstStateOfType(ctx, target.ID, typ); st != nil {
				is.StateID = st.ID
			} else {
				is.StateID = s.defaultState(ctx, target, who, a)
			}
		}
		var keep []int64
		labels, _ := s.store.TeamLabels(ctx, target.ID)
		byName := map[string]int64{}
		for _, l := range labels {
			byName[strings.ToLower(l.Name)] = l.ID
		}
		for _, lid := range is.LabelIDs {
			if l, err := s.store.GetLabel(ctx, lid); err == nil {
				if id, ok := byName[strings.ToLower(l.Name)]; ok {
					keep = append(keep, id)
				}
			}
		}
		is.LabelIDs = keep
		is.CycleID = nil
		if is.AssigneeID != nil && s.store.TeamRole(ctx, target.ID, *is.AssigneeID) == "" {
			is.AssigneeID = nil
		}
		rec("team", strconv.FormatInt(team.ID, 10), strconv.FormatInt(target.ID, 10), team.Name+" ("+old+")", target.Name+" ("+is.Identifier+")")
		team = target
	}

	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		if t == "" {
			return nil, badRequest("the title can't be empty")
		}
		if len(t) > 500 {
			t = t[:500]
		}
		rec("title", is.Title, t, "", "")
		is.Title = t
	}
	if in.Description != nil && *in.Description != is.Description {
		if !creating {
			rec("description", "", "", "", "")
		}
		is.Description = *in.Description
	}
	if in.State != "" && in.StateID == nil {
		states, _ := s.store.TeamStates(ctx, team.ID)
		for _, st := range states {
			if strings.EqualFold(st.Name, in.State) {
				in.StateID = &st.ID
				break
			}
		}
		if in.StateID == nil {
			for _, st := range states {
				if strings.EqualFold(st.Type, in.State) || (in.State == "done" && st.Type == "completed") {
					in.StateID = &st.ID
					break
				}
			}
		}
		if in.StateID == nil {
			return nil, badRequest("the team has no state called " + in.State)
		}
	}
	if in.StateID != nil && *in.StateID != is.StateID {
		st, err := s.store.GetState(ctx, *in.StateID)
		if err != nil || st.TeamID != team.ID {
			return nil, badRequest("that state belongs to another team")
		}
		rec("state", strconv.FormatInt(is.StateID, 10), strconv.FormatInt(st.ID, 10), names.state(is.StateID), st.Name)
		is.StateID = st.ID
		if st.Type != "triage" {
			is.SnoozedUntil = ""
		}
	}
	if in.Priority != nil {
		p := *in.Priority
		if p < 0 || p > 4 {
			return nil, badRequest("priority is 0 (none), 1 (urgent), 2 (high), 3 (medium) or 4 (low)")
		}
		rec("priority", strconv.Itoa(is.Priority), strconv.Itoa(p), priorityName(is.Priority), priorityName(p))
		is.Priority = p
	}
	if in.Estimate != nil {
		var est *float64
		raw := strings.TrimSpace(string(*in.Estimate))
		if raw != "null" && raw != "" && raw != `""` {
			var v float64
			if err := json.Unmarshal(*in.Estimate, &v); err != nil || v < 0 || v > 1000 {
				return nil, badRequest("estimate is a number of points")
			}
			est = &v
		}
		rec("estimate", fmtEst(is.Estimate), fmtEst(est), "", "")
		is.Estimate = est
	}
	if in.Assignee != "" && in.AssigneeID == nil {
		id := s.personByHandle(ctx, team.ID, a.uid, in.Assignee)
		if id < 0 {
			return nil, badRequest(in.Assignee + " is not on the team")
		}
		in.AssigneeID = &id
	}
	if in.AssigneeID != nil {
		var next *int64
		if *in.AssigneeID > 0 {
			if s.store.TeamRole(ctx, team.ID, *in.AssigneeID) == "" {
				return nil, badRequest("the assignee must be on the team")
			}
			next = ptr(*in.AssigneeID)
		}
		rec("assignee", idStr(is.AssigneeID), idStr(next), names.person(is.AssigneeID), names.person(next))
		is.AssigneeID = next
	}
	if in.ProjectID != nil {
		var next *int64
		if *in.ProjectID > 0 {
			p, err := s.store.GetProject(ctx, *in.ProjectID)
			if err != nil || p.DeletedAt != nil || !a.canAny(p.TeamIDs, needRead) {
				return nil, badRequest("unknown project")
			}
			next = ptr(p.ID)
		}
		if idStr(next) != idStr(is.ProjectID) {
			rec("project", idStr(is.ProjectID), idStr(next), names.project(is.ProjectID), names.project(next))
			is.ProjectID = next
			if in.MilestoneID == nil {
				is.MilestoneID = nil
			}
		}
	}
	if in.MilestoneID != nil {
		var next *int64
		if *in.MilestoneID > 0 {
			m, err := s.store.GetMilestone(ctx, *in.MilestoneID)
			if err != nil || is.ProjectID == nil || m.ProjectID != *is.ProjectID {
				return nil, badRequest("the milestone belongs to another project")
			}
			next = ptr(m.ID)
		}
		rec("milestone", idStr(is.MilestoneID), idStr(next), names.milestone(is.MilestoneID), names.milestone(next))
		is.MilestoneID = next
	}
	if in.Cycle != "" && in.CycleID == nil {
		today := s.today(ctx, a.uid)
		var c *store.Cycle
		switch in.Cycle {
		case "current":
			c = s.store.CurrentCycle(ctx, team.ID, today)
		case "next":
			c = s.store.NextCycle(ctx, team.ID, today)
		case "none":
			in.CycleID = ptr(int64(0))
		}
		if c != nil {
			in.CycleID = &c.ID
		} else if in.Cycle != "none" {
			return nil, badRequest("the team has no " + in.Cycle + " cycle")
		}
	}
	if in.CycleID != nil {
		var next *int64
		if *in.CycleID > 0 {
			c, err := s.store.GetCycle(ctx, *in.CycleID, "")
			if err != nil || c.TeamID != team.ID {
				return nil, badRequest("the cycle belongs to another team")
			}
			next = ptr(c.ID)
		}
		rec("cycle", idStr(is.CycleID), idStr(next), names.cycle(is.CycleID), names.cycle(next))
		is.CycleID = next
	}
	if in.ParentID != nil {
		var next *int64
		raw := strings.Trim(strings.TrimSpace(string(*in.ParentID)), `"`)
		if raw != "" && raw != "null" && raw != "0" {
			pid, err := s.issueID(ctx, raw)
			if err != nil {
				return nil, badRequest("unknown parent issue")
			}
			p, err := s.store.GetIssue(ctx, pid, a.uid)
			if err != nil || a.teams[p.TeamID] == "" || p.DeletedAt != nil {
				return nil, badRequest("unknown parent issue")
			}
			if !creating {
				// No loops: the new parent may not be the issue or one of its descendants.
				for cur := p; cur != nil; {
					if cur.ID == is.ID {
						return nil, badRequest("an issue can't be inside its own sub-issue")
					}
					if cur.ParentID == nil {
						break
					}
					cur, _ = s.store.GetIssue(ctx, *cur.ParentID, a.uid)
				}
			}
			next = ptr(p.ID)
			if creating && is.ProjectID == nil && p.ProjectID != nil {
				is.ProjectID = p.ProjectID
			}
		}
		rec("parent", idStr(is.ParentID), idStr(next), names.issue(is.ParentID), names.issue(next))
		is.ParentID = next
	}
	if in.DueDate != nil {
		d := strings.TrimSpace(*in.DueDate)
		if d != "" {
			if _, err := time.Parse("2006-01-02", d); err != nil {
				return nil, badRequest("due_date looks like 2026-10-23")
			}
		}
		rec("due_date", is.DueDate, d, "", "")
		is.DueDate = d
	}

	// Labels: replace, then add / remove, then names.
	labels := append([]int64(nil), is.LabelIDs...)
	if in.LabelIDs != nil {
		labels = append([]int64(nil), (*in.LabelIDs)...)
	}
	labels = append(labels, in.AddLabelIDs...)
	if len(in.RemoveLabelIDs) > 0 {
		drop := map[int64]bool{}
		for _, id := range in.RemoveLabelIDs {
			drop[id] = true
		}
		var kept []int64
		for _, id := range labels {
			if !drop[id] {
				kept = append(kept, id)
			}
		}
		labels = kept
	}
	if len(in.Labels) > 0 {
		teamLabels, _ := s.store.TeamLabels(ctx, team.ID)
		for _, name := range in.Labels {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			found := int64(0)
			for _, l := range teamLabels {
				if strings.EqualFold(l.Name, name) && !l.IsGroup {
					found = l.ID
				}
			}
			if found == 0 {
				l := &store.Label{TeamID: team.ID, Name: name, Color: "#94a3b8"}
				if err := s.store.SaveLabel(ctx, l); err != nil {
					return nil, err
				}
				found = l.ID
			}
			labels = append(labels, found)
		}
	}
	clean, err := s.cleanLabels(ctx, team.ID, labels)
	if err != nil {
		return nil, err
	}
	if !creating {
		before := map[int64]bool{}
		for _, id := range is.LabelIDs {
			before[id] = true
		}
		after := map[int64]bool{}
		for _, id := range clean {
			after[id] = true
			if !before[id] {
				rec("label_added", "", strconv.FormatInt(id, 10), "", names.label(id))
			}
		}
		for id := range before {
			if !after[id] {
				rec("label_removed", strconv.FormatInt(id, 10), "", names.label(id), "")
			}
		}
	}
	is.LabelIDs = clean

	if in.SortOrder != nil {
		is.SortOrder = *in.SortOrder
	}
	if in.SnoozedUntil != nil {
		v := strings.TrimSpace(*in.SnoozedUntil)
		if v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				if d, err2 := time.ParseInLocation("2006-01-02", v, s.loc(ctx, a.uid)); err2 == nil {
					t = d
				} else {
					return nil, badRequest("snoozed_until looks like 2026-10-25 or an RFC 3339 time")
				}
			}
			v = t.UTC().Format(time.RFC3339)
		}
		rec("snoozed", is.SnoozedUntil, v, "", "")
		is.SnoozedUntil = v
	}
	if in.Archived != nil {
		if *in.Archived && is.ArchivedAt == nil {
			now := store.Now()
			is.ArchivedAt = &now
			rec("archived", "", "1", "", "")
		} else if !*in.Archived && is.ArchivedAt != nil {
			is.ArchivedAt = nil
			rec("archived", "1", "", "", "")
		}
	}
	return hist, nil
}

// cleanLabels keeps the team's labels only, no group labels, and one label per group.
func (s *Server) cleanLabels(ctx context.Context, teamID int64, ids []int64) ([]int64, error) {
	all, err := s.store.TeamLabels(ctx, teamID)
	if err != nil {
		return nil, err
	}
	byID := map[int64]store.Label{}
	for _, l := range all {
		byID[l.ID] = l
	}
	group := map[int64]int64{} // group → chosen label (the last one wins)
	seen := map[int64]bool{}
	var out []int64
	for _, id := range ids {
		l, ok := byID[id]
		if !ok || l.IsGroup || seen[id] {
			continue
		}
		seen[id] = true
		if l.ParentID != nil {
			if prev, ok := group[*l.ParentID]; ok {
				for i, x := range out {
					if x == prev {
						out = append(out[:i], out[i+1:]...)
						break
					}
				}
			}
			group[*l.ParentID] = id
		}
		out = append(out, id)
	}
	if out == nil {
		out = []int64{}
	}
	return out, nil
}

// personByHandle resolves "me", an email or @name to a team member (-1 when not on the team; 0 for "none").
func (s *Server) personByHandle(ctx context.Context, teamID, uid int64, h string) int64 {
	h = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(h)), "@")
	switch h {
	case "me":
		return uid
	case "none", "nobody", "unassigned":
		return 0
	}
	members, _ := s.store.TeamMembers(ctx, teamID)
	for _, m := range members {
		if strings.EqualFold(m.Email, h) || strings.EqualFold(m.Username, h) || strings.EqualFold(strings.Split(m.Email, "@")[0], h) || strings.EqualFold(m.Name, h) {
			return m.ID
		}
	}
	return -1
}

func priorityName(p int) string {
	return [...]string{"No priority", "Urgent", "High", "Medium", "Low"}[max(0, min(p, 4))]
}

func fmtEst(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

func idStr(v *int64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatInt(*v, 10)
}

// namer resolves ids to the names history entries keep.
type namer struct {
	s   *Server
	ctx context.Context
}

func newNamer(s *Server, ctx context.Context) namer { return namer{s, ctx} }

func (n namer) state(id int64) string {
	if st, err := n.s.store.GetState(n.ctx, id); err == nil {
		return st.Name
	}
	return ""
}

func (n namer) person(id *int64) string {
	if id == nil {
		return ""
	}
	if u, err := n.s.store.GetUser(n.ctx, *id); err == nil {
		return firstNonEmpty(u.Name, u.Email)
	}
	return ""
}

func (n namer) project(id *int64) string {
	if id == nil {
		return ""
	}
	if refs, _ := n.s.store.ProjectRefs(n.ctx, []int64{*id}); len(refs) > 0 {
		return refs[*id].Name
	}
	return ""
}

func (n namer) milestone(id *int64) string {
	if id == nil {
		return ""
	}
	if m, err := n.s.store.GetMilestone(n.ctx, *id); err == nil {
		return m.Name
	}
	return ""
}

func (n namer) cycle(id *int64) string {
	if id == nil {
		return ""
	}
	if c, err := n.s.store.GetCycle(n.ctx, *id, ""); err == nil {
		return cycleName(c)
	}
	return ""
}

func cycleName(c *store.Cycle) string {
	if c.Name != "" {
		return c.Name
	}
	return fmt.Sprintf("Cycle %d", c.Number)
}

func (n namer) issue(id *int64) string {
	if id == nil {
		return ""
	}
	if refs, _ := n.s.store.IssueRefs(n.ctx, []int64{*id}); len(refs) > 0 {
		return refs[*id].Identifier
	}
	return ""
}

func (n namer) label(id int64) string {
	if l, err := n.s.store.GetLabel(n.ctx, id); err == nil {
		return l.Name
	}
	return ""
}

// ---------- update ----------

func (s *Server) handlePatchIssue(w http.ResponseWriter, r *http.Request) {
	var in issueInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, chi.URLParam(r, "ref"), needWrite)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	out, err := s.updateIssue(r.Context(), a, s.actorOf(r), is, in)
	if err != nil {
		s.fail(w, err, "update issue")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// updateIssue applies a change to an issue and tells everyone concerned.
func (s *Server) updateIssue(ctx context.Context, a *access, who actor, is *store.Issue, in issueInput) (*store.Issue, error) {
	team, err := s.store.GetTeam(ctx, is.TeamID)
	if err != nil {
		return nil, err
	}
	before := *is
	before.LabelIDs = append([]int64(nil), is.LabelIDs...)
	hist, err := s.applyIssueInput(ctx, a, who, team, is, in, false)
	if err != nil {
		return nil, err
	}
	if len(hist) == 0 && in.SortOrder == nil {
		return is, nil
	}
	topic := "projects.issue_updated"
	if newType := s.stateType(ctx, is.StateID); newType == "completed" && before.StateType != "completed" {
		topic = "projects.issue_completed"
	}
	if err := s.store.UpdateIssue(ctx, is, before.StateType, hist, s.lazyEvent(ctx, topic, is, who, map[string]any{"changes": fieldsOf(hist)})); err != nil {
		return nil, err
	}
	fresh, err := s.store.GetIssue(ctx, is.ID, a.uid)
	if err != nil {
		return nil, err
	}
	s.afterIssueUpdate(ctx, who, &before, fresh, hist)
	return fresh, nil
}

func (s *Server) stateType(ctx context.Context, id int64) string {
	if st, err := s.store.GetState(ctx, id); err == nil {
		return st.Type
	}
	return ""
}

func fieldsOf(hist []store.History) []string {
	var out []string
	for _, h := range hist {
		out = append(out, h.Field)
	}
	return out
}

// lazyEvent queues a Core event about an issue in the saving transaction.
// Everything that needs the database is looked up before the transaction
// starts: the hook runs while it holds SQLite's only connection.
func (s *Server) lazyEvent(ctx context.Context, topic string, is *store.Issue, who actor, extra map[string]any) store.TxHook {
	if !s.core.Configured() {
		return func(*sql.Tx) error { return nil }
	}
	p := map[string]any{"audience_emails": s.teamEmails(ctx, is.TeamID), "source": who.app}
	if who.id > 0 {
		p["user_email"] = s.userEmail(ctx, who.id)
	}
	if is.AssigneeID != nil {
		p["assignee_email"] = s.userEmail(ctx, *is.AssigneeID)
	}
	for k, v := range extra {
		p[k] = v
	}
	return func(tx *sql.Tx) error {
		p["id"], p["identifier"], p["title"], p["team_id"], p["state_id"] = is.ID, is.Identifier, is.Title, is.TeamID, is.StateID
		p["priority"], p["due_date"], p["project_id"], p["assignee_id"], p["url"] = is.Priority, is.DueDate, is.ProjectID, is.AssigneeID, who.base+"/issue/"+is.Identifier
		return s.coreEvent(ctx, topic, "issue:"+strconv.FormatInt(is.ID, 10), p)(tx)
	}
}

// afterIssueCreate notifies the assignee and the people mentioned, and tells the browsers.
func (s *Server) afterIssueCreate(ctx context.Context, who actor, is *store.Issue) {
	link := who.base + "/issue/" + is.Identifier
	if is.AssigneeID != nil {
		s.notifyPerson(ctx, store.Notification{UserID: *is.AssigneeID, Kind: "assigned", IssueID: &is.ID, TeamID: &is.TeamID, ActorID: who.idPtr(), ActorApp: who.app,
			Title: who.name + " assigned you " + is.Identifier, Body: is.Title}, link)
	}
	for _, uid := range s.mentionedPeople(ctx, []int64{is.TeamID}, is.Description) {
		_ = s.store.Subscribe(ctx, is.ID, uid, true)
		s.notifyPerson(ctx, store.Notification{UserID: uid, Kind: "mentioned", IssueID: &is.ID, TeamID: &is.TeamID, ActorID: who.idPtr(), ActorApp: who.app,
			Title: who.name + " mentioned you in " + is.Identifier, Body: is.Title}, link)
	}
	s.send(ctx, change{Kind: "issue", TeamIDs: []int64{is.TeamID}, IDs: []int64{is.ID}, ActorID: who.id})
	s.webhook(is.TeamID, "issue.created", is)
}

// afterIssueUpdate notifies about assignment, status and mentions, keeps a
// linked Pocket Tasks to-do in step and tells the browsers.
func (s *Server) afterIssueUpdate(ctx context.Context, who actor, before, is *store.Issue, hist []store.History) {
	link := who.base + "/issue/" + is.Identifier
	teams := []int64{is.TeamID}
	if before.TeamID != is.TeamID {
		teams = append(teams, before.TeamID)
	}
	for _, h := range hist {
		switch h.Field {
		case "assignee":
			if is.AssigneeID != nil {
				s.notifyPerson(ctx, store.Notification{UserID: *is.AssigneeID, Kind: "assigned", IssueID: &is.ID, TeamID: &is.TeamID, ActorID: who.idPtr(), ActorApp: who.app,
					Title: who.name + " assigned you " + is.Identifier, Body: is.Title}, link)
			}
		case "state":
			for _, uid := range s.store.Subscribers(ctx, is.ID) {
				s.notifyPerson(ctx, store.Notification{UserID: uid, Kind: "status", IssueID: &is.ID, TeamID: &is.TeamID, ActorID: who.idPtr(), ActorApp: who.app,
					Title: is.Identifier + " moved to " + h.ToLabel, Body: is.Title + " — by " + who.name}, link)
			}
		case "description":
			for _, uid := range newMentions(ctx, s, []int64{is.TeamID}, before.Description, is.Description) {
				_ = s.store.Subscribe(ctx, is.ID, uid, true)
				s.notifyPerson(ctx, store.Notification{UserID: uid, Kind: "mentioned", IssueID: &is.ID, TeamID: &is.TeamID, ActorID: who.idPtr(), ActorApp: who.app,
					Title: who.name + " mentioned you in " + is.Identifier, Body: is.Title}, link)
			}
		}
	}
	if is.StateType != before.StateType && (is.StateType == "completed" || before.StateType == "completed") {
		go s.syncLinkedTasks(context.WithoutCancel(ctx), is, is.StateType == "completed")
	}
	s.send(ctx, change{Kind: "issue", TeamIDs: teams, IDs: []int64{is.ID}, ActorID: who.id})
	event := "issue.updated"
	if is.StateType == "completed" && before.StateType != "completed" {
		event = "issue.completed"
	}
	s.webhook(is.TeamID, event, map[string]any{"issue": is, "changes": hist})
}

// ---------- bulk ----------

func (s *Server) handleBulkIssues(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs    []json.RawMessage `json:"ids"`
		Set    issueInput        `json:"set"`
		Action string            `json:"action"` // update (default) | delete | restore
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(in.IDs) > 500 {
		writeErr(w, http.StatusBadRequest, "at most 500 issues at once")
		return
	}
	ctx := r.Context()
	a := s.accessOf(r)
	who := s.actorOf(r)
	done, failed := 0, []map[string]any{}
	for _, raw := range in.IDs {
		ref := strings.Trim(string(raw), `"`)
		var err error
		switch in.Action {
		case "delete":
			var is *store.Issue
			if is, err = s.loadIssue(r, a, ref, needWrite); err == nil {
				err = s.trashIssue(ctx, who, is)
			}
		case "restore":
			// loadIssue refuses trashed issues outside /restore; load directly.
			var is *store.Issue
			var id int64
			if id, err = s.issueID(ctx, ref); err == nil {
				if is, err = s.store.GetIssue(ctx, id, a.uid); err == nil {
					err = a.check(is.TeamID, needWrite)
				}
			}
			if err == nil {
				err = s.store.RestoreIssue(ctx, is.ID, who.id)
				s.send(ctx, change{Kind: "issue", TeamIDs: []int64{is.TeamID}, IDs: []int64{is.ID}, ActorID: who.id})
			}
		default:
			var is *store.Issue
			if is, err = s.loadIssue(r, a, ref, needWrite); err == nil {
				_, err = s.updateIssue(ctx, a, who, is, in.Set)
			}
		}
		if err != nil {
			failed = append(failed, map[string]any{"id": ref, "error": err.Error()})
		} else {
			done++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"done": done, "failed": failed})
}

// ---------- trash ----------

func (s *Server) handleDeleteIssue(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, chi.URLParam(r, "ref"), needWrite)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	if err := s.trashIssue(r.Context(), s.actorOf(r), is); err != nil {
		s.fail(w, err, "delete issue")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) trashIssue(ctx context.Context, who actor, is *store.Issue) error {
	if err := s.store.TrashIssue(ctx, is.ID, who.id, who.app); err != nil {
		return err
	}
	s.send(ctx, change{Kind: "issue", TeamIDs: []int64{is.TeamID}, IDs: []int64{is.ID}, ActorID: who.id})
	s.webhook(is.TeamID, "issue.deleted", map[string]any{"id": is.ID, "identifier": is.Identifier, "title": is.Title})
	return nil
}

func (s *Server) handleRestoreIssue(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, chi.URLParam(r, "ref"), needWrite)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	if err := s.store.RestoreIssue(r.Context(), is.ID, me(r)); err != nil {
		s.fail(w, err, "restore issue")
		return
	}
	s.changed(r, "issue", []int64{is.TeamID}, is.ID)
	fresh, _ := s.store.GetIssue(r.Context(), is.ID, me(r))
	writeJSON(w, http.StatusOK, fresh)
}

func (s *Server) handleIssueHistory(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	h, err := s.store.IssueHistory(r.Context(), is.ID)
	if err != nil {
		s.fail(w, err, "history")
		return
	}
	writeJSON(w, http.StatusOK, h)
}

// ---------- relations ----------

func (s *Server) handleIssueRelations(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	writeJSON(w, http.StatusOK, s.relationsOut(r.Context(), a, is.ID))
}

func (s *Server) handleAddRelation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		Type  string `json:"type"`  // blocks | blocked_by | related | duplicate | duplicated_by
		Issue string `json:"issue"` // id or identifier
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, chi.URLParam(r, "ref"), needWrite)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	other, err := s.loadIssue(r, a, in.Issue, needRead)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "unknown issue "+in.Issue)
		return
	}
	if other.ID == is.ID {
		writeErr(w, http.StatusBadRequest, "an issue can't relate to itself")
		return
	}
	rel := &store.Relation{IssueID: is.ID, RelatedID: other.ID, Type: in.Type}
	switch in.Type {
	case "blocks", "related", "duplicate":
	case "blocked_by":
		rel = &store.Relation{IssueID: other.ID, RelatedID: is.ID, Type: "blocks"}
	case "duplicated_by":
		rel = &store.Relation{IssueID: other.ID, RelatedID: is.ID, Type: "duplicate"}
	default:
		writeErr(w, http.StatusBadRequest, "type is blocks, blocked_by, related, duplicate or duplicated_by")
		return
	}
	if err := s.store.AddRelation(ctx, rel, me(r)); err != nil {
		s.fail(w, err, "relation")
		return
	}
	_ = s.store.AddHistory(ctx, store.History{IssueID: &is.ID, ActorID: ptr(me(r)), ActorApp: s.linkApp(r), Field: "relation", ToValue: in.Type, ToLabel: other.Identifier})
	// Marking a duplicate closes it in the team's "Duplicate" (or first canceled) state.
	if rel.Type == "duplicate" {
		dup, _ := s.store.GetIssue(ctx, rel.IssueID, me(r))
		if dup != nil && dup.StateType != "canceled" && dup.StateType != "completed" && a.can(dup.TeamID, needWrite) {
			states, _ := s.store.TeamStates(ctx, dup.TeamID)
			var target *store.State
			for i, st := range states {
				if st.Type == "canceled" && (target == nil || strings.EqualFold(st.Name, "duplicate")) {
					target = &states[i]
				}
			}
			if target != nil {
				_, _ = s.updateIssue(ctx, a, s.actorOf(r), dup, issueInput{StateID: &target.ID})
			}
		}
	}
	s.changed(r, "issue", []int64{is.TeamID, other.TeamID}, is.ID, other.ID)
	writeJSON(w, http.StatusCreated, s.relationsOut(ctx, a, is.ID))
}

func (s *Server) handleDeleteRelation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rel, err := s.store.GetRelation(ctx, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "relation")
		return
	}
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, strconv.FormatInt(rel.IssueID, 10), needWrite)
	if err != nil {
		if is, err = s.loadIssue(r, a, strconv.FormatInt(rel.RelatedID, 10), needWrite); err != nil {
			s.fail(w, err, "relation")
			return
		}
	}
	if err := s.store.DeleteRelation(ctx, rel.ID); err != nil {
		s.fail(w, err, "relation")
		return
	}
	s.changed(r, "issue", []int64{is.TeamID}, rel.IssueID, rel.RelatedID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSubscription(w http.ResponseWriter, r *http.Request) {
	var in struct {
		On bool `json:"on"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	if err := s.store.Subscribe(r.Context(), is.ID, me(r), in.On); err != nil {
		s.fail(w, err, "subscribe")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"subscribed": in.On})
}

// ---------- comments ----------

func (s *Server) handleListComments(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	list, err := s.store.Comments(r.Context(), is.ID, 0)
	if err != nil {
		s.fail(w, err, "comments")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Body     string `json:"body"`
		ParentID int64  `json:"parent_id"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, chi.URLParam(r, "ref"), needRead) // guests may comment
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	op, err := core.NewOperation(fmt.Sprintf("%d:%s:issues:%d:comment", a.uid, s.linkApp(r), is.ID), r.Header.Get("Idempotency-Key"), in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var receipt struct {
		ID int64 `json:"id"`
	}
	if done, err := op.Replay(r.Context(), s.store.DB, &receipt); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	} else if done {
		c, err := s.store.GetComment(r.Context(), receipt.ID)
		if err != nil {
			s.fail(w, err, "comment")
			return
		}
		writeJSON(w, http.StatusOK, c)
		return
	}
	c, err := s.addComment(r.Context(), s.actorOf(r), is, in.Body, in.ParentID, opHook(r.Context(), op))
	if err != nil {
		s.fail(w, err, "comment")
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

// addComment saves a comment on an issue and notifies followers and the people mentioned.
func (s *Server) addComment(ctx context.Context, who actor, is *store.Issue, body string, parentID int64, receipt ...func(id int64) store.TxHook) (*store.Comment, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, badRequest("the comment is empty")
	}
	c := &store.Comment{IssueID: &is.ID, UserID: who.idPtr(), Body: body, SourceApp: who.app}
	if parentID > 0 {
		p, err := s.store.GetComment(ctx, parentID)
		if err != nil || p.IssueID == nil || *p.IssueID != is.ID {
			return nil, badRequest("unknown comment to reply to")
		}
		if p.ParentID != nil {
			parentID = *p.ParentID // one level of threads
		}
		c.ParentID = &parentID
	}
	payload := map[string]any{"issue_id": is.ID, "identifier": is.Identifier, "title": is.Title, "body": excerpt(body, 500),
		"url": who.base + "/issue/" + is.Identifier, "audience_emails": s.teamEmails(ctx, is.TeamID)}
	if who.id > 0 {
		payload["user_email"] = s.userEmail(ctx, who.id)
	}
	hooks := []store.TxHook{s.coreEvent(ctx, "projects.comment_added", "issue:"+strconv.FormatInt(is.ID, 10), payload)}
	for _, rc := range receipt {
		hooks = append(hooks, func(tx *sql.Tx) error { return rc(c.ID)(tx) })
	}
	if err := s.store.SaveComment(ctx, c, hooks...); err != nil {
		return nil, err
	}
	link := who.base + "/issue/" + is.Identifier
	mentioned := map[int64]bool{}
	for _, uid := range s.mentionedPeople(ctx, []int64{is.TeamID}, body) {
		mentioned[uid] = true
		_ = s.store.Subscribe(ctx, is.ID, uid, true)
		s.notifyPerson(ctx, store.Notification{UserID: uid, Kind: "mentioned", IssueID: &is.ID, TeamID: &is.TeamID, CommentID: &c.ID, ActorID: who.idPtr(), ActorApp: who.app,
			Title: who.name + " mentioned you on " + is.Identifier, Body: excerpt(body, 200)}, link)
	}
	for _, uid := range s.store.Subscribers(ctx, is.ID) {
		if mentioned[uid] {
			continue
		}
		s.notifyPerson(ctx, store.Notification{UserID: uid, Kind: "comment", IssueID: &is.ID, TeamID: &is.TeamID, CommentID: &c.ID, ActorID: who.idPtr(), ActorApp: who.app,
			Title: who.name + " commented on " + is.Identifier, Body: excerpt(body, 200)}, link)
	}
	s.send(ctx, change{Kind: "comment", TeamIDs: []int64{is.TeamID}, IDs: []int64{is.ID}, ActorID: who.id})
	s.webhook(is.TeamID, "comment.created", map[string]any{"issue": map[string]any{"id": is.ID, "identifier": is.Identifier, "title": is.Title}, "comment": c})
	return c, nil
}

// commentFor loads a comment and checks the person may see it (and, when own, wrote it).
func (s *Server) commentFor(r *http.Request, a *access, own bool) (*store.Comment, *store.Issue, error) {
	c, err := s.store.GetComment(r.Context(), idParam(r, "id"))
	if err != nil {
		return nil, nil, err
	}
	var is *store.Issue
	if c.IssueID != nil {
		if is, err = s.loadIssue(r, a, strconv.FormatInt(*c.IssueID, 10), needRead); err != nil {
			return nil, nil, err
		}
	} else if c.ProjectUpdateID != nil {
		u, err := s.store.GetUpdate(r.Context(), *c.ProjectUpdateID)
		if err != nil {
			return nil, nil, err
		}
		if _, err := s.loadProject(r, a, u.ProjectID, needRead); err != nil {
			return nil, nil, err
		}
	}
	if own && (c.UserID == nil || *c.UserID != a.uid) {
		return nil, nil, errForbidden("only the author can change a comment")
	}
	return c, is, nil
}

func (s *Server) handleEditComment(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Body string `json:"body"`
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Body) == "" {
		writeErr(w, http.StatusBadRequest, "the comment is empty")
		return
	}
	a := s.accessOf(r)
	c, is, err := s.commentFor(r, a, true)
	if err != nil {
		s.fail(w, err, "comment")
		return
	}
	before := c.Body
	c.Body = strings.TrimSpace(in.Body)
	if err := s.store.SaveComment(r.Context(), c); err != nil {
		s.fail(w, err, "comment")
		return
	}
	if is != nil {
		who := s.actorOf(r)
		for _, uid := range newMentions(r.Context(), s, []int64{is.TeamID}, before, c.Body) {
			s.notifyPerson(r.Context(), store.Notification{UserID: uid, Kind: "mentioned", IssueID: &is.ID, TeamID: &is.TeamID, CommentID: &c.ID, ActorID: who.idPtr(),
				Title: who.name + " mentioned you on " + is.Identifier, Body: excerpt(c.Body, 200)}, who.base+"/issue/"+is.Identifier)
		}
		s.changed(r, "comment", []int64{is.TeamID}, is.ID)
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	c, is, err := s.commentFor(r, a, false)
	if err != nil {
		s.fail(w, err, "comment")
		return
	}
	// Authors delete their own; team admins any.
	if (c.UserID == nil || *c.UserID != a.uid) && (is == nil || !a.can(is.TeamID, needAdmin)) {
		writeErr(w, http.StatusForbidden, "only the author or a team admin can delete a comment")
		return
	}
	if err := s.store.DeleteComment(r.Context(), c.ID); err != nil {
		s.fail(w, err, "comment")
		return
	}
	if is != nil {
		s.changed(r, "comment", []int64{is.TeamID}, is.ID)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleReaction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Emoji string `json:"emoji"`
	}
	if err := decode(r, &in); err != nil || in.Emoji == "" || len(in.Emoji) > 32 {
		writeErr(w, http.StatusBadRequest, "emoji is required")
		return
	}
	a := s.accessOf(r)
	c, is, err := s.commentFor(r, a, false)
	if err != nil {
		s.fail(w, err, "comment")
		return
	}
	if _, err := s.store.ToggleReaction(r.Context(), c.ID, a.uid, in.Emoji); err != nil {
		s.fail(w, err, "reaction")
		return
	}
	c, _ = s.store.GetComment(r.Context(), c.ID)
	if is != nil {
		s.changed(r, "comment", []int64{is.TeamID}, is.ID)
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleResolveComment(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Resolved bool `json:"resolved"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a := s.accessOf(r)
	c, is, err := s.commentFor(r, a, false)
	if err != nil {
		s.fail(w, err, "comment")
		return
	}
	if err := s.store.SetResolved(r.Context(), c.ID, in.Resolved); err != nil {
		s.fail(w, err, "comment")
		return
	}
	if is != nil {
		s.changed(r, "comment", []int64{is.TeamID}, is.ID)
	}
	c, _ = s.store.GetComment(r.Context(), c.ID)
	writeJSON(w, http.StatusOK, c)
}

// ---------- contacts on an issue ----------

func (s *Server) handleIssueContacts(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	list, err := s.store.ContactLinks(r.Context(), 0, is.ID)
	if err != nil {
		s.fail(w, err, "contacts")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type contactInput struct {
	ContactID string `json:"contact_id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Org       string `json:"org"`
	Role      string `json:"role"`
	URL       string `json:"url"`
}

func (in contactInput) link() (*store.ContactLink, error) {
	c := &store.ContactLink{ContactID: strings.TrimSpace(in.ContactID), Name: strings.TrimSpace(in.Name), Email: strings.ToLower(strings.TrimSpace(in.Email)),
		Org: strings.TrimSpace(in.Org), Role: strings.TrimSpace(in.Role), URL: strings.TrimSpace(in.URL)}
	if c.Name == "" {
		c.Name = c.Email
	}
	if c.Name == "" {
		return nil, badRequest("a contact needs a name or an email")
	}
	return c, nil
}

func (s *Server) handleAddIssueContact(w http.ResponseWriter, r *http.Request) {
	var in contactInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, chi.URLParam(r, "ref"), needWrite)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	c, err := in.link()
	if err != nil {
		s.fail(w, err, "contact")
		return
	}
	c.IssueID = &is.ID
	if c.URL == "" && c.ContactID != "" {
		c.URL = s.contactPageURL(r.Context(), me(r), c.ContactID)
	}
	if err := s.store.AddContactLink(r.Context(), c, me(r)); err != nil {
		s.fail(w, err, "contact")
		return
	}
	_ = s.store.AddHistory(r.Context(), store.History{IssueID: &is.ID, ActorID: ptr(me(r)), Field: "contact_added", ToLabel: c.Name})
	s.changed(r, "issue", []int64{is.TeamID}, is.ID)
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleDeleteContactLink(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, err := s.store.GetContactLink(ctx, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "contact")
		return
	}
	a := s.accessOf(r)
	var teams []int64
	if c.IssueID != nil {
		is, err := s.loadIssue(r, a, strconv.FormatInt(*c.IssueID, 10), needWrite)
		if err != nil {
			s.fail(w, err, "contact")
			return
		}
		teams = []int64{is.TeamID}
	} else if c.ProjectID != nil {
		p, err := s.loadProject(r, a, *c.ProjectID, needWrite)
		if err != nil {
			s.fail(w, err, "contact")
			return
		}
		teams = p.TeamIDs
	}
	if err := s.store.DeleteContactLink(ctx, c.ID); err != nil {
		s.fail(w, err, "contact")
		return
	}
	s.changed(r, "issue", teams)
	w.WriteHeader(http.StatusNoContent)
}

// sortIssuesBy sorts in place (used by integrations that merge lists).
func sortIssuesBy(list []*store.Issue, less func(a, b *store.Issue) bool) {
	sort.SliceStable(list, func(i, j int) bool { return less(list[i], list[j]) })
}
