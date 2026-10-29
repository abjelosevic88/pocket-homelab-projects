package server

// Shared notes on an issue: Markdown the whole team sees and edits, stored
// here (with the team, wherever its instance runs). Written on the issue, or
// shared from a person's own Pocket Notes note as a copy — their note stays
// private, "Share again" replaces the copy.

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

func (s *Server) sharedNoteRoutes(r chi.Router) {
	r.Get("/issues/{ref}/shared-notes", s.handleSharedNotes)
	r.Post("/issues/{ref}/shared-notes", s.handleCreateSharedNote)
	r.Get("/issues/{ref}/shared-notes/{id}", s.handleGetSharedNote)
	r.Patch("/issues/{ref}/shared-notes/{id}", s.handlePatchSharedNote)
	r.Delete("/issues/{ref}/shared-notes/{id}", s.handleDeleteSharedNote)
	r.Post("/issues/{ref}/notes/{note}/share", s.handleShareNote)
}

func (s *Server) handleSharedNotes(w http.ResponseWriter, r *http.Request) {
	is, err := s.loadIssue(r, s.accessOf(r), chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	list, err := s.store.IssueDocs(r.Context(), is.ID, r.URL.Query().Get("body") == "1")
	if err != nil {
		s.fail(w, err, "shared notes")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateSharedNote(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	is, err := s.loadIssue(r, s.accessOf(r), chi.URLParam(r, "ref"), needWrite)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	uid := me(r)
	d := &store.IssueDoc{IssueID: is.ID, Title: firstNonEmpty(strings.TrimSpace(in.Title), "Untitled"), Body: in.Body, CreatedBy: &uid, UpdatedBy: &uid}
	if err := s.store.SaveIssueDoc(r.Context(), d); err != nil {
		s.fail(w, err, "shared note")
		return
	}
	_ = s.store.AddHistory(r.Context(), store.History{IssueID: &is.ID, ActorID: &uid, ActorApp: s.linkApp(r), Field: "shared_note_created", ToLabel: d.Title})
	s.changed(r, "issue", []int64{is.TeamID}, is.ID)
	writeJSON(w, http.StatusCreated, d)
}

// sharedNoteFor loads a shared note of the issue in the address.
func (s *Server) sharedNoteFor(r *http.Request, need int) (*store.IssueDoc, *store.Issue, error) {
	is, err := s.loadIssue(r, s.accessOf(r), chi.URLParam(r, "ref"), need)
	if err != nil {
		return nil, nil, err
	}
	d, err := s.store.GetIssueDoc(r.Context(), idParam(r, "id"))
	if err != nil {
		return nil, nil, err
	}
	if d.IssueID != is.ID {
		return nil, nil, store.ErrNotFound
	}
	return d, is, nil
}

// handleGetSharedNote: the note with its text, the issue it is on and
// whether the person may edit it (what its page needs).
func (s *Server) handleGetSharedNote(w http.ResponseWriter, r *http.Request) {
	d, is, err := s.sharedNoteFor(r, needRead)
	if err != nil {
		s.fail(w, err, "shared note")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		*store.IssueDoc
		Identifier string `json:"identifier"`
		IssueTitle string `json:"issue_title"`
		TeamID     int64  `json:"team_id"`
		CanEdit    bool   `json:"can_edit"`
	}{d, is.Identifier, is.Title, is.TeamID, s.accessOf(r).check(is.TeamID, needWrite) == nil})
}

// handlePatchSharedNote saves an edit. base is the version the editor
// started from: when someone else saved since, nothing is written and the
// answer is 409 with the newer note, so nobody overwrites another person's
// words without seeing them (force: true saves anyway).
func (s *Server) handlePatchSharedNote(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title *string `json:"title"`
		Body  *string `json:"body"`
		Base  string  `json:"base"`
		Force bool    `json:"force"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	d, is, err := s.sharedNoteFor(r, needWrite)
	if err != nil {
		s.fail(w, err, "shared note")
		return
	}
	if in.Base != "" && in.Base != d.UpdatedAt && !in.Force {
		name := "Someone"
		if d.UpdatedBy != nil {
			if u, err := s.store.GetUser(r.Context(), *d.UpdatedBy); err == nil {
				name = firstNonEmpty(u.Name, u.Email)
			}
		}
		writeJSON(w, http.StatusConflict, map[string]any{"error": name + " changed this note meanwhile", "note": d})
		return
	}
	if in.Title != nil {
		d.Title = firstNonEmpty(strings.TrimSpace(*in.Title), "Untitled")
	}
	if in.Body != nil {
		d.Body = *in.Body
	}
	d.UpdatedBy = ptr(me(r))
	if err := s.store.SaveIssueDoc(r.Context(), d); err != nil {
		s.fail(w, err, "shared note")
		return
	}
	s.changed(r, "issue", []int64{is.TeamID}, is.ID)
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleDeleteSharedNote(w http.ResponseWriter, r *http.Request) {
	d, is, err := s.sharedNoteFor(r, needWrite)
	if err != nil {
		s.fail(w, err, "shared note")
		return
	}
	if err := s.store.DeleteIssueDoc(r.Context(), d.ID); err != nil {
		s.fail(w, err, "shared note")
		return
	}
	_ = s.store.AddHistory(r.Context(), store.History{IssueID: &is.ID, ActorID: ptr(me(r)), ActorApp: s.linkApp(r), Field: "shared_note_removed", FromLabel: d.Title})
	s.changed(r, "issue", []int64{is.TeamID}, is.ID)
	w.WriteHeader(http.StatusNoContent)
}

// handleShareNote copies one of the person's own notes onto the issue as a
// shared note, or replaces the copy they shared from it before. The link
// back to this issue is left out of the copy (it is on the issue already).
func (s *Server) handleShareNote(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	noteID := chi.URLParam(r, "note")
	if !digitsOnly(noteID) {
		writeErr(w, http.StatusBadRequest, "not a note id")
		return
	}
	is, err := s.loadIssue(r, s.accessOf(r), chi.URLParam(r, "ref"), needWrite)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	app, _ := pocketApp("notes")
	sb := s.siblingAppOrFail(w, r, app.reading())
	if sb == nil {
		return
	}
	c, cancel := context.WithTimeout(ctx, siblingTimeout)
	defer cancel()
	var n struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := sb.call(c, http.MethodGet, "/api/v1/notes/"+noteID, nil, &n); err != nil {
		writeErr(w, http.StatusBadGateway, siblingErr("Pocket Notes", err))
		return
	}
	body, _ := unlinkFromNote(n.Content, s.issueURL(r, is.Identifier))
	body = strings.TrimSpace(stripTitleLine(body, n.Title))
	uid := me(r)
	now := store.Now()
	d, err := s.store.SharedCopy(ctx, is.ID, uid, noteID)
	replaced := err == nil
	if !replaced {
		d = &store.IssueDoc{IssueID: is.ID, SourceNote: noteID, CreatedBy: &uid}
	}
	d.Title, d.Body, d.SharedAt, d.UpdatedBy = firstNonEmpty(strings.TrimSpace(n.Title), "Untitled"), body, now, &uid
	if err := s.store.SaveIssueDoc(ctx, d); err != nil {
		s.fail(w, err, "shared note")
		return
	}
	field := "shared_note_added"
	if replaced {
		field = "shared_note_updated"
	}
	_ = s.store.AddHistory(ctx, store.History{IssueID: &is.ID, ActorID: &uid, ActorApp: s.linkApp(r), Field: field, ToLabel: d.Title})
	s.changed(r, "issue", []int64{is.TeamID}, is.ID)
	writeJSON(w, http.StatusOK, map[string]any{"note": d, "replaced": replaced})
}

// stripTitleLine drops a leading "# Title" heading that repeats the note's
// title (Pocket Notes keeps one in the text; the shared note shows its title).
func stripTitleLine(body, title string) string {
	t := strings.TrimLeft(body, "\n")
	first, rest, _ := strings.Cut(t, "\n")
	if h := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(first), "# ")); strings.HasPrefix(strings.TrimSpace(first), "# ") && strings.EqualFold(h, strings.TrimSpace(title)) {
		return rest
	}
	return body
}
