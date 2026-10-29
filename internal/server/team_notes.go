package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
)

// A team's folder in Pocket Notes. Notes are each person's own, so the
// folder is a name every member has in their own notes: notes started from
// the team's issues go there, the team's Notes page lists it, and Pocket AI
// conversations about its issues put notes there.

const defaultNotesFolder = "Projects"

// teamNotesFolder is where notes about the team's issues go.
func (s *Server) teamNotesFolder(ctx context.Context, teamID int64) string {
	return firstNonEmpty(s.store.TeamNotesFolder(ctx, teamID), defaultNotesFolder)
}

// cleanNotesFolder tidies a folder path the way Pocket Notes writes them.
func cleanNotesFolder(v string) string {
	parts := []string{}
	for _, p := range strings.Split(strings.ReplaceAll(v, "\\", "/"), "/") {
		if p = strings.TrimSpace(p); p != "" && p != "." && p != ".." {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "/")
}

// GET /teams/{ref}/notes: the folder and the person's notes in it.
func (s *Server) handleTeamNotes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var folder string
	if rem := remoteFrom(ctx); rem != nil {
		// a connected instance's team: its folder setting, the person's notes here
		var t struct {
			NotesFolder string `json:"notes_folder"`
		}
		if err := s.remoteCall(ctx, rem, http.MethodGet, "/api/v1/teams/"+url.PathEscape(chi.URLParam(r, "ref")), nil, &t); err != nil {
			s.fail(w, err, "team")
			return
		}
		folder = t.NotesFolder
	} else {
		t, err := s.loadTeam(r, s.accessOf(r), chi.URLParam(r, "ref"), needRead)
		if err != nil {
			s.fail(w, err, "team")
			return
		}
		folder = s.store.TeamNotesFolder(ctx, t.ID)
	}
	out := map[string]any{"folder": folder, "default_folder": defaultNotesFolder}
	if folder == "" {
		writeJSON(w, http.StatusOK, out)
		return
	}
	sec := s.relatedSection(ctx, userFrom(ctx), "notes", func(c context.Context, sb *sibling, sec *RelatedSection) error {
		out["folder_url"] = sb.public + "/?in=" + url.QueryEscape(folder)
		var list []struct {
			ID       flexID `json:"id"`
			Title    string `json:"title"`
			Category string `json:"category"`
			Excerpt  string `json:"excerpt"`
			Modified int64  `json:"modified"`
			Pinned   bool   `json:"pinned"`
		}
		if err := sb.call(c, http.MethodGet, "/api/v1/notes?recursive=1&sort=modified&limit=200&category="+url.QueryEscape(folder), nil, &list); err != nil {
			return err
		}
		for _, n := range list {
			sec.Items = append(sec.Items, map[string]any{"id": string(n.ID), "title": n.Title, "folder": n.Category, "snippet": n.Excerpt,
				"modified": n.Modified, "pinned": n.Pinned, "url": sb.public + "/note/" + string(n.ID)})
		}
		return nil
	})
	out["notes"] = sec
	writeJSON(w, http.StatusOK, out)
}

// PUT /teams/{ref}/notes {folder}: team admins choose the folder ("" = none).
func (s *Server) handlePutTeamNotes(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Folder string `json:"folder"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	t, err := s.loadTeam(r, s.accessOf(r), chi.URLParam(r, "ref"), needAdmin)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	folder := cleanNotesFolder(in.Folder)
	if len(folder) > 300 {
		writeErr(w, http.StatusBadRequest, "the folder name is too long")
		return
	}
	if err := s.store.SetTeamNotesFolder(r.Context(), t.ID, folder); err != nil {
		s.fail(w, err, "team notes")
		return
	}
	s.changed(r, "team", []int64{t.ID}, t.ID)
	s.handleTeamNotes(w, r)
}

// GET /notes/folders: the person's folders in Pocket Notes, for pickers.
func (s *Server) handleNoteFolders(w http.ResponseWriter, r *http.Request) {
	app, _ := pocketApp("notes")
	sb := s.siblingAppOrFail(w, r, app.reading())
	if sb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), siblingTimeout)
	defer cancel()
	var list []struct {
		Path  string `json:"path"`
		Count int    `json:"count"`
	}
	if err := sb.call(ctx, http.MethodGet, "/api/v1/folders", nil, &list); err != nil {
		writeErr(w, http.StatusBadGateway, siblingErr("Pocket Notes", err))
		return
	}
	writeJSON(w, http.StatusOK, list)
}
