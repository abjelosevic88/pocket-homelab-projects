package store

import (
	"context"
	"sort"
	"time"
)

// SearchHit is one result of the global search.
type SearchHit struct {
	Kind      string `json:"kind"` // issue | project | doc | comment
	ID        int64  `json:"id"`
	IssueID   int64  `json:"issue_id,omitempty"`
	ProjectID int64  `json:"project_id,omitempty"`
	Title     string `json:"title"`
	Snippet   string `json:"snippet"`
	Ident     string `json:"identifier,omitempty"`
}

// Search finds documents and comments on the given teams (issues and
// projects are searched with ListIssues / ListProjects).
func (s *Store) Search(ctx context.Context, teamIDs []int64, q string, limit int) ([]SearchHit, error) {
	m := ftsQuery(q)
	if m == "" || len(teamIDs) == 0 {
		return []SearchHit{}, nil
	}
	if limit <= 0 {
		limit = 20
	}
	tq, targs := inInts(teamIDs)
	out := []SearchHit{}
	rows, err := s.DB.QueryContext(ctx, `SELECT d.id, d.project_id, d.title, snippet(docs_fts, 1, '', '', '…', 12) FROM docs_fts
		JOIN project_docs d ON d.id = docs_fts.rowid JOIN projects p ON p.id = d.project_id
		WHERE docs_fts MATCH ? AND p.deleted_at IS NULL AND p.id IN (SELECT project_id FROM project_teams WHERE team_id IN (`+tq+`)) ORDER BY rank LIMIT ?`,
		append(append([]any{m}, targs...), limit)...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.ID, &h.ProjectID, &h.Title, &h.Snippet); err != nil {
			rows.Close()
			return nil, err
		}
		h.Kind = "doc"
		out = append(out, h)
	}
	rows.Close()
	rows, err = s.DB.QueryContext(ctx, `SELECT c.id, i.id, t.key || '-' || i.number, i.title, snippet(comments_fts, 0, '', '', '…', 14) FROM comments_fts
		JOIN comments c ON c.id = comments_fts.rowid JOIN issues i ON i.id = c.issue_id JOIN teams t ON t.id = i.team_id
		WHERE comments_fts MATCH ? AND c.deleted_at IS NULL AND i.deleted_at IS NULL AND i.team_id IN (`+tq+`) ORDER BY rank LIMIT ?`,
		append(append([]any{m}, targs...), limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.ID, &h.IssueID, &h.Ident, &h.Title, &h.Snippet); err != nil {
			return nil, err
		}
		h.Kind = "comment"
		out = append(out, h)
	}
	return out, rows.Err()
}

// Insights are the numbers behind the charts of a team or a project.
type Insights struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Weekly   []Week `json:"weekly"`
	Flow     []Flow `json:"flow"`
	Velocity []Velo `json:"velocity"`
	Assignee []Part `json:"by_assignee"`
	Label    []Part `json:"by_label"`
	Priority []Part `json:"by_priority"`
	Project  []Part `json:"by_project"`
	State    []Part `json:"by_state"`
	Times    struct {
		LeadDaysMedian  float64 `json:"lead_days_median"`
		CycleDaysMedian float64 `json:"cycle_days_median"`
		LeadDaysP80     float64 `json:"lead_days_p80"`
		CycleDaysP80    float64 `json:"cycle_days_p80"`
		Completed       int     `json:"completed"`
	} `json:"times"`
	Totals struct {
		Open      int     `json:"open"`
		Started   int     `json:"started"`
		Completed int     `json:"completed"`
		Created   int     `json:"created"`
		Overdue   int     `json:"overdue"`
		Points    float64 `json:"open_points"`
		Minutes   int     `json:"minutes_logged"`
	} `json:"totals"`
}

// Week counts issues created and completed in a week (starting Monday).
type Week struct {
	Week      string `json:"week"`
	Created   int    `json:"created"`
	Completed int    `json:"completed"`
	Canceled  int    `json:"canceled"`
}

// Flow is one day of the cumulative flow chart.
type Flow struct {
	Day    string         `json:"day"`
	Counts map[string]int `json:"counts"`
}

// Velo is what a cycle completed.
type Velo struct {
	CycleID   int64   `json:"cycle_id"`
	Number    int     `json:"number"`
	Name      string  `json:"name"`
	StartsOn  string  `json:"starts_on"`
	EndsOn    string  `json:"ends_on"`
	Scope     int     `json:"scope"`
	Completed int     `json:"completed"`
	Points    float64 `json:"points"`
	Done      float64 `json:"completed_points"`
}

// Part is one slice of a breakdown (open work).
type Part struct {
	ID     int64   `json:"id"`
	Name   string  `json:"name"`
	Color  string  `json:"color"`
	Open   int     `json:"open"`
	Points float64 `json:"points"`
	Done   int     `json:"completed"`
}

// InsightsFor computes the charts for the issues of teams, optionally one project, between from and to (dates).
func (s *Store) InsightsFor(ctx context.Context, teamIDs []int64, projectID int64, from, to, today string) (*Insights, error) {
	in := &Insights{From: from, To: to, Weekly: []Week{}, Flow: []Flow{}, Velocity: []Velo{}, Assignee: []Part{}, Label: []Part{}, Priority: []Part{}, Project: []Part{}, State: []Part{}}
	if len(teamIDs) == 0 {
		return in, nil
	}
	tq, targs := inInts(teamIDs)
	scope := `i.deleted_at IS NULL AND i.team_id IN (` + tq + `)`
	args := append([]any{}, targs...)
	if projectID > 0 {
		scope += ` AND i.project_id = ?`
		args = append(args, projectID)
	}
	fromTS, toTS := from+"T00:00:00Z", to+"T23:59:59Z"

	// Weekly created / completed.
	weeks := map[string]*Week{}
	wk := func(ts string) string {
		t := ParseTime(ts)
		if t.IsZero() {
			return ""
		}
		wd := (int(t.Weekday()) + 6) % 7
		return t.AddDate(0, 0, -wd).Format(dateFmt)
	}
	start, _ := time.Parse(dateFmt, from)
	end, _ := time.Parse(dateFmt, to)
	for d := start.AddDate(0, 0, -((int(start.Weekday()) + 6) % 7)); !d.After(end); d = d.AddDate(0, 0, 7) {
		k := d.Format(dateFmt)
		weeks[k] = &Week{Week: k}
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT i.created_at, COALESCE(i.completed_at, ''), COALESCE(i.canceled_at, '') FROM issues i WHERE `+scope+`
		AND (i.created_at BETWEEN ? AND ? OR i.completed_at BETWEEN ? AND ? OR i.canceled_at BETWEEN ? AND ?)`, append(args, fromTS, toTS, fromTS, toTS, fromTS, toTS)...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c, d, x string
		if err := rows.Scan(&c, &d, &x); err != nil {
			rows.Close()
			return nil, err
		}
		if c >= fromTS && c <= toTS {
			if w := weeks[wk(c)]; w != nil {
				w.Created++
				in.Totals.Created++
			}
		}
		if d != "" && d >= fromTS && d <= toTS {
			if w := weeks[wk(d)]; w != nil {
				w.Completed++
			}
		}
		if x != "" && x >= fromTS && x <= toTS {
			if w := weeks[wk(x)]; w != nil {
				w.Canceled++
			}
		}
	}
	rows.Close()
	for _, w := range weeks {
		in.Weekly = append(in.Weekly, *w)
	}
	sort.Slice(in.Weekly, func(i, j int) bool { return in.Weekly[i].Week < in.Weekly[j].Week })

	// Lead and cycle time of issues completed in the range.
	rows, err = s.DB.QueryContext(ctx, `SELECT i.created_at, COALESCE(i.started_at, ''), i.completed_at FROM issues i WHERE `+scope+` AND i.completed_at BETWEEN ? AND ?`, append(args, fromTS, toTS)...)
	if err != nil {
		return nil, err
	}
	var lead, cyc []float64
	for rows.Next() {
		var c, st, d string
		if err := rows.Scan(&c, &st, &d); err != nil {
			rows.Close()
			return nil, err
		}
		ct, dt := ParseTime(c), ParseTime(d)
		if !ct.IsZero() && !dt.IsZero() {
			lead = append(lead, dt.Sub(ct).Hours()/24)
		}
		if stt := ParseTime(st); !stt.IsZero() && !dt.IsZero() {
			cyc = append(cyc, dt.Sub(stt).Hours()/24)
		}
	}
	rows.Close()
	in.Times.Completed = len(lead)
	in.Times.LeadDaysMedian, in.Times.LeadDaysP80 = quantile(lead, .5), quantile(lead, .8)
	in.Times.CycleDaysMedian, in.Times.CycleDaysP80 = quantile(cyc, .5), quantile(cyc, .8)

	// Totals and breakdowns of open work.
	open := scope + ` AND ws.type IN ('triage','backlog','unstarted','started') AND i.archived_at IS NULL`
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1), COALESCE(SUM(i.estimate), 0), COALESCE(SUM(CASE WHEN ws.type = 'started' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN i.due_date <> '' AND i.due_date < ? THEN 1 ELSE 0 END), 0)
		FROM issues i JOIN workflow_states ws ON ws.id = i.state_id WHERE `+open, append([]any{today}, args...)...).Scan(&in.Totals.Open, &in.Totals.Points, &in.Totals.Started, &in.Totals.Overdue)
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM issues i WHERE `+scope+` AND i.completed_at BETWEEN ? AND ?`, append(args, fromTS, toTS)...).Scan(&in.Totals.Completed)
	_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(te.minutes), 0) FROM time_entries te JOIN issues i ON i.id = te.issue_id WHERE `+scope+` AND te.spent_on BETWEEN ? AND ?`, append(args, from, to)...).Scan(&in.Totals.Minutes)

	part := func(sel, join, group string) ([]Part, error) {
		rows, err := s.DB.QueryContext(ctx, `SELECT `+sel+`, COUNT(1), COALESCE(SUM(i.estimate), 0) FROM issues i JOIN workflow_states ws ON ws.id = i.state_id `+join+`
			WHERE `+open+` GROUP BY `+group+` ORDER BY COUNT(1) DESC`, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []Part{}
		for rows.Next() {
			var p Part
			if err := rows.Scan(&p.ID, &p.Name, &p.Color, &p.Open, &p.Points); err != nil {
				return nil, err
			}
			out = append(out, p)
		}
		return out, rows.Err()
	}
	if in.Assignee, err = part(`COALESCE(u.id, 0), COALESCE(CASE WHEN u.name = '' THEN u.email ELSE u.name END, 'Unassigned'), COALESCE(u.color, '#94a3b8')`, `LEFT JOIN users u ON u.id = i.assignee_id`, `COALESCE(u.id, 0)`); err != nil {
		return nil, err
	}
	if in.Label, err = part(`l.id, l.name, l.color`, `JOIN issue_labels il ON il.issue_id = i.id JOIN labels l ON l.id = il.label_id`, `l.id`); err != nil {
		return nil, err
	}
	if in.Priority, err = part(`i.priority, CASE i.priority WHEN 1 THEN 'Urgent' WHEN 2 THEN 'High' WHEN 3 THEN 'Medium' WHEN 4 THEN 'Low' ELSE 'No priority' END, ''`, ``, `i.priority`); err != nil {
		return nil, err
	}
	if in.Project, err = part(`COALESCE(p.id, 0), COALESCE(p.name, 'No project'), COALESCE(p.color, '')`, `LEFT JOIN projects p ON p.id = i.project_id`, `COALESCE(p.id, 0)`); err != nil {
		return nil, err
	}
	if in.State, err = part(`ws.id, ws.name, ws.color`, ``, `ws.id`); err != nil {
		return nil, err
	}

	// Cumulative flow from the daily snapshots (teams only).
	if projectID == 0 {
		rows, err = s.DB.QueryContext(ctx, `SELECT day, state_type, SUM(issues) FROM flow_snapshots WHERE team_id IN (`+tq+`) AND day BETWEEN ? AND ? GROUP BY day, state_type ORDER BY day`, append(append([]any{}, targs...), from, to)...)
		if err != nil {
			return nil, err
		}
		byDay := map[string]map[string]int{}
		var days []string
		for rows.Next() {
			var day, typ string
			var n int
			if err := rows.Scan(&day, &typ, &n); err != nil {
				rows.Close()
				return nil, err
			}
			if byDay[day] == nil {
				byDay[day] = map[string]int{}
				days = append(days, day)
			}
			byDay[day][typ] = n
		}
		rows.Close()
		for _, d := range days {
			in.Flow = append(in.Flow, Flow{Day: d, Counts: byDay[d]})
		}

		// Velocity of the last finished cycles.
		rows, err = s.DB.QueryContext(ctx, `SELECT c.id, c.number, c.name, c.starts_on, c.ends_on,
			(SELECT COUNT(1) FROM issues i JOIN workflow_states ws ON ws.id = i.state_id WHERE i.cycle_id = c.id AND i.deleted_at IS NULL AND ws.type <> 'canceled'),
			(SELECT COUNT(1) FROM issues i JOIN workflow_states ws ON ws.id = i.state_id WHERE i.cycle_id = c.id AND i.deleted_at IS NULL AND ws.type = 'completed'),
			(SELECT COALESCE(SUM(i.estimate), 0) FROM issues i JOIN workflow_states ws ON ws.id = i.state_id WHERE i.cycle_id = c.id AND i.deleted_at IS NULL AND ws.type <> 'canceled'),
			(SELECT COALESCE(SUM(i.estimate), 0) FROM issues i JOIN workflow_states ws ON ws.id = i.state_id WHERE i.cycle_id = c.id AND i.deleted_at IS NULL AND ws.type = 'completed')
			FROM cycles c WHERE c.team_id IN (`+tq+`) AND c.starts_on <= ? ORDER BY c.starts_on DESC LIMIT 12`, append(append([]any{}, targs...), today)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var v Velo
			if err := rows.Scan(&v.CycleID, &v.Number, &v.Name, &v.StartsOn, &v.EndsOn, &v.Scope, &v.Completed, &v.Points, &v.Done); err != nil {
				rows.Close()
				return nil, err
			}
			in.Velocity = append(in.Velocity, v)
		}
		rows.Close()
		sort.Slice(in.Velocity, func(i, j int) bool { return in.Velocity[i].StartsOn < in.Velocity[j].StartsOn })
	}
	return in, nil
}

func quantile(v []float64, q float64) float64 {
	if len(v) == 0 {
		return 0
	}
	c := append([]float64(nil), v...)
	sort.Float64s(c)
	idx := q * float64(len(c)-1)
	lo := int(idx)
	hi := min(lo+1, len(c)-1)
	f := idx - float64(lo)
	r := c[lo]*(1-f) + c[hi]*f
	return float64(int(r*10+.5)) / 10
}
