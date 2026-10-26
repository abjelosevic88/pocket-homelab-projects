package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

func (s *Server) teamRoutes(r chi.Router) {
	r.Get("/teams", s.handleListTeams)
	r.Post("/teams", s.handleCreateTeam)
	r.Get("/team-presets", s.handleTeamPresets)
	r.Get("/teams/{ref}", s.handleGetTeam)
	r.Patch("/teams/{ref}", s.handlePatchTeam)
	r.Delete("/teams/{ref}", s.handleDeleteTeam)
	r.Post("/teams/{ref}/leave", s.handleLeaveTeam)
	r.Get("/teams/{ref}/members", s.handleTeamMembers)
	r.Post("/teams/{ref}/members", s.handleAddMember)
	r.Patch("/teams/{ref}/members/{uid}", s.handleSetMemberRole)
	r.Delete("/teams/{ref}/members/{uid}", s.handleRemoveMember)
	r.Get("/teams/{ref}/states", s.handleTeamStates)
	r.Post("/teams/{ref}/states", s.handleCreateState)
	r.Post("/teams/{ref}/states/reorder", s.handleReorderStates)
	r.Patch("/states/{id}", s.handlePatchState)
	r.Delete("/states/{id}", s.handleDeleteState)
	r.Get("/teams/{ref}/labels", s.handleTeamLabels)
	r.Post("/teams/{ref}/labels", s.handleCreateLabel)
	r.Patch("/labels/{id}", s.handlePatchLabel)
	r.Delete("/labels/{id}", s.handleDeleteLabel)
	r.Get("/teams/{ref}/templates", s.handleTeamTemplates)
	r.Post("/teams/{ref}/templates", s.handleCreateTemplate)
	r.Patch("/templates/{id}", s.handlePatchTemplate)
	r.Delete("/templates/{id}", s.handleDeleteTemplate)
	r.Get("/teams/{ref}/webhooks", s.handleTeamWebhooks)
	r.Post("/teams/{ref}/webhooks", s.handleCreateWebhook)
	r.Patch("/webhooks/{id}", s.handlePatchWebhook)
	r.Delete("/webhooks/{id}", s.handleDeleteWebhook)
	r.Post("/webhooks/{id}/test", s.handleTestWebhook)
	r.Get("/teams/{ref}/git", s.handleTeamGit)
	// a person's work on one day (Pocket Day): day.go
	r.Get("/day/{date}", s.handleDay)
	r.Post("/teams/{ref}/git/rotate", s.handleRotateGit)
}

// fillTeam adds members, workflow, labels and counts to a team.
func (s *Server) fillTeam(ctx context.Context, t *store.Team) {
	t.Members, _ = s.store.TeamMembers(ctx, t.ID)
	t.States, _ = s.store.TeamStates(ctx, t.ID)
	t.Labels, _ = s.store.TeamLabels(ctx, t.ID)
	t.Counts = s.store.TeamCounts(ctx, t.ID)
	t.NotesFolder = s.store.TeamNotesFolder(ctx, t.ID)
}

func (s *Server) handleListTeams(w http.ResponseWriter, r *http.Request) {
	teams, err := s.store.ListTeams(r.Context(), me(r))
	if err != nil {
		s.fail(w, err, "teams")
		return
	}
	for i := range teams {
		s.fillTeam(r.Context(), &teams[i])
	}
	writeJSON(w, http.StatusOK, teams)
}

func (s *Server) handleTeamPresets(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	for _, p := range store.TeamPresets {
		var states []string
		for _, st := range p.States {
			states = append(states, st.Name)
		}
		out = append(out, map[string]any{"id": p.ID, "name": p.Name, "description": p.Description, "states": states, "cycles": p.Cycles, "triage": p.Triage, "estimates": p.Estimates})
	}
	writeJSON(w, http.StatusOK, out)
}

type teamInput struct {
	Key                *string `json:"key"`
	Name               *string `json:"name"`
	Description        *string `json:"description"`
	Color              *string `json:"color"`
	Icon               *string `json:"icon"`
	Preset             string  `json:"preset"`
	TriageEnabled      *bool   `json:"triage_enabled"`
	CyclesEnabled      *bool   `json:"cycles_enabled"`
	CycleWeeks         *int    `json:"cycle_weeks"`
	CycleStartDay      *int    `json:"cycle_start_day"`
	CycleCooldownWeeks *int    `json:"cycle_cooldown_weeks"`
	CycleUpcoming      *int    `json:"cycle_upcoming"`
	CycleRollover      *bool   `json:"cycle_rollover"`
	EstimateScale      *string `json:"estimate_scale"`
	AutoArchiveMonths  *int    `json:"auto_archive_months"`
	DefaultStateID     *int64  `json:"default_state_id"`
	DefaultAssignee    *string `json:"default_assignee"` // creator | none | a member's user id
	Archived           *bool   `json:"archived"`
	Members            []struct {
		UserID int64  `json:"user_id"`
		Email  string `json:"email"`
		Role   string `json:"role"`
	} `json:"members"`
}

func (in teamInput) apply(t *store.Team) error {
	if in.Key != nil {
		t.Key = strings.ToUpper(strings.TrimSpace(*in.Key))
	}
	if in.Name != nil {
		t.Name = strings.TrimSpace(*in.Name)
	}
	if t.Name == "" {
		return badRequest("a team needs a name")
	}
	if in.Description != nil {
		t.Description = *in.Description
	}
	if in.Color != nil {
		t.Color = *in.Color
	}
	if in.Icon != nil {
		t.Icon = *in.Icon
	}
	if in.TriageEnabled != nil {
		t.TriageEnabled = *in.TriageEnabled
	}
	if in.CyclesEnabled != nil {
		t.CyclesEnabled = *in.CyclesEnabled
	}
	if in.CycleWeeks != nil {
		if *in.CycleWeeks < 1 || *in.CycleWeeks > 8 {
			return badRequest("cycles are 1 to 8 weeks long")
		}
		t.CycleWeeks = *in.CycleWeeks
	}
	if in.CycleStartDay != nil {
		t.CycleStartDay = ((*in.CycleStartDay % 7) + 7) % 7
	}
	if in.CycleCooldownWeeks != nil {
		t.CycleCooldownWeeks = max(0, min(*in.CycleCooldownWeeks, 4))
	}
	if in.CycleUpcoming != nil {
		t.CycleUpcoming = max(1, min(*in.CycleUpcoming, 6))
	}
	if in.CycleRollover != nil {
		t.CycleRollover = *in.CycleRollover
	}
	if in.EstimateScale != nil {
		switch *in.EstimateScale {
		case "none", "linear", "fibonacci", "exponential", "tshirt":
			t.EstimateScale = *in.EstimateScale
		default:
			return badRequest("estimate_scale is none, linear, fibonacci, exponential or tshirt")
		}
	}
	if in.AutoArchiveMonths != nil {
		t.AutoArchiveMonths = max(0, min(*in.AutoArchiveMonths, 24))
	}
	if in.Archived != nil {
		if *in.Archived && t.ArchivedAt == nil {
			now := store.Now()
			t.ArchivedAt = &now
		} else if !*in.Archived {
			t.ArchivedAt = nil
		}
	}
	return nil
}

func (s *Server) handleCreateTeam(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if s.linkApp(r) != "" {
		writeErr(w, http.StatusForbidden, "apps can't create teams")
		return
	}
	var in teamInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	t := &store.Team{}
	if err := in.apply(t); err != nil {
		s.fail(w, err, "team")
		return
	}
	if t.Key == "" {
		t.Key = s.suggestKey(ctx, t.Name)
	}
	if err := s.store.CreateTeam(ctx, t, store.Preset(in.Preset), me(r)); err != nil {
		if err == store.ErrConflict {
			writeErr(w, http.StatusConflict, "another team already uses the key "+t.Key)
			return
		}
		s.fail(w, badRequest(err.Error()), "team")
		return
	}
	for _, m := range in.Members {
		u, err := s.personFor(ctx, userFrom(ctx), m.UserID, m.Email)
		if err != nil || u.ID == me(r) {
			continue
		}
		role := m.Role
		if roleRank(role) == 0 {
			role = "member"
		}
		_ = s.store.SetTeamMember(ctx, t.ID, u.ID, role)
		s.notifyAdded(ctx, s.actorOf(r), u.ID, t)
	}
	if st, _ := s.store.GetSettings(ctx, me(r)); st.DefaultTeamID == 0 {
		_ = s.store.SetUserSetting(ctx, me(r), "default_team_id", strconv.FormatInt(t.ID, 10))
	}
	if t.CyclesEnabled {
		_, _ = s.store.PlanCycles(ctx, t, s.boxToday())
	}
	t, _ = s.store.GetTeam(ctx, t.ID)
	t.Role = "admin"
	s.fillTeam(ctx, t)
	s.changed(r, "team", []int64{t.ID}, t.ID)
	writeJSON(w, http.StatusCreated, t)
}

// suggestKey derives a free team key from a name ("Home renovation" → "HR").
func (s *Server) suggestKey(ctx context.Context, name string) string {
	var initials, letters []rune
	for _, w := range strings.Fields(strings.ToUpper(name)) {
		for i, r := range w {
			if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
				if i == 0 || len(initials) == 0 {
					initials = append(initials, r)
				}
				letters = append(letters, r)
			}
			break
		}
	}
	for _, r := range strings.ToUpper(name) {
		if r >= 'A' && r <= 'Z' {
			letters = append(letters, r)
		}
	}
	base := string(initials)
	if len(base) < 2 {
		base = string(letters)
		if len(base) > 3 {
			base = base[:3]
		}
	}
	if len(base) > 5 {
		base = base[:5]
	}
	if base == "" || base[0] < 'A' || base[0] > 'Z' {
		base = "T" + base
	}
	key := base
	for i := 2; i < 100; i++ {
		if _, err := s.store.GetTeamByKey(ctx, key); err != nil {
			return key
		}
		key = base + strconv.Itoa(i)
	}
	return "T" + strings.ToUpper(store.RandomToken(2))
}

func (s *Server) handleGetTeam(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	t, err := s.loadTeam(r, a, chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	s.fillTeam(r.Context(), t)
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handlePatchTeam(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in teamInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a := s.accessOf(r)
	t, err := s.loadTeam(r, a, chi.URLParam(r, "ref"), needAdmin)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	oldKey := t.Key
	if err := in.apply(t); err != nil {
		s.fail(w, err, "team")
		return
	}
	if in.DefaultStateID != nil {
		if *in.DefaultStateID == 0 {
			t.DefaultStateID = nil
		} else if st, err := s.store.GetState(ctx, *in.DefaultStateID); err == nil && st.TeamID == t.ID {
			t.DefaultStateID = &st.ID
		} else {
			writeErr(w, http.StatusBadRequest, "unknown state")
			return
		}
	}
	if in.DefaultAssignee != nil {
		v := strings.TrimSpace(*in.DefaultAssignee)
		switch v {
		case "creator", "none":
		default:
			id, err := strconv.ParseInt(v, 10, 64)
			if err != nil || id <= 0 || s.store.TeamRole(ctx, t.ID, id) == "" {
				writeErr(w, http.StatusBadRequest, "the default assignee is creator, none or a member of the team")
				return
			}
		}
		t.DefaultAssignee = v
	}
	if err := s.store.UpdateTeam(ctx, t); err != nil {
		if err == store.ErrConflict {
			writeErr(w, http.StatusConflict, "another team already uses the key "+t.Key)
			return
		}
		s.fail(w, badRequest(err.Error()), "team")
		return
	}
	if !strings.EqualFold(oldKey, t.Key) {
		_ = s.store.RecordKeyAliases(ctx, t.ID, oldKey)
		// Re-index identifiers.
		list, _, _ := s.store.ListIssues(ctx, store.IssueFilter{TeamIDs: []int64{t.ID}, AllowedTeamIDs: []int64{t.ID}, Archived: "include", Limit: 5000, Snoozed: "include"})
		for _, is := range list {
			_, _ = s.store.DB.ExecContext(ctx, `UPDATE issues_fts SET ident = ? WHERE rowid = ?`, is.Identifier, is.ID)
		}
	}
	if t.CyclesEnabled {
		_, _ = s.store.PlanCycles(ctx, t, s.boxToday())
	}
	s.fillTeam(ctx, t)
	s.changed(r, "team", []int64{t.ID}, t.ID)
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleDeleteTeam(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	t, err := s.loadTeam(r, a, chi.URLParam(r, "ref"), needAdmin)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	if !strings.EqualFold(r.URL.Query().Get("confirm"), t.Key) {
		writeErr(w, http.StatusBadRequest, "confirm with ?confirm="+t.Key+" — this deletes every issue of the team")
		return
	}
	members := s.store.TeamMemberIDs(r.Context(), t.ID)
	var issueIDs []int64
	if rows, err := s.store.DB.QueryContext(r.Context(), `SELECT id FROM issues WHERE team_id = ?`, t.ID); err == nil {
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil {
				issueIDs = append(issueIDs, id)
			}
		}
		rows.Close()
	}
	if err := s.store.DeleteTeam(r.Context(), t.ID); err != nil {
		s.fail(w, err, "delete team")
		return
	}
	for _, id := range issueIDs {
		_ = os.RemoveAll(filepath.Join(s.cfg.DataDir, "files", strconv.FormatInt(id, 10)))
	}
	s.send(r.Context(), change{Kind: "team", IDs: []int64{t.ID}, users: members, ActorID: me(r)})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleLeaveTeam(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	t, err := s.loadTeam(r, a, chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	if t.Role == "admin" && s.store.CountTeamAdmins(r.Context(), t.ID) <= 1 && len(s.store.TeamMemberIDs(r.Context(), t.ID)) > 1 {
		writeErr(w, http.StatusBadRequest, "make someone else an admin first")
		return
	}
	if err := s.store.RemoveTeamMember(r.Context(), t.ID, me(r)); err != nil {
		s.fail(w, err, "leave")
		return
	}
	s.send(r.Context(), change{Kind: "team", TeamIDs: []int64{t.ID}, users: []int64{me(r)}, IDs: []int64{t.ID}})
	w.WriteHeader(http.StatusNoContent)
}

// ---------- members ----------

func (s *Server) handleTeamMembers(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	t, err := s.loadTeam(r, a, chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	list, err := s.store.TeamMembers(r.Context(), t.ID)
	if err != nil {
		s.fail(w, err, "members")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleAddMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		UserID int64  `json:"user_id"`
		Email  string `json:"email"`
		Role   string `json:"role"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a := s.accessOf(r)
	t, err := s.loadTeam(r, a, chi.URLParam(r, "ref"), needAdmin)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	u, err := s.personFor(ctx, userFrom(ctx), in.UserID, in.Email)
	if err != nil {
		s.fail(w, err, "member")
		return
	}
	role := in.Role
	if roleRank(role) == 0 {
		role = "member"
	}
	was := s.store.TeamRole(ctx, t.ID, u.ID)
	if err := s.store.SetTeamMember(ctx, t.ID, u.ID, role); err != nil {
		s.fail(w, err, "member")
		return
	}
	if was == "" {
		s.notifyAdded(ctx, s.actorOf(r), u.ID, t)
	}
	s.changed(r, "team", []int64{t.ID}, t.ID)
	s.handleTeamMembers(w, r)
}

func (s *Server) notifyAdded(ctx context.Context, who actor, uid int64, t *store.Team) {
	s.notifyPerson(ctx, store.Notification{UserID: uid, Kind: "added", TeamID: &t.ID, ActorID: who.idPtr(),
		Title: who.name + " added you to the team " + t.Name, Body: "Issues of " + t.Name + " start with " + t.Key + "-."}, who.base+"/team/"+t.Key)
}

func (s *Server) handleSetMemberRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		Role string `json:"role"`
	}
	if err := decode(r, &in); err != nil || roleRank(in.Role) == 0 {
		writeErr(w, http.StatusBadRequest, "role is admin, member or guest")
		return
	}
	a := s.accessOf(r)
	t, err := s.loadTeam(r, a, chi.URLParam(r, "ref"), needAdmin)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	uid := idParam(r, "uid")
	cur := s.store.TeamRole(ctx, t.ID, uid)
	if cur == "" {
		writeErr(w, http.StatusNotFound, "not on the team")
		return
	}
	if cur == "admin" && in.Role != "admin" && s.store.CountTeamAdmins(ctx, t.ID) <= 1 {
		writeErr(w, http.StatusBadRequest, "a team keeps at least one admin")
		return
	}
	if err := s.store.SetTeamMember(ctx, t.ID, uid, in.Role); err != nil {
		s.fail(w, err, "member")
		return
	}
	s.changed(r, "team", []int64{t.ID}, t.ID)
	s.handleTeamMembers(w, r)
}

func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := s.accessOf(r)
	t, err := s.loadTeam(r, a, chi.URLParam(r, "ref"), needAdmin)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	uid := idParam(r, "uid")
	if s.store.TeamRole(ctx, t.ID, uid) == "admin" && s.store.CountTeamAdmins(ctx, t.ID) <= 1 {
		writeErr(w, http.StatusBadRequest, "a team keeps at least one admin")
		return
	}
	if err := s.store.RemoveTeamMember(ctx, t.ID, uid); err != nil {
		s.fail(w, err, "member")
		return
	}
	// Their open issues on the team become unassigned.
	_, _ = s.store.DB.ExecContext(ctx, `UPDATE issues SET assignee_id = NULL, updated_at = ? WHERE team_id = ? AND assignee_id = ? AND completed_at IS NULL AND canceled_at IS NULL`, store.Now(), t.ID, uid)
	s.send(ctx, change{Kind: "team", TeamIDs: []int64{t.ID}, users: []int64{uid}, IDs: []int64{t.ID}, ActorID: me(r)})
	s.handleTeamMembers(w, r)
}

// ---------- people ----------

type corePerson struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

var peopleCache struct {
	sync.Mutex
	list []corePerson
	at   time.Time
}

// corePeople asks Core for the household (cached for a minute).
func (s *Server) corePeople(ctx context.Context) []corePerson {
	if !s.core.Configured() || s.core.Token() == "" {
		return nil
	}
	peopleCache.Lock()
	defer peopleCache.Unlock()
	if time.Since(peopleCache.at) < time.Minute {
		return peopleCache.list
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, s.core.BaseURL()+"/api/v1/people", nil)
	if err != nil {
		return peopleCache.list
	}
	req.Header.Set("Authorization", "Bearer "+s.core.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return peopleCache.list
	}
	defer res.Body.Close()
	var list []corePerson
	if res.StatusCode != http.StatusOK || json.NewDecoder(res.Body).Decode(&list) != nil {
		return peopleCache.list
	}
	for i := range list {
		list[i].Email = strings.ToLower(strings.TrimSpace(list[i].Email))
	}
	peopleCache.list, peopleCache.at = list, time.Now()
	return list
}

// handlePeople lists who can be added to a team: everyone with an account here
// plus the Pocket Core household (id 0 until they have an account).
func (s *Server) handlePeople(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out, err := s.store.People(ctx)
	if err != nil {
		s.fail(w, err, "people")
		return
	}
	known := map[string]bool{}
	for _, p := range out {
		known[strings.ToLower(p.Email)] = true
	}
	for _, p := range s.corePeople(ctx) {
		if p.Email != "" && !known[p.Email] {
			known[p.Email] = true
			out = append(out, store.Person{Name: p.Name, Email: p.Email})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(firstNonEmpty(out[i].Name, out[i].Email)) < strings.ToLower(firstNonEmpty(out[j].Name, out[j].Email))
	})
	writeJSON(w, http.StatusOK, out)
}

// personFor finds the account to add by id or email — making a member account
// for someone Core knows (or, for a box admin, anyone) who has not been here yet.
func (s *Server) personFor(ctx context.Context, by *store.User, id int64, email string) (*store.User, error) {
	if id > 0 {
		u, err := s.store.GetUser(ctx, id)
		if err != nil {
			return nil, badRequest("unknown person")
		}
		return u, nil
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return nil, badRequest("give a person or an email address")
	}
	if u, err := s.store.GetUserByEmail(ctx, email); err == nil {
		return u, nil
	}
	name := ""
	known := false
	for _, p := range s.corePeople(ctx) {
		if p.Email == email {
			known, name = true, p.Name
		}
	}
	if !known && (by == nil || by.Role != "admin") {
		return nil, badRequest(email + " has no account here yet; an admin can invite them, or add them to Pocket Core first")
	}
	u := &store.User{Email: email, Name: name, Role: "member"}
	if err := s.store.CreateUser(ctx, u); err != nil {
		return nil, err
	}
	s.log.Info("account made for an invitation", "email", email)
	return u, nil
}

// ---------- workflow states ----------

func (s *Server) handleTeamStates(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	t, err := s.loadTeam(r, a, chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	list, err := s.store.TeamStates(r.Context(), t.ID)
	if err != nil {
		s.fail(w, err, "states")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type stateInput struct {
	Name        *string  `json:"name"`
	Type        *string  `json:"type"`
	Color       *string  `json:"color"`
	Description *string  `json:"description"`
	Position    *float64 `json:"position"`
}

func (in stateInput) apply(st *store.State) error {
	if in.Name != nil {
		st.Name = strings.TrimSpace(*in.Name)
	}
	if st.Name == "" {
		return badRequest("a state needs a name")
	}
	if in.Type != nil {
		if !store.ValidStateType(*in.Type) {
			return badRequest("type is triage, backlog, unstarted, started, completed or canceled")
		}
		st.Type = *in.Type
	}
	if st.Type == "" {
		st.Type = "unstarted"
	}
	if in.Color != nil {
		st.Color = *in.Color
	}
	if in.Description != nil {
		st.Description = *in.Description
	}
	if in.Position != nil {
		st.Position = *in.Position
	}
	return nil
}

func (s *Server) handleCreateState(w http.ResponseWriter, r *http.Request) {
	var in stateInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a := s.accessOf(r)
	t, err := s.loadTeam(r, a, chi.URLParam(r, "ref"), needAdmin)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	st := &store.State{TeamID: t.ID}
	if err := in.apply(st); err != nil {
		s.fail(w, err, "state")
		return
	}
	if err := s.store.SaveState(r.Context(), st); err != nil {
		s.fail(w, err, "state")
		return
	}
	s.changed(r, "team", []int64{t.ID}, t.ID)
	writeJSON(w, http.StatusCreated, st)
}

func (s *Server) stateFor(r *http.Request, need int) (*store.State, error) {
	st, err := s.store.GetState(r.Context(), idParam(r, "id"))
	if err != nil {
		return nil, err
	}
	if err := s.accessOf(r).check(st.TeamID, need); err != nil {
		return nil, err
	}
	return st, nil
}

func (s *Server) handlePatchState(w http.ResponseWriter, r *http.Request) {
	var in stateInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	st, err := s.stateFor(r, needAdmin)
	if err != nil {
		s.fail(w, err, "state")
		return
	}
	oldType := st.Type
	if err := in.apply(st); err != nil {
		s.fail(w, err, "state")
		return
	}
	if oldType != st.Type && (oldType == "triage" || st.Type == "triage") {
		writeErr(w, http.StatusBadRequest, "switch Triage on or off in the team settings instead")
		return
	}
	if err := s.store.SaveState(r.Context(), st); err != nil {
		s.fail(w, err, "state")
		return
	}
	s.changed(r, "team", []int64{st.TeamID}, st.TeamID)
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleDeleteState(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, err := s.stateFor(r, needAdmin)
	if err != nil {
		s.fail(w, err, "state")
		return
	}
	states, _ := s.store.TeamStates(ctx, st.TeamID)
	moveTo := queryInt64(r, "move_to")
	var target *store.State
	for i, x := range states {
		if x.ID == moveTo && x.ID != st.ID {
			target = &states[i]
		}
	}
	if target == nil {
		// The first other state of the same type, else of any type.
		for i, x := range states {
			if x.ID != st.ID && x.Type == st.Type {
				target = &states[i]
				break
			}
		}
	}
	if target == nil {
		writeErr(w, http.StatusBadRequest, "a team needs at least one state of each kind it uses; add another "+st.Type+" state first")
		return
	}
	if err := s.store.DeleteState(ctx, st.ID, target.ID); err != nil {
		s.fail(w, err, "state")
		return
	}
	s.changed(r, "team", []int64{st.TeamID}, st.TeamID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleReorderStates(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a := s.accessOf(r)
	t, err := s.loadTeam(r, a, chi.URLParam(r, "ref"), needAdmin)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	for i, id := range in.IDs {
		if st, err := s.store.GetState(r.Context(), id); err == nil && st.TeamID == t.ID {
			st.Position = float64(i + 1)
			_ = s.store.SaveState(r.Context(), st)
		}
	}
	s.changed(r, "team", []int64{t.ID}, t.ID)
	s.handleTeamStates(w, r)
}

// ---------- labels ----------

func (s *Server) handleTeamLabels(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	t, err := s.loadTeam(r, a, chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	list, err := s.store.TeamLabels(r.Context(), t.ID)
	if err != nil {
		s.fail(w, err, "labels")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type labelInput struct {
	Name        *string  `json:"name"`
	Color       *string  `json:"color"`
	Description *string  `json:"description"`
	ParentID    *int64   `json:"parent_id"`
	IsGroup     *bool    `json:"is_group"`
	Position    *float64 `json:"position"`
}

func (s *Server) applyLabel(ctx context.Context, l *store.Label, in labelInput) error {
	if in.Name != nil {
		l.Name = strings.TrimSpace(*in.Name)
	}
	if l.Name == "" {
		return badRequest("a label needs a name")
	}
	if in.Color != nil {
		l.Color = *in.Color
	}
	if in.Description != nil {
		l.Description = *in.Description
	}
	if in.IsGroup != nil {
		l.IsGroup = *in.IsGroup
	}
	if in.Position != nil {
		l.Position = *in.Position
	}
	if in.ParentID != nil {
		if *in.ParentID == 0 {
			l.ParentID = nil
		} else {
			p, err := s.store.GetLabel(ctx, *in.ParentID)
			if err != nil || p.TeamID != l.TeamID || !p.IsGroup || p.ID == l.ID {
				return badRequest("the parent must be a label group of the same team")
			}
			l.ParentID = &p.ID
		}
	}
	if l.IsGroup && l.ParentID != nil {
		return badRequest("groups can't be inside groups")
	}
	return nil
}

func (s *Server) handleCreateLabel(w http.ResponseWriter, r *http.Request) {
	var in labelInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a := s.accessOf(r)
	t, err := s.loadTeam(r, a, chi.URLParam(r, "ref"), needWrite)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	l := &store.Label{TeamID: t.ID, Color: "#94a3b8"}
	if err := s.applyLabel(r.Context(), l, in); err != nil {
		s.fail(w, err, "label")
		return
	}
	if err := s.store.SaveLabel(r.Context(), l); err != nil {
		s.fail(w, err, "label")
		return
	}
	s.changed(r, "team", []int64{t.ID}, t.ID)
	writeJSON(w, http.StatusCreated, l)
}

func (s *Server) handlePatchLabel(w http.ResponseWriter, r *http.Request) {
	var in labelInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	l, err := s.store.GetLabel(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "label")
		return
	}
	if err := s.accessOf(r).check(l.TeamID, needWrite); err != nil {
		s.fail(w, err, "label")
		return
	}
	if err := s.applyLabel(r.Context(), l, in); err != nil {
		s.fail(w, err, "label")
		return
	}
	if err := s.store.SaveLabel(r.Context(), l); err != nil {
		s.fail(w, err, "label")
		return
	}
	s.changed(r, "team", []int64{l.TeamID}, l.TeamID)
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) handleDeleteLabel(w http.ResponseWriter, r *http.Request) {
	l, err := s.store.GetLabel(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "label")
		return
	}
	if err := s.accessOf(r).check(l.TeamID, needAdmin); err != nil {
		s.fail(w, err, "label")
		return
	}
	if err := s.store.DeleteLabel(r.Context(), l.ID); err != nil {
		s.fail(w, err, "label")
		return
	}
	s.changed(r, "team", []int64{l.TeamID}, l.TeamID)
	w.WriteHeader(http.StatusNoContent)
}

// ---------- templates ----------

func (s *Server) handleTeamTemplates(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	t, err := s.loadTeam(r, a, chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	list, err := s.store.Templates(r.Context(), []int64{t.ID})
	if err != nil {
		s.fail(w, err, "templates")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type templateInput struct {
	Name          *string             `json:"name"`
	Title         *string             `json:"title"`
	Description   *string             `json:"description"`
	Data          *store.TemplateData `json:"data"`
	Recur         *string             `json:"recur"`
	RecurInterval *int                `json:"recur_interval"`
	NextAt        *string             `json:"next_at"`
}

func (s *Server) applyTemplate(ctx context.Context, t *store.Template, in templateInput) error {
	if in.Name != nil {
		t.Name = strings.TrimSpace(*in.Name)
	}
	if t.Name == "" {
		return badRequest("a template needs a name")
	}
	if in.Title != nil {
		t.Title = *in.Title
	}
	if in.Description != nil {
		t.Description = *in.Description
	}
	if in.Data != nil {
		d := *in.Data
		if d.StateID != nil {
			if st, err := s.store.GetState(ctx, *d.StateID); err != nil || st.TeamID != t.TeamID {
				return badRequest("the state belongs to another team")
			}
		}
		if d.Priority != nil && (*d.Priority < 0 || *d.Priority > 4) {
			return badRequest("priority is 0–4")
		}
		clean, err := s.cleanLabels(ctx, t.TeamID, d.LabelIDs)
		if err != nil {
			return err
		}
		d.LabelIDs = clean
		b, _ := json.Marshal(d)
		t.Data = b
	}
	if in.Recur != nil {
		switch *in.Recur {
		case "", "daily", "weekly", "monthly", "yearly", "weekdays":
			t.Recur = *in.Recur
		default:
			return badRequest("recur is daily, weekdays, weekly, monthly or yearly")
		}
	}
	if in.RecurInterval != nil {
		t.RecurInterval = max(1, min(*in.RecurInterval, 365))
	}
	if in.NextAt != nil {
		if *in.NextAt != "" {
			if _, err := time.Parse("2006-01-02", *in.NextAt); err != nil {
				return badRequest("next_at looks like 2026-11-01")
			}
		}
		t.NextAt = *in.NextAt
	}
	if t.Recur != "" && t.NextAt == "" {
		t.NextAt = s.boxToday().Format("2006-01-02")
	}
	if t.Recur != "" && strings.TrimSpace(t.Title) == "" {
		return badRequest("a repeating template needs an issue title")
	}
	return nil
}

func (s *Server) handleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	var in templateInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a := s.accessOf(r)
	team, err := s.loadTeam(r, a, chi.URLParam(r, "ref"), needWrite)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	t := &store.Template{TeamID: team.ID, CreatedBy: ptr(me(r))}
	if err := s.applyTemplate(r.Context(), t, in); err != nil {
		s.fail(w, err, "template")
		return
	}
	if err := s.store.SaveTemplate(r.Context(), t); err != nil {
		s.fail(w, err, "template")
		return
	}
	s.changed(r, "team", []int64{team.ID}, team.ID)
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) handlePatchTemplate(w http.ResponseWriter, r *http.Request) {
	var in templateInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	t, err := s.store.GetTemplate(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "template")
		return
	}
	if err := s.accessOf(r).check(t.TeamID, needWrite); err != nil {
		s.fail(w, err, "template")
		return
	}
	if err := s.applyTemplate(r.Context(), t, in); err != nil {
		s.fail(w, err, "template")
		return
	}
	if err := s.store.SaveTemplate(r.Context(), t); err != nil {
		s.fail(w, err, "template")
		return
	}
	s.changed(r, "team", []int64{t.TeamID}, t.TeamID)
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.GetTemplate(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "template")
		return
	}
	if err := s.accessOf(r).check(t.TeamID, needWrite); err != nil {
		s.fail(w, err, "template")
		return
	}
	if err := s.store.DeleteTemplate(r.Context(), t.ID); err != nil {
		s.fail(w, err, "template")
		return
	}
	s.changed(r, "team", []int64{t.TeamID}, t.TeamID)
	w.WriteHeader(http.StatusNoContent)
}

// ---------- webhooks ----------

func (s *Server) handleTeamWebhooks(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	t, err := s.loadTeam(r, a, chi.URLParam(r, "ref"), needAdmin)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	list, err := s.store.Webhooks(r.Context(), t.ID)
	if err != nil {
		s.fail(w, err, "webhooks")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type webhookInput struct {
	Name   *string `json:"name"`
	URL    *string `json:"url"`
	Secret *string `json:"secret"`
	Events *string `json:"events"`
	Active *bool   `json:"active"`
}

func (in webhookInput) apply(h *store.Webhook) error {
	if in.Name != nil {
		h.Name = strings.TrimSpace(*in.Name)
	}
	if in.URL != nil {
		u, err := url.Parse(strings.TrimSpace(*in.URL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return badRequest("the webhook needs an http(s) address")
		}
		h.URL = u.String()
	}
	if h.URL == "" {
		return badRequest("the webhook needs an address")
	}
	if in.Secret != nil {
		h.Secret = *in.Secret
	}
	if in.Events != nil {
		h.Events = strings.TrimSpace(*in.Events)
	}
	if h.Events == "" {
		h.Events = "*"
	}
	if in.Active != nil {
		h.Active = *in.Active
	}
	return nil
}

func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	var in webhookInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a := s.accessOf(r)
	t, err := s.loadTeam(r, a, chi.URLParam(r, "ref"), needAdmin)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	h := &store.Webhook{TeamID: t.ID, Active: true}
	if err := in.apply(h); err != nil {
		s.fail(w, err, "webhook")
		return
	}
	if err := s.store.SaveWebhook(r.Context(), h, me(r)); err != nil {
		s.fail(w, err, "webhook")
		return
	}
	h.HasSecret = h.Secret != ""
	writeJSON(w, http.StatusCreated, h)
}

func (s *Server) webhookFor(r *http.Request) (*store.Webhook, error) {
	h, err := s.store.GetWebhook(r.Context(), idParam(r, "id"))
	if err != nil {
		return nil, err
	}
	if err := s.accessOf(r).check(h.TeamID, needAdmin); err != nil {
		return nil, err
	}
	return h, nil
}

func (s *Server) handlePatchWebhook(w http.ResponseWriter, r *http.Request) {
	var in webhookInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	h, err := s.webhookFor(r)
	if err != nil {
		s.fail(w, err, "webhook")
		return
	}
	if err := in.apply(h); err != nil {
		s.fail(w, err, "webhook")
		return
	}
	if err := s.store.SaveWebhook(r.Context(), h, me(r)); err != nil {
		s.fail(w, err, "webhook")
		return
	}
	h.HasSecret = h.Secret != ""
	writeJSON(w, http.StatusOK, h)
}

func (s *Server) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	h, err := s.webhookFor(r)
	if err != nil {
		s.fail(w, err, "webhook")
		return
	}
	if err := s.store.DeleteWebhook(r.Context(), h.ID); err != nil {
		s.fail(w, err, "webhook")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTestWebhook(w http.ResponseWriter, r *http.Request) {
	h, err := s.webhookFor(r)
	if err != nil {
		s.fail(w, err, "webhook")
		return
	}
	status, err := s.deliverWebhook(r.Context(), *h, "ping", map[string]any{"team_id": h.TeamID, "message": "Pocket Projects says hello"})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "status": status, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": status})
}

// ---------- incoming git webhooks ----------

func (s *Server) handleTeamGit(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	t, err := s.loadTeam(r, a, chi.URLParam(r, "ref"), needAdmin)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": s.publicURL(r) + "/hooks/git/" + t.GitSecret,
		"help": fmt.Sprintf("Add this address as a webhook (JSON, push and pull request events) in Forgejo or GitHub. Commits and pull requests that mention %s-123 are linked; \"fixes %s-123\" completes the issue when merged.", t.Key, t.Key)})
}

func (s *Server) handleRotateGit(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	t, err := s.loadTeam(r, a, chi.URLParam(r, "ref"), needAdmin)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	if _, err := s.store.RotateGitSecret(r.Context(), t.ID); err != nil {
		s.fail(w, err, "rotate")
		return
	}
	s.handleTeamGit(w, r)
}
