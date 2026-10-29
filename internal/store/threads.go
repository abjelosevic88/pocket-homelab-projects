package store

import "context"

// IssueThread is one of a person's Pocket AI conversations on an issue.
// Only that person sees it: it is never an attachment or a history entry.
type IssueThread struct {
	ID        int64
	IssueID   int64
	UserID    int64
	DraftID   string
	Action    string
	Cwd       string
	URL       string
	State     string
	CreatedAt string
	UpdatedAt string
}

const threadCols = `id, issue_id, user_id, draft_id, action, cwd, url, state, created_at, updated_at`

func scanThread(row interface{ Scan(...any) error }) (*IssueThread, error) {
	var t IssueThread
	if err := row.Scan(&t.ID, &t.IssueID, &t.UserID, &t.DraftID, &t.Action, &t.Cwd, &t.URL, &t.State, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	return &t, nil
}

// SaveThread inserts a conversation (ID 0) or updates its address and state.
func (s *Store) SaveThread(ctx context.Context, t *IssueThread) error {
	now := Now()
	if t.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO issue_threads (issue_id, user_id, draft_id, action, cwd, url, state) VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (issue_id, user_id, draft_id) DO UPDATE SET url = excluded.url, state = excluded.state, updated_at = excluded.updated_at`,
			t.IssueID, t.UserID, t.DraftID, t.Action, t.Cwd, t.URL, t.State)
		if err != nil {
			return err
		}
		t.ID, _ = res.LastInsertId()
		t.CreatedAt, t.UpdatedAt = now, now
		return nil
	}
	t.UpdatedAt = now
	_, err := s.DB.ExecContext(ctx, `UPDATE issue_threads SET url = ?, state = ?, updated_at = ? WHERE id = ?`, t.URL, t.State, now, t.ID)
	return err
}

// FindThread is the person's conversation with that Pocket AI draft on the issue.
func (s *Store) FindThread(ctx context.Context, issueID, userID int64, draftID string) (*IssueThread, error) {
	return scanThread(s.DB.QueryRowContext(ctx, `SELECT `+threadCols+` FROM issue_threads WHERE issue_id = ? AND user_id = ? AND draft_id = ?`, issueID, userID, draftID))
}

// DeleteThread takes a conversation off its issue.
func (s *Store) DeleteThread(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM issue_threads WHERE id = ?`, id)
	return err
}

// ThreadsOf lists a person's conversations started in [from, to), oldest first.
func (s *Store) ThreadsOf(ctx context.Context, userID int64, from, to string) ([]IssueThread, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+threadCols+` FROM issue_threads WHERE user_id = ? AND created_at >= ? AND created_at < ? ORDER BY created_at`, userID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IssueThread{}
	for rows.Next() {
		t, err := scanThread(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// ThreadUsersSince lists the people who started a conversation since then.
func (s *Store) ThreadUsersSince(ctx context.Context, since string) ([]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT user_id FROM issue_threads WHERE created_at > ?`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
