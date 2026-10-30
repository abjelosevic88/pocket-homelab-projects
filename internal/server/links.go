package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// Pocket Core app links (docs/links.md in pocket-homelab-core): another
// Pocket app may act here for a person with a pcx_ token, only within these
// scopes. Every route not listed in linkRoutes stays closed to it.

var linkOffers = []core.Offer{
	{Scope: "projects:read", Name: "See your projects and issues", Description: "Read your teams, issues, projects, milestones and cycles, the dates for a calendar, and what links to a contact, a note or a drawing (Pocket Calendar's \"Project deadlines\", Pocket Contacts' projects of a person, Pocket Day's work on a day, Pocket Draw's \"Used in\")."},
	{Scope: "projects:write", Name: "Create issues", Description: "Create issues and comments, post project updates, link pull requests to issues and add facts to a team's AI memory; change only the issues that app created itself."},
}

type linkRoute struct {
	methods string
	path    *regexp.Regexp
	scope   string
}

var linkRoutes = []linkRoute{
	{"GET", regexp.MustCompile(`^/api/v1/auth/me$`), ""},
	{"GET", regexp.MustCompile(`^/api/v1/(teams|issues|projects|cycles|calendar|related|mentions|search|dashboard|people|bootstrap|workspaces)$`), "projects:read"},
	{"GET", regexp.MustCompile(`^/api/v1/day/[0-9]{4}-[0-9]{2}-[0-9]{2}$`), "projects:read"},
	{"GET", regexp.MustCompile(`^/api/v1/(teams|issues|projects|cycles|milestones)/[A-Za-z0-9-]+(/(states|labels|members|cycles|comments|history|relations|attachments|milestones|updates|docs|links|contacts|sub-issues))?$`), "projects:read"},
	{"POST", regexp.MustCompile(`^/api/v1/issues$`), "projects:write"},
	{"PATCH", regexp.MustCompile(`^/api/v1/issues/[A-Za-z0-9-]+$`), "projects:write"},
	{"POST", regexp.MustCompile(`^/api/v1/issues/[A-Za-z0-9-]+/comments$`), "projects:write"},
	{"POST", regexp.MustCompile(`^/api/v1/projects/[0-9]+/updates$`), "projects:write"},
	{"POST", regexp.MustCompile(`^/api/v1/teams/[A-Za-z0-9-]+/memory$`), "projects:write"},
	{"POST", regexp.MustCompile(`^/api/v1/issues/[A-Za-z0-9-]+/pull-requests$`), "projects:write"},
}

var errLinkScope = errors.New("outside the link's scope")

func linkAllows(scopes []string, r *http.Request) bool {
	for _, lr := range linkRoutes {
		if !strings.Contains(" "+lr.methods+" ", " "+r.Method+" ") || !lr.path.MatchString(r.URL.Path) {
			continue
		}
		if lr.scope == "" {
			return true
		}
		for _, sc := range scopes {
			if sc == lr.scope {
				return true
			}
		}
	}
	return false
}

// linkUser validates a pcx_ token and maps it to the person it acts for.
func (s *Server) linkUser(ctx context.Context, tok string, r *http.Request) (*store.User, error) {
	in, err := s.core.Introspect(ctx, tok)
	if err != nil {
		return nil, err
	}
	if !in.Active || !in.Delegated() || in.User == nil || in.User.Email == "" {
		return nil, http.ErrNoCookie
	}
	if !linkAllows(strings.Fields(in.Scope), r) {
		return nil, errLinkScope
	}
	app := "link"
	if in.App != nil && in.App.Slug != "" {
		app = in.App.Slug
	}
	s.linkApps.Store(tok, app) // the slug: Core's introspection names the app as an object
	return s.coreIdentityUser(ctx, in)
}

// linkApp is the Pocket app acting through a link token ("" otherwise).
func (s *Server) linkApp(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return ""
	}
	tok := strings.TrimSpace(h[7:])
	if !core.IsLinkToken(tok) {
		return ""
	}
	if v, ok := s.linkApps.Load(tok); ok {
		if app, ok := v.(string); ok {
			return app
		}
	}
	return "link"
}

// source names who wrote a change: the acting app, "" for the web app and the API.
func (s *Server) source(r *http.Request) string {
	return s.linkApp(r)
}

// reportModes tells Core how each person reaches the Pocket apps used here
// (Core, Direct or off).
func (s *Server) reportModes(ctx context.Context) {
	if !s.core.Configured() {
		return
	}
	var modes []core.ConnMode
	for _, uid := range s.store.UserIDs(ctx) {
		email := s.userEmail(ctx, uid)
		if email == "" {
			continue
		}
		st, err := s.store.GetSettings(ctx, uid)
		if err != nil {
			continue
		}
		for _, app := range pocketApps {
			modes = append(modes, core.ConnMode{User: email, Target: app.Slug, Mode: s.appMode(st, app)})
		}
	}
	if err := s.core.ReportModes(ctx, modes); err != nil {
		s.log.Debug("pocket core: report modes", "err", err)
	}
}

func (s *Server) userEmail(ctx context.Context, uid int64) string {
	if u, err := s.store.GetUser(ctx, uid); err == nil {
		return u.Email
	}
	return ""
}
