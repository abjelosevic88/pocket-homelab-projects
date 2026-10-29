package store

import (
	"context"
	"database/sql"
)

// IssueDoc is a shared note on an issue: the whole team sees and edits it.
type IssueDoc struct {
	ID         int64  `json:"id"`
	IssueID    int64  `json:"issue_id"`
	Title      string `json:"title"`
	Body       string `json:"body,omitempty"`
	SourceNote string `json:"source_note"` // the sharer's Pocket Notes note ("" = written here)
	SharedAt   string `json:"shared_at"`
	CreatedBy  *int64 `json:"created_by"`
	UpdatedBy  *int64 `json:"updated_by"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

const issueDocCols = `id, issue_id, title, body, source_note, shared_at, created_by, updated_by, created_at, updated_at`

func scanIssueDoc(row interface{ Scan(...any) error }) (*IssueDoc, error) {
	var d IssueDoc
	var cb, ub sql.NullInt64
	if err := row.Scan(&d.ID, &d.IssueID, &d.Title, &d.Body, &d.SourceNote, &d.SharedAt, &cb, &ub, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	d.CreatedBy, d.UpdatedBy = nullI64(cb), nullI64(ub)
	return &d, nil
}

// IssueDocs lists an issue's shared notes, newest first; withBody adds the text.
func (s *Store) IssueDocs(ctx context.Context, issueID int64, withBody bool) ([]IssueDoc, error) {
	cols := `id, issue_id, title, '', source_note, shared_at, created_by, updated_by, created_at, updated_at`
	if withBody {
		cols = issueDocCols
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+cols+` FROM issue_docs WHERE issue_id = ? ORDER BY created_at DESC, id DESC`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IssueDoc{}
	for rows.Next() {
		d, err := scanIssueDoc(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// GetIssueDoc loads one shared note with its text.
func (s *Store) GetIssueDoc(ctx context.Context, id int64) (*IssueDoc, error) {
	return scanIssueDoc(s.DB.QueryRowContext(ctx, `SELECT `+issueDocCols+` FROM issue_docs WHERE id = ?`, id))
}

// SharedCopy is the copy a person shared from one of their notes onto the issue.
func (s *Store) SharedCopy(ctx context.Context, issueID, userID int64, noteID string) (*IssueDoc, error) {
	return scanIssueDoc(s.DB.QueryRowContext(ctx, `SELECT `+issueDocCols+` FROM issue_docs WHERE issue_id = ? AND created_by = ? AND source_note = ? ORDER BY id LIMIT 1`, issueID, userID, noteID))
}

// SaveIssueDoc creates or updates a shared note.
func (s *Store) SaveIssueDoc(ctx context.Context, d *IssueDoc) error {
	now := Now()
	if d.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO issue_docs (issue_id, title, body, source_note, shared_at, created_by, updated_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			d.IssueID, d.Title, d.Body, d.SourceNote, d.SharedAt, ptrArg(d.CreatedBy), ptrArg(d.UpdatedBy), now, now)
		if err != nil {
			return err
		}
		d.ID, _ = res.LastInsertId()
		d.CreatedAt, d.UpdatedAt = now, now
	} else {
		d.UpdatedAt = now
		if _, err := s.DB.ExecContext(ctx, `UPDATE issue_docs SET title = ?, body = ?, shared_at = ?, updated_by = ?, updated_at = ? WHERE id = ?`,
			d.Title, d.Body, d.SharedAt, ptrArg(d.UpdatedBy), now, d.ID); err != nil {
			return err
		}
	}
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM issue_docs_fts WHERE rowid = ?`, d.ID)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO issue_docs_fts (rowid, title, body) VALUES (?, ?, ?)`, d.ID, d.Title, d.Body)
	return err
}

// DeleteIssueDoc removes a shared note.
func (s *Store) DeleteIssueDoc(ctx context.Context, id int64) error {
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM issue_docs_fts WHERE rowid = ?`, id)
	_, err := s.DB.ExecContext(ctx, `DELETE FROM issue_docs WHERE id = ?`, id)
	return err
}
