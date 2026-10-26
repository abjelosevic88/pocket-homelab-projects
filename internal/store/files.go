package store

import (
	"context"
	"database/sql"
	"errors"
)

// TeamFiles is where a team's issue attachments go.
type TeamFiles struct {
	TeamID    int64  `json:"team_id"`
	Mode      string `json:"mode"`     // "" = Pocket Projects' own disk | "pocket_files"
	Folder    string `json:"folder"`   // the folder in Pocket Files (its WebDAV path)
	OwnerID   *int64 `json:"owner_id"` // whose Pocket Files link reaches it
	UpdatedAt string `json:"updated_at"`
}

// GetTeamFiles returns the team's setting (Pocket Projects' disk when never saved).
func (s *Store) GetTeamFiles(ctx context.Context, teamID int64) (*TeamFiles, error) {
	t := TeamFiles{TeamID: teamID}
	var owner sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT mode, folder, owner_id, updated_at FROM team_files WHERE team_id = ?`, teamID).Scan(&t.Mode, &t.Folder, &owner, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return &t, nil
	}
	if err != nil {
		return nil, err
	}
	if owner.Valid {
		t.OwnerID = &owner.Int64
	}
	return &t, nil
}

// SaveTeamFiles stores the team's setting.
func (s *Store) SaveTeamFiles(ctx context.Context, t *TeamFiles) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO team_files(team_id, mode, folder, owner_id, updated_at) VALUES(?,?,?,?,?)
		ON CONFLICT(team_id) DO UPDATE SET mode=excluded.mode, folder=excluded.folder, owner_id=excluded.owner_id, updated_at=excluded.updated_at`,
		t.TeamID, t.Mode, t.Folder, t.OwnerID, Now())
	return err
}

// TeamFileAttachments lists the uploaded files on a team's issues.
func (s *Store) TeamFileAttachments(ctx context.Context, teamID int64) ([]Attachment, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+prefixed("a.", attachmentCols)+` FROM attachments a JOIN issues i ON i.id = a.issue_id
		WHERE i.team_id = ? AND a.kind = 'file' ORDER BY a.id`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Attachment{}
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// SetAttachmentFile records where an attachment's file is (size and meta).
func (s *Store) SetAttachmentFile(ctx context.Context, id, size int64, url string, meta []byte) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE attachments SET size = ?, url = ?, meta = ?, updated_at = ? WHERE id = ?`, size, url, string(meta), Now(), id)
	return err
}

func prefixed(p, cols string) string {
	out := ""
	for i, c := range splitCols(cols) {
		if i > 0 {
			out += ", "
		}
		out += p + c
	}
	return out
}

func splitCols(cols string) []string {
	var out []string
	cur := ""
	for _, r := range cols {
		switch r {
		case ',':
			out = append(out, cur)
			cur = ""
		case ' ', '\t', '\n':
		default:
			cur += string(r)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// TeamNotesFolder is the team's folder in Pocket Notes ("" = none chosen).
func (s *Store) TeamNotesFolder(ctx context.Context, teamID int64) string {
	var f string
	_ = s.DB.QueryRowContext(ctx, `SELECT folder FROM team_notes WHERE team_id = ?`, teamID).Scan(&f)
	return f
}

// SetTeamNotesFolder stores the team's folder in Pocket Notes.
func (s *Store) SetTeamNotesFolder(ctx context.Context, teamID int64, folder string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO team_notes(team_id, folder, updated_at) VALUES(?,?,?)
		ON CONFLICT(team_id) DO UPDATE SET folder=excluded.folder, updated_at=excluded.updated_at`, teamID, folder, Now())
	return err
}
