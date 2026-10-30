package server

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// The worklog (docs/worklog.md in pocket-homelab-time): a person's own time
// on issues for Pocket Time to show (worklog:read), and the time tracked in
// Pocket Time for an issue written back here (worklog:write), idempotent on
// external_id. A Pocket app acting through a link changes and removes only
// the rows it made itself. Everything is the person's own time only.

func init() {
	extraRoutes = append(extraRoutes, func(s *Server, r chi.Router) {
		r.Get("/worklog", s.handleWorklog)
		r.Get("/worklog/items", s.handleWorklogItems)
		r.Post("/worklog", s.handleWorklogSave)
		r.Delete("/worklog/{id}", s.handleWorklogDelete)
	})
}

// worklogMaxMinutes is the longest single record (Pocket Time allows a week).
const worklogMaxMinutes = 7 * 24 * 60

func issueLabel(ident, title string) string { return strings.TrimSpace(ident + " " + title) }

// handleWorklog lists the person's finished time between from and to (inclusive).
func (s *Server) handleWorklog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := me(r)
	today := s.today(ctx, uid)
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if !validDate(from) || !validDate(to) {
		writeErr(w, http.StatusBadRequest, "from and to are dates (YYYY-MM-DD)")
		return
	}
	if to == "" {
		to = today
	}
	if from == "" {
		t, _ := time.Parse("2006-01-02", to)
		from = t.AddDate(0, 0, -30).Format("2006-01-02")
	}
	list, err := s.store.FinishedTimeOf(ctx, uid, from, to)
	if err != nil {
		s.fail(w, err, "worklog")
		return
	}
	ids := make([]int64, 0, len(list))
	for _, e := range list {
		ids = append(ids, e.IssueID)
	}
	refs, _ := s.store.IssueRefs(ctx, ids)
	a := s.accessOf(r)
	loc := s.loc(ctx, uid)
	out := []map[string]any{}
	for _, e := range list {
		ref, ok := refs[e.IssueID]
		if !ok || !a.can(ref.TeamID, needRead) {
			continue // left the team: no longer theirs to show
		}
		row := map[string]any{"id": strconv.FormatInt(e.ID, 10), "title": ref.Title, "date": e.SpentOn, "duration_s": e.Minutes * 60,
			"ref": "issue:" + ref.Identifier, "label": issueLabel(ref.Identifier, ref.Title), "url": "/issue/" + ref.Identifier,
			"source": firstNonEmpty(e.Source, "app"), "external_id": e.ExternalID}
		if t := store.ParseTime(e.BeganAt); !t.IsZero() {
			row["started_at"] = t.In(loc).Format(time.RFC3339)
			row["ended_at"] = t.Add(time.Duration(e.Minutes) * time.Minute).In(loc).Format(time.RFC3339)
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out})
}

// handleWorklogItems lists what time can be for: the issues the person
// tracked time on lately, those assigned to them, then recently changed
// ones on their teams; q searches.
func (s *Server) handleWorklogItems(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := s.accessOf(r)
	var teams []int64
	for _, id := range a.ids() {
		if a.can(id, needWrite) {
			teams = append(teams, id)
		}
	}
	const limit = 50
	var list []*store.Issue
	seen := map[int64]bool{}
	add := func(is []*store.Issue) {
		for _, x := range is {
			if !seen[x.ID] && len(list) < limit {
				seen[x.ID] = true
				list = append(list, x)
			}
		}
	}
	open := []string{"triage", "backlog", "unstarted", "started"}
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		is, _, err := s.store.ListIssues(ctx, store.IssueFilter{AllowedTeamIDs: teams, Q: q, Sort: "updated", Limit: limit, ViewerID: a.uid})
		if err != nil {
			s.fail(w, err, "issues")
			return
		}
		add(is)
	} else {
		if recent := s.store.RecentTimeIssues(ctx, a.uid, 15); len(recent) > 0 {
			is, _, _ := s.store.ListIssues(ctx, store.IssueFilter{AllowedTeamIDs: teams, IDs: recent, StateTypes: open, Limit: 15, ViewerID: a.uid})
			pos := map[int64]int{}
			for i, id := range recent {
				pos[id] = i
			}
			sort.SliceStable(is, func(i, j int) bool { return pos[is[i].ID] < pos[is[j].ID] })
			add(is)
		}
		mine, _, _ := s.store.ListIssues(ctx, store.IssueFilter{AllowedTeamIDs: teams, AssigneeIDs: []int64{a.uid}, StateTypes: open, Sort: "updated", Limit: limit, ViewerID: a.uid})
		add(mine)
		rest, _, _ := s.store.ListIssues(ctx, store.IssueFilter{AllowedTeamIDs: teams, StateTypes: open, Sort: "updated", Limit: limit, ViewerID: a.uid})
		add(rest)
	}
	teamName := map[int64]string{}
	if ts, err := s.store.ListTeams(ctx, a.uid); err == nil {
		for _, t := range ts {
			teamName[t.ID] = t.Name
		}
	}
	stateName := map[int64]string{}
	if sts, err := s.store.StatesOf(ctx, teams); err == nil {
		for _, st := range sts {
			stateName[st.ID] = st.Name
		}
	}
	out := []map[string]any{}
	for _, is := range list {
		detail := teamName[is.TeamID]
		if n := stateName[is.StateID]; n != "" {
			detail = strings.TrimPrefix(detail+" · "+n, " · ")
		}
		out = append(out, map[string]any{"ref": "issue:" + is.Identifier, "label": issueLabel(is.Identifier, is.Title), "detail": detail, "url": "/issue/" + is.Identifier})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// handleWorklogSave logs time tracked elsewhere (Pocket Time) on an issue;
// the same external_id again updates that record.
func (s *Server) handleWorklogSave(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		Ref        string `json:"ref"`
		Title      string `json:"title"`
		Note       string `json:"note"`
		StartedAt  string `json:"started_at"`
		EndedAt    string `json:"ended_at"`
		Date       string `json:"date"`
		DurationS  *int   `json:"duration_s"`
		ExternalID string `json:"external_id"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	key := strings.TrimSpace(in.Ref)
	if k, ok := strings.CutPrefix(key, "issue:"); ok {
		key = strings.TrimSpace(k)
	} else if key != "" {
		writeErr(w, http.StatusBadRequest, "the ref is an issue, like issue:WEB-12")
		return
	}
	if key == "" {
		writeErr(w, http.StatusBadRequest, "say which issue the time is for (ref issue:WEB-12)")
		return
	}
	a := s.accessOf(r)
	var is *store.Issue
	if id, err := s.store.IssueIDByIdentifier(ctx, key); err == nil {
		if x, err := s.store.GetIssue(ctx, id, a.uid); err == nil && x.DeletedAt == nil && a.can(x.TeamID, needWrite) {
			is = x
		}
	}
	if is == nil {
		writeErr(w, http.StatusBadRequest, "there is no issue "+key+" you can log time on")
		return
	}
	began := store.ParseTime(in.StartedAt)
	dur := -1
	if in.DurationS != nil {
		dur = *in.DurationS
	} else if end := store.ParseTime(in.EndedAt); !began.IsZero() && !end.Before(began) {
		dur = int(end.Sub(began).Seconds())
	}
	// at least a minute, like a timer stopped here at once
	minutes := max(1, (dur+30)/60)
	if dur < 0 || minutes > worklogMaxMinutes {
		writeErr(w, http.StatusBadRequest, "say how long, up to a week (duration_s, or started_at and ended_at)")
		return
	}
	date := strings.TrimSpace(in.Date)
	if date != "" && !validDate(date) {
		writeErr(w, http.StatusBadRequest, "date looks like 2026-10-28")
		return
	}
	if date == "" {
		if !began.IsZero() {
			date = began.In(s.loc(ctx, a.uid)).Format("2006-01-02")
		} else {
			date = s.today(ctx, a.uid)
		}
	}
	src := s.source(r)
	ext := strings.TrimSpace(in.ExternalID)
	if len(ext) > 200 {
		writeErr(w, http.StatusBadRequest, "external_id is at most 200 characters")
		return
	}
	note := strings.TrimSpace(in.Note)
	if len([]rune(note)) > 4000 {
		note = string([]rune(note)[:4000])
	}
	t := &store.TimeEntry{IssueID: is.ID, UserID: a.uid, Source: src, ExternalID: ext}
	var oldIssue int64
	if ext != "" {
		if cur, err := s.store.TimeEntryByExternal(ctx, a.uid, src, ext); err == nil {
			t, oldIssue = cur, cur.IssueID
			t.IssueID = is.ID
		}
	}
	t.SpentOn, t.Minutes, t.Note, t.StartedAt = date, minutes, note, ""
	t.BeganAt = ""
	if !began.IsZero() {
		t.BeganAt = began.UTC().Format(time.RFC3339)
	}
	if err := s.store.SaveTimeEntry(ctx, t); err != nil {
		s.fail(w, err, "time")
		return
	}
	teams := []int64{is.TeamID}
	if oldIssue != 0 && oldIssue != is.ID {
		if old, err := s.store.GetIssue(ctx, oldIssue, 0); err == nil {
			teams = append(teams, old.TeamID)
			s.changed(r, "issue", []int64{old.TeamID}, old.ID)
		}
	}
	s.changed(r, "issue", teams, is.ID)
	s.send(ctx, change{Kind: "timer", users: []int64{a.uid}})
	writeJSON(w, http.StatusOK, map[string]any{"id": strconv.FormatInt(t.ID, 10), "url": "/issue/" + is.Identifier})
}

// handleWorklogDelete removes one of the person's records; a linked app
// only those it made.
func (s *Server) handleWorklogDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	t, err := s.store.GetTimeEntry(ctx, id)
	if err != nil || t.UserID != me(r) || t.StartedAt != "" {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if app := s.linkApp(r); app != "" && t.Source != app {
		writeErr(w, http.StatusForbidden, "this time was not logged by that app")
		return
	}
	if err := s.store.DeleteTimeEntry(ctx, t.ID); err != nil {
		s.fail(w, err, "time")
		return
	}
	if is, err := s.store.GetIssue(ctx, t.IssueID, 0); err == nil {
		s.changed(r, "issue", []int64{is.TeamID}, is.ID)
	}
	s.send(ctx, change{Kind: "timer", users: []int64{me(r)}})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
