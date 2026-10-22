package store

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
)

// ErrForbidden is returned when the person's team role does not allow a change.
var ErrForbidden = errors.New("forbidden")

const teamCols = `id, key, name, description, color, icon, issue_seq, triage_enabled, cycles_enabled, cycle_weeks, cycle_start_day,
	cycle_cooldown_weeks, cycle_upcoming, cycle_rollover, estimate_scale, auto_archive_months, default_state_id, git_secret,
	created_by, created_at, updated_at, archived_at`

func scanTeam(row interface{ Scan(...any) error }) (*Team, error) {
	var t Team
	var def, by sql.NullInt64
	var archived sql.NullString
	if err := row.Scan(&t.ID, &t.Key, &t.Name, &t.Description, &t.Color, &t.Icon, &t.IssueSeq, &t.TriageEnabled, &t.CyclesEnabled,
		&t.CycleWeeks, &t.CycleStartDay, &t.CycleCooldownWeeks, &t.CycleUpcoming, &t.CycleRollover, &t.EstimateScale,
		&t.AutoArchiveMonths, &def, &t.GitSecret, &by, &t.CreatedAt, &t.UpdatedAt, &archived); err != nil {
		return nil, notFound(err)
	}
	t.DefaultStateID, t.CreatedBy, t.ArchivedAt = nullI64(def), nullI64(by), nullStr(archived)
	return &t, nil
}

// TeamKeyRe is what a team key may look like (letters and digits, 1–7, starting with a letter).
var TeamKeyRe = regexp.MustCompile(`^[A-Z][A-Z0-9]{0,6}$`)

// GetTeam loads a team without checking access.
func (s *Store) GetTeam(ctx context.Context, id int64) (*Team, error) {
	return scanTeam(s.DB.QueryRowContext(ctx, `SELECT `+teamCols+` FROM teams WHERE id = ?`, id))
}

// GetTeamByKey loads a team by its key.
func (s *Store) GetTeamByKey(ctx context.Context, key string) (*Team, error) {
	return scanTeam(s.DB.QueryRowContext(ctx, `SELECT `+teamCols+` FROM teams WHERE key = ?`, strings.ToUpper(key)))
}

// GetTeamByGitSecret finds the team an incoming git webhook belongs to.
func (s *Store) GetTeamByGitSecret(ctx context.Context, secret string) (*Team, error) {
	if secret == "" {
		return nil, ErrNotFound
	}
	return scanTeam(s.DB.QueryRowContext(ctx, `SELECT `+teamCols+` FROM teams WHERE git_secret = ?`, secret))
}

// TeamRole is the person's role on a team ("" when not a member).
func (s *Store) TeamRole(ctx context.Context, teamID, userID int64) string {
	var role string
	_ = s.DB.QueryRowContext(ctx, `SELECT role FROM team_members WHERE team_id = ? AND user_id = ?`, teamID, userID).Scan(&role)
	return role
}

// UserTeamIDs lists the teams a person is on, with their role.
func (s *Store) UserTeamIDs(ctx context.Context, userID int64) (map[int64]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT tm.team_id, tm.role FROM team_members tm JOIN teams t ON t.id = tm.team_id WHERE tm.user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var role string
		if err := rows.Scan(&id, &role); err != nil {
			return nil, err
		}
		out[id] = role
	}
	return out, rows.Err()
}

// ListTeams returns the teams a person is on (archived ones too, flagged).
func (s *Store) ListTeams(ctx context.Context, userID int64) ([]Team, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+prefixCols("t.", teamCols)+`, tm.role FROM teams t JOIN team_members tm ON tm.team_id = t.id
		WHERE tm.user_id = ? ORDER BY t.archived_at IS NOT NULL, lower(t.name)`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Team{}
	for rows.Next() {
		var role string
		t, err := scanTeamRole(rows, &role)
		if err != nil {
			return nil, err
		}
		t.Role = role
		out = append(out, *t)
	}
	return out, rows.Err()
}

func scanTeamRole(rows *sql.Rows, role *string) (*Team, error) {
	var t Team
	var def, by sql.NullInt64
	var archived sql.NullString
	if err := rows.Scan(&t.ID, &t.Key, &t.Name, &t.Description, &t.Color, &t.Icon, &t.IssueSeq, &t.TriageEnabled, &t.CyclesEnabled,
		&t.CycleWeeks, &t.CycleStartDay, &t.CycleCooldownWeeks, &t.CycleUpcoming, &t.CycleRollover, &t.EstimateScale,
		&t.AutoArchiveMonths, &def, &t.GitSecret, &by, &t.CreatedAt, &t.UpdatedAt, &archived, role); err != nil {
		return nil, err
	}
	t.DefaultStateID, t.CreatedBy, t.ArchivedAt = nullI64(def), nullI64(by), nullStr(archived)
	return &t, nil
}

// AllTeams lists every team on the box (background jobs only).
func (s *Store) AllTeams(ctx context.Context) ([]Team, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+teamCols+` FROM teams WHERE archived_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Team{}
	for rows.Next() {
		t, err := scanTeam(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func prefixCols(p, cols string) string {
	parts := strings.Split(cols, ",")
	for i, c := range parts {
		parts[i] = p + strings.TrimSpace(c)
	}
	return strings.Join(parts, ", ")
}

// TeamPreset seeds a new team's workflow and labels.
type TeamPreset struct {
	ID          string
	Name        string
	Description string
	States      []State
	Labels      []Label
	Cycles      bool
	Estimates   string
	Triage      bool
}

// TeamPresets are the starting points offered when a team is created.
var TeamPresets = []TeamPreset{
	{ID: "software", Name: "Software", Description: "Backlog → Todo → In Progress → In Review → Done, two-week cycles, story points.",
		States: []State{{Name: "Backlog", Type: "backlog", Color: "#94a3b8"}, {Name: "Todo", Type: "unstarted", Color: "#64748b"},
			{Name: "In Progress", Type: "started", Color: "#f59e0b"}, {Name: "In Review", Type: "started", Color: "#22c55e"},
			{Name: "Done", Type: "completed", Color: "#5e6ad2"}, {Name: "Canceled", Type: "canceled", Color: "#94a3b8"},
			{Name: "Duplicate", Type: "canceled", Color: "#94a3b8"}},
		Labels: []Label{{Name: "Bug", Color: "#ef4444"}, {Name: "Feature", Color: "#8b5cf6"}, {Name: "Improvement", Color: "#3b82f6"},
			{Name: "Chore", Color: "#64748b"}, {Name: "Design", Color: "#ec4899"}, {Name: "Docs", Color: "#14b8a6"}},
		Cycles: true, Estimates: "fibonacci"},
	{ID: "kanban", Name: "Simple kanban", Description: "To do → Doing → Done. Nothing else to set up.",
		States: []State{{Name: "Backlog", Type: "backlog", Color: "#94a3b8"}, {Name: "To do", Type: "unstarted", Color: "#64748b"},
			{Name: "Doing", Type: "started", Color: "#f59e0b"}, {Name: "Done", Type: "completed", Color: "#5e6ad2"},
			{Name: "Canceled", Type: "canceled", Color: "#94a3b8"}},
		Labels: []Label{{Name: "Idea", Color: "#8b5cf6"}, {Name: "Urgent", Color: "#ef4444"}}},
	{ID: "household", Name: "Home & household", Description: "Ideas → Planned → In progress → Waiting → Done, for renovations, repairs, purchases and chores.",
		States: []State{{Name: "Ideas", Type: "backlog", Color: "#94a3b8"}, {Name: "Planned", Type: "unstarted", Color: "#64748b"},
			{Name: "In progress", Type: "started", Color: "#f59e0b"}, {Name: "Waiting", Type: "started", Color: "#a855f7"},
			{Name: "Done", Type: "completed", Color: "#10b981"}, {Name: "Dropped", Type: "canceled", Color: "#94a3b8"}},
		Labels: []Label{{Name: "Repair", Color: "#ef4444"}, {Name: "Purchase", Color: "#f59e0b"}, {Name: "Garden", Color: "#22c55e"},
			{Name: "Cleaning", Color: "#0ea5e9"}, {Name: "Paperwork", Color: "#64748b"}, {Name: "Car", Color: "#6366f1"}}},
	{ID: "client", Name: "Client work", Description: "Requests land in Triage; Backlog → Todo → In Progress → Waiting on client → Review → Done; hours tracked.",
		States: []State{{Name: "Triage", Type: "triage", Color: "#f97316"}, {Name: "Backlog", Type: "backlog", Color: "#94a3b8"},
			{Name: "Todo", Type: "unstarted", Color: "#64748b"}, {Name: "In Progress", Type: "started", Color: "#f59e0b"},
			{Name: "Waiting on client", Type: "started", Color: "#a855f7"}, {Name: "Review", Type: "started", Color: "#22c55e"},
			{Name: "Done", Type: "completed", Color: "#5e6ad2"}, {Name: "Canceled", Type: "canceled", Color: "#94a3b8"}},
		Labels: []Label{{Name: "Billable", Color: "#10b981"}, {Name: "Non-billable", Color: "#94a3b8"}, {Name: "Bug", Color: "#ef4444"},
			{Name: "Change request", Color: "#8b5cf6"}, {Name: "Support", Color: "#0ea5e9"}},
		Triage: true, Estimates: "linear"},
	{ID: "personal", Name: "Personal goals", Description: "Someday → Next → Doing → Done for your own projects and goals.",
		States: []State{{Name: "Someday", Type: "backlog", Color: "#94a3b8"}, {Name: "Next", Type: "unstarted", Color: "#64748b"},
			{Name: "Doing", Type: "started", Color: "#f59e0b"}, {Name: "Done", Type: "completed", Color: "#10b981"},
			{Name: "Let go", Type: "canceled", Color: "#94a3b8"}},
		Labels: []Label{{Name: "Health", Color: "#10b981"}, {Name: "Learning", Color: "#3b82f6"}, {Name: "Money", Color: "#f59e0b"},
			{Name: "Errand", Color: "#64748b"}}},
}

// Preset finds a team preset by id (software when unknown).
func Preset(id string) TeamPreset {
	for _, p := range TeamPresets {
		if p.ID == id {
			return p
		}
	}
	return TeamPresets[0]
}

// CreateTeam makes a team with the preset's workflow and labels; the creator becomes its admin.
func (s *Store) CreateTeam(ctx context.Context, t *Team, preset TeamPreset, creatorID int64) error {
	t.Key = strings.ToUpper(strings.TrimSpace(t.Key))
	if !TeamKeyRe.MatchString(t.Key) {
		return errors.New("the key is 1–7 letters or digits and starts with a letter")
	}
	if t.Color == "" {
		t.Color = "#5e6ad2"
	}
	if t.EstimateScale == "" {
		t.EstimateScale = preset.Estimates
		if t.EstimateScale == "" {
			t.EstimateScale = "none"
		}
	}
	if t.CycleWeeks <= 0 {
		t.CycleWeeks = 2
	}
	if t.CycleUpcoming <= 0 {
		t.CycleUpcoming = 2
	}
	if t.AutoArchiveMonths == 0 {
		t.AutoArchiveMonths = 6
	}
	t.CyclesEnabled = t.CyclesEnabled || preset.Cycles
	t.TriageEnabled = t.TriageEnabled || preset.Triage
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	res, err := tx.ExecContext(ctx, `INSERT INTO teams (key, name, description, color, icon, triage_enabled, cycles_enabled, cycle_weeks, cycle_start_day,
		cycle_cooldown_weeks, cycle_upcoming, cycle_rollover, estimate_scale, auto_archive_months, git_secret, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Key, t.Name, t.Description, t.Color, t.Icon, t.TriageEnabled, t.CyclesEnabled, t.CycleWeeks, t.CycleStartDay,
		t.CycleCooldownWeeks, t.CycleUpcoming, true, t.EstimateScale, t.AutoArchiveMonths, RandomToken(20), creatorID)
	if err != nil {
		return conflict(err)
	}
	t.ID, _ = res.LastInsertId()
	if _, err := tx.ExecContext(ctx, `INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, 'admin')`, t.ID, creatorID); err != nil {
		return err
	}
	states := preset.States
	if !t.TriageEnabled {
		var kept []State
		for _, st := range states {
			if st.Type != "triage" {
				kept = append(kept, st)
			}
		}
		states = kept
	} else if !hasType(states, "triage") {
		states = append([]State{{Name: "Triage", Type: "triage", Color: "#f97316"}}, states...)
	}
	var defaultState int64
	for i, st := range states {
		r, err := tx.ExecContext(ctx, `INSERT INTO workflow_states (team_id, name, type, color, position) VALUES (?, ?, ?, ?, ?)`, t.ID, st.Name, st.Type, st.Color, float64(i+1))
		if err != nil {
			return err
		}
		if st.Type == "unstarted" && defaultState == 0 {
			defaultState, _ = r.LastInsertId()
		}
	}
	if defaultState > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE teams SET default_state_id = ? WHERE id = ?`, defaultState, t.ID); err != nil {
			return err
		}
		t.DefaultStateID = &defaultState
	}
	for i, l := range preset.Labels {
		if _, err := tx.ExecContext(ctx, `INSERT INTO labels (team_id, name, color, position) VALUES (?, ?, ?, ?)`, t.ID, l.Name, l.Color, float64(i+1)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func hasType(states []State, typ string) bool {
	for _, s := range states {
		if s.Type == typ {
			return true
		}
	}
	return false
}

// UpdateTeam saves a team's settings.
func (s *Store) UpdateTeam(ctx context.Context, t *Team) error {
	t.Key = strings.ToUpper(strings.TrimSpace(t.Key))
	if !TeamKeyRe.MatchString(t.Key) {
		return errors.New("the key is 1–7 letters or digits and starts with a letter")
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE teams SET key = ?, name = ?, description = ?, color = ?, icon = ?, triage_enabled = ?, cycles_enabled = ?,
		cycle_weeks = ?, cycle_start_day = ?, cycle_cooldown_weeks = ?, cycle_upcoming = ?, cycle_rollover = ?, estimate_scale = ?,
		auto_archive_months = ?, default_state_id = ?, archived_at = ?, updated_at = ? WHERE id = ?`,
		t.Key, t.Name, t.Description, t.Color, t.Icon, t.TriageEnabled, t.CyclesEnabled, t.CycleWeeks, t.CycleStartDay,
		t.CycleCooldownWeeks, t.CycleUpcoming, t.CycleRollover, t.EstimateScale, t.AutoArchiveMonths, ptrArg(t.DefaultStateID),
		t.ArchivedAt, Now(), t.ID)
	return conflict(err)
}

// RotateGitSecret gives the team's incoming git webhook a new address.
func (s *Store) RotateGitSecret(ctx context.Context, teamID int64) (string, error) {
	sec := RandomToken(20)
	_, err := s.DB.ExecContext(ctx, `UPDATE teams SET git_secret = ? WHERE id = ?`, sec, teamID)
	return sec, err
}

// RecordKeyAliases keeps the identifiers of a team's issues reachable after its key changes.
func (s *Store) RecordKeyAliases(ctx context.Context, teamID int64, oldKey string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO issue_aliases (identifier, issue_id) SELECT ? || '-' || number, id FROM issues WHERE team_id = ?`, strings.ToUpper(oldKey), teamID)
	return err
}

// DeleteTeam removes a team with everything in it.
func (s *Store) DeleteTeam(ctx context.Context, id int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	// Issues reference workflow states without a cascade: drop them first.
	if _, err := tx.ExecContext(ctx, `DELETE FROM issues_fts WHERE rowid IN (SELECT id FROM issues WHERE team_id = ?)`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM issues WHERE team_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM teams WHERE id = ?`, id); err != nil {
		return err
	}
	// Projects that worked only with this team go too.
	if _, err := tx.ExecContext(ctx, `UPDATE projects SET deleted_at = ? WHERE deleted_at IS NULL AND id NOT IN (SELECT project_id FROM project_teams)`, Now()); err != nil {
		return err
	}
	return tx.Commit()
}

// ---------- members ----------

// TeamMembers lists a team's people with their roles.
func (s *Store) TeamMembers(ctx context.Context, teamID int64) ([]TeamMember, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT u.id, u.name, u.email, u.username, u.color, tm.role, tm.created_at FROM team_members tm
		JOIN users u ON u.id = tm.user_id WHERE tm.team_id = ? ORDER BY lower(CASE WHEN u.name = '' THEN u.email ELSE u.name END)`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TeamMember{}
	for rows.Next() {
		var m TeamMember
		if err := rows.Scan(&m.ID, &m.Name, &m.Email, &m.Username, &m.Color, &m.Role, &m.JoinedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// TeamMemberIDs lists the ids of a team's people.
func (s *Store) TeamMemberIDs(ctx context.Context, teamID int64) []int64 {
	rows, err := s.DB.QueryContext(ctx, `SELECT user_id FROM team_members WHERE team_id = ?`, teamID)
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

// SetTeamMember adds a person to a team or changes their role.
func (s *Store) SetTeamMember(ctx context.Context, teamID, userID int64, role string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, ?)
		ON CONFLICT(team_id, user_id) DO UPDATE SET role = excluded.role`, teamID, userID, role)
	return err
}

// RemoveTeamMember takes a person off a team (they keep nothing of it).
func (s *Store) RemoveTeamMember(ctx context.Context, teamID, userID int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM team_members WHERE team_id = ? AND user_id = ?`, teamID, userID)
	return err
}

// CountTeamAdmins counts the admins of a team.
func (s *Store) CountTeamAdmins(ctx context.Context, teamID int64) int {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM team_members WHERE team_id = ? AND role = 'admin'`, teamID).Scan(&n)
	return n
}

// SharedPeople lists everyone who shares at least one team with the person (themselves included).
func (s *Store) SharedPeople(ctx context.Context, userID int64) ([]Person, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT u.id, u.name, u.email, u.username, u.color FROM users u
		WHERE u.id = ? OR u.id IN (SELECT tm2.user_id FROM team_members tm1 JOIN team_members tm2 ON tm2.team_id = tm1.team_id WHERE tm1.user_id = ?)
		ORDER BY lower(CASE WHEN u.name = '' THEN u.email ELSE u.name END)`, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Person{}
	for rows.Next() {
		var p Person
		if err := rows.Scan(&p.ID, &p.Name, &p.Email, &p.Username, &p.Color); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---------- workflow states ----------

// TeamStates lists a team's workflow in order.
func (s *Store) TeamStates(ctx context.Context, teamID int64) ([]State, error) {
	return s.statesWhere(ctx, `team_id = ?`, teamID)
}

// StatesOf loads the workflows of several teams.
func (s *Store) StatesOf(ctx context.Context, teamIDs []int64) ([]State, error) {
	if len(teamIDs) == 0 {
		return []State{}, nil
	}
	q, args := inInts(teamIDs)
	return s.statesWhere(ctx, `team_id IN (`+q+`)`, args...)
}

func (s *Store) statesWhere(ctx context.Context, where string, args ...any) ([]State, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, team_id, name, type, color, description, position FROM workflow_states WHERE `+where+`
		ORDER BY team_id, CASE type WHEN 'triage' THEN 0 WHEN 'backlog' THEN 1 WHEN 'unstarted' THEN 2 WHEN 'started' THEN 3 WHEN 'completed' THEN 4 ELSE 5 END, position`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []State{}
	for rows.Next() {
		var st State
		if err := rows.Scan(&st.ID, &st.TeamID, &st.Name, &st.Type, &st.Color, &st.Description, &st.Position); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// GetState loads one workflow state.
func (s *Store) GetState(ctx context.Context, id int64) (*State, error) {
	var st State
	err := s.DB.QueryRowContext(ctx, `SELECT id, team_id, name, type, color, description, position FROM workflow_states WHERE id = ?`, id).
		Scan(&st.ID, &st.TeamID, &st.Name, &st.Type, &st.Color, &st.Description, &st.Position)
	if err != nil {
		return nil, notFound(err)
	}
	return &st, nil
}

// FirstStateOfType returns a team's first state of a type (nil when it has none).
func (s *Store) FirstStateOfType(ctx context.Context, teamID int64, typ string) *State {
	states, err := s.TeamStates(ctx, teamID)
	if err != nil {
		return nil
	}
	for _, st := range states {
		if st.Type == typ {
			return &st
		}
	}
	return nil
}

// ValidStateType reports whether t is a workflow state type.
func ValidStateType(t string) bool {
	for _, x := range StateTypes {
		if x == t {
			return true
		}
	}
	return false
}

// SaveState creates or updates a workflow state.
func (s *Store) SaveState(ctx context.Context, st *State) error {
	if st.ID == 0 {
		if st.Position == 0 {
			_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(position), 0) + 1 FROM workflow_states WHERE team_id = ?`, st.TeamID).Scan(&st.Position)
		}
		res, err := s.DB.ExecContext(ctx, `INSERT INTO workflow_states (team_id, name, type, color, description, position) VALUES (?, ?, ?, ?, ?, ?)`,
			st.TeamID, st.Name, st.Type, st.Color, st.Description, st.Position)
		if err != nil {
			return err
		}
		st.ID, _ = res.LastInsertId()
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE workflow_states SET name = ?, type = ?, color = ?, description = ?, position = ? WHERE id = ?`,
		st.Name, st.Type, st.Color, st.Description, st.Position, st.ID)
	return err
}

// DeleteState removes a workflow state, moving its issues to another one first.
func (s *Store) DeleteState(ctx context.Context, id, moveTo int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, `UPDATE issues SET state_id = ?, updated_at = ? WHERE state_id = ?`, moveTo, Now(), id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE teams SET default_state_id = NULL WHERE default_state_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM workflow_states WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// CountIssuesInState counts the issues (trash included) in a state.
func (s *Store) CountIssuesInState(ctx context.Context, id int64) int {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM issues WHERE state_id = ?`, id).Scan(&n)
	return n
}

// ---------- labels ----------

// TeamLabels lists a team's labels with how many open issues carry each.
func (s *Store) TeamLabels(ctx context.Context, teamID int64) ([]Label, error) {
	return s.labelsWhere(ctx, `l.team_id = ?`, teamID)
}

// LabelsOf loads the labels of several teams.
func (s *Store) LabelsOf(ctx context.Context, teamIDs []int64) ([]Label, error) {
	if len(teamIDs) == 0 {
		return []Label{}, nil
	}
	q, args := inInts(teamIDs)
	return s.labelsWhere(ctx, `l.team_id IN (`+q+`)`, args...)
}

func (s *Store) labelsWhere(ctx context.Context, where string, args ...any) ([]Label, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT l.id, l.team_id, l.parent_id, l.name, l.color, l.description, l.is_group, l.position,
		(SELECT COUNT(1) FROM issue_labels il JOIN issues i ON i.id = il.issue_id WHERE il.label_id = l.id AND i.deleted_at IS NULL AND i.archived_at IS NULL)
		FROM labels l WHERE `+where+` ORDER BY l.team_id, l.position, lower(l.name)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Label{}
	for rows.Next() {
		var l Label
		var parent sql.NullInt64
		if err := rows.Scan(&l.ID, &l.TeamID, &parent, &l.Name, &l.Color, &l.Description, &l.IsGroup, &l.Position, &l.Issues); err != nil {
			return nil, err
		}
		l.ParentID = nullI64(parent)
		out = append(out, l)
	}
	return out, rows.Err()
}

// GetLabel loads one label.
func (s *Store) GetLabel(ctx context.Context, id int64) (*Label, error) {
	var l Label
	var parent sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT id, team_id, parent_id, name, color, description, is_group, position FROM labels WHERE id = ?`, id).
		Scan(&l.ID, &l.TeamID, &parent, &l.Name, &l.Color, &l.Description, &l.IsGroup, &l.Position)
	if err != nil {
		return nil, notFound(err)
	}
	l.ParentID = nullI64(parent)
	return &l, nil
}

// SaveLabel creates or updates a label.
func (s *Store) SaveLabel(ctx context.Context, l *Label) error {
	if l.ID == 0 {
		if l.Position == 0 {
			_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(position), 0) + 1 FROM labels WHERE team_id = ?`, l.TeamID).Scan(&l.Position)
		}
		res, err := s.DB.ExecContext(ctx, `INSERT INTO labels (team_id, parent_id, name, color, description, is_group, position) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			l.TeamID, ptrArg(l.ParentID), l.Name, l.Color, l.Description, l.IsGroup, l.Position)
		if err != nil {
			return err
		}
		l.ID, _ = res.LastInsertId()
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE labels SET parent_id = ?, name = ?, color = ?, description = ?, is_group = ?, position = ? WHERE id = ?`,
		ptrArg(l.ParentID), l.Name, l.Color, l.Description, l.IsGroup, l.Position, l.ID)
	return err
}

// DeleteLabel removes a label (and the labels of a group).
func (s *Store) DeleteLabel(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM labels WHERE id = ?`, id)
	return err
}

// TeamCounts returns the numbers shown on a team's card.
func (s *Store) TeamCounts(ctx context.Context, teamID int64) map[string]int {
	out := map[string]int{}
	for k, q := range map[string]string{
		"open":     `SELECT COUNT(1) FROM issues i JOIN workflow_states ws ON ws.id = i.state_id WHERE i.team_id = ? AND i.deleted_at IS NULL AND ws.type IN ('triage','backlog','unstarted','started')`,
		"started":  `SELECT COUNT(1) FROM issues i JOIN workflow_states ws ON ws.id = i.state_id WHERE i.team_id = ? AND i.deleted_at IS NULL AND ws.type = 'started'`,
		"triage":   `SELECT COUNT(1) FROM issues i JOIN workflow_states ws ON ws.id = i.state_id WHERE i.team_id = ? AND i.deleted_at IS NULL AND ws.type = 'triage'`,
		"members":  `SELECT COUNT(1) FROM team_members WHERE team_id = ?`,
		"projects": `SELECT COUNT(1) FROM project_teams pt JOIN projects p ON p.id = pt.project_id WHERE pt.team_id = ? AND p.deleted_at IS NULL`,
	} {
		var n int
		if err := s.DB.QueryRowContext(ctx, q, teamID).Scan(&n); err == nil {
			out[k] = n
		}
	}
	return out
}
