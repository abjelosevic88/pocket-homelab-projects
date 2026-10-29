package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// TeamAI is how a team's Pocket AI code conversations start: the code
// folder, the agent and its settings, the team's instructions (a system
// prompt) and its memory (durable facts the agent saved with approval).
type TeamAI struct {
	TeamID       int64  `json:"team_id"`
	Cwd          string `json:"cwd"`
	Provider     string `json:"provider"`
	AccountID    *int64 `json:"account_id"`
	HostID       int64  `json:"host_id"` // the Pocket AI machine it runs on, 0 = Pocket AI's own server
	Model        string `json:"model"`
	Effort       string `json:"effort"`
	Mode         string `json:"mode"`
	Instructions string `json:"instructions"`
	Memory       string `json:"memory"`
	SendNow      bool   `json:"send_now"`  // send the prompt as the conversation opens
	LinkRefs     bool   `json:"link_refs"` // branches, commits and PRs mention the issue and link back
	UpdatedAt    string `json:"updated_at"`
}

// MaxAIText caps the instructions and the memory of a team.
const MaxAIText = 32 << 10

// ErrMemoryFull is returned when an appended fact would not fit.
var ErrMemoryFull = errors.New("memory full")

// GetTeamAI returns the team's settings (empty ones when never saved).
func (s *Store) GetTeamAI(ctx context.Context, teamID int64) (*TeamAI, error) {
	t := TeamAI{TeamID: teamID}
	var acc sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT cwd, provider, account_id, model, effort, mode, instructions, memory, send_now, link_refs, host_id, updated_at FROM team_ai WHERE team_id = ?`, teamID).
		Scan(&t.Cwd, &t.Provider, &acc, &t.Model, &t.Effort, &t.Mode, &t.Instructions, &t.Memory, &t.SendNow, &t.LinkRefs, &t.HostID, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return &t, nil
	}
	if err != nil {
		return nil, err
	}
	if acc.Valid {
		t.AccountID = &acc.Int64
	}
	return &t, nil
}

// SaveTeamAI stores everything but the memory.
func (s *Store) SaveTeamAI(ctx context.Context, t *TeamAI) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO team_ai(team_id, cwd, provider, account_id, model, effort, mode, instructions, send_now, link_refs, host_id, updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(team_id) DO UPDATE SET cwd=excluded.cwd, host_id=excluded.host_id, provider=excluded.provider, account_id=excluded.account_id, model=excluded.model,
		effort=excluded.effort, mode=excluded.mode, instructions=excluded.instructions, send_now=excluded.send_now, link_refs=excluded.link_refs, updated_at=excluded.updated_at`,
		t.TeamID, t.Cwd, t.Provider, t.AccountID, t.Model, t.Effort, t.Mode, t.Instructions, t.SendNow, t.LinkRefs, t.HostID, Now())
	return err
}

// SetTeamMemory replaces the memory.
func (s *Store) SetTeamMemory(ctx context.Context, teamID int64, memory string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO team_ai(team_id, memory, updated_at) VALUES(?,?,?)
		ON CONFLICT(team_id) DO UPDATE SET memory=excluded.memory, updated_at=excluded.updated_at`, teamID, memory, Now())
	return err
}

// AppendTeamMemory adds one line; ErrMemoryFull when it would pass MaxAIText.
func (s *Store) AppendTeamMemory(ctx context.Context, teamID int64, line string) (string, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var mem string
	err = tx.QueryRowContext(ctx, `SELECT memory FROM team_ai WHERE team_id = ?`, teamID).Scan(&mem)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	mem = strings.TrimRight(mem, "\n")
	if mem != "" {
		mem += "\n"
	}
	mem += line
	if len(mem) > MaxAIText {
		return "", ErrMemoryFull
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO team_ai(team_id, memory, updated_at) VALUES(?,?,?)
		ON CONFLICT(team_id) DO UPDATE SET memory=excluded.memory, updated_at=excluded.updated_at`, teamID, mem, Now()); err != nil {
		return "", err
	}
	return mem, tx.Commit()
}

// ProjectAICwd is a project's code folder override ("" = the team's).
func (s *Store) ProjectAICwd(ctx context.Context, projectID int64) string {
	var cwd string
	_ = s.DB.QueryRowContext(ctx, `SELECT cwd FROM project_ai WHERE project_id = ?`, projectID).Scan(&cwd)
	return cwd
}

// SetProjectAICwd sets (or with "" clears) a project's code folder.
func (s *Store) SetProjectAICwd(ctx context.Context, projectID int64, cwd string) error {
	if cwd == "" {
		_, err := s.DB.ExecContext(ctx, `DELETE FROM project_ai WHERE project_id = ?`, projectID)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO project_ai(project_id, cwd, updated_at) VALUES(?,?,?)
		ON CONFLICT(project_id) DO UPDATE SET cwd=excluded.cwd, updated_at=excluded.updated_at`, projectID, cwd, Now())
	return err
}

// PersonAI is a person's own setup for a team's code conversations; what
// they leave empty is the team's.
type PersonAI struct {
	TeamID    int64  `json:"team_id"`
	UserID    int64  `json:"user_id"`
	HostID    *int64 `json:"host_id"`    // nil = the team's machine
	Cwd       string `json:"cwd"`        // "" = the team's (or the project's) folder
	Provider  string `json:"provider"`   // "" = the team's
	AccountID *int64 `json:"account_id"` // nil = the team's, -1 = the person's default login
	Model     string `json:"model"`
	Effort    string `json:"effort"`
	Mode      string `json:"mode"`
	UpdatedAt string `json:"updated_at"`
}

// GetPersonAI returns a person's setup in a team (an empty one when never saved).
func (s *Store) GetPersonAI(ctx context.Context, teamID, userID int64) (*PersonAI, error) {
	p := PersonAI{TeamID: teamID, UserID: userID}
	var host, acc sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT host_id, cwd, provider, account_id, model, effort, mode, updated_at FROM person_ai WHERE team_id = ? AND user_id = ?`, teamID, userID).
		Scan(&host, &p.Cwd, &p.Provider, &acc, &p.Model, &p.Effort, &p.Mode, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return &p, nil
	}
	if err != nil {
		return nil, err
	}
	if host.Valid {
		p.HostID = &host.Int64
	}
	if acc.Valid {
		p.AccountID = &acc.Int64
	}
	return &p, nil
}

// Empty: nothing of the person's own (the team's setup as it is).
func (p *PersonAI) Empty() bool {
	return p.HostID == nil && p.Cwd == "" && p.Provider == "" && p.AccountID == nil && p.Model == "" && p.Effort == "" && p.Mode == ""
}

// SavePersonAI stores a person's setup; an empty one is removed.
func (s *Store) SavePersonAI(ctx context.Context, p *PersonAI) error {
	if p.Empty() {
		_, err := s.DB.ExecContext(ctx, `DELETE FROM person_ai WHERE team_id = ? AND user_id = ?`, p.TeamID, p.UserID)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO person_ai(team_id, user_id, host_id, cwd, provider, account_id, model, effort, mode, updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(team_id, user_id) DO UPDATE SET host_id=excluded.host_id, cwd=excluded.cwd, provider=excluded.provider, account_id=excluded.account_id,
		model=excluded.model, effort=excluded.effort, mode=excluded.mode, updated_at=excluded.updated_at`,
		p.TeamID, p.UserID, p.HostID, p.Cwd, p.Provider, p.AccountID, p.Model, p.Effort, p.Mode, Now())
	return err
}

// With is the team's setup with the person's on top. Own: the person works
// in a folder of their own (their machine or their folder), so a project's
// folder — a path on the team's machine — doesn't apply to them.
func (t *TeamAI) With(p *PersonAI) (out *TeamAI, own bool) {
	c := *t
	if p == nil {
		return &c, false
	}
	if p.HostID != nil && *p.HostID != c.HostID {
		c.HostID, c.Cwd, own = *p.HostID, p.Cwd, true // the team's folder is a path on another machine
	} else if p.Cwd != "" {
		c.Cwd, own = p.Cwd, true
	}
	for _, f := range []struct {
		dst *string
		v   string
	}{{&c.Provider, p.Provider}, {&c.Model, p.Model}, {&c.Effort, p.Effort}, {&c.Mode, p.Mode}} {
		if f.v != "" {
			*f.dst = f.v
		}
	}
	if p.AccountID != nil {
		c.AccountID = p.AccountID
		if *p.AccountID < 0 {
			c.AccountID = nil
		}
	}
	if c.HostID != 0 {
		c.AccountID = nil // another machine signs in with its own login
	}
	return &c, own
}
