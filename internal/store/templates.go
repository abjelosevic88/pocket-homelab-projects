package store

import (
	"context"
	"fmt"
	"strings"
)

// TemplateScope names the team a person's action templates are for.
func TemplateScope(teamID int64) string { return fmt.Sprintf("team:%d", teamID) }

// RemoteTemplateScope names a connected instance's team.
func RemoteTemplateScope(remoteID int64, teamKey string) string {
	return fmt.Sprintf("remote:%d:%s", remoteID, strings.ToUpper(teamKey))
}

// ActionTemplates is the person's own wording per action in a team.
func (s *Store) ActionTemplates(ctx context.Context, userID int64, scope string) (map[string]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT action, text FROM action_templates WHERE user_id = ? AND scope = ?`, userID, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var a, t string
		if err := rows.Scan(&a, &t); err != nil {
			return nil, err
		}
		out[a] = t
	}
	return out, rows.Err()
}

// SaveActionTemplates replaces the person's templates in a team; an empty
// text means the built-in one again.
func (s *Store) SaveActionTemplates(ctx context.Context, userID int64, scope string, list map[string]string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for action, text := range list {
		if strings.TrimSpace(text) == "" {
			if _, err := tx.ExecContext(ctx, `DELETE FROM action_templates WHERE user_id = ? AND scope = ? AND action = ?`, userID, scope, action); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO action_templates(user_id, scope, action, text, updated_at) VALUES(?,?,?,?,?)
			ON CONFLICT(user_id, scope, action) DO UPDATE SET text = excluded.text, updated_at = excluded.updated_at`, userID, scope, action, text, Now()); err != nil {
			return err
		}
	}
	return tx.Commit()
}
