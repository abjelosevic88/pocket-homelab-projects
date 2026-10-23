package server

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// jobs runs the background work: cycles (planned ahead, rolled over,
// snapshotted), repeating issues, due-date and project-update reminders and
// housekeeping. Everything is idempotent, so a restart or a skipped minute
// does no harm.
func (s *Server) jobs(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	var lastCycles, lastHourly, lastPRs time.Time
	for {
		now := time.Now()
		if now.Sub(lastCycles) >= 10*time.Minute {
			s.cycleJob(ctx)
			lastCycles = now
		}
		if now.Sub(lastPRs) >= 5*time.Minute {
			s.prJob(ctx)
			lastPRs = now
		}
		s.recurringJob(ctx)
		s.dueJob(ctx, now)
		if now.Sub(lastHourly) >= time.Hour {
			s.updateReminderJob(ctx, now)
			s.housekeeping(ctx)
			lastHourly = now
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (s *Server) cycleJob(ctx context.Context) {
	today := s.boxToday()
	day := today.Format("2006-01-02")
	teams, err := s.store.AllTeams(ctx)
	if err != nil {
		return
	}
	for i := range teams {
		t := &teams[i]
		if made, err := s.store.PlanCycles(ctx, t, today); err != nil {
			s.log.Warn("plan cycles", "team", t.Key, "err", err)
		} else {
			for _, c := range made {
				if c.StartsOn <= day && s.core.Configured() {
					_ = s.coreEventNow(ctx, "projects.cycle_started", "cycle:"+strconv.FormatInt(c.ID, 10), map[string]any{"id": c.ID, "team_id": t.ID,
						"number": c.Number, "starts_on": c.StartsOn, "ends_on": c.EndsOn, "audience_emails": s.teamEmails(ctx, t.ID)})
				}
			}
		}
		_ = s.store.SnapshotFlow(ctx, t.ID, day)
	}
	ended, _ := s.store.EndedCycles(ctx, day)
	for i := range ended {
		c := &ended[i]
		t, err := s.store.GetTeam(ctx, c.TeamID)
		if err != nil {
			continue
		}
		if full, err := s.store.GetCycle(ctx, c.ID, day); err == nil {
			c = full
		}
		moved, err := s.finishCycle(ctx, s.systemActor("system"), c, t.CycleRollover)
		if err != nil {
			s.log.Warn("finish cycle", "team", t.Key, "cycle", c.Number, "err", err)
		} else {
			s.log.Info("cycle finished", "team", t.Key, "cycle", c.Number, "moved", moved)
		}
	}
	for _, id := range s.store.ActiveCycleIDs(ctx, day) {
		_ = s.store.SnapshotCycle(ctx, id, day)
	}
}

// nextRecurrence advances a repeating template's day.
func nextRecurrence(t store.Template, from time.Time) time.Time {
	n := max(1, t.RecurInterval)
	switch t.Recur {
	case "daily":
		return from.AddDate(0, 0, n)
	case "weekdays":
		d := from.AddDate(0, 0, 1)
		for d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
			d = d.AddDate(0, 0, 1)
		}
		return d
	case "weekly":
		return from.AddDate(0, 0, 7*n)
	case "monthly":
		return from.AddDate(0, n, 0)
	case "yearly":
		return from.AddDate(n, 0, 0)
	}
	return from.AddDate(100, 0, 0)
}

func (s *Server) recurringJob(ctx context.Context) {
	today := s.boxToday()
	due, err := s.store.DueTemplates(ctx, today.Format("2006-01-02"))
	if err != nil {
		return
	}
	for _, t := range due {
		// Advance first so a failure can't create the same issue every minute.
		next, _ := time.Parse("2006-01-02", t.NextAt)
		for !next.After(today) {
			next = nextRecurrence(t, next)
		}
		tt := t
		tt.NextAt = next.Format("2006-01-02")
		who := s.systemActor("system")
		who.name = "Repeating issue"
		a := &access{teams: map[int64]string{t.TeamID: "admin"}}
		if t.CreatedBy != nil && s.store.TeamRole(ctx, t.TeamID, *t.CreatedBy) != "" {
			who.id = *t.CreatedBy
			a.uid = who.id
		}
		in := issueInput{TemplateID: t.ID, TeamID: &t.TeamID}
		is, err := s.createIssue(ctx, a, who, in)
		if err != nil {
			s.log.Warn("repeating issue", "template", t.Name, "err", err)
		} else {
			tt.LastIssueID = &is.ID
		}
		if err := s.store.SaveTemplate(ctx, &tt); err != nil {
			s.log.Warn("repeating issue: save template", "template", t.Name, "err", err)
		}
	}
}

// dueJob tells each person, at their digest time, about their issues due
// tomorrow and the ones that became overdue.
func (s *Server) dueJob(ctx context.Context, now time.Time) {
	for _, uid := range s.store.UserIDs(ctx) {
		st, err := s.store.GetSettings(ctx, uid)
		if err != nil {
			continue
		}
		loc := s.loc(ctx, uid)
		local := now.In(loc)
		hh, mm := clockOf(st.DigestTime)
		at := time.Date(local.Year(), local.Month(), local.Day(), hh, mm, 0, 0, loc)
		if local.Before(at) || local.Sub(at) > 2*time.Hour {
			continue
		}
		today := local.Format("2006-01-02")
		if !s.store.ReminderOnce(ctx, uid, "digest:"+today) {
			continue
		}
		a := s.accessFor(ctx, uid)
		tomorrow := local.AddDate(0, 0, 1).Format("2006-01-02")
		f := store.IssueFilter{AllowedTeamIDs: a.ids(), ViewerID: uid, AssigneeIDs: []int64{uid}, StateTypes: []string{"triage", "backlog", "unstarted", "started"}, DueTo: tomorrow, Limit: 200, Today: today}
		list, _, err := s.store.ListIssues(ctx, f)
		if err != nil {
			continue
		}
		for _, is := range list {
			what := "is due tomorrow"
			if is.DueDate < today {
				what = "is overdue (was due " + is.DueDate + ")"
			} else if is.DueDate == today {
				what = "is due today"
			}
			key := fmt.Sprintf("due:%d:%s", is.ID, is.DueDate)
			if is.DueDate < today {
				key = fmt.Sprintf("overdue:%d:%s", is.ID, today)
			}
			if !s.store.ReminderOnce(ctx, uid, key) {
				continue
			}
			s.notifyPerson(ctx, store.Notification{UserID: uid, Kind: "due", IssueID: &is.ID, TeamID: &is.TeamID, ActorApp: "system",
				Title: is.Identifier + " " + what, Body: is.Title}, s.cfg.BaseURL+"/issue/"+is.Identifier)
		}
	}
}

// updateReminderJob asks the leads of started projects for an update once per cadence.
func (s *Server) updateReminderJob(ctx context.Context, now time.Time) {
	projects, err := s.store.ActiveProjects(ctx)
	if err != nil {
		return
	}
	for _, p := range projects {
		if p.LeadID == nil || p.UpdateCadenceDays <= 0 {
			continue
		}
		last := s.store.LastUpdateAt(ctx, p.ID)
		if last != "" {
			p.LastUpdate = &store.ProjectUpdate{CreatedAt: last}
		}
		if !s.updateDue(p, now) {
			continue
		}
		period := now.Unix() / int64(p.UpdateCadenceDays*86400)
		if !s.store.ReminderOnce(ctx, *p.LeadID, fmt.Sprintf("update:%d:%d", p.ID, period)) {
			continue
		}
		s.notifyPerson(ctx, store.Notification{UserID: *p.LeadID, Kind: "update_due", ProjectID: &p.ID, ActorApp: "system",
			Title: p.Name + " needs a status update", Body: "Post how the project is going — Pocket AI can draft it from the last days' work."},
			s.cfg.BaseURL+"/project/"+strconv.FormatInt(p.ID, 10)+"?tab=updates")
	}
}

func (s *Server) housekeeping(ctx context.Context) {
	if n := s.store.AutoArchive(ctx); n > 0 {
		s.log.Info("archived finished issues", "count", n)
	}
	s.store.PurgeTrash(ctx, 30)
	s.store.PruneNotifications(ctx, 30)
	s.store.PruneReminders(ctx, 60)
	_ = s.store.PurgeSessions(ctx)
}
