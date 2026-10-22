package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

func (s *Server) cycleRoutes(r chi.Router) {
	r.Get("/cycles", s.handleListCycles)
	r.Get("/teams/{ref}/cycles", s.handleTeamCycles)
	r.Post("/teams/{ref}/cycles", s.handleCreateCycle)
	r.Get("/cycles/{id}", s.handleGetCycle)
	r.Patch("/cycles/{id}", s.handlePatchCycle)
	r.Delete("/cycles/{id}", s.handleDeleteCycle)
	r.Post("/cycles/{id}/complete", s.handleCompleteCycle)
}

func (s *Server) handleListCycles(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	teams := a.ids()
	if v := queryIDs(r, "team_id"); len(v) > 0 {
		teams = intersectIDs(teams, v)
	}
	list, err := s.store.TeamCycles(r.Context(), teams, s.today(r.Context(), a.uid))
	if err != nil {
		s.fail(w, err, "cycles")
		return
	}
	if st := r.URL.Query().Get("status"); st != "" {
		var keep []store.Cycle
		for _, c := range list {
			if strings.Contains(","+st+",", ","+c.Status+",") {
				keep = append(keep, c)
			}
		}
		list = keep
		if list == nil {
			list = []store.Cycle{}
		}
	}
	writeJSON(w, http.StatusOK, list)
}

func intersectIDs(a, b []int64) []int64 {
	set := map[int64]bool{}
	for _, x := range a {
		set[x] = true
	}
	out := []int64{}
	for _, x := range b {
		if set[x] {
			out = append(out, x)
		}
	}
	return out
}

func (s *Server) handleTeamCycles(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	t, err := s.loadTeam(r, a, chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	list, err := s.store.TeamCycles(r.Context(), []int64{t.ID}, s.today(r.Context(), a.uid))
	if err != nil {
		s.fail(w, err, "cycles")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// CycleDetail is a cycle with its burn-up and breakdowns.
type CycleDetail struct {
	*store.Cycle
	Snapshots []store.CycleSnapshot `json:"snapshots"`
	Assignees []store.Part          `json:"by_assignee"`
	Labels    []store.Part          `json:"by_label"`
	Projects  []store.Part          `json:"by_project"`
	Days      int                   `json:"days"`
	DaysLeft  int                   `json:"days_left"`
}

func (s *Server) cycleFor(r *http.Request, need int) (*store.Cycle, error) {
	a := s.accessOf(r)
	c, err := s.store.GetCycle(r.Context(), idParam(r, "id"), s.today(r.Context(), a.uid))
	if err != nil {
		return nil, err
	}
	if err := a.check(c.TeamID, need); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Server) handleGetCycle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, err := s.cycleFor(r, needRead)
	if err != nil {
		s.fail(w, err, "cycle")
		return
	}
	d := CycleDetail{Cycle: c}
	d.Snapshots, _ = s.store.CycleSnapshots(ctx, c.ID)
	start, _ := time.Parse("2006-01-02", c.StartsOn)
	end, _ := time.Parse("2006-01-02", c.EndsOn)
	today, _ := time.Parse("2006-01-02", s.today(ctx, me(r)))
	d.Days = int(end.Sub(start).Hours()/24) + 1
	d.DaysLeft = max(0, int(end.Sub(today).Hours()/24)+1)
	if today.Before(start) {
		d.DaysLeft = d.Days
	}
	// Breakdowns of the cycle's work (open and done).
	d.Assignees = s.cycleParts(ctx, c.ID, `COALESCE(u.id, 0), COALESCE(CASE WHEN u.name = '' THEN u.email ELSE u.name END, 'Unassigned'), COALESCE(u.color, '#94a3b8')`, `LEFT JOIN users u ON u.id = i.assignee_id`, `COALESCE(u.id, 0)`)
	d.Labels = s.cycleParts(ctx, c.ID, `l.id, l.name, l.color`, `JOIN issue_labels il ON il.issue_id = i.id JOIN labels l ON l.id = il.label_id`, `l.id`)
	d.Projects = s.cycleParts(ctx, c.ID, `COALESCE(p.id, 0), COALESCE(p.name, 'No project'), COALESCE(p.color, '')`, `LEFT JOIN projects p ON p.id = i.project_id`, `COALESCE(p.id, 0)`)
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) cycleParts(ctx context.Context, cycleID int64, sel, join, group string) []store.Part {
	rows, err := s.store.DB.QueryContext(ctx, `SELECT `+sel+`,
		SUM(CASE WHEN ws.type NOT IN ('completed','canceled') THEN 1 ELSE 0 END), COALESCE(SUM(i.estimate), 0), SUM(CASE WHEN ws.type = 'completed' THEN 1 ELSE 0 END)
		FROM issues i JOIN workflow_states ws ON ws.id = i.state_id `+join+` WHERE i.cycle_id = ? AND i.deleted_at IS NULL AND ws.type <> 'canceled'
		GROUP BY `+group+` ORDER BY COUNT(1) DESC`, cycleID)
	if err != nil {
		return []store.Part{}
	}
	defer rows.Close()
	out := []store.Part{}
	for rows.Next() {
		var p store.Part
		if rows.Scan(&p.ID, &p.Name, &p.Color, &p.Open, &p.Points, &p.Done) == nil {
			out = append(out, p)
		}
	}
	return out
}

type cycleInput struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	StartsOn    *string `json:"starts_on"`
	EndsOn      *string `json:"ends_on"`
}

func (s *Server) applyCycle(ctx context.Context, c *store.Cycle, in cycleInput) error {
	if in.Name != nil {
		c.Name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		c.Description = *in.Description
	}
	if in.StartsOn != nil {
		c.StartsOn = *in.StartsOn
	}
	if in.EndsOn != nil {
		c.EndsOn = *in.EndsOn
	}
	if c.StartsOn == "" || c.EndsOn == "" || !validDate(c.StartsOn) || !validDate(c.EndsOn) || c.EndsOn < c.StartsOn {
		return badRequest("a cycle needs starts_on and ends_on (YYYY-MM-DD, end on or after the start)")
	}
	if s.store.CycleOverlaps(ctx, c.TeamID, c.ID, c.StartsOn, c.EndsOn) {
		return badRequest("the dates overlap another cycle of the team")
	}
	return nil
}

func (s *Server) handleCreateCycle(w http.ResponseWriter, r *http.Request) {
	var in cycleInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a := s.accessOf(r)
	t, err := s.loadTeam(r, a, chi.URLParam(r, "ref"), needWrite)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	c := &store.Cycle{TeamID: t.ID}
	if err := s.applyCycle(r.Context(), c, in); err != nil {
		s.fail(w, err, "cycle")
		return
	}
	if err := s.store.SaveCycle(r.Context(), c); err != nil {
		s.fail(w, err, "cycle")
		return
	}
	s.changed(r, "cycle", []int64{t.ID}, c.ID)
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handlePatchCycle(w http.ResponseWriter, r *http.Request) {
	var in cycleInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	c, err := s.cycleFor(r, needWrite)
	if err != nil {
		s.fail(w, err, "cycle")
		return
	}
	if err := s.applyCycle(r.Context(), c, in); err != nil {
		s.fail(w, err, "cycle")
		return
	}
	if err := s.store.SaveCycle(r.Context(), c); err != nil {
		s.fail(w, err, "cycle")
		return
	}
	s.changed(r, "cycle", []int64{c.TeamID}, c.ID)
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleDeleteCycle(w http.ResponseWriter, r *http.Request) {
	c, err := s.cycleFor(r, needAdmin)
	if err != nil {
		s.fail(w, err, "cycle")
		return
	}
	if err := s.store.DeleteCycle(r.Context(), c.ID); err != nil {
		s.fail(w, err, "cycle")
		return
	}
	s.changed(r, "cycle", []int64{c.TeamID}, c.ID)
	w.WriteHeader(http.StatusNoContent)
}

// handleCompleteCycle ends a cycle now, moving unfinished work to the next one (or out of cycles).
func (s *Server) handleCompleteCycle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		MoveTo string `json:"move_to"` // next (default) | none
	}
	_ = decode(r, &in)
	c, err := s.cycleFor(r, needWrite)
	if err != nil {
		s.fail(w, err, "cycle")
		return
	}
	moved, err := s.finishCycle(ctx, s.actorOf(r), c, in.MoveTo != "none")
	if err != nil {
		s.fail(w, err, "cycle")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"moved": moved})
}

// finishCycle marks a cycle completed and moves its unfinished issues on.
func (s *Server) finishCycle(ctx context.Context, who actor, c *store.Cycle, rollover bool) (int, error) {
	team, err := s.store.GetTeam(ctx, c.TeamID)
	if err != nil {
		return 0, err
	}
	_ = s.store.SnapshotCycle(ctx, c.ID, c.EndsOn)
	var next *store.Cycle
	if rollover {
		if team.CyclesEnabled {
			_, _ = s.store.PlanCycles(ctx, team, s.boxToday())
		}
		next = s.store.NextCycle(ctx, team.ID, c.EndsOn)
		if next != nil && next.StartsOn <= s.boxToday().Format("2006-01-02") && next.EndsOn < s.boxToday().Format("2006-01-02") {
			next = s.store.CurrentCycle(ctx, team.ID, s.boxToday().Format("2006-01-02"))
		}
	}
	a := &access{uid: who.id, teams: map[int64]string{team.ID: "admin"}}
	moved := 0
	for _, id := range s.store.UnfinishedInCycle(ctx, c.ID) {
		is, err := s.store.GetIssue(ctx, id, 0)
		if err != nil {
			continue
		}
		var target int64
		if next != nil {
			target = next.ID
		}
		if _, err := s.updateIssue(ctx, a, who, is, issueInput{CycleID: &target}); err == nil {
			moved++
		}
	}
	if err := s.store.MarkCycleCompleted(ctx, c.ID); err != nil {
		return moved, err
	}
	if s.core.Configured() {
		_ = s.coreEventNow(ctx, "projects.cycle_completed", "cycle:"+strconv.FormatInt(c.ID, 10), map[string]any{"id": c.ID, "team_id": c.TeamID, "number": c.Number,
			"name": cycleName(c), "moved": moved, "completed": c.Progress.Completed, "scope": c.Progress.Total, "audience_emails": s.teamEmails(ctx, c.TeamID)})
	}
	s.send(ctx, change{Kind: "cycle", TeamIDs: []int64{c.TeamID}, IDs: []int64{c.ID}})
	s.webhook(c.TeamID, "cycle.completed", map[string]any{"cycle": c, "moved": moved})
	return moved, nil
}

// coreEventNow queues a Core event outside a domain transaction.
func (s *Server) coreEventNow(ctx context.Context, topic, subject string, payload map[string]any) error {
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if err := s.coreEvent(ctx, topic, subject, payload)(tx); err != nil {
		return err
	}
	return tx.Commit()
}
