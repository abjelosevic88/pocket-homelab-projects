package store

import (
	"context"
	"database/sql"
	"time"
)

const dateFmt = "2006-01-02"

func scanCycle(row interface{ Scan(...any) error }) (*Cycle, error) {
	var c Cycle
	var done sql.NullString
	if err := row.Scan(&c.ID, &c.TeamID, &c.Number, &c.Name, &c.Description, &c.StartsOn, &c.EndsOn, &done, &c.CreatedAt); err != nil {
		return nil, notFound(err)
	}
	c.CompletedAt = nullStr(done)
	return &c, nil
}

const cycleCols = `id, team_id, number, name, description, starts_on, ends_on, completed_at, created_at`

// CycleStatus says where a cycle stands on a day.
func CycleStatus(c *Cycle, today string) string {
	switch {
	case c.EndsOn < today:
		return "past"
	case c.StartsOn > today:
		return "upcoming"
	}
	return "current"
}

// TeamCycles lists a team's cycles (newest last) with progress.
func (s *Store) TeamCycles(ctx context.Context, teamIDs []int64, today string) ([]Cycle, error) {
	if len(teamIDs) == 0 {
		return []Cycle{}, nil
	}
	q, args := inInts(teamIDs)
	rows, err := s.DB.QueryContext(ctx, `SELECT `+cycleCols+` FROM cycles WHERE team_id IN (`+q+`) ORDER BY team_id, starts_on`, args...)
	if err != nil {
		return nil, err
	}
	out := []Cycle{}
	var ids []int64
	for rows.Next() {
		c, err := scanCycle(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		c.Status = CycleStatus(c, today)
		out = append(out, *c)
		ids = append(ids, c.ID)
	}
	rows.Close()
	if len(ids) == 0 {
		return out, nil
	}
	q, args = inInts(ids)
	prog, err := s.progressBy(ctx, `i.cycle_id`, `i.cycle_id IN (`+q+`)`, args...)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Progress = prog[out[i].ID]
	}
	return out, nil
}

// GetCycle loads one cycle with progress.
func (s *Store) GetCycle(ctx context.Context, id int64, today string) (*Cycle, error) {
	c, err := scanCycle(s.DB.QueryRowContext(ctx, `SELECT `+cycleCols+` FROM cycles WHERE id = ?`, id))
	if err != nil {
		return nil, err
	}
	c.Status = CycleStatus(c, today)
	prog, err := s.progressBy(ctx, `i.cycle_id`, `i.cycle_id = ?`, id)
	if err != nil {
		return nil, err
	}
	c.Progress = prog[id]
	return c, nil
}

// SaveCycle creates or updates a cycle (a new one gets the team's next number).
func (s *Store) SaveCycle(ctx context.Context, c *Cycle) error {
	if c.ID == 0 {
		_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(number), 0) + 1 FROM cycles WHERE team_id = ?`, c.TeamID).Scan(&c.Number)
		res, err := s.DB.ExecContext(ctx, `INSERT INTO cycles (team_id, number, name, description, starts_on, ends_on) VALUES (?, ?, ?, ?, ?, ?)`,
			c.TeamID, c.Number, c.Name, c.Description, c.StartsOn, c.EndsOn)
		if err != nil {
			return conflict(err)
		}
		c.ID, _ = res.LastInsertId()
		c.CreatedAt = Now()
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE cycles SET name = ?, description = ?, starts_on = ?, ends_on = ?, completed_at = ? WHERE id = ?`,
		c.Name, c.Description, c.StartsOn, c.EndsOn, c.CompletedAt, c.ID)
	return err
}

// DeleteCycle removes a cycle; its issues lose their cycle.
func (s *Store) DeleteCycle(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM cycles WHERE id = ?`, id)
	return err
}

// CycleOverlaps reports whether [start, end] overlaps another cycle of the team.
func (s *Store) CycleOverlaps(ctx context.Context, teamID, exceptID int64, start, end string) bool {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM cycles WHERE team_id = ? AND id <> ? AND starts_on <= ? AND ends_on >= ?`, teamID, exceptID, end, start).Scan(&n)
	return n > 0
}

// PlanCycles makes sure a team with cycles on has its current cycle and the
// upcoming ones. It returns the cycles it created.
func (s *Store) PlanCycles(ctx context.Context, t *Team, today time.Time) ([]Cycle, error) {
	if !t.CyclesEnabled || t.CycleWeeks <= 0 {
		return nil, nil
	}
	var last Cycle
	err := s.DB.QueryRowContext(ctx, `SELECT `+cycleCols+` FROM cycles WHERE team_id = ? ORDER BY ends_on DESC LIMIT 1`, t.ID).
		Scan(&last.ID, &last.TeamID, &last.Number, &last.Name, &last.Description, &last.StartsOn, &last.EndsOn, new(sql.NullString), &last.CreatedAt)
	var next time.Time
	if err == nil {
		end, _ := time.Parse(dateFmt, last.EndsOn)
		next = end.AddDate(0, 0, 1+7*t.CycleCooldownWeeks)
	} else {
		// The first cycle starts on the latest start day on or before today.
		d := today
		for int(d.Weekday()) != t.CycleStartDay%7 {
			d = d.AddDate(0, 0, -1)
		}
		next = d
	}
	// Skip forward over gaps longer than a cycle (cycles were off for a while).
	length := 7 * t.CycleWeeks
	for next.AddDate(0, 0, length-1).Before(today) {
		next = next.AddDate(0, 0, length+7*t.CycleCooldownWeeks)
	}
	todayStr := today.Format(dateFmt)
	var upcoming int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM cycles WHERE team_id = ? AND starts_on > ?`, t.ID, todayStr).Scan(&upcoming)
	var current int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM cycles WHERE team_id = ? AND starts_on <= ? AND ends_on >= ?`, t.ID, todayStr, todayStr).Scan(&current)
	var made []Cycle
	for i := 0; i < 20 && (current == 0 || upcoming < t.CycleUpcoming); i++ {
		c := Cycle{TeamID: t.ID, StartsOn: next.Format(dateFmt), EndsOn: next.AddDate(0, 0, length-1).Format(dateFmt)}
		if !s.CycleOverlaps(ctx, t.ID, 0, c.StartsOn, c.EndsOn) {
			if err := s.SaveCycle(ctx, &c); err != nil {
				return made, err
			}
			made = append(made, c)
		}
		if c.StartsOn <= todayStr && c.EndsOn >= todayStr {
			current++
		} else if c.StartsOn > todayStr {
			upcoming++
		}
		next = next.AddDate(0, 0, length+7*t.CycleCooldownWeeks)
	}
	return made, nil
}

// EndedCycles lists cycles that ended before today and are not completed yet.
func (s *Store) EndedCycles(ctx context.Context, today string) ([]Cycle, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+cycleCols+` FROM cycles WHERE completed_at IS NULL AND ends_on < ? ORDER BY ends_on`, today)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Cycle
	for rows.Next() {
		c, err := scanCycle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// NextCycle returns the team's first cycle starting after day (nil when none).
func (s *Store) NextCycle(ctx context.Context, teamID int64, after string) *Cycle {
	c, err := scanCycle(s.DB.QueryRowContext(ctx, `SELECT `+cycleCols+` FROM cycles WHERE team_id = ? AND starts_on > ? ORDER BY starts_on LIMIT 1`, teamID, after))
	if err != nil {
		return nil
	}
	return c
}

// CurrentCycle returns the team's cycle covering day (nil when none).
func (s *Store) CurrentCycle(ctx context.Context, teamID int64, day string) *Cycle {
	c, err := scanCycle(s.DB.QueryRowContext(ctx, `SELECT `+cycleCols+` FROM cycles WHERE team_id = ? AND starts_on <= ? AND ends_on >= ? LIMIT 1`, teamID, day, day))
	if err != nil {
		return nil
	}
	return c
}

// UnfinishedInCycle lists the ids of a cycle's issues that are not done.
func (s *Store) UnfinishedInCycle(ctx context.Context, cycleID int64) []int64 {
	rows, err := s.DB.QueryContext(ctx, `SELECT i.id FROM issues i JOIN workflow_states ws ON ws.id = i.state_id
		WHERE i.cycle_id = ? AND i.deleted_at IS NULL AND ws.type NOT IN ('completed','canceled')`, cycleID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}

// MarkCycleCompleted stamps a cycle as done.
func (s *Store) MarkCycleCompleted(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE cycles SET completed_at = ? WHERE id = ?`, Now(), id)
	return err
}

// SnapshotCycle records today's burn-up numbers of a cycle.
func (s *Store) SnapshotCycle(ctx context.Context, cycleID int64, day string) error {
	var snap CycleSnapshot
	rows, err := s.DB.QueryContext(ctx, `SELECT ws.type, COUNT(1), COALESCE(SUM(i.estimate), 0) FROM issues i JOIN workflow_states ws ON ws.id = i.state_id
		WHERE i.cycle_id = ? AND i.deleted_at IS NULL AND ws.type <> 'canceled' GROUP BY ws.type`, cycleID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var typ string
		var n int
		var pts float64
		if err := rows.Scan(&typ, &n, &pts); err != nil {
			rows.Close()
			return err
		}
		snap.Scope += n
		snap.ScopePoints += pts
		switch typ {
		case "completed":
			snap.Completed += n
			snap.CompletedPoints += pts
			snap.Started += n
			snap.StartedPoints += pts
		case "started":
			snap.Started += n
			snap.StartedPoints += pts
		}
	}
	rows.Close()
	_, err = s.DB.ExecContext(ctx, `INSERT INTO cycle_snapshots (cycle_id, day, scope, started, completed, scope_points, started_points, completed_points)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(cycle_id, day) DO UPDATE SET scope = excluded.scope, started = excluded.started, completed = excluded.completed,
		scope_points = excluded.scope_points, started_points = excluded.started_points, completed_points = excluded.completed_points`,
		cycleID, day, snap.Scope, snap.Started, snap.Completed, snap.ScopePoints, snap.StartedPoints, snap.CompletedPoints)
	return err
}

// CycleSnapshots returns a cycle's burn-up history.
func (s *Store) CycleSnapshots(ctx context.Context, cycleID int64) ([]CycleSnapshot, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT day, scope, started, completed, scope_points, started_points, completed_points FROM cycle_snapshots WHERE cycle_id = ? ORDER BY day`, cycleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CycleSnapshot{}
	for rows.Next() {
		var c CycleSnapshot
		if err := rows.Scan(&c.Day, &c.Scope, &c.Started, &c.Completed, &c.ScopePoints, &c.StartedPoints, &c.CompletedPoints); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ActiveCycleIDs lists the cycles covering day (for daily snapshots).
func (s *Store) ActiveCycleIDs(ctx context.Context, day string) []int64 {
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM cycles WHERE starts_on <= ? AND ends_on >= ?`, day, day)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}

// SnapshotFlow records a team's issues per state type for the cumulative flow chart.
func (s *Store) SnapshotFlow(ctx context.Context, teamID int64, day string) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT ws.type, COUNT(i.id), COALESCE(SUM(i.estimate), 0) FROM workflow_states ws
		LEFT JOIN issues i ON i.state_id = ws.id AND i.deleted_at IS NULL AND i.archived_at IS NULL WHERE ws.team_id = ? GROUP BY ws.type`, teamID)
	if err != nil {
		return err
	}
	type row struct {
		typ string
		n   int
		pts float64
	}
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.typ, &r.n, &r.pts); err != nil {
			rows.Close()
			return err
		}
		all = append(all, r)
	}
	rows.Close()
	for _, r := range all {
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO flow_snapshots (team_id, day, state_type, issues, points) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(team_id, day, state_type) DO UPDATE SET issues = excluded.issues, points = excluded.points`, teamID, day, r.typ, r.n, r.pts); err != nil {
			return err
		}
	}
	return nil
}
