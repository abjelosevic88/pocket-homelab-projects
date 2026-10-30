package server

// A team's shared space (sidebar "Shared"): notes and drawings everyone on
// the team sees. Pocket Notes and Pocket Draw are each person's own, so what
// is shared here is a copy kept with the team:
//   - notes are written here, or copied from one of the person's notes
//     (sharing it again replaces the copy);
//   - drawings are copied from the person's Pocket Draw — the scene and its
//     picture — and follow the original: every couple of minutes the copy is
//     renewed from the sharer's Draw when its picture changed. Anyone can
//     make their own copy of it in their Pocket Draw to work on.
// On a connected instance's team, drawings come from that instance's Pocket
// Draw (like drawings in its issues: everything but sharing a note passes
// through); a note comes from the person's own Notes here and is sent there
// (…/shared/import).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// the largest drawing (scene + picture) a team space keeps
const maxSharedDrawing = 12 << 20

func init() {
	extraRoutes = append(extraRoutes, func(s *Server, r chi.Router) {
		r.Get("/teams/{ref}/shared", s.handleTeamShared)
		r.Post("/teams/{ref}/shared", s.handleCreateTeamNote)
		r.Post("/teams/{ref}/shared/drawings", s.handleShareDrawing)
		r.Post("/teams/{ref}/shared/notes", s.handleShareNoteToTeam)
		r.Post("/teams/{ref}/shared/import", s.handleImportTeamItem)
		r.Get("/teams/{ref}/shared/{id}", s.handleGetTeamItem)
		r.Patch("/teams/{ref}/shared/{id}", s.handlePatchTeamItem)
		r.Delete("/teams/{ref}/shared/{id}", s.handleDeleteTeamItem)
		r.Get("/teams/{ref}/shared/{id}/picture.svg", s.handleTeamItemPicture)
		r.Get("/teams/{ref}/shared/{id}/data", s.handleTeamItemData)
		r.Post("/teams/{ref}/shared/{id}/copy", s.handleCopyTeamDrawing)
		r.Post("/teams/{ref}/shared/{id}/refresh", s.handleRefreshTeamDrawing)
	})
}

// remoteSharedRoutes: on a connected instance's team, a note is shared from
// the person's own Pocket Notes here.
func (s *Server) remoteSharedRoutes(r chi.Router) {
	r.Post("/teams/{ref}/shared/notes", s.handleShareNoteToTeam)
}

// ---------- reading ----------

func (s *Server) handleTeamShared(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	t, err := s.loadTeam(r, a, chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	items, err := s.store.TeamItems(r.Context(), t.ID)
	if err != nil {
		s.fail(w, err, "shared space")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "can_edit": a.check(t.ID, needWrite) == nil})
}

// teamItemFor loads an item of the team in the address.
func (s *Server) teamItemFor(r *http.Request, need int) (*store.TeamItem, *store.Team, error) {
	t, err := s.loadTeam(r, s.accessOf(r), chi.URLParam(r, "ref"), need)
	if err != nil {
		return nil, nil, err
	}
	it, err := s.store.GetTeamItem(r.Context(), idParam(r, "id"))
	if err != nil {
		return nil, nil, err
	}
	if it.TeamID != t.ID {
		return nil, nil, store.ErrNotFound
	}
	return it, t, nil
}

// handleGetTeamItem: the item (a note with its text), its team and what the
// person may do with it.
func (s *Server) handleGetTeamItem(w http.ResponseWriter, r *http.Request) {
	it, t, err := s.teamItemFor(r, needRead)
	if err != nil {
		s.fail(w, err, "shared item")
		return
	}
	if it.Kind == store.ItemDrawing {
		it.Body = ""
	}
	mine := it.CreatedBy != nil && *it.CreatedBy == me(r)
	if !mine {
		it.SourceURL = "" // the original is in the sharer's own Pocket Draw
	}
	writeJSON(w, http.StatusOK, struct {
		*store.TeamItem
		TeamKey  string `json:"team_key"`
		TeamName string `json:"team_name"`
		CanEdit  bool   `json:"can_edit"`
		Mine     bool   `json:"mine"`
	}{it, t.Key, t.Name, s.accessOf(r).check(t.ID, needWrite) == nil, mine})
}

// handleTeamItemPicture is a shared drawing's picture, kept here.
func (s *Server) handleTeamItemPicture(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", drawPictureCSP)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	it, _, err := s.teamItemFor(r, needRead)
	if err != nil || it.Kind != store.ItemDrawing || it.Picture == "" {
		drawPlaceholder(w, "No picture")
		return
	}
	etag := fmt.Sprintf(`"t%d-%d"`, it.ID, it.PictureRev)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, max-age=60")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", `attachment; filename="`+safeFileName(it.Title)+`.svg"`)
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	_, _ = io.WriteString(w, it.Picture)
}

// handleTeamItemData is a shared drawing as Pocket Draw takes it (for a copy).
func (s *Server) handleTeamItemData(w http.ResponseWriter, r *http.Request) {
	it, _, err := s.teamItemFor(r, needRead)
	if err != nil {
		s.fail(w, err, "shared item")
		return
	}
	if it.Kind != store.ItemDrawing {
		writeErr(w, http.StatusBadRequest, "that is not a drawing")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"kind": it.DrawKind, "title": it.Title, "data": it.DrawData, "picture": it.Picture})
}

// ---------- notes written here ----------

func (s *Server) handleCreateTeamNote(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	t, err := s.loadTeam(r, s.accessOf(r), chi.URLParam(r, "ref"), needWrite)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	uid := me(r)
	it := &store.TeamItem{TeamID: t.ID, Kind: store.ItemNote, Title: firstNonEmpty(strings.TrimSpace(in.Title), "Untitled"), Body: in.Body, Follow: true, CreatedBy: &uid, UpdatedBy: &uid}
	if err := s.store.SaveTeamItem(r.Context(), it); err != nil {
		s.fail(w, err, "shared note")
		return
	}
	s.changed(r, "shared", []int64{t.ID}, it.ID)
	writeJSON(w, http.StatusCreated, it)
}

// handlePatchTeamItem: a note's title and text (base = the version the
// editor started from → 409 with the newer one when someone saved since,
// unless force), a drawing's title (only once it no longer follows its
// original, which renames it too) and follow (anyone who edits may stop it;
// only its sharer can start it again, from their own Pocket Draw).
func (s *Server) handlePatchTeamItem(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title  *string `json:"title"`
		Body   *string `json:"body"`
		Follow *bool   `json:"follow"`
		Base   string  `json:"base"`
		Force  bool    `json:"force"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	it, t, err := s.teamItemFor(r, needWrite)
	if err != nil {
		s.fail(w, err, "shared item")
		return
	}
	if in.Base != "" && in.Base != it.UpdatedAt && !in.Force {
		name := "Someone"
		if it.UpdatedBy != nil {
			if u, err := s.store.GetUser(r.Context(), *it.UpdatedBy); err == nil {
				name = firstNonEmpty(u.Name, u.Email)
			}
		}
		if it.Kind == store.ItemDrawing {
			it.Body = ""
		}
		writeJSON(w, http.StatusConflict, map[string]any{"error": name + " changed this meanwhile", "note": it})
		return
	}
	following := it.Kind == store.ItemDrawing && it.SourceApp == "draw" && it.Follow
	if in.Follow != nil && it.Kind == store.ItemDrawing {
		if *in.Follow && !it.Follow {
			if it.CreatedBy == nil || *it.CreatedBy != me(r) {
				writeErr(w, http.StatusForbidden, "only the person who shared it can follow their drawing again, from their own Pocket Draw")
				return
			}
			it.SourceRev = "" // take the newest one on the next round
		}
		it.Follow = *in.Follow
		following = it.Follow
	}
	if in.Title != nil {
		if following && strings.TrimSpace(*in.Title) != it.Title {
			writeErr(w, http.StatusBadRequest, "this drawing follows its original: rename it in Pocket Draw, or stop following first")
			return
		}
		it.Title = firstNonEmpty(strings.TrimSpace(*in.Title), "Untitled")
	}
	if in.Body != nil && it.Kind == store.ItemNote {
		it.Body = *in.Body
	}
	it.UpdatedBy = ptr(me(r))
	if err := s.store.SaveTeamItem(r.Context(), it); err != nil {
		s.fail(w, err, "shared item")
		return
	}
	s.changed(r, "shared", []int64{t.ID}, it.ID)
	if it.Kind == store.ItemDrawing {
		it.Body = ""
	}
	writeJSON(w, http.StatusOK, it)
}

func (s *Server) handleDeleteTeamItem(w http.ResponseWriter, r *http.Request) {
	it, t, err := s.teamItemFor(r, needWrite)
	if err != nil {
		s.fail(w, err, "shared item")
		return
	}
	if err := s.store.DeleteTeamItem(r.Context(), it.ID); err != nil {
		s.fail(w, err, "shared item")
		return
	}
	s.changed(r, "shared", []int64{t.ID}, it.ID)
	w.WriteHeader(http.StatusNoContent)
}

// ---------- sharing from the person's own apps ----------

// teamCopy is what is shared: the same for a copy made here and one sent
// from a connected instance.
type teamCopy struct {
	Kind      string `json:"kind"` // note | drawing
	SourceApp string `json:"source_app"`
	SourceID  string `json:"source_id"`
	SourceURL string `json:"source_url"`
	SourceRev string `json:"source_rev"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	DrawKind  string `json:"draw_kind"`
	DrawData  string `json:"draw_data"`
	Picture   string `json:"picture"`
	ItemID    int64  `json:"-"` // renewing that item (gone or no longer following → 404 / 409)
}

// handleShareDrawing: POST /teams/{ref}/shared/drawings {drawing} copies one
// of the person's Pocket Draw drawings into the team (again = renewed).
func (s *Server) handleShareDrawing(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Drawing flexID `json:"drawing"`
	}
	if err := decode(r, &in); err != nil || !digitsOnly(string(in.Drawing)) {
		writeErr(w, http.StatusBadRequest, "drawing (a Pocket Draw id) is required")
		return
	}
	ctx := r.Context()
	if _, err := s.loadTeam(r, s.accessOf(r), chi.URLParam(r, "ref"), needWrite); err != nil {
		s.fail(w, err, "team")
		return
	}
	c, _, err := s.pullDrawing(ctx, userFrom(ctx), string(in.Drawing), "")
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	s.answerCopy(w, r, c)
}

// handleShareNoteToTeam: POST /teams/{ref}/shared/notes {note} copies one of
// the person's Pocket Notes notes into the team (again = replaced).
func (s *Server) handleShareNoteToTeam(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Note flexID `json:"note"`
	}
	if err := decode(r, &in); err != nil || !digitsOnly(string(in.Note)) {
		writeErr(w, http.StatusBadRequest, "note (a Pocket Notes id) is required")
		return
	}
	ctx := r.Context()
	rem := remoteFrom(ctx)
	if rem == nil {
		if _, err := s.loadTeam(r, s.accessOf(r), chi.URLParam(r, "ref"), needWrite); err != nil {
			s.fail(w, err, "team")
			return
		}
	}
	app, _ := pocketApp("notes")
	sb := s.siblingAppOrFail(w, r, app.reading())
	if sb == nil {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, siblingTimeout)
	defer cancel()
	var n struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := sb.call(cctx, http.MethodGet, "/api/v1/notes/"+string(in.Note), nil, &n); err != nil {
		writeErr(w, http.StatusBadGateway, siblingErr("Pocket Notes", err))
		return
	}
	title := firstNonEmpty(strings.TrimSpace(n.Title), "Untitled")
	c := &teamCopy{Kind: store.ItemNote, SourceApp: "notes", SourceID: string(in.Note), Title: title, Body: strings.TrimSpace(stripTitleLine(n.Content, n.Title))}
	if rem != nil {
		var out map[string]any
		if err := s.remoteCall(ctx, rem, http.MethodPost, "/api/v1/teams/"+url.PathEscape(chi.URLParam(r, "ref"))+"/shared/import", c, &out); err != nil {
			s.fail(w, err, "shared note")
			return
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	s.answerCopy(w, r, c)
}

// handleImportTeamItem: POST /teams/{ref}/shared/import {source_id, title,
// body} is a note shared by the person's own Pocket Projects, where their
// Pocket Notes is.
func (s *Server) handleImportTeamItem(w http.ResponseWriter, r *http.Request) {
	var c teamCopy
	if err := decode(r, &c); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json (or too large)")
		return
	}
	c.SourceID = strings.TrimSpace(c.SourceID)
	if c.SourceID == "" || len(c.SourceID) > 64 {
		writeErr(w, http.StatusBadRequest, "source_id is required")
		return
	}
	s.answerCopy(w, r, &teamCopy{Kind: store.ItemNote, SourceApp: "notes", SourceID: c.SourceID, Title: c.Title, Body: c.Body})
}

func validDrawKind(k string) bool { return k == "sketch" || k == "mermaid" || k == "drawio" }

// answerCopy keeps the copy in the team and answers {item, replaced}.
func (s *Server) answerCopy(w http.ResponseWriter, r *http.Request, c *teamCopy) {
	t, err := s.loadTeam(r, s.accessOf(r), chi.URLParam(r, "ref"), needWrite)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	it, replaced, err := s.saveTeamCopy(r.Context(), t.ID, me(r), c)
	if err != nil {
		s.fail(w, err, "shared item")
		return
	}
	s.changed(r, "shared", []int64{t.ID}, it.ID)
	it.Body = ""
	writeJSON(w, http.StatusOK, map[string]any{"item": it, "replaced": replaced})
}

// saveTeamCopy makes the person's copy, or renews the one they made from the
// same note or drawing before.
func (s *Server) saveTeamCopy(ctx context.Context, teamID, uid int64, c *teamCopy) (*store.TeamItem, bool, error) {
	var it *store.TeamItem
	var err error
	if c.ItemID > 0 {
		it, err = s.store.GetTeamItem(ctx, c.ItemID)
		if err != nil || it.TeamID != teamID || it.CreatedBy == nil || *it.CreatedBy != uid || it.SourceID != c.SourceID {
			return nil, false, store.ErrNotFound
		}
		if !it.Follow {
			return nil, false, remoteError{status: http.StatusConflict, msg: "it no longer follows its original"}
		}
	} else {
		it, err = s.store.TeamCopy(ctx, teamID, uid, c.SourceApp, c.SourceID)
	}
	replaced := err == nil
	if !replaced {
		it = &store.TeamItem{TeamID: teamID, Kind: c.Kind, SourceApp: c.SourceApp, SourceID: c.SourceID, CreatedBy: &uid}
	}
	it.Title = firstNonEmpty(strings.TrimSpace(c.Title), "Untitled")
	it.Body, it.SharedAt, it.UpdatedBy, it.SyncError, it.Follow = c.Body, store.Now(), &uid, "", true
	if c.Kind == store.ItemDrawing {
		if len(c.DrawData)+len(c.Picture) > maxSharedDrawing {
			return nil, false, badRequest(fmt.Sprintf("that drawing is larger than %d MB", maxSharedDrawing>>20))
		}
		it.DrawKind, it.DrawData, it.Picture, it.SourceURL, it.SourceRev = c.DrawKind, c.DrawData, c.Picture, c.SourceURL, c.SourceRev
		it.PictureRev++
	}
	if err := s.store.SaveTeamItem(ctx, it); err != nil {
		return nil, false, err
	}
	return it, replaced, nil
}

// ---------- drawings from Pocket Draw ----------

var errUnchanged = errors.New("unchanged")

// pullDrawing reads one of the person's drawings from their Pocket Draw.
// With since (the picture's ETag last taken) an unchanged drawing answers
// errUnchanged without reading it all. gone = Draw says there is no such drawing.
func (s *Server) pullDrawing(ctx context.Context, u *store.User, id, since string) (c *teamCopy, gone bool, err error) {
	sb, err := s.siblingFor(ctx, u, drawApp().reading())
	if err != nil {
		return nil, false, fmt.Errorf("Pocket Draw: %v", err)
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	svg, etag, status, err := sb.fetchSince(cctx, "/api/v1/drawings/"+id+"/preview.svg", since, maxSharedDrawing)
	switch {
	case status == http.StatusNotModified:
		return nil, false, errUnchanged
	case status == http.StatusNotFound:
		// no such drawing, or one without a picture yet
		var d struct {
			ID flexID `json:"id"`
		}
		if e := sb.call(cctx, http.MethodGet, "/api/v1/drawings/"+id, nil, &d); e != nil {
			return nil, true, errors.New("that drawing isn't in your Pocket Draw (any more)")
		}
		return nil, false, errors.New("that drawing has no picture yet: open it in Pocket Draw once, then share it")
	case err != nil:
		return nil, false, errors.New(siblingErr("Pocket Draw", err))
	}
	var d struct {
		Kind  string `json:"kind"`
		Title string `json:"title"`
		Data  string `json:"data"`
	}
	if err := sb.call(cctx, http.MethodGet, "/api/v1/drawings/"+id, nil, &d); err != nil {
		return nil, strings.Contains(err.Error(), " 404 "), errors.New(siblingErr("Pocket Draw", err))
	}
	if !validDrawKind(d.Kind) {
		return nil, false, errors.New("Pocket Draw answered a drawing of an unknown kind")
	}
	return &teamCopy{Kind: store.ItemDrawing, SourceApp: "draw", SourceID: id, SourceURL: sb.public + "/d/" + id, SourceRev: etag,
		Title: firstNonEmpty(strings.TrimSpace(d.Title), "Untitled drawing"), Body: drawingText(d.Kind, d.Data), DrawKind: d.Kind, DrawData: d.Data, Picture: string(svg)}, false, nil
}

// drawingText is the words in a drawing, for search.
func drawingText(kind, data string) string {
	switch kind {
	case "mermaid":
		return data
	case "sketch":
		var scene struct {
			Elements []struct {
				Type      string `json:"type"`
				Text      string `json:"text"`
				IsDeleted bool   `json:"isDeleted"`
			} `json:"elements"`
		}
		if json.Unmarshal([]byte(data), &scene) != nil {
			return ""
		}
		words := []string{}
		for _, e := range scene.Elements {
			if e.Type == "text" && !e.IsDeleted && strings.TrimSpace(e.Text) != "" {
				words = append(words, strings.TrimSpace(e.Text))
			}
		}
		return strings.Join(words, "\n")
	}
	return ""
}

// fetchSince reads a file from the app unless it still has the ETag since
// (304): the body, its ETag and the status.
func (sb *sibling) fetchSince(ctx context.Context, path, since string, max int64) ([]byte, string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sb.base+path, nil)
	if err != nil {
		return nil, "", 0, err
	}
	req.Header.Set("Authorization", "Bearer "+sb.token)
	req.Header.Set("User-Agent", "PocketProjects/"+Version)
	if since != "" {
		req.Header.Set("If-None-Match", since)
	}
	res, err := siblingHTTP.Do(req)
	if err != nil {
		return nil, "", 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, "", res.StatusCode, fmt.Errorf("%s answered %d", path, res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, max+1))
	if err != nil {
		return nil, "", res.StatusCode, err
	}
	if int64(len(b)) > max {
		return nil, "", res.StatusCode, fmt.Errorf("the picture is larger than %d MB", max>>20)
	}
	return b, res.Header.Get("ETag"), res.StatusCode, nil
}

// handleCopyTeamDrawing makes the person's own copy of a shared drawing in
// their Pocket Draw, to work on; answers its address there.
func (s *Server) handleCopyTeamDrawing(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var d struct {
		Kind    string `json:"kind"`
		Title   string `json:"title"`
		Data    string `json:"data"`
		Picture string `json:"picture"`
	}
	it, _, err := s.teamItemFor(r, needRead)
	if err != nil {
		s.fail(w, err, "shared drawing")
		return
	}
	if it.Kind != store.ItemDrawing {
		writeErr(w, http.StatusBadRequest, "that is not a drawing")
		return
	}
	d.Kind, d.Title, d.Data, d.Picture = it.DrawKind, it.Title, it.DrawData, it.Picture
	sb := s.siblingAppOrFail(w, r, drawApp().only("drawings:write"))
	if sb == nil {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var out struct {
		ID flexID `json:"id"`
	}
	body := map[string]string{"kind": d.Kind, "title": d.Title, "data": d.Data, "preview": d.Picture}
	if err := sb.call(cctx, http.MethodPost, "/api/v1/drawings", body, &out); err != nil {
		writeErr(w, http.StatusBadGateway, siblingErr("Pocket Draw", err))
		return
	}
	if !digitsOnly(string(out.ID)) {
		writeErr(w, http.StatusBadGateway, "Pocket Draw answered without a drawing id")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": string(out.ID), "url": sb.public + "/d/" + string(out.ID)})
}

// handleRefreshTeamDrawing: its sharer takes the newest version now.
func (s *Server) handleRefreshTeamDrawing(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	it, _, err := s.teamItemFor(r, needWrite)
	if err != nil {
		s.fail(w, err, "shared drawing")
		return
	}
	if it.Kind != store.ItemDrawing || it.CreatedBy == nil || *it.CreatedBy != me(r) {
		writeErr(w, http.StatusForbidden, "only the person who shared it can take a new version from their Pocket Draw")
		return
	}
	it.SourceRev = ""
	if err := s.followDrawing(ctx, it); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- following the originals ----------

// teamSpaceLoop renews shared drawings from their originals every 2 minutes.
func (s *Server) teamSpaceLoop(ctx context.Context) {
	tick := time.NewTicker(2 * time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		items, err := s.store.FollowedDrawings(ctx)
		if err == nil {
			for i := range items {
				if err := s.followDrawing(ctx, &items[i]); err != nil {
					s.log.Debug("shared drawing", "item", items[i].ID, "err", err)
				}
			}
		}
	}
}

// followDrawing renews a shared drawing when its original changed (as the
// person who shared it). Trouble is kept on the item for the page to show.
func (s *Server) followDrawing(ctx context.Context, it *store.TeamItem) error {
	u, err := s.store.GetUser(ctx, *it.CreatedBy)
	if err != nil {
		return err
	}
	c, gone, err := s.pullDrawing(ctx, u, it.SourceID, it.SourceRev)
	if errors.Is(err, errUnchanged) {
		if it.SyncError != "" {
			_ = s.store.SetTeamItemSync(ctx, it.ID, "")
		}
		return nil
	}
	if err != nil {
		msg := err.Error()
		if gone {
			msg = "The original was deleted in Pocket Draw; this copy stays."
		}
		if msg != it.SyncError {
			_ = s.store.SetTeamItemSync(ctx, it.ID, msg)
			s.send(ctx, change{Kind: "shared", TeamIDs: []int64{it.TeamID}, IDs: []int64{it.ID}})
		}
		return err
	}
	full, err := s.store.GetTeamItem(ctx, it.ID)
	if err != nil || !full.Follow {
		return err
	}
	if _, _, err := s.saveTeamCopy(ctx, full.TeamID, u.ID, &teamCopy{Kind: store.ItemDrawing, SourceApp: "draw", SourceID: c.SourceID, SourceURL: c.SourceURL,
		SourceRev: c.SourceRev, Title: c.Title, Body: c.Body, DrawKind: c.DrawKind, DrawData: c.DrawData, Picture: c.Picture, ItemID: full.ID}); err != nil {
		return err
	}
	s.send(ctx, change{Kind: "shared", TeamIDs: []int64{full.TeamID}, IDs: []int64{full.ID}})
	return nil
}

// safeFileName is a title usable as a file name.
func safeFileName(t string) string {
	t = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < 32 {
			return '-'
		}
		return r
	}, strings.TrimSpace(t))
	return firstNonEmpty(t, "drawing")
}
