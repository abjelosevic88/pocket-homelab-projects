package server

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// handleBootstrap answers everything the web app needs to start in one call.
func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	a := s.accessOf(r)
	teams, err := s.store.ListTeams(ctx, u.ID)
	if err != nil {
		s.fail(w, err, "teams")
		return
	}
	for i := range teams {
		s.fillTeam(ctx, &teams[i])
	}
	projects, _ := s.store.ListProjects(ctx, store.ProjectFilter{AllowedTeamIDs: a.ids()})
	var pids []int64
	for _, p := range projects {
		pids = append(pids, p.ID)
	}
	milestones, _ := s.store.MilestonesOf(ctx, pids)
	cycles, _ := s.store.TeamCycles(ctx, a.ids(), s.today(ctx, u.ID))
	views, _ := s.store.Views(ctx, u.ID, a.ids())
	favorites, _ := s.store.Favorites(ctx, u.ID)
	people, _ := s.store.SharedPeople(ctx, u.ID)
	templates, _ := s.store.Templates(ctx, a.ids())
	st, _ := s.store.GetSettings(ctx, u.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"user": u, "settings": st, "teams": teams, "projects": projects, "milestones": milestones, "cycles": cycles,
		"views": views, "favorites": favorites, "people": people, "templates": templates,
		"unread": s.store.UnreadCount(ctx, u.ID), "today": s.today(ctx, u.ID), "version": Version,
		"timer": s.store.RunningTimer(ctx, u.ID), "presets": store.TeamPresets, "core": s.core.Configured(),
		"core_connected": s.coreConnected(),
	})
}

// handleDashboard summarises the person's work: what is assigned, due,
// in progress, the current cycles and projects that need attention.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := s.accessOf(r)
	today := s.today(ctx, a.uid)
	base := store.IssueFilter{AllowedTeamIDs: a.ids(), ViewerID: a.uid, Today: today, AssigneeIDs: []int64{a.uid}, StateTypes: []string{"triage", "backlog", "unstarted", "started"}}
	mine, total, _ := s.store.ListIssues(ctx, withSort(base, "priority", 50))
	overdue := withSort(base, "due", 50)
	overdue.Due = "overdue"
	od, odTotal, _ := s.store.ListIssues(ctx, overdue)
	week := withSort(base, "due", 50)
	week.Due = "week"
	wk, wkTotal, _ := s.store.ListIssues(ctx, week)
	started := withSort(base, "updated", 50)
	started.StateTypes = []string{"started"}
	_, ipTotal, _ := s.store.ListIssues(ctx, started)
	triage := store.IssueFilter{AllowedTeamIDs: a.ids(), ViewerID: a.uid, StateTypes: []string{"triage"}, Limit: 1}
	_, trTotal, _ := s.store.ListIssues(ctx, triage)
	cycles, _ := s.store.TeamCycles(ctx, a.ids(), today)
	var current []store.Cycle
	for _, c := range cycles {
		if c.Status == "current" {
			current = append(current, c)
		}
	}
	projects, _ := s.store.ListProjects(ctx, store.ProjectFilter{AllowedTeamIDs: a.ids(), Statuses: []string{"started", "planned", "paused"}})
	var attention []map[string]any
	now := time.Now()
	for _, p := range projects {
		reasons := []string{}
		if p.Health == "at_risk" || p.Health == "off_track" {
			reasons = append(reasons, strings.ReplaceAll(p.Health, "_", " "))
		}
		if p.TargetDate != "" && p.TargetDate < today && p.Status != "completed" {
			reasons = append(reasons, "past its target date")
		}
		if p.LeadID != nil && *p.LeadID == a.uid && s.updateDue(p, now) {
			reasons = append(reasons, "needs an update")
		}
		if len(reasons) > 0 {
			attention = append(attention, map[string]any{"id": p.ID, "name": p.Name, "status": p.Status, "health": p.Health, "target_date": p.TargetDate,
				"progress": p.Progress.Percent, "reasons": reasons, "url": s.projectURL(r, p.ID)})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"today": today, "assigned_open": total, "overdue": odTotal, "due_this_week": wkTotal, "in_progress": ipTotal, "triage": trTotal,
		"unread": s.store.UnreadCount(ctx, a.uid), "my_issues": s.briefIssues(r, mine, 15), "overdue_issues": s.briefIssues(r, od, 15),
		"due_soon": s.briefIssues(r, wk, 15), "current_cycles": current, "projects_needing_attention": attention, "app_url": s.publicURL(r),
	})
}

func withSort(f store.IssueFilter, sortBy string, limit int) store.IssueFilter {
	f.Sort, f.Limit = sortBy, limit
	return f
}

// briefIssues is a compact form for summaries (and the MCP tools).
func (s *Server) briefIssues(r *http.Request, list []*store.Issue, n int) []map[string]any {
	out := []map[string]any{}
	states := map[int64]string{}
	for i, is := range list {
		if i == n {
			break
		}
		if _, ok := states[is.StateID]; !ok {
			if st, err := s.store.GetState(r.Context(), is.StateID); err == nil {
				states[is.StateID] = st.Name
			}
		}
		out = append(out, map[string]any{"id": is.ID, "identifier": is.Identifier, "title": is.Title, "state": states[is.StateID], "state_type": is.StateType,
			"priority": priorityName(is.Priority), "due_date": is.DueDate, "url": s.issueURL(r, is.Identifier)})
	}
	return out
}

// handleSearch looks for issues, projects, documents and comments.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	a := s.accessOf(r)
	out := map[string]any{"issues": []any{}, "projects": []any{}, "docs": []store.SearchHit{}, "comments": []store.SearchHit{}}
	if q == "" {
		writeJSON(w, http.StatusOK, out)
		return
	}
	limit := queryInt(r, "limit")
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	issues, _, _ := s.store.ListIssues(ctx, store.IssueFilter{AllowedTeamIDs: a.ids(), ViewerID: a.uid, Q: q, Archived: "include", Sort: "updated", Limit: limit, Snoozed: "include"})
	out["issues"] = issues
	projects, _ := s.store.ListProjects(ctx, store.ProjectFilter{AllowedTeamIDs: a.ids(), Q: q})
	if len(projects) > limit {
		projects = projects[:limit]
	}
	out["projects"] = projects
	hits, _ := s.store.Search(ctx, a.ids(), q, limit)
	var docs, comments []store.SearchHit
	for _, h := range hits {
		if h.Kind == "doc" || h.Kind == "shared_note" {
			docs = append(docs, h)
		} else {
			comments = append(comments, h)
		}
	}
	if docs != nil {
		out["docs"] = docs
	}
	if comments != nil {
		out["comments"] = comments
	}
	writeJSON(w, http.StatusOK, out)
}

// handleRelatedByURL lists the projects and issues that link to an address:
// a contact's page (stakeholders, issue contacts), a note, any resource.
// Pocket Contacts and Pocket Notes ask this with projects:read.
func (s *Server) handleRelatedByURL(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	raw := strings.TrimSpace(r.URL.Query().Get("url"))
	if raw == "" {
		writeErr(w, http.StatusBadRequest, "url is required")
		return
	}
	a := s.accessOf(r)
	variants := []string{raw, strings.TrimRight(raw, "/")}
	seenP, seenI := map[int64]bool{}, map[int64]bool{}
	projects := []map[string]any{}
	issues := []map[string]any{}
	for _, u := range variants {
		ps, _ := s.store.ListProjects(ctx, store.ProjectFilter{AllowedTeamIDs: a.ids(), URL: u})
		for _, p := range ps {
			if !seenP[p.ID] {
				seenP[p.ID] = true
				projects = append(projects, map[string]any{"id": p.ID, "name": p.Name, "status": p.Status, "health": p.Health, "target_date": p.TargetDate,
					"progress": p.Progress.Percent, "url": s.projectURL(r, p.ID)})
			}
		}
		is, _, _ := s.store.ListIssues(ctx, store.IssueFilter{AllowedTeamIDs: a.ids(), ViewerID: a.uid, URL: u, Sort: "updated", Limit: 50, Snoozed: "include"})
		for _, x := range is {
			if !seenI[x.ID] {
				seenI[x.ID] = true
				issues = append(issues, map[string]any{"id": x.ID, "identifier": x.Identifier, "title": x.Title, "state_type": x.StateType,
					"done": x.StateType == "completed" || x.StateType == "canceled", "due_date": x.DueDate, "url": s.issueURL(r, x.Identifier)})
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": projects, "issues": issues, "app_url": s.publicURL(r)})
}

// CalendarItem is one dated thing for a calendar: an issue's due date, a
// project's start or target, a milestone, a cycle.
type CalendarItem struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"` // issue | project_start | project_target | milestone | cycle
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	Date     string `json:"date"`
	End      string `json:"end,omitempty"` // inclusive last day (cycles)
	Color    string `json:"color"`
	URL      string `json:"url"`
	Done     bool   `json:"done"`
	Mine     bool   `json:"mine"`
	RefID    int64  `json:"ref_id"`
	TeamID   int64  `json:"team_id,omitempty"`
}

// calendarItems collects the items between from and to (dates, inclusive).
// scope: mine = issues assigned to the person and their projects; all = everything on their teams.
func (s *Server) calendarItems(r *http.Request, a *access, from, to, scope string, cycles bool) []CalendarItem {
	ctx := r.Context()
	out := []CalendarItem{}
	f := store.IssueFilter{AllowedTeamIDs: a.ids(), ViewerID: a.uid, DueFrom: from, DueTo: to, Limit: 2000, Archived: "include", Snoozed: "include"}
	if scope != "all" {
		f.AssigneeIDs = []int64{a.uid}
	}
	issues, _, _ := s.store.ListIssues(ctx, f)
	teamColor := map[int64]string{}
	teams, _ := s.store.ListTeams(ctx, a.uid)
	for _, t := range teams {
		teamColor[t.ID] = t.Color
	}
	for _, is := range issues {
		out = append(out, CalendarItem{ID: "issue:" + strconv.FormatInt(is.ID, 10), Kind: "issue", Title: is.Identifier + " " + is.Title, Subtitle: priorityName(is.Priority),
			Date: is.DueDate, Color: teamColor[is.TeamID], URL: s.issueURL(r, is.Identifier), Done: is.StateType == "completed" || is.StateType == "canceled",
			Mine: is.AssigneeID != nil && *is.AssigneeID == a.uid, RefID: is.ID, TeamID: is.TeamID})
	}
	pf := store.ProjectFilter{AllowedTeamIDs: a.ids()}
	if scope != "all" {
		pf.MemberID = a.uid
	}
	projects, _ := s.store.ListProjects(ctx, pf)
	var pids []int64
	pname := map[int64]*store.Project{}
	for _, p := range projects {
		pids = append(pids, p.ID)
		pname[p.ID] = p
		done := p.Status == "completed" || p.Status == "canceled"
		color := firstNonEmpty(p.Color, "#5e6ad2")
		if p.StartDate != "" && p.StartDate >= from && p.StartDate <= to {
			out = append(out, CalendarItem{ID: "project-start:" + strconv.FormatInt(p.ID, 10), Kind: "project_start", Title: "Start: " + p.Name, Date: p.StartDate,
				Color: color, URL: s.projectURL(r, p.ID), Done: done, RefID: p.ID})
		}
		if p.TargetDate != "" && p.TargetDate >= from && p.TargetDate <= to {
			out = append(out, CalendarItem{ID: "project-target:" + strconv.FormatInt(p.ID, 10), Kind: "project_target", Title: "Target: " + p.Name,
				Subtitle: fmt.Sprintf("%.0f%% done", p.Progress.Percent), Date: p.TargetDate, Color: color, URL: s.projectURL(r, p.ID), Done: done, RefID: p.ID})
		}
	}
	ms, _ := s.store.MilestonesOf(ctx, pids)
	for _, m := range ms {
		if m.TargetDate == "" || m.TargetDate < from || m.TargetDate > to {
			continue
		}
		p := pname[m.ProjectID]
		out = append(out, CalendarItem{ID: "milestone:" + strconv.FormatInt(m.ID, 10), Kind: "milestone", Title: "◆ " + m.Name, Subtitle: p.Name, Date: m.TargetDate,
			Color: firstNonEmpty(p.Color, "#5e6ad2"), URL: s.projectURL(r, p.ID), RefID: m.ID})
	}
	if cycles {
		cs, _ := s.store.TeamCycles(ctx, a.ids(), s.today(ctx, a.uid))
		for _, c := range cs {
			if c.EndsOn < from || c.StartsOn > to {
				continue
			}
			key := ""
			for _, t := range teams {
				if t.ID == c.TeamID {
					key = t.Key
				}
			}
			out = append(out, CalendarItem{ID: "cycle:" + strconv.FormatInt(c.ID, 10), Kind: "cycle", Title: key + " " + cycleName(&c), Date: c.StartsOn, End: c.EndsOn,
				Color: teamColor[c.TeamID], URL: s.publicURL(r) + "/team/" + key + "/cycles/" + strconv.FormatInt(c.ID, 10), Done: c.Status == "past", RefID: c.ID, TeamID: c.TeamID})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

func (s *Server) handleCalendar(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if from == "" || to == "" || !validDate(from) || !validDate(to) || to < from {
		writeErr(w, http.StatusBadRequest, "from and to are dates (YYYY-MM-DD)")
		return
	}
	if f, _ := time.Parse("2006-01-02", from); f.AddDate(0, 0, 400).Format("2006-01-02") < to {
		writeErr(w, http.StatusBadRequest, "at most 400 days at a time")
		return
	}
	scope := r.URL.Query().Get("scope")
	writeJSON(w, http.StatusOK, map[string]any{"items": s.calendarItems(r, a, from, to, scope, r.URL.Query().Get("cycles") != "0"), "from": from, "to": to})
}

// handleFeed serves the person's secret iCalendar feed: their issues' due
// dates, their projects' targets and milestones.
func (s *Server) handleFeed(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, err := s.store.GetUserByFeedToken(ctx, chi.URLParam(r, "token"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a := s.accessFor(ctx, u.ID)
	today := time.Now().In(s.loc(ctx, u.ID))
	items := s.calendarItems(r, a, today.AddDate(0, -3, 0).Format("2006-01-02"), today.AddDate(1, 0, 0).Format("2006-01-02"), "mine", false)
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Pocket Projects//EN\r\nCALSCALE:GREGORIAN\r\nX-WR-CALNAME:Pocket Projects\r\nX-PUBLISHED-TTL:PT1H\r\n")
	stamp := time.Now().UTC().Format("20060102T150405Z")
	for _, it := range items {
		d, _ := time.Parse("2006-01-02", it.Date)
		title := it.Title
		if it.Done {
			title = "✓ " + title
		}
		b.WriteString("BEGIN:VEVENT\r\n")
		b.WriteString("UID:" + strings.ReplaceAll(it.ID, ":", "-") + "@pocket-projects\r\n")
		b.WriteString("DTSTAMP:" + stamp + "\r\n")
		b.WriteString("DTSTART;VALUE=DATE:" + d.Format("20060102") + "\r\n")
		b.WriteString("DTEND;VALUE=DATE:" + d.AddDate(0, 0, 1).Format("20060102") + "\r\n")
		b.WriteString(icsLine("SUMMARY", title))
		if it.Subtitle != "" {
			b.WriteString(icsLine("DESCRIPTION", it.Subtitle+"\n"+it.URL))
		} else {
			b.WriteString(icsLine("DESCRIPTION", it.URL))
		}
		b.WriteString(icsLine("URL", it.URL))
		b.WriteString("TRANSP:TRANSPARENT\r\nEND:VEVENT\r\n")
	}
	b.WriteString("END:VCALENDAR\r\n")
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=600")
	_, _ = w.Write([]byte(b.String()))
}

// icsLine escapes a property value and folds it at 75 octets.
func icsLine(name, value string) string {
	v := strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`).Replace(value)
	line := name + ":" + v
	var b strings.Builder
	for len(line) > 75 {
		cut := 75
		for cut > 0 && (line[cut]&0xC0) == 0x80 {
			cut--
		}
		b.WriteString(line[:cut] + "\r\n ")
		line = line[cut:]
	}
	b.WriteString(line + "\r\n")
	return b.String()
}

// handleInsights computes the charts for a team (team_id) or a project (project_id).
func (s *Server) handleInsights(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := s.accessOf(r)
	today := s.today(ctx, a.uid)
	to := r.URL.Query().Get("to")
	if !validDate(to) || to == "" {
		to = today
	}
	from := r.URL.Query().Get("from")
	if !validDate(from) || from == "" {
		t, _ := time.Parse("2006-01-02", to)
		from = t.AddDate(0, 0, -7*12).Format("2006-01-02")
	}
	teams := a.ids()
	if v := queryIDs(r, "team_id"); len(v) > 0 {
		teams = intersectIDs(teams, v)
	}
	pid := queryInt64(r, "project_id")
	if pid > 0 {
		p, err := s.loadProject(r, a, pid, needRead)
		if err != nil {
			s.fail(w, err, "project")
			return
		}
		teams = intersectIDs(a.ids(), p.TeamIDs)
	}
	in, err := s.store.InsightsFor(ctx, teams, pid, from, to, today)
	if err != nil {
		s.fail(w, err, "insights")
		return
	}
	writeJSON(w, http.StatusOK, in)
}
