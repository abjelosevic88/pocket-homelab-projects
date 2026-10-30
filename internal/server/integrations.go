package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// The other Pocket apps, reached as the person (apps.go): meetings in Pocket
// Calendar, people from Pocket Contacts, to-dos in Pocket Tasks, notes in
// Pocket Notes. Each call stands alone: an app that is off, not linked or
// down answers a plain-words error and nothing else breaks.

const siblingTimeout = 10 * time.Second

func (s *Server) integrationRoutes(r chi.Router) {
	r.Get("/integrations", s.handleIntegrations)
	r.Put("/integrations/{id}", s.handleSaveIntegration)
	r.Get("/contacts/suggest", s.handleContactSuggest)
	r.Get("/notes/search", s.handleNoteSearch)
	r.Get("/calendar/events", s.handleCalendarEvents)
	r.Post("/issues/{ref}/task", s.handleIssueTask)
	r.Post("/issues/{ref}/event", s.handleIssueEvent)
	r.Post("/projects/{id}/event", s.handleProjectEvent)
	r.Post("/milestones/{id}/event", s.handleMilestoneEvent)
	r.Get("/projects/{id}/related", s.handleProjectRelated)
	r.Get("/issues/{ref}/notes", s.handleIssueNotes)
	r.Post("/issues/{ref}/notes", s.handleIssueLinkNote)
	r.Delete("/issues/{ref}/notes/{note}", s.handleIssueUnlinkNote)
	r.Get("/issues/{ref}/notes/{note}/content", s.handleIssueNoteContent)
	r.Get("/teams/{ref}/notes", s.handleTeamNotes)
	r.Put("/teams/{ref}/notes", s.handlePutTeamNotes)
	r.Get("/notes/folders", s.handleNoteFolders)
	r.Get("/pr-review/status", s.handlePRReviewStatus)
	r.Post("/pr-review/open", s.handlePRReviewOpen)
}

// siblingErr says in plain words why another app did not answer.
func siblingErr(app string, err error) string {
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "Client.Timeout") {
		return app + " did not answer in time"
	}
	if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such host") {
		return app + " can't be reached right now"
	}
	return app + ": " + err.Error()
}

func (s *Server) siblingOrFail(w http.ResponseWriter, r *http.Request, appID string) *sibling {
	app, _ := pocketApp(appID)
	return s.siblingAppOrFail(w, r, app)
}

func (s *Server) siblingAppOrFail(w http.ResponseWriter, r *http.Request, app PocketApp) *sibling {
	sb, err := s.siblingFor(r.Context(), userFrom(r.Context()), app)
	if err != nil {
		writeErr(w, http.StatusBadGateway, app.Name+": "+err.Error())
		return nil
	}
	return sb
}

// flexID reads an id another app sends as a number or a string.
type flexID string

func (f *flexID) UnmarshalJSON(b []byte) error {
	*f = flexID(strings.Trim(string(b), `"`))
	return nil
}

// ---------- Pocket Contacts ----------

func (s *Server) handleContactSuggest(w http.ResponseWriter, r *http.Request) {
	sb := s.siblingOrFail(w, r, "contacts")
	if sb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), siblingTimeout)
	defer cancel()
	var list []struct {
		ID       flexID `json:"id"`
		Name     string `json:"name"`
		Email    string `json:"email"`
		Phone    string `json:"phone"`
		PhotoURL string `json:"photo_url"`
		Org      string `json:"org"`
	}
	if err := sb.call(ctx, http.MethodGet, "/api/v1/suggest?limit=12&q="+url.QueryEscape(r.URL.Query().Get("q")), nil, &list); err != nil {
		writeErr(w, http.StatusBadGateway, siblingErr("Pocket Contacts", err))
		return
	}
	out := []map[string]any{}
	for _, c := range list {
		photo := c.PhotoURL
		if strings.HasPrefix(photo, "/") {
			photo = sb.public + photo
		}
		out = append(out, map[string]any{"contact_id": string(c.ID), "name": c.Name, "email": c.Email, "phone": c.Phone, "org": c.Org,
			"photo_url": photo, "url": sb.public + "/contacts/" + string(c.ID)})
	}
	writeJSON(w, http.StatusOK, out)
}

// contactPageURL is the address of a contact in Pocket Contacts ("" when unknown).
func (s *Server) contactPageURL(ctx context.Context, uid int64, contactID string) string {
	app, _ := pocketApp("contacts")
	st, _ := s.store.GetSettings(ctx, uid)
	if s.appMode(st, app) == modeDirect && st.IntegrationURLs["contacts"] != "" {
		return strings.TrimRight(st.IntegrationURLs["contacts"], "/") + "/contacts/" + contactID
	}
	if s.core.Configured() {
		if res, err := s.core.Resolve(ctx, "contacts"); err == nil {
			return strings.TrimRight(firstNonEmpty(res.PublicURL, res.URL), "/") + "/contacts/" + contactID
		}
	}
	return ""
}

// ---------- Pocket Notes ----------

func (s *Server) handleNoteSearch(w http.ResponseWriter, r *http.Request) {
	app, _ := pocketApp("notes")
	sb := s.siblingAppOrFail(w, r, app.reading())
	if sb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), siblingTimeout)
	defer cancel()
	var hits []struct {
		ID      flexID `json:"id"`
		Title   string `json:"title"`
		Snippet string `json:"snippet"`
		Folder  string `json:"category"`
	}
	if err := sb.call(ctx, http.MethodGet, "/api/v1/search?q="+url.QueryEscape(r.URL.Query().Get("q")), nil, &hits); err != nil {
		writeErr(w, http.StatusBadGateway, siblingErr("Pocket Notes", err))
		return
	}
	out := []map[string]any{}
	for i, h := range hits {
		if i == 15 {
			break
		}
		out = append(out, map[string]any{"id": string(h.ID), "title": h.Title, "snippet": strings.NewReplacer("[[[", "", "]]]", "").Replace(h.Snippet),
			"folder": h.Folder, "url": sb.public + "/note/" + string(h.ID)})
	}
	writeJSON(w, http.StatusOK, out)
}

// RelatedSection is one app's part of GET /projects/{id}/related.
type RelatedSection struct {
	Status string           `json:"status"` // ok | off | error
	Detail string           `json:"detail"`
	AppURL string           `json:"app_url"`
	NewURL string           `json:"new_url,omitempty"`
	Items  []map[string]any `json:"items"`
}

func (s *Server) relatedSection(ctx context.Context, u *store.User, appID string, fill func(context.Context, *sibling, *RelatedSection) error) RelatedSection {
	sec := RelatedSection{Items: []map[string]any{}}
	app, _ := pocketApp(appID)
	app = app.reading()
	st, _ := s.store.GetSettings(ctx, u.ID)
	switch mode := s.appMode(st, app); {
	case mode == modeOff:
		sec.Status, sec.Detail = "off", app.Name+" is switched off in Settings → Apps"
		return sec
	case mode == modeCore && !s.core.Configured():
		sec.Status, sec.Detail = "off", "there is no Pocket Core on this box; connect "+app.Name+" directly in Settings → Apps"
		return sec
	}
	sb, err := s.siblingFor(ctx, u, app)
	if err != nil {
		sec.Status, sec.Detail = "error", err.Error()
		return sec
	}
	sec.AppURL = sb.public
	c, cancel := context.WithTimeout(ctx, siblingTimeout)
	defer cancel()
	if err := fill(c, sb, &sec); err != nil {
		sec.Status, sec.Detail, sec.Items = "error", siblingErr(app.Name, err), []map[string]any{}
		return sec
	}
	sec.Status = "ok"
	return sec
}

// handleProjectRelated: notes linking to the project and meetings about it.
func (s *Server) handleProjectRelated(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := s.accessOf(r)
	p, err := s.loadProject(r, a, idParam(r, "id"), needRead)
	if err != nil {
		s.fail(w, err, "project")
		return
	}
	u := userFrom(ctx)
	page := s.projectURL(r, p.ID)
	out := map[string]RelatedSection{}
	done := make(chan struct{}, 2)
	var notes, cal RelatedSection
	go func() {
		notes = s.notesAbout(ctx, u, page, p.Name, p.Name, defaultNotesFolder)
		done <- struct{}{}
	}()
	go func() {
		cal = s.relatedSection(ctx, u, "calendar", func(c context.Context, sb *sibling, sec *RelatedSection) error {
			var found []calOccurrence
			if err := sb.call(c, http.MethodGet, "/api/v1/search?q="+url.QueryEscape(page), nil, &found); err != nil {
				return err
			}
			for _, o := range found {
				sec.Items = append(sec.Items, s.calendarItem(sb, o))
			}
			return nil
		})
		done <- struct{}{}
	}()
	<-done
	<-done
	out["notes"], out["calendar"] = notes, cal
	writeJSON(w, http.StatusOK, out)
}

// notesAbout lists the person's notes in Pocket Notes that link to page (the
// relation is the link itself, see Pocket Notes docs/notes-from-apps.md) and
// the address that starts a new one, linked, in folder.
func (s *Server) notesAbout(ctx context.Context, u *store.User, page, title, label, folder string) RelatedSection {
	return s.relatedSection(ctx, u, "notes", func(c context.Context, sb *sibling, sec *RelatedSection) error {
		q := url.Values{"title": {title}, "url": {page}, "label": {label}, "folder": {folder}}
		sec.NewURL = sb.public + "/new?" + q.Encode()
		var list []struct {
			ID        flexID `json:"id"`
			Title     string `json:"title"`
			Category  string `json:"category"`
			Excerpt   string `json:"excerpt"`
			UpdatedAt string `json:"updated_at"`
			Modified  int64  `json:"modified"`
		}
		if err := sb.call(c, http.MethodGet, "/api/v1/mentions?url="+url.QueryEscape(page), nil, &list); err != nil {
			return err
		}
		for _, n := range list {
			sec.Items = append(sec.Items, map[string]any{"id": string(n.ID), "title": n.Title, "folder": n.Category, "snippet": n.Excerpt, "updated_at": n.UpdatedAt, "modified": n.Modified, "url": sb.public + "/note/" + string(n.ID)})
		}
		return nil
	})
}

// issueNoteLink is the line that makes a note about an issue.
func issueNoteLink(ident, title, page string) string {
	label := strings.NewReplacer("[", "(", "]", ")", "\n", " ").Replace(ident + " · " + title)
	return "[" + label + "](" + page + ")"
}

// handleIssueNotes: the person's notes that link to the issue.
func (s *Server) handleIssueNotes(w http.ResponseWriter, r *http.Request) {
	is, err := s.issueInfoOf(r, needRead)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	writeJSON(w, http.StatusOK, s.notesAbout(r.Context(), userFrom(r.Context()), is.URL, is.Ident+" "+is.Title, is.Ident+" · "+is.Title, is.NotesFolder))
}

// handleIssueLinkNote links one of the person's notes to the issue by adding
// the issue's link at the end of it (nothing is stored here).
func (s *Server) handleIssueLinkNote(w http.ResponseWriter, r *http.Request) {
	var in struct {
		NoteID flexID `json:"note_id"`
	}
	if err := decode(r, &in); err != nil || !digitsOnly(string(in.NoteID)) {
		writeErr(w, http.StatusBadRequest, "note_id is required")
		return
	}
	is, err := s.issueInfoOf(r, needRead)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	app, _ := pocketApp("notes")
	sb := s.siblingAppOrFail(w, r, app.only("notes:write"))
	if sb == nil {
		return
	}
	c, cancel := context.WithTimeout(r.Context(), siblingTimeout)
	defer cancel()
	var n struct {
		ID    flexID `json:"id"`
		Title string `json:"title"`
	}
	if err := sb.call(c, http.MethodPost, "/api/v1/notes/"+string(in.NoteID)+"/append", map[string]string{"text": issueNoteLink(is.Ident, is.Title, is.URL)}, &n); err != nil {
		writeErr(w, http.StatusBadGateway, siblingErr("Pocket Notes", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": string(n.ID), "title": n.Title, "url": sb.public + "/note/" + string(n.ID)})
}

// handleIssueUnlinkNote takes the issue's link out of one of the person's
// notes, which is what unlinks it (the note itself stays).
func (s *Server) handleIssueUnlinkNote(w http.ResponseWriter, r *http.Request) {
	noteID := chi.URLParam(r, "note")
	if !digitsOnly(noteID) {
		writeErr(w, http.StatusBadRequest, "not a note id")
		return
	}
	is, err := s.issueInfoOf(r, needRead)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	app, _ := pocketApp("notes")
	sb := s.siblingAppOrFail(w, r, app.only("notes:read", "notes:write"))
	if sb == nil {
		return
	}
	c, cancel := context.WithTimeout(r.Context(), siblingTimeout)
	defer cancel()
	page := is.URL
	for try := 0; ; try++ {
		var n struct {
			Content string `json:"content"`
			ETag    string `json:"etag"`
		}
		if err := sb.call(c, http.MethodGet, "/api/v1/notes/"+noteID, nil, &n); err != nil {
			writeErr(w, http.StatusBadGateway, siblingErr("Pocket Notes", err))
			return
		}
		content, ok := unlinkFromNote(n.Content, page)
		if !ok {
			writeErr(w, http.StatusConflict, "this note doesn't link to "+is.Ident+" in a way that can be taken out here; open the note and remove the link there")
			return
		}
		err := sb.call(c, http.MethodPut, "/api/v1/notes/"+noteID, map[string]any{"content": content, "etag": n.ETag}, nil)
		if err == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// changed between reading and saving (someone typing in it): read again once
		if try == 0 && (strings.Contains(err.Error(), " 409 ") || strings.Contains(err.Error(), " 412 ")) {
			continue
		}
		writeErr(w, http.StatusBadGateway, siblingErr("Pocket Notes", err))
		return
	}
}

// handleIssueNoteContent: one of the person's notes as Markdown, for adding
// it to a Pocket AI message about the issue ("@" in the panel).
func (s *Server) handleIssueNoteContent(w http.ResponseWriter, r *http.Request) {
	noteID := chi.URLParam(r, "note")
	if !digitsOnly(noteID) {
		writeErr(w, http.StatusBadRequest, "not a note id")
		return
	}
	if _, err := s.issueInfoOf(r, needRead); err != nil {
		s.fail(w, err, "issue")
		return
	}
	app, _ := pocketApp("notes")
	sb := s.siblingAppOrFail(w, r, app.reading())
	if sb == nil {
		return
	}
	c, cancel := context.WithTimeout(r.Context(), siblingTimeout)
	defer cancel()
	var n struct {
		Title    string `json:"title"`
		Category string `json:"category"`
		Content  string `json:"content"`
	}
	if err := sb.call(c, http.MethodGet, "/api/v1/notes/"+noteID, nil, &n); err != nil {
		writeErr(w, http.StatusBadGateway, siblingErr("Pocket Notes", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"title": n.Title, "folder": n.Category, "content": n.Content, "url": sb.public + "/note/" + noteID})
}

// unlinkFromNote takes every link to page out of a note's Markdown: a line
// that is only the link goes, a link inside text keeps its words, a bare
// address goes. ok is false when nothing was found.
func unlinkFromNote(content, page string) (string, bool) {
	u, err := url.Parse(page)
	if err != nil || u.Host == "" {
		return content, false
	}
	addr := `https?://` + regexp.QuoteMeta(u.Host+strings.TrimRight(u.Path, "/")) + `(?:[/?#][^\s)>\]]*)?`
	md := `\[([^\]]*)\]\(<?` + addr + `>?(?:\s+"[^"]*")?\)`
	whole := regexp.MustCompile(`(?i)^\s*(?:[-*+]\s+)?(?:` + md + `|<` + addr + `>|` + addr + `)\s*$`)
	link := regexp.MustCompile(`(?i)` + md)
	auto := regexp.MustCompile(`(?i)<` + addr + `>`)
	bare := regexp.MustCompile(`(?i)` + addr + `([\s.,;:!)]|$)`)
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	changed := false
	for _, l := range lines {
		if whole.MatchString(l) {
			changed = true
			continue
		}
		m := link.ReplaceAllString(l, "$1")
		m = auto.ReplaceAllString(m, "")
		m = bare.ReplaceAllString(m, "$1")
		if m != l {
			changed = true
		}
		out = append(out, m)
	}
	if !changed {
		return content, false
	}
	text := strings.Join(out, "\n")
	for strings.Contains(text, "\n\n\n") {
		text = strings.ReplaceAll(text, "\n\n\n", "\n\n")
	}
	return text, true
}

func digitsOnly(v string) bool {
	if v == "" || len(v) > 19 {
		return false
	}
	for _, c := range v {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// ---------- Pocket Calendar ----------

// calOccurrence is what Pocket Calendar answers for an event.
type calOccurrence struct {
	ID          flexID `json:"id"`
	EventID     flexID `json:"event_id"`
	Title       string `json:"title"`
	Start       string `json:"start"`
	End         string `json:"end"`
	AllDay      bool   `json:"all_day"`
	Location    string `json:"location"`
	Conference  string `json:"conference"`
	URL         string `json:"url"`
	Color       string `json:"color"`
	Description string `json:"description"`
	Masked      bool   `json:"masked"`
}

func (s *Server) calendarItem(sb *sibling, o calOccurrence) map[string]any {
	id := string(o.ID)
	return map[string]any{"id": id, "title": o.Title, "start": o.Start, "end": o.End, "all_day": o.AllDay, "location": o.Location,
		"conference": o.Conference, "color": o.Color, "url": sb.public + "/event/" + id, "link": o.URL, "masked": o.Masked}
}

// handleCalendarEvents: the person's Pocket Calendar events in a range, for the Projects calendar view.
func (s *Server) handleCalendarEvents(w http.ResponseWriter, r *http.Request) {
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if !validDate(from) || !validDate(to) || from == "" || to == "" {
		writeErr(w, http.StatusBadRequest, "from and to are dates")
		return
	}
	sec := s.relatedSection(r.Context(), userFrom(r.Context()), "calendar", func(c context.Context, sb *sibling, sec *RelatedSection) error {
		var out struct {
			Events []calOccurrence `json:"events"`
		}
		q := url.Values{"from": {from}, "to": {to}}
		if tz := r.URL.Query().Get("tz"); tz != "" {
			q.Set("tz", tz)
		}
		if err := sb.call(c, http.MethodGet, "/api/v1/events?"+q.Encode(), nil, &out); err != nil {
			return err
		}
		for _, o := range out.Events {
			sec.Items = append(sec.Items, s.calendarItem(sb, o))
		}
		return nil
	})
	writeJSON(w, http.StatusOK, sec)
}

type eventInput struct {
	Title       string   `json:"title"`
	Start       string   `json:"start"` // 2026-10-23T10:00 (local) or RFC 3339; a date for all day
	DurationMin int      `json:"duration_min"`
	AllDay      bool     `json:"all_day"`
	Location    string   `json:"location"`
	Conference  string   `json:"conference"`
	Description string   `json:"description"`
	UserIDs     []int64  `json:"user_ids"` // people on the box to invite
	Emails      []string `json:"emails"`   // anyone else (stakeholders)
}

// makeEvent creates an event in the person's Pocket Calendar linking back to page.
func (s *Server) makeEvent(w http.ResponseWriter, r *http.Request, in eventInput, defTitle, page, about string) (map[string]any, bool) {
	ctx := r.Context()
	if in.DurationMin <= 0 {
		in.DurationMin = 30
	}
	title := firstNonEmpty(strings.TrimSpace(in.Title), defTitle)
	body := map[string]any{"title": title, "location": in.Location, "conference": in.Conference, "url": page,
		"description": strings.TrimSpace(in.Description + "\n\n" + about + "\n" + page)}
	if in.AllDay || len(in.Start) == 10 {
		d, err := time.Parse("2006-01-02", in.Start[:min(10, len(in.Start))])
		if err != nil {
			writeErr(w, http.StatusBadRequest, "start looks like 2026-10-23T10:00 or 2026-10-23")
			return nil, false
		}
		body["all_day"], body["start"], body["end"] = true, d.Format("2006-01-02"), d.AddDate(0, 0, 1).Format("2006-01-02")
	} else if t, err := time.Parse(time.RFC3339, in.Start); err == nil {
		body["start"], body["end"] = in.Start, t.Add(time.Duration(in.DurationMin)*time.Minute).Format(time.RFC3339)
	} else if t, ok := localTime(in.Start); ok {
		body["start"], body["end"] = t.Format("2006-01-02T15:04"), t.Add(time.Duration(in.DurationMin)*time.Minute).Format("2006-01-02T15:04")
	} else {
		writeErr(w, http.StatusBadRequest, "start looks like 2026-10-23T10:00")
		return nil, false
	}
	var attendees []map[string]string
	seen := map[string]bool{strings.ToLower(userFrom(ctx).Email): true}
	people, _ := s.store.PeopleByID(ctx, in.UserIDs)
	for _, p := range people {
		if e := strings.ToLower(p.Email); !seen[e] {
			seen[e] = true
			attendees = append(attendees, map[string]string{"email": p.Email, "name": p.Name})
		}
	}
	for _, e := range in.Emails {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" && strings.Contains(e, "@") && !seen[e] {
			seen[e] = true
			attendees = append(attendees, map[string]string{"email": e})
		}
	}
	if len(attendees) > 0 {
		body["attendees"] = attendees
	}
	sb := s.siblingOrFail(w, r, "calendar")
	if sb == nil {
		return nil, false
	}
	c, cancel := context.WithTimeout(ctx, siblingTimeout)
	defer cancel()
	var o calOccurrence
	if err := sb.call(c, http.MethodPost, "/api/v1/events", body, &o); err != nil {
		writeErr(w, http.StatusBadGateway, siblingErr("Pocket Calendar", err))
		return nil, false
	}
	return s.calendarItem(sb, o), true
}

func localTime(v string) (time.Time, bool) {
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02T15:04:05", "2006-01-02 15:04"} {
		if t, err := time.Parse(layout, strings.TrimSpace(v)); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func (s *Server) handleIssueEvent(w http.ResponseWriter, r *http.Request) {
	var in eventInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	page := s.issueURL(r, is.Identifier)
	item, ok := s.makeEvent(w, r, in, is.Identifier+" "+is.Title, page, "About "+is.Identifier+": "+is.Title)
	if !ok {
		return
	}
	meta, _ := json.Marshal(map[string]any{"start": item["start"], "end": item["end"], "all_day": item["all_day"]})
	att := &store.Attachment{IssueID: &is.ID, Kind: "event", Title: fmt.Sprint(item["title"]), Subtitle: fmt.Sprint(item["start"]), URL: fmt.Sprint(item["url"]),
		ExtApp: "calendar", ExtID: fmt.Sprint(item["id"]), Meta: meta, CreatedBy: ptr(me(r))}
	_ = s.store.SaveAttachment(r.Context(), att)
	_ = s.store.AddHistory(r.Context(), store.History{IssueID: &is.ID, ActorID: ptr(me(r)), Field: "event_scheduled", ToLabel: att.Title})
	s.changed(r, "issue", []int64{is.TeamID}, is.ID)
	writeJSON(w, http.StatusCreated, map[string]any{"item": item, "attachment": att})
}

func (s *Server) handleProjectEvent(w http.ResponseWriter, r *http.Request) {
	var in eventInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a := s.accessOf(r)
	p, err := s.loadProject(r, a, idParam(r, "id"), needRead)
	if err != nil {
		s.fail(w, err, "project")
		return
	}
	page := s.projectURL(r, p.ID)
	item, ok := s.makeEvent(w, r, in, p.Name+" sync", page, "About the project "+p.Name)
	if !ok {
		return
	}
	meta, _ := json.Marshal(map[string]any{"start": item["start"], "end": item["end"], "all_day": item["all_day"]})
	att := &store.Attachment{ProjectID: &p.ID, Kind: "event", Title: fmt.Sprint(item["title"]), Subtitle: fmt.Sprint(item["start"]), URL: fmt.Sprint(item["url"]),
		ExtApp: "calendar", ExtID: fmt.Sprint(item["id"]), Meta: meta, CreatedBy: ptr(me(r))}
	_ = s.store.SaveAttachment(r.Context(), att)
	s.changed(r, "project", p.TeamIDs, p.ID)
	writeJSON(w, http.StatusCreated, map[string]any{"item": item, "attachment": att})
}

func (s *Server) handleMilestoneEvent(w http.ResponseWriter, r *http.Request) {
	var in eventInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	m, p, err := s.milestoneFor(r, needRead)
	if err != nil {
		s.fail(w, err, "milestone")
		return
	}
	if in.Start == "" {
		in.Start, in.AllDay = m.TargetDate, true
	}
	if in.Start == "" {
		writeErr(w, http.StatusBadRequest, "give the milestone a target date or choose a start")
		return
	}
	page := s.projectURL(r, p.ID)
	item, ok := s.makeEvent(w, r, in, "◆ "+m.Name+" ("+p.Name+")", page, "Milestone of "+p.Name)
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"item": item})
}

// ---------- Pocket Tasks ----------

type taskJSON struct {
	ID     flexID `json:"id"`
	Title  string `json:"title"`
	Due    string `json:"due"`
	Status string `json:"status"`
}

// handleIssueTask puts the issue on the person's own Pocket Tasks list.
func (s *Server) handleIssueTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		Title string `json:"title"`
		Due   string `json:"due"`
		When  string `json:"when"` // today | tomorrow | "" (no start)
	}
	_ = decode(r, &in)
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	sb := s.siblingOrFail(w, r, "tasks")
	if sb == nil {
		return
	}
	page := s.issueURL(r, is.Identifier)
	body := map[string]any{"title": firstNonEmpty(strings.TrimSpace(in.Title), is.Identifier+" "+is.Title), "url": page, "notes": "[" + is.Identifier + "](" + page + ")"}
	due := firstNonEmpty(in.Due, is.DueDate)
	if due != "" {
		if !validDate(due) {
			writeErr(w, http.StatusBadRequest, "due looks like 2026-10-23")
			return
		}
		body["due"] = due
	}
	switch in.When {
	case "today":
		body["start"] = s.today(ctx, a.uid)
	case "tomorrow":
		t, _ := time.Parse("2006-01-02", s.today(ctx, a.uid))
		body["start"] = t.AddDate(0, 0, 1).Format("2006-01-02")
	}
	if is.Priority > 0 && is.Priority <= 2 {
		body["priority"] = 1
	}
	c, cancel := context.WithTimeout(ctx, siblingTimeout)
	defer cancel()
	var t taskJSON
	if err := sb.call(c, http.MethodPost, "/api/v1/tasks", body, &t); err != nil {
		writeErr(w, http.StatusBadGateway, siblingErr("Pocket Tasks", err))
		return
	}
	att := &store.Attachment{IssueID: &is.ID, Kind: "task", Title: t.Title, Subtitle: "Pocket Tasks", URL: sb.public + "/today?task=" + string(t.ID),
		ExtApp: "tasks", ExtID: string(t.ID), State: t.Status, CreatedBy: ptr(a.uid)}
	if err := s.store.SaveAttachment(ctx, att); err != nil {
		s.fail(w, err, "task")
		return
	}
	s.changed(r, "issue", []int64{is.TeamID}, is.ID)
	writeJSON(w, http.StatusCreated, att)
}

// syncLinkedTasks completes (or reopens) the Pocket Tasks to-dos linked to an issue, as the people who made them.
func (s *Server) syncLinkedTasks(ctx context.Context, is *store.Issue, done bool) {
	atts, err := s.store.Attachments(ctx, is.ID, 0)
	if err != nil {
		return
	}
	app, _ := pocketApp("tasks")
	for _, att := range atts {
		if att.Kind != "task" || att.ExtID == "" || att.CreatedBy == nil {
			continue
		}
		u, err := s.store.GetUser(ctx, *att.CreatedBy)
		if err != nil {
			continue
		}
		sb, err := s.siblingFor(ctx, u, app)
		if err != nil {
			continue
		}
		c, cancel := context.WithTimeout(ctx, siblingTimeout)
		var res struct {
			Task taskJSON `json:"task"`
		}
		err = sb.call(c, http.MethodPost, "/api/v1/tasks/"+att.ExtID+"/complete", map[string]bool{"done": done}, &res)
		cancel()
		if err != nil {
			s.log.Debug("pocket tasks: complete linked to-do", "issue", is.Identifier, "err", err)
			continue
		}
		att.State = res.Task.Status
		_ = s.store.SaveAttachment(ctx, &att)
	}
}
