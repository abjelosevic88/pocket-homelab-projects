package store

import (
	"context"
	"database/sql"
	"strings"
)

// TeamItem is a note or a drawing in a team's shared space: everyone on the
// team sees it, and those who edit the team edit it.
type TeamItem struct {
	ID         int64  `json:"id"`
	TeamID     int64  `json:"team_id"`
	Kind       string `json:"kind"` // note | drawing
	Title      string `json:"title"`
	Body       string `json:"body,omitempty"`
	Snippet    string `json:"snippet"`
	DrawKind   string `json:"draw_kind,omitempty"`
	DrawData   string `json:"-"`
	Picture    string `json:"-"`
	PictureRev int64  `json:"picture_rev"`
	SourceApp  string `json:"source_app"`
	SourceID   string `json:"source_id"`
	SourceURL  string `json:"source_url,omitempty"`
	SourceRev  string `json:"-"`
	Follow     bool   `json:"follow"`
	SyncError  string `json:"sync_error"`
	SharedAt   string `json:"shared_at"`
	CreatedBy  *int64 `json:"created_by"`
	UpdatedBy  *int64 `json:"updated_by"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

const (
	ItemNote    = "note"
	ItemDrawing = "drawing"
)

const teamItemCols = `id, team_id, kind, title, body, draw_kind, draw_data, picture, picture_rev, source_app, source_id, source_url, source_rev,
	follow, sync_error, shared_at, created_by, updated_by, created_at, updated_at`

// the list leaves out the heavy parts (the text is cut to a snippet)
const teamItemListCols = `id, team_id, kind, title, substr(body, 1, 400), draw_kind, '', '', picture_rev, source_app, source_id, source_url, source_rev,
	follow, sync_error, shared_at, created_by, updated_by, created_at, updated_at`

func scanTeamItem(row interface{ Scan(...any) error }) (*TeamItem, error) {
	var t TeamItem
	var cb, ub sql.NullInt64
	if err := row.Scan(&t.ID, &t.TeamID, &t.Kind, &t.Title, &t.Body, &t.DrawKind, &t.DrawData, &t.Picture, &t.PictureRev, &t.SourceApp, &t.SourceID,
		&t.SourceURL, &t.SourceRev, &t.Follow, &t.SyncError, &t.SharedAt, &cb, &ub, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	t.CreatedBy, t.UpdatedBy = nullI64(cb), nullI64(ub)
	return &t, nil
}

// Snip is the start of a Markdown text as one line, for lists.
func Snip(body string, max int) string {
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		l = strings.TrimSpace(l)
		for _, p := range []string{"- [ ] ", "- [x] ", "- [X] ", "* [ ] ", "* [x] ", "- ", "* ", "+ "} {
			if strings.HasPrefix(l, p) {
				l = l[len(p):]
				break
			}
		}
		lines[i] = l
	}
	s := strings.Join(strings.Fields(strings.NewReplacer("#", "", "*", "", "`", "", ">", "").Replace(strings.Join(lines, "\n"))), " ")
	if r := []rune(s); len(r) > max {
		return strings.TrimSpace(string(r[:max])) + "…"
	}
	return s
}

// TeamItems lists a team's shared space, last changed first.
func (s *Store) TeamItems(ctx context.Context, teamID int64) ([]TeamItem, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+teamItemListCols+` FROM team_items WHERE team_id = ? ORDER BY updated_at DESC, id DESC`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TeamItem{}
	for rows.Next() {
		t, err := scanTeamItem(rows)
		if err != nil {
			return nil, err
		}
		if t.Kind == ItemNote {
			t.Snippet = Snip(t.Body, 160)
		}
		t.Body = ""
		out = append(out, *t)
	}
	return out, rows.Err()
}

// GetTeamItem loads one item with everything.
func (s *Store) GetTeamItem(ctx context.Context, id int64) (*TeamItem, error) {
	return scanTeamItem(s.DB.QueryRowContext(ctx, `SELECT `+teamItemCols+` FROM team_items WHERE id = ?`, id))
}

// TeamCopy is the copy a person shared from one of their own notes or drawings.
func (s *Store) TeamCopy(ctx context.Context, teamID, userID int64, app, sourceID string) (*TeamItem, error) {
	return scanTeamItem(s.DB.QueryRowContext(ctx, `SELECT `+teamItemCols+` FROM team_items
		WHERE team_id = ? AND created_by = ? AND source_app = ? AND source_id = ? ORDER BY id LIMIT 1`, teamID, userID, app, sourceID))
}

// FollowedDrawings are the shared drawings that follow their original.
func (s *Store) FollowedDrawings(ctx context.Context) ([]TeamItem, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+teamItemListCols+` FROM team_items
		WHERE kind = 'drawing' AND source_app = 'draw' AND follow = 1 AND created_by IS NOT NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TeamItem{}
	for rows.Next() {
		t, err := scanTeamItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// SaveTeamItem creates or updates an item (and its search entry).
func (s *Store) SaveTeamItem(ctx context.Context, t *TeamItem) error {
	now := Now()
	if t.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO team_items (team_id, kind, title, body, draw_kind, draw_data, picture, picture_rev, source_app, source_id,
			source_url, source_rev, follow, sync_error, shared_at, created_by, updated_by, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			t.TeamID, t.Kind, t.Title, t.Body, t.DrawKind, t.DrawData, t.Picture, t.PictureRev, t.SourceApp, t.SourceID, t.SourceURL, t.SourceRev,
			t.Follow, t.SyncError, t.SharedAt, ptrArg(t.CreatedBy), ptrArg(t.UpdatedBy), now, now)
		if err != nil {
			return err
		}
		t.ID, _ = res.LastInsertId()
		t.CreatedAt, t.UpdatedAt = now, now
	} else {
		t.UpdatedAt = now
		if _, err := s.DB.ExecContext(ctx, `UPDATE team_items SET title = ?, body = ?, draw_kind = ?, draw_data = ?, picture = ?, picture_rev = ?,
			source_url = ?, source_rev = ?, follow = ?, sync_error = ?, shared_at = ?, updated_by = ?, updated_at = ? WHERE id = ?`,
			t.Title, t.Body, t.DrawKind, t.DrawData, t.Picture, t.PictureRev, t.SourceURL, t.SourceRev, t.Follow, t.SyncError, t.SharedAt,
			ptrArg(t.UpdatedBy), now, t.ID); err != nil {
			return err
		}
	}
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM team_items_fts WHERE rowid = ?`, t.ID)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO team_items_fts (rowid, title, body) VALUES (?, ?, ?)`, t.ID, t.Title, t.Body)
	return err
}

// SetTeamItemSync notes how following the original went, without counting
// as an edit (the item keeps its place in the list).
func (s *Store) SetTeamItemSync(ctx context.Context, id int64, syncErr string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE team_items SET sync_error = ? WHERE id = ?`, syncErr, id)
	return err
}

// DeleteTeamItem removes an item.
func (s *Store) DeleteTeamItem(ctx context.Context, id int64) error {
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM team_items_fts WHERE rowid = ?`, id)
	_, err := s.DB.ExecContext(ctx, `DELETE FROM team_items WHERE id = ?`, id)
	return err
}
