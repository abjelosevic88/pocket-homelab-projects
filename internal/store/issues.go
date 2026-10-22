package store

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// IssueFilter selects issues. Teams the viewer is not on are never included.
// The same structure is stored as a saved view's filters.
type IssueFilter struct {
	TeamIDs        []int64  `json:"team_ids,omitempty"`
	IDs            []int64  `json:"ids,omitempty"`
	StateIDs       []int64  `json:"state_ids,omitempty"`
	StateTypes     []string `json:"state_types,omitempty"`
	AssigneeIDs    []int64  `json:"assignee_ids,omitempty"` // 0 = unassigned
	CreatorIDs     []int64  `json:"creator_ids,omitempty"`
	Priorities     []int    `json:"priorities,omitempty"`
	LabelIDs       []int64  `json:"label_ids,omitempty"`  // any of them
	LabelMode      string   `json:"label_mode,omitempty"` // any | all
	NotLabelIDs    []int64  `json:"not_label_ids,omitempty"`
	ProjectIDs     []int64  `json:"project_ids,omitempty"` // 0 = no project
	MilestoneIDs   []int64  `json:"milestone_ids,omitempty"`
	CycleIDs       []int64  `json:"cycle_ids,omitempty"` // 0 = no cycle
	Cycle          string   `json:"cycle,omitempty"`     // current | next | previous (per team)
	ParentID       int64    `json:"parent_id,omitempty"`
	TopLevel       bool     `json:"top_level,omitempty"`
	Due            string   `json:"due,omitempty"` // overdue | today | week | month | none | any
	DueFrom        string   `json:"due_from,omitempty"`
	DueTo          string   `json:"due_to,omitempty"`
	CreatedSince   string   `json:"created_since,omitempty"`
	UpdatedSince   string   `json:"updated_since,omitempty"`
	ShowCompleted  string   `json:"show_completed,omitempty"` // all (default) | none | day | week | month
	SubscribedBy   int64    `json:"subscribed_by,omitempty"`
	Blocked        bool     `json:"blocked,omitempty"`
	Snoozed        string   `json:"snoozed,omitempty"`  // "" hide snoozed triage | include
	Archived       string   `json:"archived,omitempty"` // "" hide | include | only
	Trash          bool     `json:"trash,omitempty"`
	SourceApp      string   `json:"source_app,omitempty"`
	SourceRef      string   `json:"source_ref,omitempty"`
	URL            string   `json:"url,omitempty"` // issues linking to this address (source, contacts, attachments)
	Q              string   `json:"q,omitempty"`
	Sort           string   `json:"sort,omitempty"` // manual | priority | updated | created | due | title | estimate | identifier
	Limit          int      `json:"limit,omitempty"`
	Offset         int      `json:"offset,omitempty"`
	ViewerID       int64    `json:"-"`
	AllowedTeamIDs []int64  `json:"-"`
	Today          string   `json:"-"` // the viewer's local date
}

const issueSelect = `SELECT i.id, i.team_id, i.number, t.key, i.title, i.description, i.state_id, ws.type, i.priority, i.estimate, i.assignee_id,
	i.creator_id, i.project_id, i.milestone_id, i.cycle_id, i.parent_id, i.due_date, i.sort_order, i.snoozed_until, i.source_app,
	i.source_ref, i.source_url, i.created_at, i.updated_at, i.started_at, i.completed_at, i.canceled_at, i.archived_at, i.deleted_at,
	(SELECT COUNT(1) FROM issues c WHERE c.parent_id = i.id AND c.deleted_at IS NULL),
	(SELECT COUNT(1) FROM issues c JOIN workflow_states cs ON cs.id = c.state_id WHERE c.parent_id = i.id AND c.deleted_at IS NULL AND cs.type IN ('completed','canceled')),
	(SELECT COUNT(1) FROM comments cm WHERE cm.issue_id = i.id AND cm.deleted_at IS NULL),
	(SELECT COUNT(1) FROM attachments a WHERE a.issue_id = i.id),
	(SELECT COUNT(1) FROM issue_relations r JOIN issues b ON b.id = r.issue_id JOIN workflow_states bs ON bs.id = b.state_id
		WHERE r.related_id = i.id AND r.type = 'blocks' AND b.deleted_at IS NULL AND bs.type NOT IN ('completed','canceled')),
	(SELECT COUNT(1) FROM issue_relations r WHERE r.issue_id = i.id AND r.type = 'blocks'),
	EXISTS (SELECT 1 FROM issue_subscribers su WHERE su.issue_id = i.id AND su.user_id = ?)
	FROM issues i JOIN teams t ON t.id = i.team_id JOIN workflow_states ws ON ws.id = i.state_id`

func scanIssue(row interface{ Scan(...any) error }) (*Issue, error) {
	var is Issue
	var key string
	var est sql.NullFloat64
	var assignee, creator, project, milestone, cycle, parent sql.NullInt64
	var started, completed, canceled, archived, deleted sql.NullString
	if err := row.Scan(&is.ID, &is.TeamID, &is.Number, &key, &is.Title, &is.Description, &is.StateID, &is.StateType, &is.Priority, &est,
		&assignee, &creator, &project, &milestone, &cycle, &parent, &is.DueDate, &is.SortOrder, &is.SnoozedUntil, &is.SourceApp,
		&is.SourceRef, &is.SourceURL, &is.CreatedAt, &is.UpdatedAt, &started, &completed, &canceled, &archived, &deleted,
		&is.SubTotal, &is.SubDone, &is.Comments, &is.Attachments, &is.BlockedBy, &is.Blocking, &is.Subscribed); err != nil {
		return nil, notFound(err)
	}
	is.Identifier = key + "-" + strconv.FormatInt(is.Number, 10)
	is.Estimate = nullF64(est)
	is.AssigneeID, is.CreatorID, is.ProjectID = nullI64(assignee), nullI64(creator), nullI64(project)
	is.MilestoneID, is.CycleID, is.ParentID = nullI64(milestone), nullI64(cycle), nullI64(parent)
	is.StartedAt, is.CompletedAt, is.CanceledAt = nullStr(started), nullStr(completed), nullStr(canceled)
	is.ArchivedAt, is.DeletedAt = nullStr(archived), nullStr(deleted)
	is.LabelIDs = []int64{}
	return &is, nil
}

// GetIssue loads one issue (labels included) without checking access.
func (s *Store) GetIssue(ctx context.Context, id, viewerID int64) (*Issue, error) {
	is, err := scanIssue(s.DB.QueryRowContext(ctx, issueSelect+` WHERE i.id = ?`, viewerID, id))
	if err != nil {
		return nil, err
	}
	if err := s.fillLabels(ctx, []*Issue{is}); err != nil {
		return nil, err
	}
	return is, nil
}

// IdentifierRe matches an issue identifier in text ("WEB-12").
var IdentifierRe = regexp.MustCompile(`\b([A-Za-z][A-Za-z0-9]{0,6})-([0-9]{1,9})\b`)

// IssueIDByIdentifier resolves "WEB-12" (current or former identifier) to an issue id.
func (s *Store) IssueIDByIdentifier(ctx context.Context, ident string) (int64, error) {
	m := IdentifierRe.FindStringSubmatch(strings.TrimSpace(ident))
	if m == nil || m[0] != strings.TrimSpace(ident) {
		return 0, ErrNotFound
	}
	var id int64
	err := s.DB.QueryRowContext(ctx, `SELECT i.id FROM issues i JOIN teams t ON t.id = i.team_id WHERE t.key = ? AND i.number = ?`, strings.ToUpper(m[1]), m[2]).Scan(&id)
	if err == nil {
		return id, nil
	}
	err = s.DB.QueryRowContext(ctx, `SELECT issue_id FROM issue_aliases WHERE identifier = ?`, strings.ToUpper(ident)).Scan(&id)
	return id, notFound(err)
}

func (s *Store) fillLabels(ctx context.Context, list []*Issue) error {
	if len(list) == 0 {
		return nil
	}
	byID := map[int64]*Issue{}
	ids := make([]int64, 0, len(list))
	for _, is := range list {
		byID[is.ID] = is
		ids = append(ids, is.ID)
	}
	for start := 0; start < len(ids); start += 900 {
		end := min(start+900, len(ids))
		q, args := inInts(ids[start:end])
		rows, err := s.DB.QueryContext(ctx, `SELECT il.issue_id, il.label_id FROM issue_labels il JOIN labels l ON l.id = il.label_id
			WHERE il.issue_id IN (`+q+`) ORDER BY l.position`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var iid, lid int64
			if err := rows.Scan(&iid, &lid); err != nil {
				rows.Close()
				return err
			}
			byID[iid].LabelIDs = append(byID[iid].LabelIDs, lid)
		}
		rows.Close()
	}
	return nil
}

// ListIssues returns the issues matching f and the total before paging.
func (s *Store) ListIssues(ctx context.Context, f IssueFilter) ([]*Issue, int, error) {
	if len(f.AllowedTeamIDs) == 0 {
		return []*Issue{}, 0, nil
	}
	var where []string
	var args []any
	add := func(cond string, a ...any) { where = append(where, cond); args = append(args, a...) }

	teams := f.AllowedTeamIDs
	if len(f.TeamIDs) > 0 {
		teams = intersect(teams, f.TeamIDs)
		if len(teams) == 0 {
			return []*Issue{}, 0, nil
		}
	}
	q, a := inInts(teams)
	add(`i.team_id IN (`+q+`)`, a...)
	if f.Trash {
		add(`i.deleted_at IS NOT NULL`)
	} else {
		add(`i.deleted_at IS NULL`)
	}
	switch f.Archived {
	case "include":
	case "only":
		add(`i.archived_at IS NOT NULL`)
	default:
		if len(f.IDs) == 0 && !f.Trash {
			add(`i.archived_at IS NULL`)
		}
	}
	if len(f.IDs) > 0 {
		q, a := inInts(f.IDs)
		add(`i.id IN (`+q+`)`, a...)
	}
	if len(f.StateIDs) > 0 {
		q, a := inInts(f.StateIDs)
		add(`i.state_id IN (`+q+`)`, a...)
	}
	if len(f.StateTypes) > 0 {
		q, a := inStrings(f.StateTypes)
		add(`ws.type IN (`+q+`)`, a...)
	}
	nullable := func(col string, ids []int64) {
		var cond []string
		var vals []int64
		for _, id := range ids {
			if id == 0 {
				cond = append(cond, col+` IS NULL`)
			} else {
				vals = append(vals, id)
			}
		}
		if len(vals) > 0 {
			q, a := inInts(vals)
			cond = append(cond, col+` IN (`+q+`)`)
			args = append(args, a...)
		}
		if len(cond) > 0 {
			where = append(where, `(`+strings.Join(cond, ` OR `)+`)`)
		}
	}
	nullable(`i.assignee_id`, f.AssigneeIDs)
	nullable(`i.creator_id`, f.CreatorIDs)
	nullable(`i.project_id`, f.ProjectIDs)
	nullable(`i.milestone_id`, f.MilestoneIDs)
	nullable(`i.cycle_id`, f.CycleIDs)
	if len(f.Priorities) > 0 {
		ph := make([]string, len(f.Priorities))
		for i, p := range f.Priorities {
			ph[i] = "?"
			args = append(args, p)
		}
		where = append(where, `i.priority IN (`+strings.Join(ph, ",")+`)`)
	}
	if len(f.LabelIDs) > 0 {
		q, a := inInts(f.LabelIDs)
		if f.LabelMode == "all" {
			add(`(SELECT COUNT(DISTINCT il.label_id) FROM issue_labels il WHERE il.issue_id = i.id AND il.label_id IN (`+q+`)) = ?`, append(a, len(f.LabelIDs))...)
		} else {
			add(`EXISTS (SELECT 1 FROM issue_labels il WHERE il.issue_id = i.id AND il.label_id IN (`+q+`))`, a...)
		}
	}
	if len(f.NotLabelIDs) > 0 {
		q, a := inInts(f.NotLabelIDs)
		add(`NOT EXISTS (SELECT 1 FROM issue_labels il WHERE il.issue_id = i.id AND il.label_id IN (`+q+`))`, a...)
	}
	switch f.Cycle {
	case "current", "next", "previous":
		today := f.Today
		if today == "" {
			today = time.Now().Format("2006-01-02")
		}
		switch f.Cycle {
		case "current":
			add(`i.cycle_id IN (SELECT c.id FROM cycles c WHERE c.team_id = i.team_id AND c.starts_on <= ? AND c.ends_on >= ?)`, today, today)
		case "next":
			add(`i.cycle_id = (SELECT c.id FROM cycles c WHERE c.team_id = i.team_id AND c.starts_on > ? ORDER BY c.starts_on LIMIT 1)`, today)
		case "previous":
			add(`i.cycle_id = (SELECT c.id FROM cycles c WHERE c.team_id = i.team_id AND c.ends_on < ? ORDER BY c.ends_on DESC LIMIT 1)`, today)
		}
	}
	if f.ParentID > 0 {
		add(`i.parent_id = ?`, f.ParentID)
	}
	if f.TopLevel {
		add(`i.parent_id IS NULL`)
	}
	today := f.Today
	if today == "" {
		today = time.Now().Format("2006-01-02")
	}
	tday, _ := time.Parse("2006-01-02", today)
	switch f.Due {
	case "overdue":
		add(`i.due_date <> '' AND i.due_date < ? AND ws.type NOT IN ('completed','canceled')`, today)
	case "today":
		add(`i.due_date <> '' AND i.due_date <= ?`, today)
	case "week":
		add(`i.due_date <> '' AND i.due_date <= ?`, tday.AddDate(0, 0, 7).Format("2006-01-02"))
	case "month":
		add(`i.due_date <> '' AND i.due_date <= ?`, tday.AddDate(0, 1, 0).Format("2006-01-02"))
	case "none":
		add(`i.due_date = ''`)
	case "any":
		add(`i.due_date <> ''`)
	}
	if f.DueFrom != "" {
		add(`i.due_date <> '' AND i.due_date >= ?`, f.DueFrom)
	}
	if f.DueTo != "" {
		add(`i.due_date <> '' AND i.due_date <= ?`, f.DueTo)
	}
	if f.CreatedSince != "" {
		add(`i.created_at >= ?`, f.CreatedSince)
	}
	if f.UpdatedSince != "" {
		add(`i.updated_at >= ?`, f.UpdatedSince)
	}
	switch f.ShowCompleted {
	case "none":
		add(`ws.type NOT IN ('completed','canceled')`)
	case "day", "week", "month":
		d := map[string]int{"day": 1, "week": 7, "month": 30}[f.ShowCompleted]
		since := time.Now().UTC().AddDate(0, 0, -d).Format(time.RFC3339)
		add(`(ws.type NOT IN ('completed','canceled') OR COALESCE(i.completed_at, i.canceled_at, i.updated_at) >= ?)`, since)
	}
	if f.SubscribedBy > 0 {
		add(`EXISTS (SELECT 1 FROM issue_subscribers su WHERE su.issue_id = i.id AND su.user_id = ?)`, f.SubscribedBy)
	}
	if f.Blocked {
		add(`EXISTS (SELECT 1 FROM issue_relations r JOIN issues b ON b.id = r.issue_id JOIN workflow_states bs ON bs.id = b.state_id
			WHERE r.related_id = i.id AND r.type = 'blocks' AND b.deleted_at IS NULL AND bs.type NOT IN ('completed','canceled'))`)
	}
	if f.Snoozed == "" {
		add(`(i.snoozed_until = '' OR i.snoozed_until <= ?)`, time.Now().UTC().Format(time.RFC3339))
	}
	if f.SourceApp != "" {
		add(`i.source_app = ?`, f.SourceApp)
	}
	if f.SourceRef != "" {
		add(`i.source_ref = ?`, f.SourceRef)
	}
	if f.URL != "" {
		add(`(i.source_url = ? OR EXISTS (SELECT 1 FROM contact_links cl WHERE cl.issue_id = i.id AND cl.url = ?)
			OR EXISTS (SELECT 1 FROM attachments at WHERE at.issue_id = i.id AND at.url = ?))`, f.URL, f.URL, f.URL)
	}
	if strings.TrimSpace(f.Q) != "" {
		qq := strings.TrimSpace(f.Q)
		if id, err := s.IssueIDByIdentifier(ctx, qq); err == nil {
			add(`i.id = ?`, id)
		} else if m := ftsQuery(qq); m != "" {
			add(`(i.id IN (SELECT rowid FROM issues_fts WHERE issues_fts MATCH ?) OR i.title LIKE ? ESCAPE '\')`, m, "%"+likeEscape(qq)+"%")
		}
	}
	cond := strings.Join(where, " AND ")

	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM issues i JOIN workflow_states ws ON ws.id = i.state_id WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := `i.sort_order, i.id DESC`
	switch f.Sort {
	case "priority":
		order = `CASE WHEN i.priority = 0 THEN 9 ELSE i.priority END, i.sort_order, i.id DESC`
	case "updated":
		order = `i.updated_at DESC`
	case "created":
		order = `i.created_at DESC`
	case "due":
		order = `i.due_date = '', i.due_date, CASE WHEN i.priority = 0 THEN 9 ELSE i.priority END`
	case "title":
		order = `lower(i.title)`
	case "estimate":
		order = `i.estimate IS NULL, i.estimate DESC`
	case "identifier":
		order = `t.key, i.number DESC`
	case "completed":
		order = `COALESCE(i.completed_at, i.canceled_at, i.updated_at) DESC`
	}
	limit := f.Limit
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	rows, err := s.DB.QueryContext(ctx, issueSelect+` WHERE `+cond+` ORDER BY `+order+` LIMIT ? OFFSET ?`,
		append(append([]any{f.ViewerID}, args...), limit, max(f.Offset, 0))...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*Issue{}
	for rows.Next() {
		is, err := scanIssue(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, is)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	rows.Close()
	if err := s.fillLabels(ctx, out); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func intersect(a, b []int64) []int64 {
	set := map[int64]bool{}
	for _, x := range a {
		set[x] = true
	}
	var out []int64
	for _, x := range b {
		if set[x] {
			out = append(out, x)
			set[x] = false
		}
	}
	return out
}

// ftsQuery turns typed words into an FTS5 prefix query ("" when nothing usable).
func ftsQuery(q string) string {
	var parts []string
	for _, w := range strings.FieldsFunc(q, func(r rune) bool {
		return !(r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r > 127)
	}) {
		parts = append(parts, `"`+strings.ReplaceAll(w, `"`, ``)+`"*`)
	}
	return strings.Join(parts, " ")
}

// TxHook runs inside the transaction that saves something (events, receipts).
type TxHook func(tx *sql.Tx) error

// CreateIssue inserts an issue with the next number of its team, its labels,
// its first history entry, subscribers and the search index, in one transaction.
func (s *Store) CreateIssue(ctx context.Context, is *Issue, actorID int64, actorApp string, hooks ...TxHook) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	var number int64
	var key string
	if err := tx.QueryRowContext(ctx, `UPDATE teams SET issue_seq = issue_seq + 1 WHERE id = ? RETURNING issue_seq, key`, is.TeamID).Scan(&number, &key); err != nil {
		return notFound(err)
	}
	is.Number = number
	is.Identifier = key + "-" + strconv.FormatInt(number, 10)
	if is.SortOrder == 0 {
		var minSort sql.NullFloat64
		_ = tx.QueryRowContext(ctx, `SELECT MIN(sort_order) FROM issues WHERE team_id = ?`, is.TeamID).Scan(&minSort)
		is.SortOrder = minSort.Float64 - 1
		if !minSort.Valid {
			is.SortOrder = 0
		}
	}
	now := Now()
	is.CreatedAt, is.UpdatedAt = now, now
	if err := s.stampState(ctx, tx, is, ""); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO issues (team_id, number, title, description, state_id, priority, estimate, assignee_id, creator_id,
		project_id, milestone_id, cycle_id, parent_id, due_date, sort_order, snoozed_until, source_app, source_ref, source_url, template_id,
		created_at, updated_at, started_at, completed_at, canceled_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		is.TeamID, is.Number, is.Title, is.Description, is.StateID, is.Priority, ptrF(is.Estimate), ptrArg(is.AssigneeID), ptrArg(is.CreatorID),
		ptrArg(is.ProjectID), ptrArg(is.MilestoneID), ptrArg(is.CycleID), ptrArg(is.ParentID), is.DueDate, is.SortOrder, is.SnoozedUntil,
		is.SourceApp, is.SourceRef, is.SourceURL, nil, now, now, is.StartedAt, is.CompletedAt, is.CanceledAt)
	if err != nil {
		return err
	}
	is.ID, _ = res.LastInsertId()
	if err := setLabels(ctx, tx, is.ID, is.LabelIDs); err != nil {
		return err
	}
	for _, uid := range []*int64{is.CreatorID, is.AssigneeID} {
		if uid != nil && *uid > 0 {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO issue_subscribers (issue_id, user_id) VALUES (?, ?)`, is.ID, *uid); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO history (issue_id, actor_id, actor_app, field) VALUES (?, ?, ?, 'created')`, is.ID, nullID(actorID), actorApp); err != nil {
		return err
	}
	if err := indexIssue(ctx, tx, is); err != nil {
		return err
	}
	if is.ProjectID != nil {
		if err := ensureProjectTeam(ctx, tx, *is.ProjectID, is.TeamID); err != nil {
			return err
		}
	}
	for _, h := range hooks {
		if err := h(tx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func nullID(id int64) any {
	if id <= 0 {
		return nil
	}
	return id
}

// stampState sets started/completed/canceled stamps for the issue's state
// (prevType is the type it had before, "" for a new issue).
func (s *Store) stampState(ctx context.Context, tx *sql.Tx, is *Issue, prevType string) error {
	var typ string
	if err := tx.QueryRowContext(ctx, `SELECT type FROM workflow_states WHERE id = ? AND team_id = ?`, is.StateID, is.TeamID).Scan(&typ); err != nil {
		return fmt.Errorf("state %d is not in the issue's team", is.StateID)
	}
	is.StateType = typ
	if typ == prevType {
		return nil
	}
	now := Now()
	switch typ {
	case "started":
		if is.StartedAt == nil {
			is.StartedAt = &now
		}
		is.CompletedAt, is.CanceledAt = nil, nil
	case "completed":
		if is.StartedAt == nil {
			is.StartedAt = &now
		}
		is.CompletedAt, is.CanceledAt = &now, nil
	case "canceled":
		is.CanceledAt, is.CompletedAt = &now, nil
	default:
		is.StartedAt, is.CompletedAt, is.CanceledAt = nil, nil, nil
	}
	if typ != "completed" && typ != "canceled" {
		is.ArchivedAt = nil
	}
	return nil
}

// UpdateIssue saves every column of an issue, its labels, the given history
// entries and the search index, in one transaction.
func (s *Store) UpdateIssue(ctx context.Context, is *Issue, prevType string, hist []History, hooks ...TxHook) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if err := s.stampState(ctx, tx, is, prevType); err != nil {
		return err
	}
	is.UpdatedAt = Now()
	if _, err := tx.ExecContext(ctx, `UPDATE issues SET team_id = ?, number = ?, title = ?, description = ?, state_id = ?, priority = ?, estimate = ?,
		assignee_id = ?, project_id = ?, milestone_id = ?, cycle_id = ?, parent_id = ?, due_date = ?, sort_order = ?, snoozed_until = ?,
		updated_at = ?, started_at = ?, completed_at = ?, canceled_at = ?, archived_at = ? WHERE id = ?`,
		is.TeamID, is.Number, is.Title, is.Description, is.StateID, is.Priority, ptrF(is.Estimate), ptrArg(is.AssigneeID),
		ptrArg(is.ProjectID), ptrArg(is.MilestoneID), ptrArg(is.CycleID), ptrArg(is.ParentID), is.DueDate, is.SortOrder, is.SnoozedUntil,
		is.UpdatedAt, is.StartedAt, is.CompletedAt, is.CanceledAt, is.ArchivedAt, is.ID); err != nil {
		return err
	}
	if err := setLabels(ctx, tx, is.ID, is.LabelIDs); err != nil {
		return err
	}
	if is.AssigneeID != nil {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO issue_subscribers (issue_id, user_id) VALUES (?, ?)`, is.ID, *is.AssigneeID); err != nil {
			return err
		}
	}
	for _, h := range hist {
		if _, err := tx.ExecContext(ctx, `INSERT INTO history (issue_id, actor_id, actor_app, field, from_value, to_value, from_label, to_label) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			is.ID, ptrArg(h.ActorID), h.ActorApp, h.Field, h.FromValue, h.ToValue, h.FromLabel, h.ToLabel); err != nil {
			return err
		}
	}
	if err := indexIssue(ctx, tx, is); err != nil {
		return err
	}
	if is.ProjectID != nil {
		if err := ensureProjectTeam(ctx, tx, *is.ProjectID, is.TeamID); err != nil {
			return err
		}
	}
	for _, h := range hooks {
		if err := h(tx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func setLabels(ctx context.Context, tx *sql.Tx, issueID int64, ids []int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM issue_labels WHERE issue_id = ?`, issueID); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO issue_labels (issue_id, label_id) VALUES (?, ?)`, issueID, id); err != nil {
			return err
		}
	}
	return nil
}

func indexIssue(ctx context.Context, tx *sql.Tx, is *Issue) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM issues_fts WHERE rowid = ?`, is.ID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO issues_fts (rowid, ident, title, description) VALUES (?, ?, ?, ?)`, is.ID, is.Identifier, is.Title, is.Description)
	return err
}

func ensureProjectTeam(ctx context.Context, tx *sql.Tx, projectID, teamID int64) error {
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO project_teams (project_id, team_id) VALUES (?, ?)`, projectID, teamID)
	return err
}

// MoveIssueToTeam gives an issue the next number of another team, keeping
// its old identifier as an alias.
func (s *Store) NextIssueNumber(ctx context.Context, teamID int64) (int64, string, error) {
	var number int64
	var key string
	err := s.DB.QueryRowContext(ctx, `UPDATE teams SET issue_seq = issue_seq + 1 WHERE id = ? RETURNING issue_seq, key`, teamID).Scan(&number, &key)
	return number, key, notFound(err)
}

// AddAlias records a former identifier of an issue.
func (s *Store) AddAlias(ctx context.Context, ident string, issueID int64) error {
	_, err := s.DB.ExecContext(ctx, `INSERT OR REPLACE INTO issue_aliases (identifier, issue_id) VALUES (?, ?)`, strings.ToUpper(ident), issueID)
	return err
}

// TrashIssue moves an issue (and its sub-issues) to the trash; RestoreIssue brings it back.
func (s *Store) TrashIssue(ctx context.Context, id int64, actorID int64, actorApp string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	now := Now()
	if _, err := tx.ExecContext(ctx, `UPDATE issues SET deleted_at = ?, updated_at = ? WHERE (id = ? OR parent_id = ?) AND deleted_at IS NULL`, now, now, id, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO history (issue_id, actor_id, actor_app, field) VALUES (?, ?, ?, 'deleted')`, id, nullID(actorID), actorApp); err != nil {
		return err
	}
	return tx.Commit()
}

// RestoreIssue takes an issue (and the sub-issues trashed with it) out of the trash.
func (s *Store) RestoreIssue(ctx context.Context, id int64, actorID int64) error {
	var deleted sql.NullString
	if err := s.DB.QueryRowContext(ctx, `SELECT deleted_at FROM issues WHERE id = ?`, id).Scan(&deleted); err != nil {
		return notFound(err)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, `UPDATE issues SET deleted_at = NULL, updated_at = ? WHERE id = ? OR (parent_id = ? AND deleted_at = ?)`, Now(), id, id, deleted.String); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO history (issue_id, actor_id, field) VALUES (?, ?, 'restored')`, id, nullID(actorID)); err != nil {
		return err
	}
	return tx.Commit()
}

// PurgeTrash deletes issues that have been in the trash longer than days.
func (s *Store) PurgeTrash(ctx context.Context, days int) {
	cut := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339)
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM issues_fts WHERE rowid IN (SELECT id FROM issues WHERE deleted_at IS NOT NULL AND deleted_at < ?)`, cut)
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM issues WHERE deleted_at IS NOT NULL AND deleted_at < ?`, cut)
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM projects WHERE deleted_at IS NOT NULL AND deleted_at < ?`, cut)
}

// AutoArchive archives completed/canceled issues older than their team's setting.
func (s *Store) AutoArchive(ctx context.Context) int64 {
	res, err := s.DB.ExecContext(ctx, `UPDATE issues SET archived_at = ? WHERE archived_at IS NULL AND deleted_at IS NULL AND id IN (
		SELECT i.id FROM issues i JOIN teams t ON t.id = i.team_id JOIN workflow_states ws ON ws.id = i.state_id
		WHERE t.auto_archive_months > 0 AND ws.type IN ('completed','canceled')
		AND COALESCE(i.completed_at, i.canceled_at, i.updated_at) < strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-' || t.auto_archive_months || ' months'))`, Now())
	if err != nil {
		return 0
	}
	n, _ := res.RowsAffected()
	return n
}

// ---------- history ----------

// IssueHistory lists an issue's changes, oldest first.
func (s *Store) IssueHistory(ctx context.Context, issueID int64) ([]History, error) {
	return s.historyWhere(ctx, `issue_id = ?`, issueID)
}

// ProjectHistory lists a project's own changes, oldest first.
func (s *Store) ProjectHistory(ctx context.Context, projectID int64) ([]History, error) {
	return s.historyWhere(ctx, `project_id = ?`, projectID)
}

func (s *Store) historyWhere(ctx context.Context, where string, args ...any) ([]History, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, issue_id, project_id, actor_id, actor_app, field, from_value, to_value, from_label, to_label, created_at
		FROM history WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []History{}
	for rows.Next() {
		var h History
		var iss, proj, actor sql.NullInt64
		if err := rows.Scan(&h.ID, &iss, &proj, &actor, &h.ActorApp, &h.Field, &h.FromValue, &h.ToValue, &h.FromLabel, &h.ToLabel, &h.CreatedAt); err != nil {
			return nil, err
		}
		h.IssueID, h.ProjectID, h.ActorID = nullI64(iss), nullI64(proj), nullI64(actor)
		out = append(out, h)
	}
	return out, rows.Err()
}

// AddHistory writes one history entry outside an issue save (relations, attachments, …).
func (s *Store) AddHistory(ctx context.Context, h History) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO history (issue_id, project_id, actor_id, actor_app, field, from_value, to_value, from_label, to_label) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ptrArg(h.IssueID), ptrArg(h.ProjectID), ptrArg(h.ActorID), h.ActorApp, h.Field, h.FromValue, h.ToValue, h.FromLabel, h.ToLabel)
	return err
}

// ---------- subscribers ----------

// Subscribe adds (on) or removes a person from an issue's followers.
func (s *Store) Subscribe(ctx context.Context, issueID, userID int64, on bool) error {
	var err error
	if on {
		_, err = s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO issue_subscribers (issue_id, user_id) VALUES (?, ?)`, issueID, userID)
	} else {
		_, err = s.DB.ExecContext(ctx, `DELETE FROM issue_subscribers WHERE issue_id = ? AND user_id = ?`, issueID, userID)
	}
	return err
}

// Subscribers lists the people following an issue.
func (s *Store) Subscribers(ctx context.Context, issueID int64) []int64 {
	rows, err := s.DB.QueryContext(ctx, `SELECT user_id FROM issue_subscribers WHERE issue_id = ?`, issueID)
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

// ---------- relations ----------

// IssueRelations lists the relations an issue is part of (both directions).
func (s *Store) IssueRelations(ctx context.Context, issueID int64) ([]Relation, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, issue_id, related_id, type, created_at FROM issue_relations WHERE issue_id = ? OR related_id = ? ORDER BY id`, issueID, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Relation{}
	for rows.Next() {
		var r Relation
		if err := rows.Scan(&r.ID, &r.IssueID, &r.RelatedID, &r.Type, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AddRelation links two issues ("blocks": issue blocks related; "duplicate": issue duplicates related).
func (s *Store) AddRelation(ctx context.Context, r *Relation, actorID int64) error {
	if r.Type == "related" && r.IssueID > r.RelatedID {
		r.IssueID, r.RelatedID = r.RelatedID, r.IssueID
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO issue_relations (issue_id, related_id, type, created_by) VALUES (?, ?, ?, ?)`, r.IssueID, r.RelatedID, r.Type, nullID(actorID))
	if err != nil {
		return conflict(err)
	}
	r.ID, _ = res.LastInsertId()
	r.CreatedAt = Now()
	return nil
}

// GetRelation loads one relation.
func (s *Store) GetRelation(ctx context.Context, id int64) (*Relation, error) {
	var r Relation
	err := s.DB.QueryRowContext(ctx, `SELECT id, issue_id, related_id, type, created_at FROM issue_relations WHERE id = ?`, id).Scan(&r.ID, &r.IssueID, &r.RelatedID, &r.Type, &r.CreatedAt)
	return &r, notFound(err)
}

// DeleteRelation removes a relation.
func (s *Store) DeleteRelation(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM issue_relations WHERE id = ?`, id)
	return err
}

// IssueRefs loads small references for ids (access is the caller's business).
func (s *Store) IssueRefs(ctx context.Context, ids []int64) (map[int64]IssueRef, error) {
	out := map[int64]IssueRef{}
	if len(ids) == 0 {
		return out, nil
	}
	q, args := inInts(ids)
	rows, err := s.DB.QueryContext(ctx, `SELECT i.id, t.key, i.number, i.title, i.team_id, i.state_id, ws.type, i.priority FROM issues i
		JOIN teams t ON t.id = i.team_id JOIN workflow_states ws ON ws.id = i.state_id WHERE i.id IN (`+q+`) AND i.deleted_at IS NULL`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r IssueRef
		var key string
		var num int64
		if err := rows.Scan(&r.ID, &key, &num, &r.Title, &r.TeamID, &r.StateID, &r.StateType, &r.Priority); err != nil {
			return nil, err
		}
		r.Identifier = key + "-" + strconv.FormatInt(num, 10)
		out[r.ID] = r
	}
	return out, rows.Err()
}

// RenormalizeOrder spreads a team's sort orders evenly (when gaps get too small).
func (s *Store) RenormalizeOrder(ctx context.Context, teamID int64) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM issues WHERE team_id = ? ORDER BY sort_order, id DESC`, teamID)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE issues SET sort_order = ? WHERE id = ?`, float64(i+1)*1024, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SortedIDs returns ids sorted ascending (helper for stable test output).
func SortedIDs(ids []int64) []int64 {
	out := append([]int64(nil), ids...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
