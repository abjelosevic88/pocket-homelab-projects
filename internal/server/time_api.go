package server

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// Time tracking: entries per issue and person, a running timer per person,
// a timesheet over a date range (CSV export for invoicing).

func (s *Server) timeRoutes(r chi.Router) {
	r.Get("/issues/{ref}/time", s.handleIssueTime)
	r.Post("/issues/{ref}/time", s.handleLogTime)
	r.Post("/issues/{ref}/timer", s.handleStartTimer)
	r.Get("/timer", s.handleTimer)
	r.Post("/timer/stop", s.handleStopTimer)
	r.Patch("/time/{id}", s.handleEditTime)
	r.Delete("/time/{id}", s.handleDeleteTime)
	r.Get("/time", s.handleTimesheet)
}

func (s *Server) handleIssueTime(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	list, err := s.store.TimeEntries(r.Context(), is.ID)
	if err != nil {
		s.fail(w, err, "time")
		return
	}
	total := 0
	for _, t := range list {
		total += t.Minutes
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": list, "total_minutes": total})
}

// parseMinutes reads "90", "1h30m", "1.5h", "45m".
func parseMinutes(v string) (int, error) {
	v = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(v), " ", ""))
	if n, err := strconv.Atoi(v); err == nil {
		return n, nil
	}
	if strings.HasSuffix(v, "h") {
		if f, err := strconv.ParseFloat(strings.TrimSuffix(v, "h"), 64); err == nil {
			return int(f*60 + .5), nil
		}
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, err
	}
	return int(d.Minutes() + .5), nil
}

func (s *Server) handleLogTime(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Minutes  int    `json:"minutes"`
		Duration string `json:"duration"`
		SpentOn  string `json:"spent_on"`
		Note     string `json:"note"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Duration != "" {
		m, err := parseMinutes(in.Duration)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "duration looks like 1h30m, 90 or 1.5h")
			return
		}
		in.Minutes = m
	}
	if in.Minutes <= 0 || in.Minutes > 24*60 {
		writeErr(w, http.StatusBadRequest, "log between 1 minute and 24 hours")
		return
	}
	if in.SpentOn == "" {
		in.SpentOn = s.today(r.Context(), me(r))
	} else if !validDate(in.SpentOn) {
		writeErr(w, http.StatusBadRequest, "spent_on looks like 2026-10-22")
		return
	}
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, chi.URLParam(r, "ref"), needWrite)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	t := &store.TimeEntry{IssueID: is.ID, UserID: a.uid, SpentOn: in.SpentOn, Minutes: in.Minutes, Note: strings.TrimSpace(in.Note)}
	if err := s.store.SaveTimeEntry(r.Context(), t); err != nil {
		s.fail(w, err, "time")
		return
	}
	s.changed(r, "issue", []int64{is.TeamID}, is.ID)
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) handleTimer(w http.ResponseWriter, r *http.Request) {
	t := s.store.RunningTimer(r.Context(), me(r))
	if t == nil {
		writeJSON(w, http.StatusOK, map[string]any{"running": false})
		return
	}
	refs, _ := s.store.IssueRefs(r.Context(), []int64{t.IssueID})
	ref := refs[t.IssueID]
	writeJSON(w, http.StatusOK, map[string]any{"running": true, "entry": t, "issue": ref})
}

func (s *Server) handleStartTimer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, chi.URLParam(r, "ref"), needWrite)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	if cur := s.store.RunningTimer(ctx, a.uid); cur != nil {
		s.stopTimer(r, cur)
	}
	t := &store.TimeEntry{IssueID: is.ID, UserID: a.uid, SpentOn: s.today(ctx, a.uid), StartedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := s.store.SaveTimeEntry(ctx, t); err != nil {
		s.fail(w, err, "timer")
		return
	}
	s.send(ctx, change{Kind: "timer", users: []int64{a.uid}})
	s.handleTimer(w, r)
}

func (s *Server) stopTimer(r *http.Request, t *store.TimeEntry) {
	started := store.ParseTime(t.StartedAt)
	t.Minutes += max(1, int(time.Since(started).Minutes()+.5))
	t.StartedAt = ""
	_ = s.store.SaveTimeEntry(r.Context(), t)
	if is, err := s.store.GetIssue(r.Context(), t.IssueID, 0); err == nil {
		s.changed(r, "issue", []int64{is.TeamID}, is.ID)
	}
}

func (s *Server) handleStopTimer(w http.ResponseWriter, r *http.Request) {
	t := s.store.RunningTimer(r.Context(), me(r))
	if t == nil {
		writeJSON(w, http.StatusOK, map[string]any{"running": false})
		return
	}
	var in struct {
		Note string `json:"note"`
	}
	_ = decode(r, &in)
	if in.Note != "" {
		t.Note = in.Note
	}
	s.stopTimer(r, t)
	s.send(r.Context(), change{Kind: "timer", users: []int64{me(r)}})
	writeJSON(w, http.StatusOK, map[string]any{"running": false, "entry": t})
}

func (s *Server) timeEntryFor(r *http.Request) (*store.TimeEntry, error) {
	t, err := s.store.GetTimeEntry(r.Context(), idParam(r, "id"))
	if err != nil {
		return nil, err
	}
	if t.UserID != me(r) {
		return nil, store.ErrNotFound
	}
	return t, nil
}

func (s *Server) handleEditTime(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Minutes *int    `json:"minutes"`
		SpentOn *string `json:"spent_on"`
		Note    *string `json:"note"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	t, err := s.timeEntryFor(r)
	if err != nil {
		s.fail(w, err, "time")
		return
	}
	if in.Minutes != nil {
		if *in.Minutes <= 0 || *in.Minutes > 24*60 {
			writeErr(w, http.StatusBadRequest, "between 1 minute and 24 hours")
			return
		}
		t.Minutes = *in.Minutes
	}
	if in.SpentOn != nil && validDate(*in.SpentOn) && *in.SpentOn != "" {
		t.SpentOn = *in.SpentOn
	}
	if in.Note != nil {
		t.Note = *in.Note
	}
	if err := s.store.SaveTimeEntry(r.Context(), t); err != nil {
		s.fail(w, err, "time")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleDeleteTime(w http.ResponseWriter, r *http.Request) {
	t, err := s.timeEntryFor(r)
	if err != nil {
		s.fail(w, err, "time")
		return
	}
	if err := s.store.DeleteTimeEntry(r.Context(), t.ID); err != nil {
		s.fail(w, err, "time")
		return
	}
	s.send(r.Context(), change{Kind: "timer", users: []int64{me(r)}})
	w.WriteHeader(http.StatusNoContent)
}

// handleTimesheet lists time between from and to: the person's own, or
// (user=all) everyone's on the teams they are on. format=csv downloads it.
func (s *Server) handleTimesheet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := s.accessOf(r)
	today := s.today(ctx, a.uid)
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if !validDate(from) || from == "" {
		t, _ := time.Parse("2006-01-02", today)
		from = t.AddDate(0, 0, -((int(t.Weekday()) + 6) % 7)).Format("2006-01-02")
	}
	if !validDate(to) || to == "" {
		t, _ := time.Parse("2006-01-02", from)
		to = t.AddDate(0, 0, 6).Format("2006-01-02")
	}
	uid := a.uid
	if r.URL.Query().Get("user") == "all" {
		uid = 0
	}
	teams := a.ids()
	if v := queryIDs(r, "team_id"); len(v) > 0 {
		teams = intersectIDs(teams, v)
	}
	entries, err := s.store.TimeEntriesBetween(ctx, teams, uid, from, to)
	if err != nil {
		s.fail(w, err, "timesheet")
		return
	}
	if pid := queryInt64(r, "project_id"); pid > 0 {
		var keep []store.TimeEntry
		for _, e := range entries {
			if is, err := s.store.GetIssue(ctx, e.IssueID, 0); err == nil && is.ProjectID != nil && *is.ProjectID == pid {
				keep = append(keep, e)
			}
		}
		entries = keep
	}
	var ids, people []int64
	for _, e := range entries {
		ids = append(ids, e.IssueID)
		people = append(people, e.UserID)
	}
	refs, _ := s.store.IssueRefs(ctx, ids)
	persons, _ := s.store.PeopleByID(ctx, people)
	if r.URL.Query().Get("format") == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="timesheet-%s-%s.csv"`, from, to))
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"Date", "Person", "Issue", "Title", "Hours", "Minutes", "Note"})
		for _, e := range entries {
			ref := refs[e.IssueID]
			p := persons[e.UserID]
			_ = cw.Write([]string{e.SpentOn, firstNonEmpty(p.Name, p.Email), ref.Identifier, ref.Title, strconv.FormatFloat(float64(e.Minutes)/60, 'f', 2, 64), strconv.Itoa(e.Minutes), e.Note})
		}
		cw.Flush()
		return
	}
	type row struct {
		store.TimeEntry
		Issue  store.IssueRef `json:"issue"`
		Person store.Person   `json:"person"`
	}
	out := []row{}
	total := 0
	for _, e := range entries {
		out = append(out, row{TimeEntry: e, Issue: refs[e.IssueID], Person: persons[e.UserID]})
		total += e.Minutes
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "entries": out, "total_minutes": total})
}
