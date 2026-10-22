package server

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// The inbox: a notification per person and event, also pushed through the
// person's Apprise / ntfy / webhook route for the kinds they chose.

func (s *Server) inboxRoutes(r chi.Router) {
	r.Get("/notifications", s.handleNotifications)
	r.Get("/notifications/count", s.handleUnreadCount)
	r.Post("/notifications/read", s.handleMarkRead)
	r.Post("/notifications/archive", s.handleArchive)
	r.Post("/notifications/{id}/snooze", s.handleSnooze)
}

// notifyPerson puts an entry in someone's inbox (never the actor's own) and pushes it when they want that kind.
func (s *Server) notifyPerson(ctx context.Context, n store.Notification, link string) {
	if n.UserID <= 0 || (n.ActorID != nil && *n.ActorID == n.UserID) {
		return
	}
	if err := s.store.AddNotification(ctx, &n); err != nil {
		s.log.Warn("notification", "user", n.UserID, "err", err)
		return
	}
	s.send(ctx, change{Kind: "notification", users: []int64{n.UserID}})
	st, err := s.store.GetSettings(ctx, n.UserID)
	if err != nil || !st.Push[n.Kind] {
		return
	}
	kind, url, token := s.notifyRoute(ctx, n.UserID, st)
	if url == "" {
		return
	}
	title, body := n.Title, n.Body
	go func() {
		c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := notify.Send(c, kind, url, token, title, body, link); err != nil {
			s.log.Warn("push failed", "user", n.UserID, "kind", n.Kind, "err", err)
		}
	}()
}

// mentionRe finds @name in Markdown (not in e-mail addresses).
var mentionRe = regexp.MustCompile(`(?:^|[^\w@.])@([A-Za-z0-9][A-Za-z0-9._-]{0,39})`)

// mentionedPeople lists the team members named with @ in text.
func (s *Server) mentionedPeople(ctx context.Context, teamIDs []int64, text string) []int64 {
	names := map[string]bool{}
	for _, m := range mentionRe.FindAllStringSubmatch(text, -1) {
		names[strings.ToLower(strings.TrimRight(m[1], "._-"))] = true
	}
	if len(names) == 0 {
		return nil
	}
	seen := map[int64]bool{}
	var out []int64
	for _, t := range teamIDs {
		members, _ := s.store.TeamMembers(ctx, t)
		for _, m := range members {
			local := strings.ToLower(strings.Split(m.Email, "@")[0])
			if (names[strings.ToLower(m.Username)] || names[local]) && !seen[m.ID] {
				seen[m.ID] = true
				out = append(out, m.ID)
			}
		}
	}
	return out
}

func newMentions(ctx context.Context, s *Server, teamIDs []int64, before, after string) []int64 {
	old := map[int64]bool{}
	for _, id := range s.mentionedPeople(ctx, teamIDs, before) {
		old[id] = true
	}
	var out []int64
	for _, id := range s.mentionedPeople(ctx, teamIDs, after) {
		if !old[id] {
			out = append(out, id)
		}
	}
	return out
}

func (s *Server) actorName(ctx context.Context, r *http.Request) string {
	if app := s.linkApp(r); app != "" {
		return "Pocket " + strings.ToUpper(app[:1]) + app[1:]
	}
	if u := userFrom(ctx); u != nil {
		return firstNonEmpty(u.Name, u.Email)
	}
	return "Someone"
}

func excerpt(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

func ptr[T any](v T) *T { return &v }

// ---------- handlers ----------

func (s *Server) handleNotifications(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := s.store.Notifications(ctx, me(r), r.URL.Query().Get("filter"), queryInt(r, "limit"))
	if err != nil {
		s.fail(w, err, "notifications")
		return
	}
	a := s.accessOf(r)
	var issueIDs, projectIDs []int64
	for _, n := range list {
		if n.IssueID != nil {
			issueIDs = append(issueIDs, *n.IssueID)
		}
		if n.ProjectID != nil {
			projectIDs = append(projectIDs, *n.ProjectID)
		}
	}
	issues, _ := s.store.IssueRefs(ctx, issueIDs)
	projects, _ := s.store.ProjectRefs(ctx, projectIDs)
	out := []store.Notification{}
	for _, n := range list {
		if n.IssueID != nil {
			ref, ok := issues[*n.IssueID]
			if !ok || a.teams[ref.TeamID] == "" {
				continue // left the team or the issue is gone
			}
			n.Issue = &ref
		}
		if n.ProjectID != nil {
			if ref, ok := projects[*n.ProjectID]; ok && s.visibleProject(ctx, a, ref.ID) {
				n.Project = &ref
			} else if n.IssueID == nil {
				continue
			}
		}
		out = append(out, n)
	}
	writeJSON(w, http.StatusOK, map[string]any{"notifications": out, "unread": s.store.UnreadCount(ctx, me(r))})
}

func (s *Server) handleUnreadCount(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]int{"unread": s.store.UnreadCount(r.Context(), me(r))})
}

func (s *Server) handleMarkRead(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs  []int64 `json:"ids"`
		All  bool    `json:"all"`
		Read *bool   `json:"read"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	read := in.Read == nil || *in.Read
	if err := s.store.MarkNotifications(r.Context(), me(r), in.IDs, in.All, read); err != nil {
		s.fail(w, err, "mark read")
		return
	}
	s.send(r.Context(), change{Kind: "notification", users: []int64{me(r)}})
	s.handleUnreadCount(w, r)
}

func (s *Server) handleArchive(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs  []int64 `json:"ids"`
		Read bool    `json:"read"` // archive every read entry
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.store.ArchiveNotifications(r.Context(), me(r), in.IDs, in.Read); err != nil {
		s.fail(w, err, "archive")
		return
	}
	s.send(r.Context(), change{Kind: "notification", users: []int64{me(r)}})
	s.handleUnreadCount(w, r)
}

func (s *Server) handleSnooze(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Until string `json:"until"` // RFC 3339, "" = wake up now
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	until := ""
	if in.Until != "" {
		t, err := time.Parse(time.RFC3339, in.Until)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "until looks like 2026-10-23T09:00:00+02:00")
			return
		}
		until = t.UTC().Format(time.RFC3339)
	}
	if err := s.store.SnoozeNotification(r.Context(), me(r), idParam(r, "id"), until); err != nil {
		s.fail(w, err, "snooze")
		return
	}
	s.send(r.Context(), change{Kind: "notification", users: []int64{me(r)}})
	s.handleUnreadCount(w, r)
}
