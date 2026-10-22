package server

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// seedDemo fills an empty box with two teams, projects, cycles and issues so
// the app can be tried (DEMO_DATA=true). It never touches a box with teams.
func (s *Server) seedDemo(ctx context.Context) error {
	if teams, _ := s.store.AllTeams(ctx); len(teams) > 0 {
		return nil
	}
	owner, err := s.firstOrSystemUser(ctx, "admin@localhost", "Alex Morgan")
	if err != nil {
		return err
	}
	if owner.Username == "" || owner.Username == "admin" {
		owner.Username = "alex"
		_ = s.store.UpdateUser(ctx, owner)
	}
	people := []*store.User{owner}
	for _, p := range []struct{ email, name, user string }{{"ben@example.com", "Ben Carter", "ben"}, {"sani@example.com", "Sani Ito", "sani"}} {
		u, err := s.store.GetUserByEmail(ctx, p.email)
		if err != nil {
			u = &store.User{Email: p.email, Name: p.name, Username: p.user, Role: "member"}
			if err := s.store.CreateUser(ctx, u); err != nil {
				return err
			}
		}
		people = append(people, u)
	}
	alex, ben, sani := people[0], people[1], people[2]
	today := s.boxToday()
	day := func(d int) string { return today.AddDate(0, 0, d).Format("2006-01-02") }

	web := &store.Team{Key: "WEB", Name: "Web", Description: "The website and the client app.", Color: "#5e6ad2", Icon: "🌐", CycleStartDay: 1, CycleWeeks: 2}
	if err := s.store.CreateTeam(ctx, web, store.Preset("software"), alex.ID); err != nil {
		return err
	}
	home := &store.Team{Key: "HOME", Name: "Home", Description: "Renovation, repairs and everything around the house.", Color: "#10b981", Icon: "🏡"}
	if err := s.store.CreateTeam(ctx, home, store.Preset("household"), alex.ID); err != nil {
		return err
	}
	_ = s.store.SetTeamMember(ctx, web.ID, ben.ID, "member")
	_ = s.store.SetTeamMember(ctx, web.ID, sani.ID, "member")
	_ = s.store.SetTeamMember(ctx, home.ID, sani.ID, "member")
	// Start cycles two weeks back so there is a finished one and a current one.
	web.CyclesEnabled = true
	_, _ = s.store.PlanCycles(ctx, web, today.AddDate(0, 0, -14))
	_, _ = s.store.PlanCycles(ctx, web, today)

	mkProject := func(p *store.Project) *store.Project {
		p.CreatedBy, p.UpdateCadenceDays = &alex.ID, 7
		if p.Status == "started" {
			now := today.AddDate(0, 0, -20).Format(time.RFC3339)
			p.StartedAt = &now
		}
		_ = s.store.SaveProject(ctx, p, nil)
		return p
	}
	launch := mkProject(&store.Project{Name: "Website relaunch", Summary: "New design, faster pages and a real blog.", Icon: "🚀", Color: "#8b5cf6",
		Status: "started", Priority: 2, LeadID: &alex.ID, StartDate: day(-20), TargetDate: day(40), TeamIDs: []int64{web.ID}, MemberIDs: []int64{alex.ID, ben.ID, sani.ID},
		Content: "## Goal\nShip the new site before the holidays.\n\n## Scope\n- New design system\n- Blog with RSS\n- Lighthouse 95+"})
	mobile := mkProject(&store.Project{Name: "Mobile app beta", Summary: "TestFlight build for friends and family.", Icon: "📱", Color: "#0ea5e9",
		Status: "planned", Priority: 3, LeadID: &ben.ID, StartDate: day(30), TargetDate: day(90), TeamIDs: []int64{web.ID}, MemberIDs: []int64{ben.ID}})
	kitchen := mkProject(&store.Project{Name: "Kitchen renovation", Summary: "New cabinets, worktop and lighting.", Icon: "🍳", Color: "#f59e0b",
		Status: "started", Priority: 2, LeadID: &sani.ID, StartDate: day(-10), TargetDate: day(25), TeamIDs: []int64{home.ID}, MemberIDs: []int64{alex.ID, sani.ID}})
	m1 := &store.Milestone{ProjectID: launch.ID, Name: "Design approved", TargetDate: day(5)}
	m2 := &store.Milestone{ProjectID: launch.ID, Name: "Content migrated", TargetDate: day(25)}
	m3 := &store.Milestone{ProjectID: launch.ID, Name: "Go live", TargetDate: day(40)}
	for _, m := range []*store.Milestone{m1, m2, m3} {
		_ = s.store.SaveMilestone(ctx, m)
	}
	_ = s.store.SaveMilestone(ctx, &store.Milestone{ProjectID: kitchen.ID, Name: "Old kitchen out", TargetDate: day(3)})
	_ = s.store.SaveMilestone(ctx, &store.Milestone{ProjectID: kitchen.ID, Name: "Cabinets installed", TargetDate: day(18)})
	_ = s.store.SaveUpdate(ctx, &store.ProjectUpdate{ProjectID: launch.ID, UserID: &alex.ID, Health: "at_risk",
		Body: "Design is close, but the blog import is slower than planned.\n\n**Next**\n- Finish the article template\n- Decide on comments"})
	_ = s.store.SaveUpdate(ctx, &store.ProjectUpdate{ProjectID: kitchen.ID, UserID: &sani.ID, Health: "on_track", Body: "Cabinets ordered, delivery on schedule."})

	cur := s.store.CurrentCycle(ctx, web.ID, day(0))
	webStates, _ := s.store.TeamStates(ctx, web.ID)
	homeStates, _ := s.store.TeamStates(ctx, home.ID)
	stateOf := func(states []store.State, name string) int64 {
		for _, st := range states {
			if st.Name == name {
				return st.ID
			}
		}
		return states[0].ID
	}
	webLabels, _ := s.store.TeamLabels(ctx, web.ID)
	homeLabels, _ := s.store.TeamLabels(ctx, home.ID)
	labelOf := func(ls []store.Label, name string) []int64 {
		for _, l := range ls {
			if l.Name == name {
				return []int64{l.ID}
			}
		}
		return nil
	}
	who := actor{id: alex.ID, name: "Alex Morgan", base: s.cfg.BaseURL}
	a := &access{uid: alex.ID, teams: map[int64]string{web.ID: "admin", home.ID: "admin"}}
	add := func(team int64, title, desc, state string, states []store.State, prio int, assignee *store.User, project *store.Project, milestone *store.Milestone, labels []int64, due string, est float64, cycle bool) *store.Issue {
		in := issueInput{TeamID: &team, Title: &title, Description: &desc, Priority: &prio, LabelIDs: &labels}
		st := stateOf(states, state)
		in.StateID = &st
		if assignee != nil {
			in.AssigneeID = &assignee.ID
		}
		if project != nil {
			in.ProjectID = &project.ID
		}
		if milestone != nil {
			in.MilestoneID = &milestone.ID
		}
		if due != "" {
			in.DueDate = &due
		}
		if est > 0 {
			rm := json.RawMessage(strconv.FormatFloat(est, 'f', -1, 64))
			in.Estimate = &rm
		}
		if cycle && cur != nil {
			in.CycleID = &cur.ID
		}
		is, err := s.createIssue(ctx, a, who, in)
		if err != nil {
			s.log.Warn("demo issue", "title", title, "err", err)
			return nil
		}
		return is
	}
	i1 := add(web.ID, "Design the new article page", "Typography, code blocks and images that scale.\n\n- [x] Wireframe\n- [ ] Dark mode", "In Progress", webStates, 2, sani, launch, m1, labelOf(webLabels, "Design"), day(3), 3, true)
	add(web.ID, "Migrate 120 blog posts", "Script the export from the old CMS, keep the URLs.", "Todo", webStates, 3, ben, launch, m2, labelOf(webLabels, "Feature"), day(20), 5, true)
	add(web.ID, "Fix login loop on Safari", "Users on iOS 18 are sent back to the login page after signing in.", "In Review", webStates, 1, ben, nil, nil, labelOf(webLabels, "Bug"), day(-1), 2, true)
	add(web.ID, "RSS feed for the blog", "", "Backlog", webStates, 4, nil, launch, m3, labelOf(webLabels, "Feature"), "", 1, false)
	add(web.ID, "Lighthouse score above 95", "Lazy-load images, inline critical CSS.", "Todo", webStates, 2, alex, launch, m3, labelOf(webLabels, "Improvement"), day(30), 3, true)
	add(web.ID, "Set up TestFlight", "", "Backlog", webStates, 3, ben, mobile, nil, labelOf(webLabels, "Chore"), "", 2, false)
	done := add(web.ID, "Pick the colour palette", "", "Done", webStates, 3, sani, launch, m1, labelOf(webLabels, "Design"), day(-5), 1, true)
	_ = done
	add(home.ID, "Measure the kitchen walls", "", "Done", homeStates, 2, sani, kitchen, nil, labelOf(homeLabels, "Repair"), day(-8), 0, false)
	add(home.ID, "Order worktop", "Oak, 3 m, oiled.", "Waiting", homeStates, 2, alex, kitchen, nil, labelOf(homeLabels, "Purchase"), day(6), 0, false)
	add(home.ID, "Book the electrician", "Three new sockets and under-cabinet lights.", "Planned", homeStates, 1, alex, kitchen, nil, labelOf(homeLabels, "Repair"), day(2), 0, false)
	add(home.ID, "Replace the garden hose", "", "Ideas", homeStates, 4, nil, nil, nil, labelOf(homeLabels, "Garden"), "", 0, false)
	if i1 != nil {
		_, _ = s.addComment(ctx, actor{id: ben.ID, name: "Ben Carter", base: s.cfg.BaseURL}, i1, "Looks great — can we try a slightly larger line height? @sani", 0)
		_, _ = s.addComment(ctx, actor{id: sani.ID, name: "Sani Ito", base: s.cfg.BaseURL}, i1, "Good idea, trying 1.7 now.", 0)
	}
	s.log.Info("demo data seeded", "teams", 2, "projects", 3)
	return nil
}
