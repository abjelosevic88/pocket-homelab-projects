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
	Model        string `json:"model"`
	Effort       string `json:"effort"`
	Mode         string `json:"mode"`
	Instructions string `json:"instructions"`
	Memory       string `json:"memory"`
	SendNow      bool   `json:"send_now"` // send the prompt as the conversation opens
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
	err := s.DB.QueryRowContext(ctx, `SELECT cwd, provider, account_id, model, effort, mode, instructions, memory, send_now, updated_at FROM team_ai WHERE team_id = ?`, teamID).
		Scan(&t.Cwd, &t.Provider, &acc, &t.Model, &t.Effort, &t.Mode, &t.Instructions, &t.Memory, &t.SendNow, &t.UpdatedAt)
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
	_, err := s.DB.ExecContext(ctx, `INSERT INTO team_ai(team_id, cwd, provider, account_id, model, effort, mode, instructions, send_now, updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(team_id) DO UPDATE SET cwd=excluded.cwd, provider=excluded.provider, account_id=excluded.account_id, model=excluded.model,
		effort=excluded.effort, mode=excluded.mode, instructions=excluded.instructions, send_now=excluded.send_now, updated_at=excluded.updated_at`,
		t.TeamID, t.Cwd, t.Provider, t.AccountID, t.Model, t.Effort, t.Mode, t.Instructions, t.SendNow, Now())
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
