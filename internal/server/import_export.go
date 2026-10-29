package server

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

func (s *Server) importRoutes(r chi.Router) {
	r.Get("/export/issues.csv", s.handleExportCSV)
	r.Get("/teams/{ref}/export.json", s.handleExportTeam)
	r.Post("/teams/{ref}/import", s.handleImport)
}

// handleExportCSV downloads the issues matching the same filters as GET /issues.
func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := s.accessOf(r)
	f := s.filterFromQuery(r, a)
	if f.Limit == 0 {
		f.Limit = 5000
	}
	if f.Archived == "" {
		f.Archived = "include"
	}
	f.Snoozed = "include"
	list, _, err := s.store.ListIssues(ctx, f)
	if err != nil {
		s.fail(w, err, "export")
		return
	}
	n := newNamer(s, ctx)
	labelName := map[int64]string{}
	labels, _ := s.store.LabelsOf(ctx, a.ids())
	for _, l := range labels {
		labelName[l.ID] = l.Name
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="issues-%s.csv"`, time.Now().Format("20060102")))
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"ID", "Title", "Description", "Status", "Status Type", "Priority", "Estimate", "Assignee", "Creator", "Labels", "Project", "Milestone",
		"Cycle", "Parent", "Due Date", "Created", "Updated", "Started", "Completed", "Canceled", "Archived", "URL"})
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	for _, is := range list {
		var ls []string
		for _, id := range is.LabelIDs {
			ls = append(ls, labelName[id])
		}
		_ = cw.Write([]string{is.Identifier, is.Title, is.Description, n.state(is.StateID), is.StateType, priorityName(is.Priority), fmtEst(is.Estimate),
			s.emailOf(ctx, is.AssigneeID), s.emailOf(ctx, is.CreatorID), strings.Join(ls, ", "), n.project(is.ProjectID), n.milestone(is.MilestoneID),
			n.cycle(is.CycleID), n.issue(is.ParentID), is.DueDate, is.CreatedAt, is.UpdatedAt, str(is.StartedAt), str(is.CompletedAt), str(is.CanceledAt),
			str(is.ArchivedAt), s.issueURL(r, is.Identifier)})
	}
	cw.Flush()
}

func (s *Server) emailOf(ctx context.Context, id *int64) string {
	if id == nil {
		return ""
	}
	return s.userEmail(ctx, *id)
}

// handleExportTeam downloads a team as JSON: workflow, labels, members,
// projects, cycles, issues with comments and relations.
func (s *Server) handleExportTeam(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := s.accessOf(r)
	t, err := s.loadTeam(r, a, chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	s.fillTeam(ctx, t)
	issues, _, _ := s.store.ListIssues(ctx, store.IssueFilter{TeamIDs: []int64{t.ID}, AllowedTeamIDs: []int64{t.ID}, ViewerID: a.uid, Archived: "include", Snoozed: "include", Limit: 5000})
	type issueOut struct {
		*store.Issue
		Comments  []store.Comment  `json:"comments"`
		Relations []store.Relation `json:"relations"`
		History   []store.History  `json:"history"`
		Shared    []store.IssueDoc `json:"shared_notes"`
	}
	var out []issueOut
	for _, is := range issues {
		full, err := s.store.GetIssue(ctx, is.ID, a.uid)
		if err != nil {
			continue
		}
		c, _ := s.store.Comments(ctx, is.ID, 0)
		rel, _ := s.store.IssueRelations(ctx, is.ID)
		h, _ := s.store.IssueHistory(ctx, is.ID)
		sn, _ := s.store.IssueDocs(ctx, is.ID, true)
		out = append(out, issueOut{Issue: full, Comments: c, Relations: rel, History: h, Shared: sn})
	}
	projects, _ := s.store.ListProjects(ctx, store.ProjectFilter{AllowedTeamIDs: []int64{t.ID}})
	for _, p := range projects {
		p.Milestones, _ = s.store.Milestones(ctx, p.ID)
	}
	cycles, _ := s.store.TeamCycles(ctx, []int64{t.ID}, s.today(ctx, a.uid))
	templates, _ := s.store.Templates(ctx, []int64{t.ID})
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="team-%s-%s.json"`, strings.ToLower(t.Key), time.Now().Format("20060102")))
	_ = json.NewEncoder(w).Encode(map[string]any{"format": "pocket-projects/1", "exported_at": store.Now(), "team": t, "projects": projects,
		"cycles": cycles, "templates": templates, "issues": out})
}

// ---------- import ----------

// importColumns maps the header names of Linear, Jira, GitHub and plain CSVs onto fields.
var importColumns = map[string][]string{
	"id":          {"id", "identifier", "issue key", "key", "number", "issue id"},
	"title":       {"title", "summary", "name", "issue", "task"},
	"description": {"description", "body", "details", "notes"},
	"status":      {"status", "state", "column"},
	"priority":    {"priority"},
	"estimate":    {"estimate", "story points", "points", "story point estimate"},
	"assignee":    {"assignee", "assignees", "owner"},
	"labels":      {"labels", "label", "tags", "issue type", "type"},
	"project":     {"project", "project name", "epic", "epic link", "milestone"},
	"due":         {"due date", "due", "due_date", "deadline"},
	"parent":      {"parent", "parent issue", "parent id", "parent key"},
	"created":     {"created", "created at", "created_at", "date created"},
}

type importRow struct {
	ID, Title, Description, Status, Priority, Estimate, Assignee, Labels, Project, Due, Parent, Created string
}

func parseImportCSV(rd io.Reader) ([]importRow, map[string]string, error) {
	cr := csv.NewReader(rd)
	cr.LazyQuotes = true
	cr.FieldsPerRecord = -1
	header, err := cr.Read()
	if err != nil {
		return nil, nil, badRequest("the file is not a CSV with a header row")
	}
	col := map[string]int{}
	mapping := map[string]string{}
	for field, names := range importColumns {
		for i, h := range header {
			h = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")))
			for _, n := range names {
				if h == n {
					if _, done := col[field]; !done {
						col[field] = i
						mapping[field] = header[i]
					}
				}
			}
		}
	}
	if _, ok := col["title"]; !ok {
		return nil, mapping, badRequest("no Title (or Summary / Name) column found")
	}
	get := func(rec []string, field string) string {
		if i, ok := col[field]; ok && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}
	var rows []importRow
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, mapping, badRequest("CSV: " + err.Error())
		}
		row := importRow{ID: get(rec, "id"), Title: get(rec, "title"), Description: get(rec, "description"), Status: get(rec, "status"),
			Priority: get(rec, "priority"), Estimate: get(rec, "estimate"), Assignee: get(rec, "assignee"), Labels: get(rec, "labels"),
			Project: get(rec, "project"), Due: get(rec, "due"), Parent: get(rec, "parent"), Created: get(rec, "created")}
		if row.Title != "" {
			rows = append(rows, row)
		}
		if len(rows) > 5000 {
			return nil, mapping, badRequest("at most 5000 issues per import")
		}
	}
	return rows, mapping, nil
}

func importPriority(v string) int {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "urgent", "highest", "blocker", "critical", "p0":
		return 1
	case "2", "high", "major", "p1":
		return 2
	case "3", "medium", "normal", "p2":
		return 3
	case "4", "low", "lowest", "minor", "trivial", "p3", "p4":
		return 4
	}
	return 0
}

// importStateType guesses a state type from a foreign status name.
func importStateType(v string) string {
	v = strings.ToLower(v)
	switch {
	case v == "":
		return ""
	case strings.Contains(v, "cancel") || strings.Contains(v, "won't") || strings.Contains(v, "wont") || strings.Contains(v, "duplicate") || strings.Contains(v, "rejected"):
		return "canceled"
	case strings.Contains(v, "done") || strings.Contains(v, "closed") || strings.Contains(v, "resolved") || strings.Contains(v, "complete") || strings.Contains(v, "finished"):
		return "completed"
	case strings.Contains(v, "progress") || strings.Contains(v, "review") || strings.Contains(v, "doing") || strings.Contains(v, "started") || strings.Contains(v, "testing"):
		return "started"
	case strings.Contains(v, "backlog") || strings.Contains(v, "icebox") || strings.Contains(v, "someday"):
		return "backlog"
	case strings.Contains(v, "triage"):
		return "triage"
	}
	return "unstarted"
}

func importDate(v string) string {
	v = strings.TrimSpace(v)
	for _, layout := range []string{"2006-01-02", time.RFC3339, "2006-01-02 15:04:05", "02/Jan/06 3:04 PM", "01/02/2006", "02.01.2006", "Mon Jan 02 2006 15:04:05 GMT-0700 (MST)"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.Format("2006-01-02")
		}
	}
	if len(v) >= 10 {
		if t, err := time.Parse("2006-01-02", v[:10]); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return ""
}

// handleImport reads a CSV (multipart field "file" or JSON {csv}) into the team.
// dry_run=1 answers the detected columns and a preview without saving.
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := s.accessOf(r)
	t, err := s.loadTeam(r, a, chi.URLParam(r, "ref"), needWrite)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	var rd io.Reader
	createProjects := true
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
		f, _, err := r.FormFile("file")
		if err != nil {
			writeErr(w, http.StatusBadRequest, "send the CSV as the multipart field \"file\"")
			return
		}
		defer f.Close()
		rd = f
		createProjects = r.FormValue("create_projects") != "0"
	} else {
		var in struct {
			CSV            string `json:"csv"`
			CreateProjects *bool  `json:"create_projects"`
		}
		if err := decode(r, &in); err != nil || in.CSV == "" {
			writeErr(w, http.StatusBadRequest, "send {\"csv\": \"…\"} or a multipart file")
			return
		}
		rd = strings.NewReader(in.CSV)
		if in.CreateProjects != nil {
			createProjects = *in.CreateProjects
		}
	}
	rows, mapping, err := parseImportCSV(rd)
	if err != nil {
		s.fail(w, err, "import")
		return
	}
	if queryBool(r, "dry_run") {
		preview := rows
		if len(preview) > 10 {
			preview = preview[:10]
		}
		writeJSON(w, http.StatusOK, map[string]any{"columns": mapping, "rows": len(rows), "preview": preview})
		return
	}
	states, _ := s.store.TeamStates(ctx, t.ID)
	members, _ := s.store.TeamMembers(ctx, t.ID)
	projects, _ := s.store.ListProjects(ctx, store.ProjectFilter{AllowedTeamIDs: a.ids()})
	projectByName := map[string]int64{}
	for _, p := range projects {
		projectByName[strings.ToLower(p.Name)] = p.ID
	}
	who := s.actorOf(r)
	who.app = ""
	byForeign := map[string]int64{}
	created, failed := 0, []string{}
	for _, row := range rows {
		in := issueInput{TeamID: &t.ID, Title: ptr(row.Title)}
		if row.Description != "" {
			in.Description = ptr(row.Description)
		}
		if row.Status != "" {
			var match *store.State
			for i, st := range states {
				if strings.EqualFold(st.Name, row.Status) {
					match = &states[i]
				}
			}
			if match == nil {
				typ := importStateType(row.Status)
				for i, st := range states {
					if st.Type == typ {
						match = &states[i]
						break
					}
				}
			}
			if match != nil {
				in.StateID = &match.ID
			}
		}
		if p := importPriority(row.Priority); p > 0 {
			in.Priority = &p
		}
		if row.Estimate != "" {
			if f, err := strconv.ParseFloat(row.Estimate, 64); err == nil && f >= 0 {
				raw := json.RawMessage(strconv.FormatFloat(f, 'f', -1, 64))
				in.Estimate = &raw
			}
		}
		if row.Assignee != "" {
			first := strings.TrimSpace(strings.Split(row.Assignee, ",")[0])
			for _, m := range members {
				if strings.EqualFold(m.Email, first) || strings.EqualFold(m.Name, first) || strings.EqualFold(m.Username, first) {
					in.AssigneeID = ptr(m.ID)
				}
			}
		}
		if row.Labels != "" {
			for _, l := range strings.FieldsFunc(row.Labels, func(r rune) bool { return r == ',' || r == ';' || r == '|' }) {
				if l = strings.TrimSpace(l); l != "" {
					in.Labels = append(in.Labels, l)
				}
			}
		}
		if row.Project != "" {
			id, ok := projectByName[strings.ToLower(row.Project)]
			if !ok && createProjects {
				p := &store.Project{Name: row.Project, Status: "planned", UpdateCadenceDays: 7, CreatedBy: who.idPtr(), TeamIDs: []int64{t.ID}, MemberIDs: []int64{}, LeadID: who.idPtr()}
				if err := s.store.SaveProject(ctx, p, nil); err == nil {
					id, ok = p.ID, true
					projectByName[strings.ToLower(row.Project)] = id
				}
			}
			if ok {
				in.ProjectID = &id
			}
		}
		if d := importDate(row.Due); d != "" {
			in.DueDate = &d
		}
		is, err := s.createIssue(ctx, a, who, in)
		if err != nil {
			failed = append(failed, row.Title+": "+err.Error())
			continue
		}
		created++
		if row.ID != "" {
			byForeign[strings.ToUpper(row.ID)] = is.ID
			if store.IdentifierRe.MatchString(row.ID) && !strings.EqualFold(row.ID, is.Identifier) {
				_ = s.store.AddAlias(ctx, row.ID, is.ID)
			}
		}
		if c := importDate(row.Created); c != "" {
			_, _ = s.store.DB.ExecContext(ctx, `UPDATE issues SET created_at = ? WHERE id = ?`, c+"T09:00:00Z", is.ID)
		}
	}
	// Second pass: parents by the file's own ids.
	for _, row := range rows {
		if row.Parent == "" || row.ID == "" {
			continue
		}
		child, ok1 := byForeign[strings.ToUpper(row.ID)]
		parent, ok2 := byForeign[strings.ToUpper(strings.TrimSpace(row.Parent))]
		if ok1 && ok2 && child != parent {
			_, _ = s.store.DB.ExecContext(ctx, `UPDATE issues SET parent_id = ? WHERE id = ?`, parent, child)
		}
	}
	s.changed(r, "issue", []int64{t.ID})
	writeJSON(w, http.StatusOK, map[string]any{"created": created, "failed": failed, "columns": mapping})
}
