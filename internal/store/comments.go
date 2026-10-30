package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

const commentCols = `id, issue_id, project_update_id, parent_id, user_id, body, source_app, created_at, edited_at, resolved_at`

func scanComment(row interface{ Scan(...any) error }) (*Comment, error) {
	var c Comment
	var iss, upd, parent, uid sql.NullInt64
	var edited, resolved sql.NullString
	if err := row.Scan(&c.ID, &iss, &upd, &parent, &uid, &c.Body, &c.SourceApp, &c.CreatedAt, &edited, &resolved); err != nil {
		return nil, notFound(err)
	}
	c.IssueID, c.ProjectUpdateID, c.ParentID, c.UserID = nullI64(iss), nullI64(upd), nullI64(parent), nullI64(uid)
	c.EditedAt, c.ResolvedAt = nullStr(edited), nullStr(resolved)
	c.Reactions = []Reaction{}
	return &c, nil
}

// Comments lists the comments of an issue (issueID > 0) or of a project update, oldest first.
func (s *Store) Comments(ctx context.Context, issueID, updateID int64) ([]Comment, error) {
	col, id := "issue_id", issueID
	if updateID > 0 {
		col, id = "project_update_id", updateID
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+commentCols+` FROM comments WHERE `+col+` = ? AND deleted_at IS NULL ORDER BY created_at, id`, id)
	if err != nil {
		return nil, err
	}
	var out []Comment
	var ids []int64
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, *c)
		ids = append(ids, c.ID)
	}
	rows.Close()
	if out == nil {
		return []Comment{}, nil
	}
	reacts, err := s.reactions(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if r := reacts[out[i].ID]; r != nil {
			out[i].Reactions = r
		}
	}
	return out, nil
}

func (s *Store) reactions(ctx context.Context, ids []int64) (map[int64][]Reaction, error) {
	out := map[int64][]Reaction{}
	if len(ids) == 0 {
		return out, nil
	}
	q, args := inInts(ids)
	rows, err := s.DB.QueryContext(ctx, `SELECT comment_id, emoji, user_id FROM reactions WHERE comment_id IN (`+q+`) ORDER BY created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, uid int64
		var emoji string
		if err := rows.Scan(&cid, &emoji, &uid); err != nil {
			return nil, err
		}
		list := out[cid]
		found := false
		for i := range list {
			if list[i].Emoji == emoji {
				list[i].UserIDs = append(list[i].UserIDs, uid)
				found = true
			}
		}
		if !found {
			list = append(list, Reaction{Emoji: emoji, UserIDs: []int64{uid}})
		}
		out[cid] = list
	}
	return out, rows.Err()
}

// GetComment loads one comment (deleted ones answer not found).
func (s *Store) GetComment(ctx context.Context, id int64) (*Comment, error) {
	c, err := scanComment(s.DB.QueryRowContext(ctx, `SELECT `+commentCols+` FROM comments WHERE id = ? AND deleted_at IS NULL`, id))
	if err != nil {
		return nil, err
	}
	r, err := s.reactions(ctx, []int64{id})
	if err == nil && r[id] != nil {
		c.Reactions = r[id]
	}
	return c, nil
}

// SaveComment creates or edits a comment; a new one on an issue subscribes its author.
func (s *Store) SaveComment(ctx context.Context, c *Comment, hooks ...TxHook) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if c.ID == 0 {
		res, err := tx.ExecContext(ctx, `INSERT INTO comments (issue_id, project_update_id, parent_id, user_id, body, source_app) VALUES (?, ?, ?, ?, ?, ?)`,
			ptrArg(c.IssueID), ptrArg(c.ProjectUpdateID), ptrArg(c.ParentID), ptrArg(c.UserID), c.Body, c.SourceApp)
		if err != nil {
			return err
		}
		c.ID, _ = res.LastInsertId()
		c.CreatedAt = Now()
		if c.IssueID != nil {
			if c.UserID != nil {
				if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO issue_subscribers (issue_id, user_id) VALUES (?, ?)`, *c.IssueID, *c.UserID); err != nil {
					return err
				}
			}
			if _, err := tx.ExecContext(ctx, `UPDATE issues SET updated_at = ? WHERE id = ?`, Now(), *c.IssueID); err != nil {
				return err
			}
		}
	} else {
		now := Now()
		c.EditedAt = &now
		if _, err := tx.ExecContext(ctx, `UPDATE comments SET body = ?, edited_at = ?, resolved_at = ? WHERE id = ?`, c.Body, now, c.ResolvedAt, c.ID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM comments_fts WHERE rowid = ?`, c.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO comments_fts (rowid, body) VALUES (?, ?)`, c.ID, c.Body); err != nil {
		return err
	}
	for _, h := range hooks {
		if err := h(tx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetResolved marks a comment thread resolved or open again.
func (s *Store) SetResolved(ctx context.Context, id int64, resolved bool) error {
	var v any
	if resolved {
		v = Now()
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE comments SET resolved_at = ? WHERE id = ?`, v, id)
	return err
}

// DeleteComment hides a comment (and its replies).
func (s *Store) DeleteComment(ctx context.Context, id int64) error {
	now := Now()
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM comments_fts WHERE rowid IN (SELECT id FROM comments WHERE id = ? OR parent_id = ?)`, id, id)
	_, err := s.DB.ExecContext(ctx, `UPDATE comments SET deleted_at = ? WHERE id = ? OR parent_id = ?`, now, id, id)
	return err
}

// ToggleReaction adds or removes one emoji of a person on a comment; it reports whether it is now on.
func (s *Store) ToggleReaction(ctx context.Context, commentID, userID int64, emoji string) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM reactions WHERE comment_id = ? AND user_id = ? AND emoji = ?`, commentID, userID, emoji)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return false, nil
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO reactions (comment_id, user_id, emoji) VALUES (?, ?, ?)`, commentID, userID, emoji)
	return err == nil, err
}

// ---------- attachments ----------

const attachmentCols = `id, issue_id, project_id, kind, title, subtitle, url, file_name, mime, size, ext_app, ext_id, state, meta, created_by, created_at, updated_at`

func scanAttachment(row interface{ Scan(...any) error }) (*Attachment, error) {
	var a Attachment
	var iss, proj, by sql.NullInt64
	var meta string
	if err := row.Scan(&a.ID, &iss, &proj, &a.Kind, &a.Title, &a.Subtitle, &a.URL, &a.FileName, &a.Mime, &a.Size, &a.ExtApp, &a.ExtID, &a.State, &meta, &by, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	a.IssueID, a.ProjectID, a.CreatedBy = nullI64(iss), nullI64(proj), nullI64(by)
	if meta == "" {
		meta = "{}"
	}
	a.Meta = json.RawMessage(meta)
	return &a, nil
}

// Attachments lists what is attached to an issue (issueID > 0) or a project.
func (s *Store) Attachments(ctx context.Context, issueID, projectID int64) ([]Attachment, error) {
	col, id := "issue_id", issueID
	if projectID > 0 {
		col, id = "project_id", projectID
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+attachmentCols+` FROM attachments WHERE `+col+` = ? ORDER BY created_at, id`, id)
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

// GetAttachment loads one attachment.
func (s *Store) GetAttachment(ctx context.Context, id int64) (*Attachment, error) {
	return scanAttachment(s.DB.QueryRowContext(ctx, `SELECT `+attachmentCols+` FROM attachments WHERE id = ?`, id))
}

// FindAttachment finds an attachment of an issue by its external app and id.
func (s *Store) FindAttachment(ctx context.Context, issueID int64, extApp, extID string) (*Attachment, error) {
	return scanAttachment(s.DB.QueryRowContext(ctx, `SELECT `+attachmentCols+` FROM attachments WHERE issue_id = ? AND ext_app = ? AND ext_id = ? LIMIT 1`, issueID, extApp, extID))
}

// SaveAttachment creates or updates an attachment.
func (s *Store) SaveAttachment(ctx context.Context, a *Attachment) error {
	if len(a.Meta) == 0 {
		a.Meta = json.RawMessage("{}")
	}
	now := Now()
	if a.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO attachments (issue_id, project_id, kind, title, subtitle, url, file_name, mime, size, ext_app, ext_id, state, meta, created_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, ptrArg(a.IssueID), ptrArg(a.ProjectID), a.Kind, a.Title, a.Subtitle, a.URL, a.FileName, a.Mime, a.Size,
			a.ExtApp, a.ExtID, a.State, string(a.Meta), ptrArg(a.CreatedBy))
		if err != nil {
			return err
		}
		a.ID, _ = res.LastInsertId()
		a.CreatedAt, a.UpdatedAt = now, now
		return nil
	}
	a.UpdatedAt = now
	_, err := s.DB.ExecContext(ctx, `UPDATE attachments SET title = ?, subtitle = ?, url = ?, state = ?, meta = ?, updated_at = ? WHERE id = ?`,
		a.Title, a.Subtitle, a.URL, a.State, string(a.Meta), now, a.ID)
	return err
}

// DeleteAttachment removes an attachment row (the caller removes a stored file).
func (s *Store) DeleteAttachment(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM attachments WHERE id = ?`, id)
	return err
}

// ---------- time ----------

// TimeEntries lists time logged on an issue, newest first.
func (s *Store) TimeEntries(ctx context.Context, issueID int64) ([]TimeEntry, error) {
	return s.timeWhere(ctx, `issue_id = ?`, issueID)
}

// TimeEntriesBetween lists a set of people's time in a date range on the given teams.
func (s *Store) TimeEntriesBetween(ctx context.Context, teamIDs []int64, userID int64, from, to string) ([]TimeEntry, error) {
	if len(teamIDs) == 0 {
		return []TimeEntry{}, nil
	}
	q, args := inInts(teamIDs)
	where := `spent_on >= ? AND spent_on <= ? AND issue_id IN (SELECT id FROM issues WHERE team_id IN (` + q + `) AND deleted_at IS NULL)`
	args = append([]any{from, to}, args...)
	if userID > 0 {
		where += ` AND user_id = ?`
		args = append(args, userID)
	}
	return s.timeWhere(ctx, where, args...)
}

func (s *Store) timeWhere(ctx context.Context, where string, args ...any) ([]TimeEntry, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, issue_id, user_id, spent_on, minutes, note, started_at, created_at, source, external_id, began_at FROM time_entries WHERE `+where+` ORDER BY spent_on DESC, id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TimeEntry{}
	for rows.Next() {
		var t TimeEntry
		if err := rows.Scan(&t.ID, &t.IssueID, &t.UserID, &t.SpentOn, &t.Minutes, &t.Note, &t.StartedAt, &t.CreatedAt, &t.Source, &t.ExternalID, &t.BeganAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetTimeEntry loads one time entry.
func (s *Store) GetTimeEntry(ctx context.Context, id int64) (*TimeEntry, error) {
	var t TimeEntry
	err := s.DB.QueryRowContext(ctx, `SELECT id, issue_id, user_id, spent_on, minutes, note, started_at, created_at, source, external_id, began_at FROM time_entries WHERE id = ?`, id).
		Scan(&t.ID, &t.IssueID, &t.UserID, &t.SpentOn, &t.Minutes, &t.Note, &t.StartedAt, &t.CreatedAt, &t.Source, &t.ExternalID, &t.BeganAt)
	return &t, notFound(err)
}

// SaveTimeEntry creates or updates a time entry.
func (s *Store) SaveTimeEntry(ctx context.Context, t *TimeEntry) error {
	if t.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO time_entries (issue_id, user_id, spent_on, minutes, note, started_at, source, external_id, began_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			t.IssueID, t.UserID, t.SpentOn, t.Minutes, t.Note, t.StartedAt, t.Source, t.ExternalID, t.BeganAt)
		if err != nil {
			return err
		}
		t.ID, _ = res.LastInsertId()
		t.CreatedAt = Now()
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE time_entries SET issue_id = ?, spent_on = ?, minutes = ?, note = ?, started_at = ?, began_at = ? WHERE id = ?`,
		t.IssueID, t.SpentOn, t.Minutes, t.Note, t.StartedAt, t.BeganAt, t.ID)
	return err
}

// TimeEntryByExternal finds a person's entry that an app logged under its own id.
func (s *Store) TimeEntryByExternal(ctx context.Context, userID int64, source, externalID string) (*TimeEntry, error) {
	list, err := s.timeWhere(ctx, `user_id = ? AND source = ? AND external_id = ?`, userID, source, externalID)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return &list[0], nil
}

// FinishedTimeOf lists a person's own finished time (no running timer) in a
// date range, on issues not in the trash.
func (s *Store) FinishedTimeOf(ctx context.Context, userID int64, from, to string) ([]TimeEntry, error) {
	return s.timeWhere(ctx, `user_id = ? AND started_at = '' AND spent_on >= ? AND spent_on <= ?
		AND issue_id IN (SELECT id FROM issues WHERE deleted_at IS NULL)`, userID, from, to)
}

// RecentTimeIssues lists the issues a person logged time on lately, most recent first.
func (s *Store) RecentTimeIssues(ctx context.Context, userID int64, limit int) []int64 {
	rows, err := s.DB.QueryContext(ctx, `SELECT issue_id FROM time_entries WHERE user_id = ? GROUP BY issue_id ORDER BY MAX(id) DESC LIMIT ?`, userID, limit)
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

// DeleteTimeEntry removes a time entry.
func (s *Store) DeleteTimeEntry(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM time_entries WHERE id = ?`, id)
	return err
}

// RunningTimer returns a person's running timer (nil when none).
func (s *Store) RunningTimer(ctx context.Context, userID int64) *TimeEntry {
	list, err := s.timeWhere(ctx, `user_id = ? AND started_at <> ''`, userID)
	if err != nil || len(list) == 0 {
		return nil
	}
	return &list[0]
}

// TimeTotals sums minutes per issue for a set of issues.
func (s *Store) TimeTotals(ctx context.Context, issueIDs []int64) map[int64]int {
	out := map[int64]int{}
	if len(issueIDs) == 0 {
		return out
	}
	q, args := inInts(issueIDs)
	rows, err := s.DB.QueryContext(ctx, `SELECT issue_id, SUM(minutes) FROM time_entries WHERE issue_id IN (`+q+`) GROUP BY issue_id`, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var n int
		if rows.Scan(&id, &n) == nil {
			out[id] = n
		}
	}
	return out
}

// ---------- notifications ----------

// AddNotification puts an entry in a person's inbox. A same-kind entry about
// the same issue that is still unread is refreshed instead of duplicated.
func (s *Store) AddNotification(ctx context.Context, n *Notification) error {
	if n.IssueID != nil && n.Kind != "comment" && n.Kind != "mentioned" {
		res, err := s.DB.ExecContext(ctx, `UPDATE notifications SET title = ?, body = ?, actor_id = ?, actor_app = ?, created_at = ?, archived_at = NULL
			WHERE user_id = ? AND kind = ? AND issue_id = ? AND read_at IS NULL AND archived_at IS NULL`,
			n.Title, n.Body, ptrArg(n.ActorID), n.ActorApp, Now(), n.UserID, n.Kind, *n.IssueID)
		if err == nil {
			if c, _ := res.RowsAffected(); c > 0 {
				return nil
			}
		}
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO notifications (user_id, kind, issue_id, project_id, team_id, comment_id, actor_id, actor_app, title, body)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, n.UserID, n.Kind, ptrArg(n.IssueID), ptrArg(n.ProjectID), ptrArg(n.TeamID), ptrArg(n.CommentID),
		ptrArg(n.ActorID), n.ActorApp, n.Title, n.Body)
	if err != nil {
		return err
	}
	n.ID, _ = res.LastInsertId()
	return nil
}

// Notifications lists a person's inbox, newest first. filter: all | unread | snoozed.
func (s *Store) Notifications(ctx context.Context, userID int64, filter string, limit int) ([]Notification, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	now := time.Now().UTC().Format(time.RFC3339)
	where := `user_id = ? AND archived_at IS NULL AND (snoozed_until = '' OR snoozed_until <= ?)`
	args := []any{userID, now}
	switch filter {
	case "unread":
		where += ` AND read_at IS NULL`
	case "snoozed":
		where = `user_id = ? AND archived_at IS NULL AND snoozed_until > ?`
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, user_id, kind, issue_id, project_id, team_id, comment_id, actor_id, actor_app, title, body, read_at, snoozed_until, created_at
		FROM notifications WHERE `+where+` ORDER BY created_at DESC, id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Notification{}
	for rows.Next() {
		var n Notification
		var iss, proj, team, com, actor sql.NullInt64
		var read sql.NullString
		if err := rows.Scan(&n.ID, &n.UserID, &n.Kind, &iss, &proj, &team, &com, &actor, &n.ActorApp, &n.Title, &n.Body, &read, &n.SnoozedUntil, &n.CreatedAt); err != nil {
			return nil, err
		}
		n.IssueID, n.ProjectID, n.TeamID, n.CommentID, n.ActorID = nullI64(iss), nullI64(proj), nullI64(team), nullI64(com), nullI64(actor)
		n.ReadAt = nullStr(read)
		out = append(out, n)
	}
	return out, rows.Err()
}

// UnreadCount counts a person's unread, unsnoozed inbox entries.
func (s *Store) UnreadCount(ctx context.Context, userID int64) int {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM notifications WHERE user_id = ? AND read_at IS NULL AND archived_at IS NULL AND (snoozed_until = '' OR snoozed_until <= ?)`,
		userID, time.Now().UTC().Format(time.RFC3339)).Scan(&n)
	return n
}

// MarkNotifications marks some (or, with ids empty and all set, every) entry read or unread.
func (s *Store) MarkNotifications(ctx context.Context, userID int64, ids []int64, all, read bool) error {
	var v any
	if read {
		v = Now()
	}
	if all {
		_, err := s.DB.ExecContext(ctx, `UPDATE notifications SET read_at = ? WHERE user_id = ? AND archived_at IS NULL`, v, userID)
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	q, args := inInts(ids)
	_, err := s.DB.ExecContext(ctx, `UPDATE notifications SET read_at = ? WHERE user_id = ? AND id IN (`+q+`)`, append([]any{v, userID}, args...)...)
	return err
}

// MarkIssueRead marks every entry of a person about an issue read (opening the issue).
func (s *Store) MarkIssueRead(ctx context.Context, userID, issueID int64) {
	_, _ = s.DB.ExecContext(ctx, `UPDATE notifications SET read_at = ? WHERE user_id = ? AND issue_id = ? AND read_at IS NULL`, Now(), userID, issueID)
}

// SnoozeNotification hides an entry until a moment (RFC 3339); "" un-snoozes.
func (s *Store) SnoozeNotification(ctx context.Context, userID, id int64, until string) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE notifications SET snoozed_until = ?, read_at = CASE WHEN ? = '' THEN read_at ELSE NULL END WHERE user_id = ? AND id = ?`, until, until, userID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ArchiveNotifications removes entries from the inbox (all read ones when ids is empty and readOnly).
func (s *Store) ArchiveNotifications(ctx context.Context, userID int64, ids []int64, readOnly bool) error {
	if len(ids) == 0 {
		if !readOnly {
			return nil
		}
		_, err := s.DB.ExecContext(ctx, `UPDATE notifications SET archived_at = ? WHERE user_id = ? AND read_at IS NOT NULL AND archived_at IS NULL`, Now(), userID)
		return err
	}
	q, args := inInts(ids)
	_, err := s.DB.ExecContext(ctx, `UPDATE notifications SET archived_at = ? WHERE user_id = ? AND id IN (`+q+`)`, append([]any{Now(), userID}, args...)...)
	return err
}

// PruneNotifications deletes archived entries older than days and read ones older than 3×days.
func (s *Store) PruneNotifications(ctx context.Context, days int) {
	cut := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339)
	old := time.Now().UTC().AddDate(0, 0, -3*days).Format(time.RFC3339)
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM notifications WHERE (archived_at IS NOT NULL AND archived_at < ?) OR (read_at IS NOT NULL AND created_at < ?)`, cut, old)
}

// ---------- views & favorites ----------

func scanView(row interface{ Scan(...any) error }) (*View, error) {
	var v View
	var team sql.NullInt64
	var filters, display string
	if err := row.Scan(&v.ID, &v.UserID, &team, &v.Name, &v.Description, &v.Icon, &v.Color, &v.Kind, &filters, &display, &v.Shared, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	v.TeamID = nullI64(team)
	v.Filters, v.Display = json.RawMessage(orJSON(filters)), json.RawMessage(orJSON(display))
	return &v, nil
}

func orJSON(s string) string {
	if s == "" {
		return "{}"
	}
	return s
}

const viewCols = `id, user_id, team_id, name, description, icon, color, kind, filters, display, shared, created_at, updated_at`

// Views lists a person's own views and the views shared with their teams.
func (s *Store) Views(ctx context.Context, userID int64, teamIDs []int64) ([]View, error) {
	where := `user_id = ?`
	args := []any{userID}
	if len(teamIDs) > 0 {
		q, a := inInts(teamIDs)
		where += ` OR (shared = 1 AND team_id IN (` + q + `))`
		args = append(args, a...)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+viewCols+` FROM views WHERE `+where+` ORDER BY lower(name)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []View{}
	for rows.Next() {
		v, err := scanView(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

// GetView loads one view.
func (s *Store) GetView(ctx context.Context, id int64) (*View, error) {
	return scanView(s.DB.QueryRowContext(ctx, `SELECT `+viewCols+` FROM views WHERE id = ?`, id))
}

// SaveView creates or updates a view.
func (s *Store) SaveView(ctx context.Context, v *View) error {
	if len(v.Filters) == 0 {
		v.Filters = json.RawMessage("{}")
	}
	if len(v.Display) == 0 {
		v.Display = json.RawMessage("{}")
	}
	if v.Kind == "" {
		v.Kind = "issues"
	}
	now := Now()
	if v.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO views (user_id, team_id, name, description, icon, color, kind, filters, display, shared) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			v.UserID, ptrArg(v.TeamID), v.Name, v.Description, v.Icon, v.Color, v.Kind, string(v.Filters), string(v.Display), v.Shared)
		if err != nil {
			return err
		}
		v.ID, _ = res.LastInsertId()
		v.CreatedAt, v.UpdatedAt = now, now
		return nil
	}
	v.UpdatedAt = now
	_, err := s.DB.ExecContext(ctx, `UPDATE views SET team_id = ?, name = ?, description = ?, icon = ?, color = ?, kind = ?, filters = ?, display = ?, shared = ?, updated_at = ? WHERE id = ?`,
		ptrArg(v.TeamID), v.Name, v.Description, v.Icon, v.Color, v.Kind, string(v.Filters), string(v.Display), v.Shared, now, v.ID)
	return err
}

// DeleteView removes a view (and the favorites pointing at it).
func (s *Store) DeleteView(ctx context.Context, id int64) error {
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM favorites WHERE kind = 'view' AND ref_id = ?`, id)
	_, err := s.DB.ExecContext(ctx, `DELETE FROM views WHERE id = ?`, id)
	return err
}

// Favorites lists a person's sidebar favorites.
func (s *Store) Favorites(ctx context.Context, userID int64) ([]Favorite, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, kind, ref_id, sort_order FROM favorites WHERE user_id = ? ORDER BY sort_order, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Favorite{}
	for rows.Next() {
		var f Favorite
		if err := rows.Scan(&f.ID, &f.Kind, &f.RefID, &f.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// AddFavorite pins something for a person (no-op when already pinned).
func (s *Store) AddFavorite(ctx context.Context, userID int64, kind string, refID int64) (*Favorite, error) {
	var sortOrder float64
	_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order), 0) + 1 FROM favorites WHERE user_id = ?`, userID).Scan(&sortOrder)
	_, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO favorites (user_id, kind, ref_id, sort_order) VALUES (?, ?, ?, ?)`, userID, kind, refID, sortOrder)
	if err != nil {
		return nil, err
	}
	var f Favorite
	err = s.DB.QueryRowContext(ctx, `SELECT id, kind, ref_id, sort_order FROM favorites WHERE user_id = ? AND kind = ? AND ref_id = ?`, userID, kind, refID).Scan(&f.ID, &f.Kind, &f.RefID, &f.SortOrder)
	return &f, err
}

// DeleteFavorite unpins.
func (s *Store) DeleteFavorite(ctx context.Context, userID, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM favorites WHERE user_id = ? AND id = ?`, userID, id)
	return err
}

// ReorderFavorites stores a new order of a person's favorites.
func (s *Store) ReorderFavorites(ctx context.Context, userID int64, ids []int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE favorites SET sort_order = ? WHERE id = ? AND user_id = ?`, float64(i+1), id, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---------- templates ----------

const templateCols = `id, team_id, name, title, description, data, recur, recur_interval, next_at, last_issue_id, created_by, created_at, updated_at`

func scanTemplate(row interface{ Scan(...any) error }) (*Template, error) {
	var t Template
	var data string
	var last, by sql.NullInt64
	if err := row.Scan(&t.ID, &t.TeamID, &t.Name, &t.Title, &t.Description, &data, &t.Recur, &t.RecurInterval, &t.NextAt, &last, &by, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	t.Data = json.RawMessage(orJSON(data))
	t.LastIssueID, t.CreatedBy = nullI64(last), nullI64(by)
	return &t, nil
}

// Templates lists the templates of some teams.
func (s *Store) Templates(ctx context.Context, teamIDs []int64) ([]Template, error) {
	if len(teamIDs) == 0 {
		return []Template{}, nil
	}
	q, args := inInts(teamIDs)
	rows, err := s.DB.QueryContext(ctx, `SELECT `+templateCols+` FROM templates WHERE team_id IN (`+q+`) ORDER BY lower(name)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Template{}
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// GetTemplate loads one template.
func (s *Store) GetTemplate(ctx context.Context, id int64) (*Template, error) {
	return scanTemplate(s.DB.QueryRowContext(ctx, `SELECT `+templateCols+` FROM templates WHERE id = ?`, id))
}

// SaveTemplate creates or updates a template.
func (s *Store) SaveTemplate(ctx context.Context, t *Template) error {
	if len(t.Data) == 0 {
		t.Data = json.RawMessage("{}")
	}
	if t.RecurInterval <= 0 {
		t.RecurInterval = 1
	}
	now := Now()
	if t.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO templates (team_id, name, title, description, data, recur, recur_interval, next_at, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			t.TeamID, t.Name, t.Title, t.Description, string(t.Data), t.Recur, t.RecurInterval, t.NextAt, ptrArg(t.CreatedBy))
		if err != nil {
			return err
		}
		t.ID, _ = res.LastInsertId()
		t.CreatedAt, t.UpdatedAt = now, now
		return nil
	}
	t.UpdatedAt = now
	_, err := s.DB.ExecContext(ctx, `UPDATE templates SET name = ?, title = ?, description = ?, data = ?, recur = ?, recur_interval = ?, next_at = ?, last_issue_id = ?, updated_at = ? WHERE id = ?`,
		t.Name, t.Title, t.Description, string(t.Data), t.Recur, t.RecurInterval, t.NextAt, ptrArg(t.LastIssueID), now, t.ID)
	return err
}

// DeleteTemplate removes a template.
func (s *Store) DeleteTemplate(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM templates WHERE id = ?`, id)
	return err
}

// DueTemplates lists repeating templates whose next day has come.
func (s *Store) DueTemplates(ctx context.Context, today string) ([]Template, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+templateCols+` FROM templates WHERE recur <> '' AND next_at <> '' AND next_at <= ?`, today)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Template
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// ---------- webhooks ----------

// Webhooks lists a team's outgoing webhooks (teamID 0: every active one).
func (s *Store) Webhooks(ctx context.Context, teamID int64) ([]Webhook, error) {
	where, args := `team_id = ?`, []any{teamID}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, team_id, name, url, secret, events, active, last_status, last_error, last_at, created_at FROM webhooks WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Webhook{}
	for rows.Next() {
		var w Webhook
		if err := rows.Scan(&w.ID, &w.TeamID, &w.Name, &w.URL, &w.Secret, &w.Events, &w.Active, &w.LastStatus, &w.LastError, &w.LastAt, &w.CreatedAt); err != nil {
			return nil, err
		}
		w.HasSecret = w.Secret != ""
		out = append(out, w)
	}
	return out, rows.Err()
}

// GetWebhook loads one webhook.
func (s *Store) GetWebhook(ctx context.Context, id int64) (*Webhook, error) {
	var w Webhook
	err := s.DB.QueryRowContext(ctx, `SELECT id, team_id, name, url, secret, events, active, last_status, last_error, last_at, created_at FROM webhooks WHERE id = ?`, id).
		Scan(&w.ID, &w.TeamID, &w.Name, &w.URL, &w.Secret, &w.Events, &w.Active, &w.LastStatus, &w.LastError, &w.LastAt, &w.CreatedAt)
	w.HasSecret = w.Secret != ""
	return &w, notFound(err)
}

// SaveWebhook creates or updates a webhook.
func (s *Store) SaveWebhook(ctx context.Context, w *Webhook, by int64) error {
	if w.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO webhooks (team_id, name, url, secret, events, active, created_by) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			w.TeamID, w.Name, w.URL, w.Secret, w.Events, w.Active, nullID(by))
		if err != nil {
			return err
		}
		w.ID, _ = res.LastInsertId()
		w.CreatedAt = Now()
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE webhooks SET name = ?, url = ?, secret = ?, events = ?, active = ? WHERE id = ?`, w.Name, w.URL, w.Secret, w.Events, w.Active, w.ID)
	return err
}

// WebhookResult records the outcome of a delivery.
func (s *Store) WebhookResult(ctx context.Context, id int64, status int, errText string) {
	_, _ = s.DB.ExecContext(ctx, `UPDATE webhooks SET last_status = ?, last_error = ?, last_at = ? WHERE id = ?`, status, errText, Now(), id)
}

// DeleteWebhook removes a webhook.
func (s *Store) DeleteWebhook(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM webhooks WHERE id = ?`, id)
	return err
}

// BranchIssue is the issue a branch's pull request is attached to.
type BranchIssue struct {
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	PRURL      string `json:"pr_url,omitempty"`
	PRState    string `json:"pr_state,omitempty"`
}

// TeamPRBranches maps the head branches of a team's pull request attachments
// to their issues (the newest attachment wins a branch).
func (s *Store) TeamPRBranches(ctx context.Context, teamID int64) (map[string]BranchIssue, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT t.key || '-' || i.number, i.title, a.url, a.state, COALESCE(json_extract(a.meta, '$.branch'), '')
		FROM attachments a JOIN issues i ON i.id = a.issue_id JOIN teams t ON t.id = i.team_id
		WHERE i.team_id = ? AND a.kind = 'pull_request' AND i.deleted_at IS NULL ORDER BY a.id`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]BranchIssue{}
	for rows.Next() {
		var b BranchIssue
		var branch string
		if err := rows.Scan(&b.Identifier, &b.Title, &b.PRURL, &b.PRState, &branch); err != nil {
			return nil, err
		}
		if branch != "" {
			out[branch] = b
		}
	}
	return out, rows.Err()
}
