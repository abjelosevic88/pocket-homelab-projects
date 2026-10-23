package store

import (
	"context"
	"database/sql"
	"errors"
)

// TeamGitHubToken is the team's token for reading pull request status ("" when none).
func (s *Store) TeamGitHubToken(ctx context.Context, teamID int64) string {
	var tok string
	err := s.DB.QueryRowContext(ctx, `SELECT github_token FROM team_git WHERE team_id = ?`, teamID).Scan(&tok)
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	return tok
}

// SetTeamGitHubToken stores (or, with "", removes) the team's GitHub token.
func (s *Store) SetTeamGitHubToken(ctx context.Context, teamID int64, token string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO team_git(team_id, github_token, updated_at) VALUES(?,?,?)
		ON CONFLICT(team_id) DO UPDATE SET github_token=excluded.github_token, updated_at=excluded.updated_at`, teamID, token, Now())
	return err
}

// OpenPullRequest is a linked pull request whose outcome is not known yet.
type OpenPullRequest struct {
	Attachment
	TeamID int64
}

// OpenPullRequests lists open or draft GitHub pull requests attached to live
// issues of teams that have a token (teamID > 0: that team only, issueID > 0: that issue only).
func (s *Store) OpenPullRequests(ctx context.Context, teamID, issueID int64) ([]OpenPullRequest, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+prefixCols("a.", attachmentCols)+`, i.team_id FROM attachments a
		JOIN issues i ON i.id = a.issue_id
		JOIN team_git g ON g.team_id = i.team_id AND g.github_token != ''
		WHERE a.kind = 'pull_request' AND a.state IN ('open', 'draft') AND a.url LIKE 'https://github.com/%'
		  AND i.deleted_at IS NULL AND (? = 0 OR i.team_id = ?) AND (? = 0 OR i.id = ?)
		ORDER BY a.updated_at LIMIT 500`, teamID, teamID, issueID, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OpenPullRequest
	for rows.Next() {
		var p OpenPullRequest
		var iss, proj, by sql.NullInt64
		var meta string
		a := &p.Attachment
		if err := rows.Scan(&a.ID, &iss, &proj, &a.Kind, &a.Title, &a.Subtitle, &a.URL, &a.FileName, &a.Mime, &a.Size, &a.ExtApp, &a.ExtID, &a.State, &meta, &by, &a.CreatedAt, &a.UpdatedAt, &p.TeamID); err != nil {
			return nil, err
		}
		a.IssueID, a.ProjectID, a.CreatedBy = nullI64(iss), nullI64(proj), nullI64(by)
		if meta == "" {
			meta = "{}"
		}
		a.Meta = []byte(meta)
		out = append(out, p)
	}
	return out, rows.Err()
}
