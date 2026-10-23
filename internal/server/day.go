package server

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// A person's work on one day, for Pocket Day (link scope projects:read):
// everything they did, grouped per issue — created, moved, commented,
// tracked time, attached, started Pocket AI conversations — plus the commits
// and pull requests that landed on those issues (and on issues assigned to
// them), and the project updates they posted. Each issue comes with a
// one-line summary ("moved to In Review · 2 comments · PR #3983 · 1 h 20 min")
// and its events, so the journal can show the work at a glance and open any
// of it here.

type dayEvent struct {
	Time string `json:"time"` // HH:MM local, "" when untimed (tracked time)
	Kind string `json:"kind"` // created | state | comment | time | commit | pull_request | ai | attachment | edit | assign | priority | update
	Text string `json:"text"`
	URL  string `json:"url,omitempty"`
	at   time.Time
}

type dayIssue struct {
	Identifier string     `json:"identifier"`
	Title      string     `json:"title"`
	URL        string     `json:"url"`
	Team       string     `json:"team"`
	Project    string     `json:"project,omitempty"`
	Status     string     `json:"status"`
	StatusType string     `json:"status_type"`
	Done       bool       `json:"done"` // the person completed it this day
	Created    bool       `json:"created"`
	Minutes    int        `json:"minutes"`
	First      string     `json:"first,omitempty"`
	Last       string     `json:"last,omitempty"`
	Summary    string     `json:"summary"`
	Events     []dayEvent `json:"events"`
	id         int64
	mine       bool // the person acted on it (not only commits landing)
}

type dayProject struct {
	Name   string     `json:"name"`
	URL    string     `json:"url"`
	Events []dayEvent `json:"events"`
}

type dayWork struct {
	Date     string        `json:"date"`
	Timezone string        `json:"timezone"`
	Issues   []*dayIssue   `json:"issues"`
	Projects []*dayProject `json:"projects"`
	Minutes  int           `json:"minutes"`
}

const dbTime = "2006-01-02T15:04:05.000Z"

func (s *Server) handleDay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	date := chi.URLParam(r, "date")
	uid := me(r)
	loc := s.loc(ctx, uid)
	if tz := r.URL.Query().Get("tz"); tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	day, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "the date is YYYY-MM-DD")
		return
	}
	out, err := s.dayWork(ctx, r, uid, s.accessOf(r).ids(), day, loc)
	if err != nil {
		s.fail(w, err, "day")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) dayWork(ctx context.Context, r *http.Request, uid int64, teams []int64, day time.Time, loc *time.Location) (*dayWork, error) {
	from, to := day.UTC().Format(dbTime), day.AddDate(0, 0, 1).UTC().Format(dbTime)
	out := &dayWork{Date: day.Format("2006-01-02"), Timezone: loc.String(), Issues: []*dayIssue{}, Projects: []*dayProject{}}
	if len(teams) == 0 {
		return out, nil
	}
	allowed := strings.TrimSuffix(strings.Repeat("?,", len(teams)), ",")
	teamArgs := make([]any, len(teams))
	for i, t := range teams {
		teamArgs[i] = t
	}
	issues := map[int64]*dayIssue{}
	clock := func(ts string) (string, time.Time) {
		t, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return "", time.Time{}
		}
		t = t.In(loc)
		return t.Format("15:04"), t
	}
	add := func(issueID int64, mine bool, e dayEvent) {
		is := issues[issueID]
		if is == nil {
			is = &dayIssue{id: issueID, Events: []dayEvent{}}
			issues[issueID] = is
		}
		is.mine = is.mine || mine
		is.Events = append(is.Events, e)
	}
	// the person's issues in readable teams: identity, team, project, state
	load := func(ids []int64) error {
		for _, id := range ids {
			is := issues[id]
			var title, key, team, state, stype string
			var num int64
			var pid sql.NullInt64
			var pname sql.NullString
			err := s.store.DB.QueryRowContext(ctx, `SELECT i.title, i.number, t.key, t.name, ws.name, ws.type, i.project_id, p.name
				FROM issues i JOIN teams t ON t.id = i.team_id JOIN workflow_states ws ON ws.id = i.state_id LEFT JOIN projects p ON p.id = i.project_id
				WHERE i.id = ? AND i.deleted_at IS NULL AND i.team_id IN (`+allowed+`)`, append([]any{id}, teamArgs...)...).
				Scan(&title, &num, &key, &team, &state, &stype, &pid, &pname)
			if err != nil {
				delete(issues, id) // gone, or in a team the person can't see any more
				continue
			}
			is.Identifier = strings.ToUpper(key) + "-" + strconv.FormatInt(num, 10)
			is.Title, is.Team, is.Status, is.StatusType, is.Project = title, team, state, stype, pname.String
			is.URL = s.issueURL(r, is.Identifier)
		}
		return nil
	}

	// 1. what the person changed (history)
	rows, err := s.store.DB.QueryContext(ctx, `SELECT h.issue_id, h.field, h.from_label, h.to_label, h.to_value, h.created_at
		FROM history h WHERE h.actor_id = ? AND h.issue_id IS NOT NULL AND h.created_at >= ? AND h.created_at < ? ORDER BY h.created_at`, uid, from, to)
	if err != nil {
		return nil, err
	}
	stateTypes := map[string]string{}
	for rows.Next() {
		var iid int64
		var field, fromL, toL, toV, at string
		if rows.Scan(&iid, &field, &fromL, &toL, &toV, &at) != nil {
			continue
		}
		hm, t := clock(at)
		e := dayEvent{Time: hm, at: t}
		switch field {
		case "created":
			e.Kind, e.Text = "created", "Created the issue"
		case "state":
			e.Kind, e.Text = "state", "Moved to "+firstNonEmpty(toL, toV)
			stateTypes[strconv.FormatInt(iid, 10)+"|"+e.Time] = toV
		case "assignee":
			e.Kind = "assign"
			if toL != "" {
				e.Text = "Assigned to " + toL
			} else {
				e.Text = "Unassigned"
			}
		case "priority":
			e.Kind, e.Text = "priority", "Priority "+firstNonEmpty(toL, toV)
		case "title":
			e.Kind, e.Text = "edit", "Renamed"
		case "description":
			e.Kind, e.Text = "edit", "Edited the description"
		case "attachment_added":
			e.Kind, e.Text = "attachment", "Attached "+toL
		case "ai_thread":
			e.Kind, e.Text = "ai", "Pocket AI · "+firstNonEmpty(toL, toV)
			e.Text = strings.Replace(e.Text, "Pocket AI · Pocket AI · ", "Pocket AI · ", 1)
		case "git_commit", "git_pull_request":
			continue // counted from the attachments below, with their links
		case "label_added":
			e.Kind, e.Text = "edit", "Labelled "+toL
		case "label_removed":
			e.Kind, e.Text = "edit", "Removed the label "+fromL
		case "project", "estimate", "due_date", "cycle", "milestone", "parent", "team":
			e.Kind, e.Text = "edit", "Set "+strings.ReplaceAll(field, "_", " ")+map[bool]string{true: " to " + toL, false: ""}[toL != ""]
		default:
			e.Kind, e.Text = "edit", strings.ToUpper(field[:1])+strings.ReplaceAll(field[1:], "_", " ")
		}
		add(iid, true, e)
	}
	rows.Close()

	// 2. comments
	rows, err = s.store.DB.QueryContext(ctx, `SELECT issue_id, body, created_at FROM comments
		WHERE user_id = ? AND issue_id IS NOT NULL AND deleted_at IS NULL AND created_at >= ? AND created_at < ? ORDER BY created_at`, uid, from, to)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var iid int64
		var body, at string
		if rows.Scan(&iid, &body, &at) != nil {
			continue
		}
		hm, t := clock(at)
		add(iid, true, dayEvent{Time: hm, at: t, Kind: "comment", Text: "Commented: " + clip(oneLineText(body), 140)})
	}
	rows.Close()

	// 3. tracked time (spent_on is already a local date)
	minutes := map[int64]int{}
	rows, err = s.store.DB.QueryContext(ctx, `SELECT issue_id, minutes, note FROM time_entries WHERE user_id = ? AND spent_on = ? AND minutes > 0`, uid, out.Date)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var iid int64
		var m int
		var note string
		if rows.Scan(&iid, &m, &note) != nil {
			continue
		}
		minutes[iid] += m
		text := "Tracked " + fmtMinutes(m)
		if note = strings.TrimSpace(note); note != "" {
			text += " · " + clip(oneLineText(note), 100)
		}
		add(iid, true, dayEvent{Kind: "time", Text: text})
	}
	rows.Close()

	// 4. commits and pull requests that landed on the person's issues of the
	//    day, or on issues assigned to them (git authors are names, not people)
	rows, err = s.store.DB.QueryContext(ctx, `SELECT a.issue_id, a.kind, a.title, a.subtitle, a.url, a.state, a.created_at FROM attachments a JOIN issues i ON i.id = a.issue_id
		WHERE a.kind IN ('commit','pull_request') AND a.created_at >= ? AND a.created_at < ? ORDER BY a.created_at`, from, to)
	if err != nil {
		return nil, err
	}
	type gitRow struct {
		iid                         int64
		kind, title, sub, u, st, at string
	}
	var git []gitRow
	for rows.Next() {
		var g gitRow
		if rows.Scan(&g.iid, &g.kind, &g.title, &g.sub, &g.u, &g.st, &g.at) == nil {
			git = append(git, g)
		}
	}
	rows.Close()
	for _, g := range git {
		if issues[g.iid] == nil {
			var assignee sql.NullInt64
			if s.store.DB.QueryRowContext(ctx, `SELECT assignee_id FROM issues WHERE id = ?`, g.iid).Scan(&assignee) != nil || !assignee.Valid || assignee.Int64 != uid {
				continue
			}
		}
		hm, t := clock(g.at)
		e := dayEvent{Time: hm, at: t, Kind: g.kind, URL: g.u}
		if g.kind == "commit" {
			e.Text = "Commit: " + clip(oneLineText(g.title), 120)
		} else {
			e.Text = "Pull request: " + clip(oneLineText(g.title), 120)
			if g.st != "" {
				e.Text += " [" + g.st + "]"
			}
		}
		add(g.iid, false, e)
	}

	ids := make([]int64, 0, len(issues))
	for id := range issues {
		ids = append(ids, id)
	}
	if err := load(ids); err != nil {
		return nil, err
	}
	for id, is := range issues {
		sort.SliceStable(is.Events, func(i, j int) bool {
			a, b := is.Events[i].Time, is.Events[j].Time
			if a == "" {
				a = "99"
			}
			if b == "" {
				b = "99"
			}
			return a < b
		})
		is.Minutes = minutes[id]
		for _, e := range is.Events {
			if e.Time != "" {
				if is.First == "" {
					is.First = e.Time
				}
				is.Last = e.Time
			}
			switch e.Kind {
			case "created":
				is.Created = true
			case "state":
				if stateTypes[strconv.FormatInt(id, 10)+"|"+e.Time] != "" {
					// the state's type decides "done", not its name
					var typ string
					if s.store.DB.QueryRowContext(ctx, `SELECT type FROM workflow_states WHERE id = ?`, stateTypes[strconv.FormatInt(id, 10)+"|"+e.Time]).Scan(&typ) == nil && typ == "completed" {
						is.Done = true
					}
				}
			}
		}
		is.Summary = daySummary(is)
		out.Issues = append(out.Issues, is)
		out.Minutes += is.Minutes
	}
	// the person's own work first, then by when it last happened
	sort.SliceStable(out.Issues, func(i, j int) bool {
		a, b := out.Issues[i], out.Issues[j]
		if a.mine != b.mine {
			return a.mine
		}
		return a.Last > b.Last
	})

	// 5. project updates
	rows, err = s.store.DB.QueryContext(ctx, `SELECT pu.project_id, p.name, pu.health, pu.body, pu.created_at FROM project_updates pu JOIN projects p ON p.id = pu.project_id
		WHERE pu.user_id = ? AND pu.created_at >= ? AND pu.created_at < ? AND p.deleted_at IS NULL ORDER BY pu.created_at`, uid, from, to)
	if err != nil {
		return nil, err
	}
	projects := map[int64]*dayProject{}
	for rows.Next() {
		var pid int64
		var name, health, body, at string
		if rows.Scan(&pid, &name, &health, &body, &at) != nil {
			continue
		}
		p := projects[pid]
		if p == nil {
			p = &dayProject{Name: name, URL: s.projectURL(r, pid), Events: []dayEvent{}}
			projects[pid] = p
			out.Projects = append(out.Projects, p)
		}
		hm, _ := clock(at)
		p.Events = append(p.Events, dayEvent{Time: hm, Kind: "update", Text: "Posted an update (" + strings.ReplaceAll(health, "_", " ") + "): " + clip(oneLineText(body), 140)})
	}
	rows.Close()
	return out, nil
}

// daySummary is the one line under an issue: what happened, counted.
func daySummary(is *dayIssue) string {
	var parts []string
	counts := map[string]int{}
	lastState, prNames := "", []string{}
	for _, e := range is.Events {
		counts[e.Kind]++
		switch e.Kind {
		case "state":
			lastState = strings.TrimPrefix(e.Text, "Moved to ")
		case "pull_request":
			prNames = append(prNames, strings.TrimPrefix(e.Text, "Pull request: "))
		}
	}
	if is.Created {
		parts = append(parts, "created")
	}
	if lastState != "" {
		if is.Done {
			parts = append(parts, "done ("+lastState+")")
		} else {
			parts = append(parts, "moved to "+lastState)
		}
	}
	plural := func(n int, one, many string) string {
		if n == 1 {
			return "1 " + one
		}
		return fmt.Sprintf("%d %s", n, many)
	}
	if n := counts["comment"]; n > 0 {
		parts = append(parts, plural(n, "comment", "comments"))
	}
	if n := counts["commit"]; n > 0 {
		parts = append(parts, plural(n, "commit", "commits"))
	}
	if n := counts["pull_request"]; n > 0 {
		parts = append(parts, plural(n, "pull request", "pull requests"))
	}
	if n := counts["ai"]; n > 0 {
		parts = append(parts, plural(n, "Pocket AI conversation", "Pocket AI conversations"))
	}
	if n := counts["attachment"]; n > 0 {
		parts = append(parts, plural(n, "file", "files"))
	}
	if n := counts["edit"] + counts["assign"] + counts["priority"]; n > 0 && len(parts) == 0 {
		parts = append(parts, plural(n, "change", "changes"))
	}
	if is.Minutes > 0 {
		parts = append(parts, fmtMinutes(is.Minutes)+" tracked")
	}
	return strings.Join(parts, " · ")
}

func fmtMinutes(m int) string {
	if m < 60 {
		return fmt.Sprintf("%d min", m)
	}
	if m%60 == 0 {
		return fmt.Sprintf("%d h", m/60)
	}
	return fmt.Sprintf("%d h %d min", m/60, m%60)
}

func oneLineText(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i]) + " …"
	}
	return s
}
