package store

import (
	"context"
	"strings"
)

// LinkText is a piece of text that may link somewhere: an issue's
// description or comment, a project's description, update or comment on one.
type LinkText struct {
	Kind string // issue | project
	ID   int64
	Text string
}

// LinkTexts lists the texts on the given teams that contain one of the
// needles (host and path of an address, matched without case), newest first;
// the caller decides whether they really link there. Trash and deleted
// comments are left out.
func (s *Store) LinkTexts(ctx context.Context, teamIDs []int64, needles []string, limit int) ([]LinkText, error) {
	if len(teamIDs) == 0 || len(needles) == 0 {
		return []LinkText{}, nil
	}
	if limit <= 0 {
		limit = 500
	}
	tq, targs := inInts(teamIDs)
	var args []any
	like := func(col string) string {
		parts := make([]string, len(needles))
		for i, n := range needles {
			parts[i] = col + ` LIKE ? ESCAPE '\'`
			args = append(args, "%"+likeEscape(n)+"%")
		}
		return "(" + strings.Join(parts, " OR ") + ")"
	}
	teams := func() string { args = append(args, targs...); return tq }
	project := func() string {
		return `p.deleted_at IS NULL AND p.id IN (SELECT project_id FROM project_teams WHERE team_id IN (` + teams() + `))`
	}
	// Each part adds its arguments as it is built, so the order of the
	// placeholders and the arguments is the same.
	var q []string
	q = append(q, `SELECT 'issue', i.id, i.description, i.updated_at FROM issues i
		WHERE i.deleted_at IS NULL AND i.team_id IN (`+teams()+`) AND `+like("i.description"))
	q = append(q, `SELECT 'issue', i.id, c.body, c.created_at FROM comments c JOIN issues i ON i.id = c.issue_id
		WHERE c.deleted_at IS NULL AND i.deleted_at IS NULL AND i.team_id IN (`+teams()+`) AND `+like("c.body"))
	q = append(q, `SELECT 'project', p.id, p.summary || char(10) || p.content, p.updated_at FROM projects p
		WHERE `+project()+` AND (`+like("p.summary")+` OR `+like("p.content")+`)`)
	q = append(q, `SELECT 'project', p.id, u.body, u.created_at FROM project_updates u JOIN projects p ON p.id = u.project_id
		WHERE `+project()+` AND `+like("u.body"))
	q = append(q, `SELECT 'project', p.id, c.body, c.created_at FROM comments c JOIN project_updates u ON u.id = c.project_update_id JOIN projects p ON p.id = u.project_id
		WHERE c.deleted_at IS NULL AND `+project()+` AND `+like("c.body"))
	args = append(args, limit)
	rows, err := s.DB.QueryContext(ctx, strings.Join(q, "\nUNION ALL\n")+"\nORDER BY 4 DESC LIMIT ?", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LinkText{}
	for rows.Next() {
		var t LinkText
		var at string
		if err := rows.Scan(&t.Kind, &t.ID, &t.Text, &at); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
