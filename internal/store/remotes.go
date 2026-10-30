package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
)

// Remote is another Pocket Projects instance a person connected with their
// own API token there. Only that person sees or uses it.
type Remote struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"user_id"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	Token      string `json:"-"`
	Proxy      string `json:"proxy"`
	RemoteUser string `json:"remote_user"`
	InboxSeen  int64  `json:"-"`
	Unread     int    `json:"unread"` // the person's unread inbox there, as last polled
	TeamKeys   string `json:"-"`      // their team keys there, "ATH,OPS"
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

// Host is the other instance's host name (projects.example.com).
func (r *Remote) Host() string {
	if u, err := url.Parse(r.URL); err == nil && u.Host != "" {
		return u.Host
	}
	return r.URL
}

const remoteCols = `id, user_id, name, url, token, proxy, remote_user, inbox_seen, unread, team_keys, created_at, updated_at`

func scanRemote(row interface{ Scan(...any) error }) (*Remote, error) {
	var r Remote
	if err := row.Scan(&r.ID, &r.UserID, &r.Name, &r.URL, &r.Token, &r.Proxy, &r.RemoteUser, &r.InboxSeen, &r.Unread, &r.TeamKeys, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	return &r, nil
}

// Remotes lists a person's connected instances.
func (s *Store) Remotes(ctx context.Context, userID int64) ([]Remote, error) {
	return s.queryRemotes(ctx, `SELECT `+remoteCols+` FROM remotes WHERE user_id = ? ORDER BY lower(name), id`, userID)
}

// AllRemotes is every person's connections (the inbox poll).
func (s *Store) AllRemotes(ctx context.Context) ([]Remote, error) {
	return s.queryRemotes(ctx, `SELECT `+remoteCols+` FROM remotes ORDER BY id`)
}

// HasRemotes: whether the person connected any other instance.
func (s *Store) HasRemotes(ctx context.Context, userID int64) bool {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM remotes WHERE user_id = ?`, userID).Scan(&n)
	return n > 0
}

// SetRemoteTeamKeys records the person's team keys over there.
func (s *Store) SetRemoteTeamKeys(ctx context.Context, id int64, keys string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE remotes SET team_keys = ? WHERE id = ?`, keys, id)
	return err
}

// SetRemoteInbox records the poll of the person's inbox over there.
func (s *Store) SetRemoteInbox(ctx context.Context, id, seen int64, unread int) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE remotes SET inbox_seen = ?, unread = ? WHERE id = ?`, seen, unread, id)
	return err
}

func (s *Store) queryRemotes(ctx context.Context, q string, args ...any) ([]Remote, error) {
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Remote{}
	for rows.Next() {
		r, err := scanRemote(rows)
		if err != nil {
			return nil, err
		}
		r.Token = s.unseal(r.Token)
		out = append(out, *r)
	}
	return out, rows.Err()
}

// GetRemote loads one of the person's connected instances.
func (s *Store) GetRemote(ctx context.Context, id, userID int64) (*Remote, error) {
	r, err := scanRemote(s.DB.QueryRowContext(ctx, `SELECT `+remoteCols+` FROM remotes WHERE id = ? AND user_id = ?`, id, userID))
	if err != nil {
		return nil, err
	}
	r.Token = s.unseal(r.Token)
	return r, nil
}

// SaveRemote creates (ID 0) or updates a connected instance.
func (s *Store) SaveRemote(ctx context.Context, r *Remote) error {
	now := Now()
	if r.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO remotes (user_id, name, url, token, proxy, remote_user, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			r.UserID, r.Name, r.URL, s.seal(r.Token), r.Proxy, r.RemoteUser, now, now)
		if err != nil {
			return err
		}
		r.ID, _ = res.LastInsertId()
		r.CreatedAt, r.UpdatedAt = now, now
		return nil
	}
	r.UpdatedAt = now
	// another address is another inbox: start its poll afresh
	_, err := s.DB.ExecContext(ctx, `UPDATE remotes SET name = ?, url = ?, token = ?, proxy = ?, remote_user = ?, updated_at = ?,
		inbox_seen = CASE WHEN url = ? THEN inbox_seen ELSE -1 END, unread = CASE WHEN url = ? THEN unread ELSE 0 END WHERE id = ? AND user_id = ?`,
		r.Name, r.URL, s.seal(r.Token), r.Proxy, r.RemoteUser, now, r.URL, r.URL, r.ID, r.UserID)
	return err
}

// DeleteRemote disconnects an instance (its conversations' records and the
// person's setup for its teams go with it; nothing over there changes).
func (s *Store) DeleteRemote(ctx context.Context, id, userID int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM remotes WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		_, err = s.DB.ExecContext(ctx, `DELETE FROM action_templates WHERE user_id = ? AND scope LIKE ?`, userID, fmt.Sprintf("remote:%d:%%", id))
	}
	return err
}

// RemoteThread is one of the person's Pocket AI conversations on an issue of
// a connected instance.
type RemoteThread struct {
	ID        int64
	RemoteID  int64
	Issue     string
	DraftID   string
	Action    string
	Cwd       string
	URL       string
	State     string
	CreatedAt string
	UpdatedAt string
}

const remoteThreadCols = `id, remote_id, issue, draft_id, action, cwd, url, state, created_at, updated_at`

func scanRemoteThread(row interface{ Scan(...any) error }) (*RemoteThread, error) {
	var t RemoteThread
	if err := row.Scan(&t.ID, &t.RemoteID, &t.Issue, &t.DraftID, &t.Action, &t.Cwd, &t.URL, &t.State, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	return &t, nil
}

// SaveRemoteThread inserts a conversation (ID 0) or updates its address and state.
func (s *Store) SaveRemoteThread(ctx context.Context, t *RemoteThread) error {
	now := Now()
	if t.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO remote_threads (remote_id, issue, draft_id, action, cwd, url, state, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (remote_id, issue, draft_id) DO UPDATE SET url = excluded.url, state = excluded.state, updated_at = excluded.updated_at`,
			t.RemoteID, t.Issue, t.DraftID, t.Action, t.Cwd, t.URL, t.State, now, now)
		if err != nil {
			return err
		}
		t.ID, _ = res.LastInsertId()
		t.CreatedAt, t.UpdatedAt = now, now
		return nil
	}
	t.UpdatedAt = now
	_, err := s.DB.ExecContext(ctx, `UPDATE remote_threads SET url = ?, state = ?, updated_at = ? WHERE id = ?`, t.URL, t.State, now, t.ID)
	return err
}

// FindRemoteThread is the conversation with that Pocket AI draft on the issue.
func (s *Store) FindRemoteThread(ctx context.Context, remoteID int64, issue, draftID string) (*RemoteThread, error) {
	return scanRemoteThread(s.DB.QueryRowContext(ctx, `SELECT `+remoteThreadCols+` FROM remote_threads WHERE remote_id = ? AND issue = ? AND draft_id = ?`, remoteID, issue, draftID))
}

// DeleteRemoteThread takes a conversation off its issue.
func (s *Store) DeleteRemoteThread(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM remote_threads WHERE id = ?`, id)
	return err
}

// RemotePersonAI is the person's own Pocket AI setup for a connected team
// (TeamID unused; the team is its key over there).
func (s *Store) RemotePersonAI(ctx context.Context, remoteID int64, teamKey string) (*PersonAI, error) {
	var p PersonAI
	var host, acc sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT host_id, cwd, provider, account_id, model, effort, mode, updated_at FROM remote_person_ai WHERE remote_id = ? AND team_key = ?`, remoteID, teamKey).
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

// SaveRemotePersonAI stores the person's setup for a connected team; an empty one is removed.
func (s *Store) SaveRemotePersonAI(ctx context.Context, remoteID int64, teamKey string, p *PersonAI) error {
	if p.Empty() {
		_, err := s.DB.ExecContext(ctx, `DELETE FROM remote_person_ai WHERE remote_id = ? AND team_key = ?`, remoteID, teamKey)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO remote_person_ai(remote_id, team_key, host_id, cwd, provider, account_id, model, effort, mode, updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(remote_id, team_key) DO UPDATE SET host_id=excluded.host_id, cwd=excluded.cwd, provider=excluded.provider, account_id=excluded.account_id,
		model=excluded.model, effort=excluded.effort, mode=excluded.mode, updated_at=excluded.updated_at`,
		remoteID, teamKey, p.HostID, p.Cwd, p.Provider, p.AccountID, p.Model, p.Effort, p.Mode, Now())
	return err
}
