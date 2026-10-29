package server

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Drawings from Pocket Draw in descriptions and comments. A drawing goes in
// as Markdown, ![title](D/d/{id}/picture.svg) with D Pocket Draw's public
// address, so it still reads as a link anywhere else. Here the browser shows
// it through GET /draw/{id}/picture.svg, fetched from Draw as the person who
// looks (the drawing is theirs, or shared with them there): the picture is
// always the current one, and someone without Draw sees a placeholder.
// Pocket Draw asks GET /mentions?url=D/d/{id} (mentions.go) for "Used in".

func init() {
	extraRoutes = append(extraRoutes, func(s *Server, r chi.Router) {
		r.Get("/draw/drawings", s.handleDrawDrawings)
		r.Post("/draw/drawings", s.handleDrawCreate)
		r.Get("/draw/{id}/picture.svg", s.handleDrawPicture)
	})
}

// drawPictureCSP keeps a drawing's SVG inert when it is opened on its own:
// no scripts, nothing loaded from elsewhere, only inline styles and data: images and fonts.
const drawPictureCSP = "default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:"

// drawKindName is how a kind of drawing is called in the picker.
func drawKindName(kind string) string {
	switch kind {
	case "mermaid":
		return "Text diagram"
	case "drawio":
		return "draw.io diagram"
	}
	return "Sketch"
}

// drawEmbed is the Markdown that shows a drawing: its picture in Pocket Draw.
func drawEmbed(public, id, title string) string {
	t := strings.Join(strings.Fields(strings.NewReplacer("[", "", "]", "", "(", "", ")", "").Replace(title)), " ")
	if t == "" {
		t = "Drawing " + id
	}
	return "![" + t + "](" + public + "/d/" + id + "/picture.svg)"
}

// drawItem is one drawing as the picker and the editor need it.
func drawItem(sb *sibling, id, title string) map[string]any {
	return map[string]any{"id": id, "title": title, "url": sb.public + "/d/" + id, "markdown": drawEmbed(sb.public, id, title)}
}

func (s *Server) handleDrawDrawings(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"items": []map[string]any{}, "connected": false}
	sb, err := s.siblingFor(r.Context(), userFrom(r.Context()), drawApp().reading())
	if err != nil {
		out["error"] = "Pocket Draw: " + err.Error()
		writeJSON(w, http.StatusOK, out)
		return
	}
	out["connected"] = true
	ctx, cancel := context.WithTimeout(r.Context(), siblingTimeout)
	defer cancel()
	var list struct {
		Drawings []struct {
			ID         flexID `json:"id"`
			Title      string `json:"title"`
			Kind       string `json:"kind"`
			FolderName string `json:"folder_name"`
			EditedAt   string `json:"edited_at"`
			HasPreview bool   `json:"has_preview"`
			PreviewRev int64  `json:"preview_rev"`
		} `json:"drawings"`
	}
	q := url.Values{"q": {strings.TrimSpace(r.URL.Query().Get("q"))}, "limit": {"24"}, "sort": {"edited"}}
	if err := sb.call(ctx, http.MethodGet, "/api/v1/drawings?"+q.Encode(), nil, &list); err != nil {
		out["error"] = siblingErr("Pocket Draw", err)
		writeJSON(w, http.StatusOK, out)
		return
	}
	items := []map[string]any{}
	for _, d := range list.Drawings {
		id := string(d.ID)
		if !digitsOnly(id) {
			continue
		}
		it := drawItem(sb, id, d.Title)
		detail := drawKindName(d.Kind)
		if d.FolderName != "" {
			detail += " · " + d.FolderName
		}
		it["kind"], it["detail"], it["edited_at"], it["has_picture"] = d.Kind, detail, d.EditedAt, d.HasPreview
		it["picture"] = fmt.Sprintf("/api/v1/draw/%s/picture.svg?r=%d", id, d.PreviewRev)
		items = append(items, it)
	}
	out["items"] = items
	writeJSON(w, http.StatusOK, out)
}

// handleDrawCreate starts a drawing in Pocket Draw (a sketch or a text
// diagram) and answers what the editor puts in and opens.
func (s *Server) handleDrawCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind  string `json:"kind"`
		Title string `json:"title"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	switch in.Kind {
	case "":
		in.Kind = "sketch"
	case "sketch", "mermaid":
	default:
		writeErr(w, http.StatusBadRequest, "kind must be sketch or mermaid")
		return
	}
	in.Title = strings.Join(strings.Fields(in.Title), " ")
	if in.Title == "" {
		in.Title = map[string]string{"sketch": "Untitled sketch", "mermaid": "Untitled diagram"}[in.Kind]
	}
	sb := s.siblingAppOrFail(w, r, drawApp().only("drawings:write"))
	if sb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), siblingTimeout)
	defer cancel()
	var d struct {
		ID    flexID `json:"id"`
		Title string `json:"title"`
		Kind  string `json:"kind"`
	}
	if err := sb.call(ctx, http.MethodPost, "/api/v1/drawings", map[string]string{"kind": in.Kind, "title": in.Title}, &d); err != nil {
		writeErr(w, http.StatusBadGateway, siblingErr("Pocket Draw", err))
		return
	}
	if !digitsOnly(string(d.ID)) {
		writeErr(w, http.StatusBadGateway, "Pocket Draw answered without a drawing id")
		return
	}
	writeJSON(w, http.StatusCreated, drawItem(sb, string(d.ID), firstNonEmpty(d.Title, in.Title)))
}

// handleDrawPicture shows a drawing's current picture, fetched from Pocket
// Draw as the person. Whatever goes wrong answers a neutral placeholder, so
// an issue never shows a broken image.
func (s *Server) handleDrawPicture(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", drawPictureCSP)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	id := chi.URLParam(r, "id")
	if !digitsOnly(id) {
		drawPlaceholder(w, "Drawing not available here")
		return
	}
	sb, err := s.siblingFor(r.Context(), userFrom(r.Context()), drawApp().reading())
	if err != nil {
		drawPlaceholder(w, "Drawing not available here")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), siblingTimeout)
	defer cancel()
	svg, status, err := sb.fetch(ctx, "/api/v1/drawings/"+id+"/preview.svg", 16<<20)
	switch {
	case status == http.StatusNotFound:
		drawPlaceholder(w, "No picture yet — open it in Pocket Draw once")
		return
	case err != nil:
		s.log.Debug("pocket draw: picture", "id", id, "err", err)
		drawPlaceholder(w, "Drawing not available here")
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "private, max-age=30")
	_, _ = w.Write(svg)
}

// drawPlaceholder is the picture shown instead of a drawing.
func drawPlaceholder(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" width="480" height="270" viewBox="0 0 480 270">`+
		`<rect x="1" y="1" width="478" height="268" rx="12" fill="#f8fafc" stroke="#cbd5e1" stroke-width="2" stroke-dasharray="8 6"/>`+
		`<text x="240" y="124" text-anchor="middle" font-family="system-ui, -apple-system, sans-serif" font-size="13" fill="#94a3b8">Pocket Draw</text>`+
		`<text x="240" y="152" text-anchor="middle" font-family="system-ui, -apple-system, sans-serif" font-size="16" fill="#475569">%s</text></svg>`, html.EscapeString(text))
}

// drawApp is Pocket Draw as listed in apps.go.
func drawApp() PocketApp {
	app, _ := pocketApp("draw")
	return app
}

// fetch reads a file (not JSON) from the app: the body, the status, and an
// error for anything but 200.
func (sb *sibling) fetch(ctx context.Context, path string, max int64) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sb.base+path, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+sb.token)
	req.Header.Set("User-Agent", "PocketProjects/"+Version)
	res, err := siblingHTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, res.StatusCode, fmt.Errorf("%s answered %d", path, res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, max+1))
	if err != nil {
		return nil, res.StatusCode, err
	}
	if int64(len(b)) > max {
		return nil, res.StatusCode, fmt.Errorf("%s is larger than %d bytes", path, max)
	}
	return b, res.StatusCode, nil
}
