package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

func (s *Server) viewRoutes(r chi.Router) {
	r.Get("/views", s.handleListViews)
	r.Post("/views", s.handleCreateView)
	r.Get("/views/{id}", s.handleGetView)
	r.Patch("/views/{id}", s.handlePatchView)
	r.Delete("/views/{id}", s.handleDeleteView)
	r.Get("/favorites", s.handleFavorites)
	r.Post("/favorites", s.handleAddFavorite)
	r.Post("/favorites/reorder", s.handleReorderFavorites)
	r.Delete("/favorites/{id}", s.handleDeleteFavorite)
}

func (s *Server) handleListViews(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	list, err := s.store.Views(r.Context(), a.uid, a.ids())
	if err != nil {
		s.fail(w, err, "views")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type viewInput struct {
	Name        *string          `json:"name"`
	Description *string          `json:"description"`
	Icon        *string          `json:"icon"`
	Color       *string          `json:"color"`
	Kind        *string          `json:"kind"`
	TeamID      *int64           `json:"team_id"`
	Filters     *json.RawMessage `json:"filters"`
	Display     *json.RawMessage `json:"display"`
	Shared      *bool            `json:"shared"`
}

func (in viewInput) apply(a *access, v *store.View) error {
	if in.Name != nil {
		v.Name = strings.TrimSpace(*in.Name)
	}
	if v.Name == "" {
		return badRequest("a view needs a name")
	}
	if in.Description != nil {
		v.Description = *in.Description
	}
	if in.Icon != nil {
		v.Icon = *in.Icon
	}
	if in.Color != nil {
		v.Color = *in.Color
	}
	if in.Kind != nil && (*in.Kind == "issues" || *in.Kind == "projects") {
		v.Kind = *in.Kind
	}
	if in.TeamID != nil {
		if *in.TeamID == 0 {
			v.TeamID = nil
		} else if a.teams[*in.TeamID] == "" {
			return badRequest("unknown team")
		} else {
			v.TeamID = in.TeamID
		}
	}
	if in.Filters != nil {
		var f store.IssueFilter
		if err := json.Unmarshal(*in.Filters, &f); err != nil {
			return badRequest("filters are not a valid filter")
		}
		v.Filters = *in.Filters
	}
	if in.Display != nil {
		if !json.Valid(*in.Display) {
			return badRequest("display is not JSON")
		}
		v.Display = *in.Display
	}
	if in.Shared != nil {
		v.Shared = *in.Shared
	}
	if v.Shared && v.TeamID == nil {
		return badRequest("choose the team to share the view with")
	}
	if v.Shared && !a.can(*v.TeamID, needWrite) {
		return badRequest("guests can't share views with the team")
	}
	return nil
}

func (s *Server) handleCreateView(w http.ResponseWriter, r *http.Request) {
	var in viewInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a := s.accessOf(r)
	v := &store.View{UserID: a.uid, Kind: "issues"}
	if err := in.apply(a, v); err != nil {
		s.fail(w, err, "view")
		return
	}
	if err := s.store.SaveView(r.Context(), v); err != nil {
		s.fail(w, err, "view")
		return
	}
	s.viewChanged(r, v)
	writeJSON(w, http.StatusCreated, v)
}

func (s *Server) viewChanged(r *http.Request, v *store.View) {
	if v.Shared && v.TeamID != nil {
		s.changed(r, "view", []int64{*v.TeamID}, v.ID)
	} else {
		s.send(r.Context(), change{Kind: "view", users: []int64{v.UserID}, IDs: []int64{v.ID}})
	}
}

// viewFor loads a view the person may see; own=true for changes (a shared
// view can be changed by any member of its team).
func (s *Server) viewFor(r *http.Request, own bool) (*store.View, *access, error) {
	v, err := s.store.GetView(r.Context(), idParam(r, "id"))
	if err != nil {
		return nil, nil, err
	}
	a := s.accessOf(r)
	mine := v.UserID == a.uid
	shared := v.Shared && v.TeamID != nil && a.teams[*v.TeamID] != ""
	if !mine && !shared {
		return nil, nil, store.ErrNotFound
	}
	if own && !mine && !a.can(*v.TeamID, needWrite) {
		return nil, nil, errForbidden("guests can't change the team's views")
	}
	return v, a, nil
}

func (s *Server) handleGetView(w http.ResponseWriter, r *http.Request) {
	v, _, err := s.viewFor(r, false)
	if err != nil {
		s.fail(w, err, "view")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handlePatchView(w http.ResponseWriter, r *http.Request) {
	var in viewInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	v, a, err := s.viewFor(r, true)
	if err != nil {
		s.fail(w, err, "view")
		return
	}
	if err := in.apply(a, v); err != nil {
		s.fail(w, err, "view")
		return
	}
	if err := s.store.SaveView(r.Context(), v); err != nil {
		s.fail(w, err, "view")
		return
	}
	s.viewChanged(r, v)
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleDeleteView(w http.ResponseWriter, r *http.Request) {
	v, _, err := s.viewFor(r, true)
	if err != nil {
		s.fail(w, err, "view")
		return
	}
	if err := s.store.DeleteView(r.Context(), v.ID); err != nil {
		s.fail(w, err, "view")
		return
	}
	s.viewChanged(r, v)
	w.WriteHeader(http.StatusNoContent)
}

// ---------- favorites ----------

func (s *Server) handleFavorites(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.Favorites(r.Context(), me(r))
	if err != nil {
		s.fail(w, err, "favorites")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleAddFavorite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind  string `json:"kind"`
		RefID int64  `json:"ref_id"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ctx := r.Context()
	a := s.accessOf(r)
	ok := false
	switch in.Kind {
	case "team":
		ok = a.teams[in.RefID] != ""
	case "project":
		ok = s.visibleProject(ctx, a, in.RefID)
	case "issue":
		if is, err := s.store.GetIssue(ctx, in.RefID, a.uid); err == nil {
			ok = a.teams[is.TeamID] != ""
		}
	case "cycle":
		if c, err := s.store.GetCycle(ctx, in.RefID, ""); err == nil {
			ok = a.teams[c.TeamID] != ""
		}
	case "view":
		if v, err := s.store.GetView(ctx, in.RefID); err == nil {
			ok = v.UserID == a.uid || (v.Shared && v.TeamID != nil && a.teams[*v.TeamID] != "")
		}
	case "doc":
		if d, err := s.store.GetDoc(ctx, in.RefID); err == nil {
			ok = s.visibleProject(ctx, a, d.ProjectID)
		}
	}
	if !ok {
		writeErr(w, http.StatusBadRequest, "nothing to pin there")
		return
	}
	f, err := s.store.AddFavorite(ctx, a.uid, in.Kind, in.RefID)
	if err != nil {
		s.fail(w, err, "favorite")
		return
	}
	s.send(ctx, change{Kind: "favorite", users: []int64{a.uid}})
	writeJSON(w, http.StatusCreated, f)
}

func (s *Server) handleReorderFavorites(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.store.ReorderFavorites(r.Context(), me(r), in.IDs); err != nil {
		s.fail(w, err, "favorites")
		return
	}
	s.handleFavorites(w, r)
}

func (s *Server) handleDeleteFavorite(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteFavorite(r.Context(), me(r), idParam(r, "id")); err != nil {
		s.fail(w, err, "favorite")
		return
	}
	s.send(r.Context(), change{Kind: "favorite", users: []int64{me(r)}})
	w.WriteHeader(http.StatusNoContent)
}
