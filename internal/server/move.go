package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// Pocket Move (docs/move.md in pocket-homelab-core). What is a person's own
// here: the issues they wrote or are working on, in the teams they still
// belong to, with their own comments. Teams, projects, cycles and other
// people's comments belong to the workspace and stay with it (a team's
// export is GET /teams/{ref}/export.json).

type moveComment struct {
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

type moveIssue struct {
	Identifier  string        `json:"identifier"`
	Team        string        `json:"team"`
	TeamName    string        `json:"team_name"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Status      string        `json:"status"`
	StatusType  string        `json:"status_type"`
	Priority    int           `json:"priority"`
	Estimate    *float64      `json:"estimate,omitempty"`
	Labels      []string      `json:"labels"`
	Project     string        `json:"project,omitempty"`
	DueDate     string        `json:"due_date,omitempty"`
	CreatedAt   string        `json:"created_at"`
	Mine        bool          `json:"assigned_to_me"`
	Comments    []moveComment `json:"comments"`
}

type moveDoc struct {
	Format     string      `json:"format"`
	ExportedAt string      `json:"exported_at"`
	Issues     []moveIssue `json:"issues"`
}

const moveFormat = "pocket-projects-mine/1"

// myIssues are the issues person a.uid created or is assigned to, in the
// teams they can still read, oldest first.
func (s *Server) myIssues(ctx context.Context, a *access) ([]*store.Issue, error) {
	seen := map[int64]bool{}
	var out []*store.Issue
	for _, f := range []store.IssueFilter{{CreatorIDs: []int64{a.uid}}, {AssigneeIDs: []int64{a.uid}}} {
		f.AllowedTeamIDs, f.ViewerID, f.Archived, f.Snoozed, f.Limit = a.ids(), a.uid, "include", "include", 100000
		list, _, err := s.store.ListIssues(ctx, f)
		if err != nil {
			return nil, err
		}
		for _, is := range list {
			if !seen[is.ID] {
				seen[is.ID] = true
				out = append(out, is)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out, nil
}

// handleExportMine: GET /export.json, the person's own issues and comments.
func (s *Server) handleExportMine(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := s.accessOf(r)
	list, err := s.myIssues(ctx, a)
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
	teams := map[int64]*store.Team{}
	doc := moveDoc{Format: moveFormat, ExportedAt: store.Now(), Issues: []moveIssue{}}
	for _, is := range list {
		if full, err := s.store.GetIssue(ctx, is.ID, a.uid); err == nil {
			is = full
		}
		t, ok := teams[is.TeamID]
		if !ok {
			t, _ = s.store.GetTeam(ctx, is.TeamID)
			teams[is.TeamID] = t
		}
		mi := moveIssue{Identifier: is.Identifier, Title: is.Title, Description: is.Description, Status: n.state(is.StateID), StatusType: is.StateType,
			Priority: is.Priority, Estimate: is.Estimate, Labels: []string{}, Project: n.project(is.ProjectID), DueDate: is.DueDate, CreatedAt: is.CreatedAt,
			Mine: is.AssigneeID != nil && *is.AssigneeID == a.uid, Comments: []moveComment{}}
		if t != nil {
			mi.Team, mi.TeamName = t.Key, t.Name
		}
		for _, id := range is.LabelIDs {
			if l := labelName[id]; l != "" {
				mi.Labels = append(mi.Labels, l)
			}
		}
		cs, _ := s.store.Comments(ctx, is.ID, 0)
		for _, c := range cs {
			if c.UserID != nil && *c.UserID == a.uid && strings.TrimSpace(c.Body) != "" {
				mi.Comments = append(mi.Comments, moveComment{Body: c.Body, CreatedAt: c.CreatedAt})
			}
		}
		doc.Issues = append(doc.Issues, mi)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="my-issues-%s.json"`, time.Now().Format("20060102")))
	_ = json.NewEncoder(w).Encode(doc)
}

// handleExportMineCSV: GET /export/my-issues.csv, the same issues in the CSV the import reads.
func (s *Server) handleExportMineCSV(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	list, err := s.myIssues(r.Context(), a)
	if err != nil {
		s.fail(w, err, "export")
		return
	}
	s.writeIssuesCSV(w, r, a, list, fmt.Sprintf("my-issues-%s.csv", time.Now().Format("20060102")))
}

// handleImportMine: POST /import, an export.json from handleExportMine. Each
// issue goes to the team with the same key when the person is a member of
// it, else to their first team; it is theirs (they wrote it), keeps its
// date, status, labels and project, and gets their comments back. An issue
// they already wrote or work on there with the same title and date is
// skipped, so a second run adds nothing.
func (s *Server) handleImportMine(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var doc moveDoc
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(&doc); err != nil || doc.Format != moveFormat {
		writeErr(w, http.StatusBadRequest, "this is not a Pocket Projects export of your issues (export.json)")
		return
	}
	a := s.accessOf(r)
	var writable []int64
	for id := range a.teams {
		if a.can(id, needWrite) {
			writable = append(writable, id)
		}
	}
	sort.Slice(writable, func(i, j int) bool { return writable[i] < writable[j] })
	if len(writable) == 0 && len(doc.Issues) > 0 {
		writeErr(w, http.StatusBadRequest, "join a team first: the issues go into one of your teams")
		return
	}
	who := s.actorOf(r)
	who.app = ""
	states := map[int64][]store.State{}
	projects, _ := s.store.ListProjects(ctx, store.ProjectFilter{AllowedTeamIDs: a.ids()})
	projectByName := map[string]int64{}
	for _, p := range projects {
		projectByName[strings.ToLower(p.Name)] = p.ID
	}
	created, comments, skipped := 0, 0, 0
	failed := []string{}
	touched := map[int64]bool{}
	for _, mi := range doc.Issues {
		title := strings.TrimSpace(mi.Title)
		if title == "" {
			continue
		}
		teamID := writable[0]
		if t, err := s.store.GetTeamByKey(ctx, mi.Team); err == nil && a.can(t.ID, needWrite) {
			teamID = t.ID
		}
		var have int64
		_ = s.store.DB.QueryRowContext(ctx, `SELECT id FROM issues WHERE team_id = ? AND (creator_id = ? OR assignee_id = ?) AND title = ? AND (? = '' OR created_at = ?) LIMIT 1`,
			teamID, a.uid, a.uid, title, mi.CreatedAt, mi.CreatedAt).Scan(&have)
		if have > 0 {
			skipped++
			continue
		}
		in := issueInput{TeamID: &teamID, Title: &title, Labels: mi.Labels, Assignee: "none"}
		if mi.Mine {
			in.Assignee = "me"
		}
		if mi.Description != "" {
			in.Description = &mi.Description
		}
		if _, ok := states[teamID]; !ok {
			states[teamID], _ = s.store.TeamStates(ctx, teamID)
		}
		if id := pickState(states[teamID], mi.Status, mi.StatusType); id > 0 {
			in.StateID = &id
		}
		if mi.Priority > 0 && mi.Priority <= 4 {
			in.Priority = &mi.Priority
		}
		if mi.Estimate != nil && *mi.Estimate >= 0 {
			raw := json.RawMessage(strconv.FormatFloat(*mi.Estimate, 'f', -1, 64))
			in.Estimate = &raw
		}
		if d := importDate(mi.DueDate); d != "" {
			in.DueDate = &d
		}
		if name := strings.TrimSpace(mi.Project); name != "" {
			id, ok := projectByName[strings.ToLower(name)]
			if !ok {
				p := &store.Project{Name: name, Status: "planned", UpdateCadenceDays: 7, CreatedBy: who.idPtr(), TeamIDs: []int64{teamID}, MemberIDs: []int64{}, LeadID: who.idPtr()}
				if err := s.store.SaveProject(ctx, p, nil); err == nil {
					id, ok = p.ID, true
					projectByName[strings.ToLower(name)] = id
				}
			}
			if ok {
				in.ProjectID = &id
			}
		}
		is, err := s.createIssue(ctx, a, who, in)
		var bad userError
		if err != nil && errors.As(err, &bad) && in.ProjectID != nil {
			in.ProjectID = nil // a project of another team: the issue still comes in
			is, err = s.createIssue(ctx, a, who, in)
		}
		if err != nil {
			failed = append(failed, title+": "+err.Error())
			continue
		}
		created++
		touched[teamID] = true
		if mi.CreatedAt != "" {
			_, _ = s.store.DB.ExecContext(ctx, `UPDATE issues SET created_at = ? WHERE id = ?`, mi.CreatedAt, is.ID)
		}
		for _, c := range mi.Comments {
			if strings.TrimSpace(c.Body) == "" {
				continue
			}
			nc := &store.Comment{IssueID: &is.ID, UserID: &a.uid, Body: c.Body}
			if err := s.store.SaveComment(ctx, nc); err != nil {
				continue
			}
			if c.CreatedAt != "" {
				_, _ = s.store.DB.ExecContext(ctx, `UPDATE comments SET created_at = ? WHERE id = ?`, c.CreatedAt, nc.ID)
			}
			comments++
		}
	}
	if len(touched) > 0 {
		var ids []int64
		for id := range touched {
			ids = append(ids, id)
		}
		s.changed(r, "issue", ids)
	}
	writeJSON(w, http.StatusOK, map[string]any{"created": created, "comments": comments, "skipped": skipped, "failed": failed})
}

// pickState finds a team's state by name, else by type.
func pickState(states []store.State, name, typ string) int64 {
	for _, st := range states {
		if name != "" && strings.EqualFold(st.Name, name) {
			return st.ID
		}
	}
	for _, st := range states {
		if typ != "" && st.Type == typ {
			return st.ID
		}
	}
	return 0
}
