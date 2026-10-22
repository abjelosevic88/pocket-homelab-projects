package store

import (
	"context"
	"database/sql"
	"strings"
)

// ProjectStatuses in roadmap order.
var ProjectStatuses = []string{"backlog", "planned", "started", "paused", "completed", "canceled"}

// ValidProjectStatus reports whether st is a project status.
func ValidProjectStatus(st string) bool {
	for _, x := range ProjectStatuses {
		if x == st {
			return true
		}
	}
	return false
}

// ValidHealth reports whether h is a project health ("" included).
func ValidHealth(h string) bool {
	return h == "" || h == "on_track" || h == "at_risk" || h == "off_track"
}

const projectCols = `p.id, p.name, p.summary, p.content, p.icon, p.color, p.status, p.health, p.priority, p.lead_id, p.start_date, p.target_date,
	p.update_cadence_days, p.sort_order, p.source_app, p.created_by, p.created_at, p.updated_at, p.started_at, p.completed_at, p.canceled_at, p.deleted_at`

func scanProject(row interface{ Scan(...any) error }) (*Project, error) {
	var p Project
	var lead, by sql.NullInt64
	var started, completed, canceled, deleted sql.NullString
	if err := row.Scan(&p.ID, &p.Name, &p.Summary, &p.Content, &p.Icon, &p.Color, &p.Status, &p.Health, &p.Priority, &lead, &p.StartDate,
		&p.TargetDate, &p.UpdateCadenceDays, &p.SortOrder, &p.SourceApp, &by, &p.CreatedAt, &p.UpdatedAt, &started, &completed, &canceled, &deleted); err != nil {
		return nil, notFound(err)
	}
	p.LeadID, p.CreatedBy = nullI64(lead), nullI64(by)
	p.StartedAt, p.CompletedAt, p.CanceledAt, p.DeletedAt = nullStr(started), nullStr(completed), nullStr(canceled), nullStr(deleted)
	p.TeamIDs, p.MemberIDs = []int64{}, []int64{}
	return &p, nil
}

// ProjectFilter selects projects among those the viewer may see.
type ProjectFilter struct {
	AllowedTeamIDs []int64
	TeamIDs        []int64
	Statuses       []string
	LeadID         int64
	MemberID       int64
	Q              string
	URL            string // projects linking to this address (stakeholders, resources)
	Trash          bool
	IDs            []int64
}

// ListProjects returns the projects of the given teams with progress, teams, members and the latest update.
func (s *Store) ListProjects(ctx context.Context, f ProjectFilter) ([]*Project, error) {
	if len(f.AllowedTeamIDs) == 0 {
		return []*Project{}, nil
	}
	teams := f.AllowedTeamIDs
	if len(f.TeamIDs) > 0 {
		teams = intersect(teams, f.TeamIDs)
		if len(teams) == 0 {
			return []*Project{}, nil
		}
	}
	q, args := inInts(teams)
	where := []string{`p.id IN (SELECT project_id FROM project_teams WHERE team_id IN (` + q + `))`}
	if f.Trash {
		where = append(where, `p.deleted_at IS NOT NULL`)
	} else {
		where = append(where, `p.deleted_at IS NULL`)
	}
	if len(f.Statuses) > 0 {
		q, a := inStrings(f.Statuses)
		where = append(where, `p.status IN (`+q+`)`)
		args = append(args, a...)
	}
	if len(f.IDs) > 0 {
		q, a := inInts(f.IDs)
		where = append(where, `p.id IN (`+q+`)`)
		args = append(args, a...)
	}
	if f.LeadID > 0 {
		where = append(where, `p.lead_id = ?`)
		args = append(args, f.LeadID)
	}
	if f.MemberID > 0 {
		where = append(where, `(p.lead_id = ? OR EXISTS (SELECT 1 FROM project_members pm WHERE pm.project_id = p.id AND pm.user_id = ?))`)
		args = append(args, f.MemberID, f.MemberID)
	}
	if f.URL != "" {
		where = append(where, `(EXISTS (SELECT 1 FROM contact_links cl WHERE cl.project_id = p.id AND cl.url = ?) OR EXISTS (SELECT 1 FROM project_links pl WHERE pl.project_id = p.id AND pl.url = ?))`)
		args = append(args, f.URL, f.URL)
	}
	if qq := strings.TrimSpace(f.Q); qq != "" {
		if m := ftsQuery(qq); m != "" {
			where = append(where, `(p.id IN (SELECT rowid FROM projects_fts WHERE projects_fts MATCH ?) OR p.name LIKE ? ESCAPE '\')`)
			args = append(args, m, "%"+likeEscape(qq)+"%")
		}
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+projectCols+` FROM projects p WHERE `+strings.Join(where, " AND ")+`
		ORDER BY CASE p.status WHEN 'started' THEN 0 WHEN 'planned' THEN 1 WHEN 'paused' THEN 2 WHEN 'backlog' THEN 3 WHEN 'completed' THEN 4 ELSE 5 END,
		p.sort_order, lower(p.name)`, args...)
	if err != nil {
		return nil, err
	}
	var out []*Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, p)
	}
	rows.Close()
	if out == nil {
		return []*Project{}, nil
	}
	return out, s.fillProjects(ctx, out)
}

// GetProject loads one project (access is the caller's business).
func (s *Store) GetProject(ctx context.Context, id int64) (*Project, error) {
	p, err := scanProject(s.DB.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects p WHERE p.id = ?`, id))
	if err != nil {
		return nil, err
	}
	if err := s.fillProjects(ctx, []*Project{p}); err != nil {
		return nil, err
	}
	p.Milestones, err = s.Milestones(ctx, p.ID)
	return p, err
}

func (s *Store) fillProjects(ctx context.Context, list []*Project) error {
	byID := map[int64]*Project{}
	ids := make([]int64, 0, len(list))
	for _, p := range list {
		byID[p.ID] = p
		ids = append(ids, p.ID)
	}
	q, args := inInts(ids)
	rows, err := s.DB.QueryContext(ctx, `SELECT project_id, team_id FROM project_teams WHERE project_id IN (`+q+`)`, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var pid, tid int64
		if rows.Scan(&pid, &tid) == nil {
			byID[pid].TeamIDs = append(byID[pid].TeamIDs, tid)
		}
	}
	rows.Close()
	rows, err = s.DB.QueryContext(ctx, `SELECT project_id, user_id FROM project_members WHERE project_id IN (`+q+`)`, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var pid, uid int64
		if rows.Scan(&pid, &uid) == nil {
			byID[pid].MemberIDs = append(byID[pid].MemberIDs, uid)
		}
	}
	rows.Close()
	prog, err := s.progressBy(ctx, `i.project_id`, `i.project_id IN (`+q+`)`, args...)
	if err != nil {
		return err
	}
	for id, pr := range prog {
		if p := byID[id]; p != nil {
			p.Progress = pr
		}
	}
	rows, err = s.DB.QueryContext(ctx, `SELECT u.id, u.project_id, u.user_id, u.health, u.body, u.source_app, u.created_at, u.edited_at,
		(SELECT COUNT(1) FROM comments c WHERE c.project_update_id = u.id AND c.deleted_at IS NULL)
		FROM project_updates u WHERE u.id IN (SELECT MAX(id) FROM project_updates WHERE project_id IN (`+q+`) GROUP BY project_id)`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		u, err := scanUpdate(rows)
		if err != nil {
			return err
		}
		if p := byID[u.ProjectID]; p != nil {
			p.LastUpdate = u
		}
	}
	return rows.Err()
}

// progressBy counts issues by state type, grouped by a column.
func (s *Store) progressBy(ctx context.Context, groupCol, where string, args ...any) (map[int64]ProjectProgress, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+groupCol+`, ws.type, COUNT(1), COALESCE(SUM(i.estimate), 0) FROM issues i
		JOIN workflow_states ws ON ws.id = i.state_id WHERE `+where+` AND i.deleted_at IS NULL AND `+groupCol+` IS NOT NULL GROUP BY `+groupCol+`, ws.type`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]ProjectProgress{}
	for rows.Next() {
		var id int64
		var typ string
		var n int
		var pts float64
		if err := rows.Scan(&id, &typ, &n, &pts); err != nil {
			return nil, err
		}
		pr := out[id]
		switch typ {
		case "canceled":
			pr.Canceled += n
		case "completed":
			pr.Completed += n
			pr.CompletedPoints += pts
			pr.Total += n
			pr.Points += pts
		case "started":
			pr.Started += n
			pr.Total += n
			pr.Points += pts
		default:
			pr.Total += n
			pr.Points += pts
		}
		out[id] = pr
	}
	for id, pr := range out {
		if pr.Total > 0 {
			pr.Percent = float64(pr.Completed) / float64(pr.Total) * 100
		}
		out[id] = pr
	}
	return out, rows.Err()
}

// ProjectCanSee reports whether any of the project's teams is in teamIDs.
func (s *Store) ProjectTeamIDs(ctx context.Context, projectID int64) []int64 {
	rows, err := s.DB.QueryContext(ctx, `SELECT team_id FROM project_teams WHERE project_id = ?`, projectID)
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

// SaveProject creates or updates a project with its teams and members.
func (s *Store) SaveProject(ctx context.Context, p *Project, hist []History, hooks ...TxHook) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	now := Now()
	p.UpdatedAt = now
	if p.ID == 0 {
		p.CreatedAt = now
		res, err := tx.ExecContext(ctx, `INSERT INTO projects (name, summary, content, icon, color, status, health, priority, lead_id, start_date, target_date,
			update_cadence_days, sort_order, source_app, created_by, created_at, updated_at, started_at, completed_at, canceled_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			p.Name, p.Summary, p.Content, p.Icon, p.Color, p.Status, p.Health, p.Priority, ptrArg(p.LeadID), p.StartDate, p.TargetDate,
			p.UpdateCadenceDays, p.SortOrder, p.SourceApp, ptrArg(p.CreatedBy), now, now, p.StartedAt, p.CompletedAt, p.CanceledAt)
		if err != nil {
			return err
		}
		p.ID, _ = res.LastInsertId()
		if _, err := tx.ExecContext(ctx, `INSERT INTO history (project_id, actor_id, actor_app, field) VALUES (?, ?, ?, 'created')`, p.ID, ptrArg(p.CreatedBy), p.SourceApp); err != nil {
			return err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE projects SET name = ?, summary = ?, content = ?, icon = ?, color = ?, status = ?, health = ?, priority = ?,
			lead_id = ?, start_date = ?, target_date = ?, update_cadence_days = ?, sort_order = ?, updated_at = ?, started_at = ?, completed_at = ?,
			canceled_at = ?, deleted_at = ? WHERE id = ?`,
			p.Name, p.Summary, p.Content, p.Icon, p.Color, p.Status, p.Health, p.Priority, ptrArg(p.LeadID), p.StartDate, p.TargetDate,
			p.UpdateCadenceDays, p.SortOrder, now, p.StartedAt, p.CompletedAt, p.CanceledAt, p.DeletedAt, p.ID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_teams WHERE project_id = ?`, p.ID); err != nil {
		return err
	}
	for _, t := range p.TeamIDs {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO project_teams (project_id, team_id) VALUES (?, ?)`, p.ID, t); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_members WHERE project_id = ?`, p.ID); err != nil {
		return err
	}
	for _, u := range p.MemberIDs {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO project_members (project_id, user_id) VALUES (?, ?)`, p.ID, u); err != nil {
			return err
		}
	}
	for _, h := range hist {
		if _, err := tx.ExecContext(ctx, `INSERT INTO history (project_id, actor_id, actor_app, field, from_value, to_value, from_label, to_label) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			p.ID, ptrArg(h.ActorID), h.ActorApp, h.Field, h.FromValue, h.ToValue, h.FromLabel, h.ToLabel); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM projects_fts WHERE rowid = ?`, p.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO projects_fts (rowid, name, body) VALUES (?, ?, ?)`, p.ID, p.Name, p.Summary+"\n"+p.Content); err != nil {
		return err
	}
	for _, h := range hooks {
		if err := h(tx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ProjectRefs loads small references for ids.
func (s *Store) ProjectRefs(ctx context.Context, ids []int64) (map[int64]ProjectRef, error) {
	out := map[int64]ProjectRef{}
	if len(ids) == 0 {
		return out, nil
	}
	q, args := inInts(ids)
	rows, err := s.DB.QueryContext(ctx, `SELECT id, name, icon, color, status FROM projects WHERE id IN (`+q+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r ProjectRef
		if err := rows.Scan(&r.ID, &r.Name, &r.Icon, &r.Color, &r.Status); err != nil {
			return nil, err
		}
		out[r.ID] = r
	}
	return out, rows.Err()
}

// ProjectPeople lists the lead and members of a project.
func (s *Store) ProjectPeople(ctx context.Context, projectID int64) []int64 {
	rows, err := s.DB.QueryContext(ctx, `SELECT user_id FROM project_members WHERE project_id = ? UNION SELECT lead_id FROM projects WHERE id = ? AND lead_id IS NOT NULL`, projectID, projectID)
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

// ---------- milestones ----------

// Milestones lists a project's milestones in order, with progress.
func (s *Store) Milestones(ctx context.Context, projectID int64) ([]Milestone, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, project_id, name, description, target_date, sort_order, created_at FROM milestones
		WHERE project_id = ? ORDER BY sort_order, target_date = '', target_date, id`, projectID)
	if err != nil {
		return nil, err
	}
	out := []Milestone{}
	for rows.Next() {
		var m Milestone
		if err := rows.Scan(&m.ID, &m.ProjectID, &m.Name, &m.Description, &m.TargetDate, &m.SortOrder, &m.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, m)
	}
	rows.Close()
	prog, err := s.progressBy(ctx, `i.milestone_id`, `i.project_id = ?`, projectID)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Progress = prog[out[i].ID]
	}
	return out, nil
}

// GetMilestone loads one milestone.
func (s *Store) GetMilestone(ctx context.Context, id int64) (*Milestone, error) {
	var m Milestone
	err := s.DB.QueryRowContext(ctx, `SELECT id, project_id, name, description, target_date, sort_order, created_at FROM milestones WHERE id = ?`, id).
		Scan(&m.ID, &m.ProjectID, &m.Name, &m.Description, &m.TargetDate, &m.SortOrder, &m.CreatedAt)
	return &m, notFound(err)
}

// SaveMilestone creates or updates a milestone.
func (s *Store) SaveMilestone(ctx context.Context, m *Milestone) error {
	if m.ID == 0 {
		if m.SortOrder == 0 {
			_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order), 0) + 1 FROM milestones WHERE project_id = ?`, m.ProjectID).Scan(&m.SortOrder)
		}
		res, err := s.DB.ExecContext(ctx, `INSERT INTO milestones (project_id, name, description, target_date, sort_order) VALUES (?, ?, ?, ?, ?)`,
			m.ProjectID, m.Name, m.Description, m.TargetDate, m.SortOrder)
		if err != nil {
			return err
		}
		m.ID, _ = res.LastInsertId()
		m.CreatedAt = Now()
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE milestones SET name = ?, description = ?, target_date = ?, sort_order = ? WHERE id = ?`, m.Name, m.Description, m.TargetDate, m.SortOrder, m.ID)
	return err
}

// DeleteMilestone removes a milestone (its issues stay in the project).
func (s *Store) DeleteMilestone(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM milestones WHERE id = ?`, id)
	return err
}

// MilestonesOf lists the milestones of several projects (calendar, roadmap).
func (s *Store) MilestonesOf(ctx context.Context, projectIDs []int64) ([]Milestone, error) {
	if len(projectIDs) == 0 {
		return []Milestone{}, nil
	}
	q, args := inInts(projectIDs)
	rows, err := s.DB.QueryContext(ctx, `SELECT id, project_id, name, description, target_date, sort_order, created_at FROM milestones WHERE project_id IN (`+q+`) ORDER BY target_date`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Milestone{}
	for rows.Next() {
		var m Milestone
		if err := rows.Scan(&m.ID, &m.ProjectID, &m.Name, &m.Description, &m.TargetDate, &m.SortOrder, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ---------- updates ----------

func scanUpdate(row interface{ Scan(...any) error }) (*ProjectUpdate, error) {
	var u ProjectUpdate
	var uid sql.NullInt64
	var edited sql.NullString
	if err := row.Scan(&u.ID, &u.ProjectID, &uid, &u.Health, &u.Body, &u.SourceApp, &u.CreatedAt, &edited, &u.Comments); err != nil {
		return nil, notFound(err)
	}
	u.UserID, u.EditedAt = nullI64(uid), nullStr(edited)
	return &u, nil
}

// ProjectUpdates lists a project's updates, newest first.
func (s *Store) ProjectUpdates(ctx context.Context, projectID int64, limit int) ([]ProjectUpdate, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT u.id, u.project_id, u.user_id, u.health, u.body, u.source_app, u.created_at, u.edited_at,
		(SELECT COUNT(1) FROM comments c WHERE c.project_update_id = u.id AND c.deleted_at IS NULL)
		FROM project_updates u WHERE u.project_id = ? ORDER BY u.created_at DESC, u.id DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProjectUpdate{}
	for rows.Next() {
		u, err := scanUpdate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

// GetUpdate loads one project update.
func (s *Store) GetUpdate(ctx context.Context, id int64) (*ProjectUpdate, error) {
	return scanUpdate(s.DB.QueryRowContext(ctx, `SELECT u.id, u.project_id, u.user_id, u.health, u.body, u.source_app, u.created_at, u.edited_at,
		(SELECT COUNT(1) FROM comments c WHERE c.project_update_id = u.id AND c.deleted_at IS NULL) FROM project_updates u WHERE u.id = ?`, id))
}

// SaveUpdate posts or edits an update; the project's health follows its latest update.
func (s *Store) SaveUpdate(ctx context.Context, u *ProjectUpdate, hooks ...TxHook) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if u.ID == 0 {
		res, err := tx.ExecContext(ctx, `INSERT INTO project_updates (project_id, user_id, health, body, source_app) VALUES (?, ?, ?, ?, ?)`, u.ProjectID, ptrArg(u.UserID), u.Health, u.Body, u.SourceApp)
		if err != nil {
			return err
		}
		u.ID, _ = res.LastInsertId()
		u.CreatedAt = Now()
	} else {
		now := Now()
		u.EditedAt = &now
		if _, err := tx.ExecContext(ctx, `UPDATE project_updates SET health = ?, body = ?, edited_at = ? WHERE id = ?`, u.Health, u.Body, now, u.ID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE projects SET health = (SELECT health FROM project_updates WHERE project_id = ? ORDER BY created_at DESC, id DESC LIMIT 1), updated_at = ? WHERE id = ?`, u.ProjectID, Now(), u.ProjectID); err != nil {
		return err
	}
	for _, h := range hooks {
		if err := h(tx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteUpdate removes an update; the health falls back to the one before.
func (s *Store) DeleteUpdate(ctx context.Context, u *ProjectUpdate) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_updates WHERE id = ?`, u.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE projects SET health = COALESCE((SELECT health FROM project_updates WHERE project_id = ? ORDER BY created_at DESC, id DESC LIMIT 1), '') WHERE id = ?`, u.ProjectID, u.ProjectID); err != nil {
		return err
	}
	return tx.Commit()
}

// LastUpdateAt is when the project last got an update ("" never).
func (s *Store) LastUpdateAt(ctx context.Context, projectID int64) string {
	var at sql.NullString
	_ = s.DB.QueryRowContext(ctx, `SELECT MAX(created_at) FROM project_updates WHERE project_id = ?`, projectID).Scan(&at)
	return at.String
}

// ActiveProjects lists every started project on the box (update reminders).
func (s *Store) ActiveProjects(ctx context.Context) ([]*Project, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+projectCols+` FROM projects p WHERE p.deleted_at IS NULL AND p.status = 'started'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---------- docs ----------

// ProjectDocs lists a project's documents (without bodies).
func (s *Store) ProjectDocs(ctx context.Context, projectID int64) ([]Doc, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, project_id, title, '', icon, created_by, updated_by, created_at, updated_at FROM project_docs WHERE project_id = ? ORDER BY lower(title)`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Doc{}
	for rows.Next() {
		d, err := scanDoc(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func scanDoc(row interface{ Scan(...any) error }) (*Doc, error) {
	var d Doc
	var cb, ub sql.NullInt64
	if err := row.Scan(&d.ID, &d.ProjectID, &d.Title, &d.Body, &d.Icon, &cb, &ub, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	d.CreatedBy, d.UpdatedBy = nullI64(cb), nullI64(ub)
	return &d, nil
}

// GetDoc loads one document.
func (s *Store) GetDoc(ctx context.Context, id int64) (*Doc, error) {
	return scanDoc(s.DB.QueryRowContext(ctx, `SELECT id, project_id, title, body, icon, created_by, updated_by, created_at, updated_at FROM project_docs WHERE id = ?`, id))
}

// SaveDoc creates or updates a document.
func (s *Store) SaveDoc(ctx context.Context, d *Doc) error {
	now := Now()
	if d.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO project_docs (project_id, title, body, icon, created_by, updated_by) VALUES (?, ?, ?, ?, ?, ?)`,
			d.ProjectID, d.Title, d.Body, d.Icon, ptrArg(d.CreatedBy), ptrArg(d.UpdatedBy))
		if err != nil {
			return err
		}
		d.ID, _ = res.LastInsertId()
		d.CreatedAt, d.UpdatedAt = now, now
	} else {
		d.UpdatedAt = now
		if _, err := s.DB.ExecContext(ctx, `UPDATE project_docs SET title = ?, body = ?, icon = ?, updated_by = ?, updated_at = ? WHERE id = ?`,
			d.Title, d.Body, d.Icon, ptrArg(d.UpdatedBy), now, d.ID); err != nil {
			return err
		}
	}
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM docs_fts WHERE rowid = ?`, d.ID)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO docs_fts (rowid, title, body) VALUES (?, ?, ?)`, d.ID, d.Title, d.Body)
	return err
}

// DeleteDoc removes a document.
func (s *Store) DeleteDoc(ctx context.Context, id int64) error {
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM docs_fts WHERE rowid = ?`, id)
	_, err := s.DB.ExecContext(ctx, `DELETE FROM project_docs WHERE id = ?`, id)
	return err
}

// ---------- resources ----------

// ProjectLinks lists a project's resources.
func (s *Store) ProjectLinks(ctx context.Context, projectID int64) ([]ProjectLink, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, project_id, title, url, app, sort_order, created_by, created_at FROM project_links WHERE project_id = ? ORDER BY sort_order, id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProjectLink{}
	for rows.Next() {
		var l ProjectLink
		var by sql.NullInt64
		if err := rows.Scan(&l.ID, &l.ProjectID, &l.Title, &l.URL, &l.App, &l.SortOrder, &by, &l.CreatedAt); err != nil {
			return nil, err
		}
		l.CreatedBy = nullI64(by)
		out = append(out, l)
	}
	return out, rows.Err()
}

// AddProjectLink adds a resource.
func (s *Store) AddProjectLink(ctx context.Context, l *ProjectLink) error {
	_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order), 0) + 1 FROM project_links WHERE project_id = ?`, l.ProjectID).Scan(&l.SortOrder)
	res, err := s.DB.ExecContext(ctx, `INSERT INTO project_links (project_id, title, url, app, sort_order, created_by) VALUES (?, ?, ?, ?, ?, ?)`,
		l.ProjectID, l.Title, l.URL, l.App, l.SortOrder, ptrArg(l.CreatedBy))
	if err != nil {
		return err
	}
	l.ID, _ = res.LastInsertId()
	l.CreatedAt = Now()
	return nil
}

// GetProjectLink loads one resource.
func (s *Store) GetProjectLink(ctx context.Context, id int64) (*ProjectLink, error) {
	var l ProjectLink
	var by sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT id, project_id, title, url, app, sort_order, created_by, created_at FROM project_links WHERE id = ?`, id).
		Scan(&l.ID, &l.ProjectID, &l.Title, &l.URL, &l.App, &l.SortOrder, &by, &l.CreatedAt)
	l.CreatedBy = nullI64(by)
	return &l, notFound(err)
}

// DeleteProjectLink removes a resource.
func (s *Store) DeleteProjectLink(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM project_links WHERE id = ?`, id)
	return err
}

// ---------- contacts ----------

// ContactLinks lists the contacts on a project (projectID > 0) or an issue.
func (s *Store) ContactLinks(ctx context.Context, projectID, issueID int64) ([]ContactLink, error) {
	col, id := "project_id", projectID
	if issueID > 0 {
		col, id = "issue_id", issueID
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, project_id, issue_id, contact_id, name, email, org, role, url, created_at FROM contact_links WHERE `+col+` = ? ORDER BY lower(name)`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ContactLink{}
	for rows.Next() {
		c, err := scanContactLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func scanContactLink(row interface{ Scan(...any) error }) (*ContactLink, error) {
	var c ContactLink
	var p, i sql.NullInt64
	if err := row.Scan(&c.ID, &p, &i, &c.ContactID, &c.Name, &c.Email, &c.Org, &c.Role, &c.URL, &c.CreatedAt); err != nil {
		return nil, notFound(err)
	}
	c.ProjectID, c.IssueID = nullI64(p), nullI64(i)
	return &c, nil
}

// GetContactLink loads one contact link.
func (s *Store) GetContactLink(ctx context.Context, id int64) (*ContactLink, error) {
	return scanContactLink(s.DB.QueryRowContext(ctx, `SELECT id, project_id, issue_id, contact_id, name, email, org, role, url, created_at FROM contact_links WHERE id = ?`, id))
}

// AddContactLink puts a contact on a project or an issue.
func (s *Store) AddContactLink(ctx context.Context, c *ContactLink, by int64) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO contact_links (project_id, issue_id, contact_id, name, email, org, role, url, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ptrArg(c.ProjectID), ptrArg(c.IssueID), c.ContactID, c.Name, c.Email, c.Org, c.Role, c.URL, nullID(by))
	if err != nil {
		return err
	}
	c.ID, _ = res.LastInsertId()
	c.CreatedAt = Now()
	return nil
}

// DeleteContactLink removes a contact link.
func (s *Store) DeleteContactLink(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM contact_links WHERE id = ?`, id)
	return err
}
