// Package server wires the HTTP API, the live event stream, the background
// jobs (cycles, reminders, repeating issues), links to the other Pocket apps,
// auth and the embedded SPA.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/crypto/bcrypt"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-projects/internal/config"
	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// Version is injected at build time.
var Version = "dev"

// Server holds dependencies.
type Server struct {
	cfg   *config.Config
	store *store.Store
	log   *slog.Logger
	webFS fs.FS
	http  *http.Client

	started  time.Time
	requests atomic.Int64
	errors   atomic.Int64

	core    *core.Client
	coreMu  sync.Mutex
	coreOK  bool
	coreErr string

	oidc   *oidcProvider
	oidcMu sync.Mutex

	linkApps sync.Map // pcx_ token → acting app slug

	hub *hub // live change stream
}

// New creates a server.
func New(cfg *config.Config, st *store.Store, log *slog.Logger, webFS fs.FS) *Server {
	ua := "PocketProjects/" + Version + " (+https://github.com/abjelosevic88/pocket-homelab-projects)"
	s := &Server{cfg: cfg, store: st, log: log, webFS: webFS, started: time.Now(), hub: newHub()}
	s.http = &http.Client{Timeout: 60 * time.Second}
	s.core = core.New(cfg.CoreURL, core.Options{DB: st.DB, TokenFile: cfg.DataDir + "/core.token", UserAgent: ua, ServiceCache: time.Minute, ServiceStale: 15 * time.Minute})
	s.oidc = &oidcProvider{issuerURL: cfg.OIDCIssuer}
	return s
}

// isOwner reports whether uid is the account the box was set up with.
func (s *Server) isOwner(ctx context.Context, uid int64) bool {
	return uid > 0 && uid == s.store.OwnerID(ctx)
}

// Bootstrap seeds the first admin (if configured), demo data and starts the workers.
func (s *Server) Bootstrap(ctx context.Context) error {
	if err := s.core.Start(ctx); err != nil {
		return err
	}
	n, err := s.store.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n == 0 && s.cfg.AuthMode == "local" && s.cfg.AdminEmail != "" && s.cfg.AdminPassword != "" {
		if len(s.cfg.AdminPassword) < 8 {
			return errors.New("ADMIN_PASSWORD must be at least 8 characters")
		}
		h, _ := bcrypt.GenerateFromPassword([]byte(s.cfg.AdminPassword), bcrypt.DefaultCost)
		u := &store.User{Email: s.cfg.AdminEmail, Name: s.cfg.AdminName, PasswordHash: string(h), Role: "admin"}
		if err := s.store.CreateUser(ctx, u); err != nil {
			return err
		}
		st, _ := s.store.GetSettings(ctx, 0)
		st.SetupComplete = true
		if err := s.store.SaveBoxSettings(ctx, st); err != nil {
			return err
		}
		s.log.Info("admin user created from environment", "email", u.Email)
	}
	if s.cfg.DemoData {
		if err := s.seedDemo(ctx); err != nil {
			return err
		}
	}
	_ = s.store.PurgeSessions(ctx)
	go s.jobs(ctx)
	if s.core.Configured() {
		go s.coreLoop(ctx)
	}
	return nil
}

// Router builds the chi router.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	if s.cfg.TrustProxy {
		r.Use(middleware.RealIP)
	}
	r.Use(middleware.RequestID)
	r.Use(s.logging)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5, "application/json", "text/html", "text/css", "application/javascript", "text/plain", "text/calendar", "text/csv"))

	r.Get("/pocket-ask.js", s.core.AskWidget("projects"))
	r.Get("/healthz", s.handleHealth)
	r.Get("/readyz", s.handleReady)
	r.Get("/api/health", s.handleHealth)
	if s.cfg.MetricsEnabled {
		r.Get("/metrics", s.handleMetrics)
	}
	// The secret calendar feed (webcal) and incoming git webhooks need no login.
	r.Get("/feed/{token}.ics", s.handleFeed)
	r.Post("/hooks/git/{secret}", s.handleGitHook)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.Timeout(180 * time.Second))
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/setup", s.handleSetup)
		r.Get("/auth/status", s.handleAuthStatus)
		r.Get("/auth/oidc/start", s.handleOIDCStart)
		r.Get("/auth/oidc/callback", s.handleOIDCCallback)
		r.Post("/client-error", s.handleClientError)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Post("/auth/logout", s.handleLogout)
			r.Get("/auth/me", s.handleMe)
			r.Put("/auth/me", s.handleUpdateMe)
			r.Get("/stream", s.handleStream)
			r.Get("/bootstrap", s.handleBootstrap)
			r.Get("/people", s.handlePeople)
			r.Get("/dashboard", s.handleDashboard)
			r.Get("/search", s.handleSearch)
			r.Get("/related", s.handleRelatedByURL)
			r.Get("/calendar", s.handleCalendar)
			r.Get("/insights", s.handleInsights)

			s.teamRoutes(r)
			s.issueRoutes(r)
			s.projectRoutes(r)
			s.cycleRoutes(r)
			s.inboxRoutes(r)
			s.viewRoutes(r)
			s.timeRoutes(r)
			s.aiRoutes(r)
			s.integrationRoutes(r)
			s.importRoutes(r)

			r.Get("/users", s.handleListUsers)
			r.Post("/users", s.handleCreateUser)
			r.Put("/users/{id}", s.handleUpdateUser)
			r.Delete("/users/{id}", s.handleDeleteUser)

			r.Get("/settings", s.handleGetSettings)
			r.Put("/settings", s.handlePutSettings)
			r.Get("/settings/system", s.handleSystemInfo)
			r.Post("/settings/feed/rotate", s.handleRotateFeed)
			r.Post("/notify/test", s.handleTestNotify)

			r.Get("/tokens", s.handleListTokens)
			r.Post("/tokens", s.handleCreateToken)
			r.Delete("/tokens/{id}", s.handleDeleteToken)

			r.Get("/backup", s.handleBackup)
		})
	})

	r.NotFound(s.handleSPA)
	return r
}

// ---------- helpers ----------

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		s.requests.Add(1)
		if ww.Status() >= 500 {
			s.errors.Add(1)
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			return
		}
		s.log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(), "dur", time.Since(start).String(), "ip", r.RemoteAddr)
	})
}

type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

type userError struct{ msg string }

func (e userError) Error() string { return e.msg }

func badRequest(msg string) error { return userError{msg} }

// forbidden is a request the person's team role does not allow.
type forbidden struct{ msg string }

func (e forbidden) Error() string { return e.msg }

func errForbidden(msg string) error { return forbidden{msg} }

func (s *Server) fail(w http.ResponseWriter, err error, context string) {
	var ue userError
	var fb forbidden
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
		return
	case errors.Is(err, store.ErrConflict):
		writeErr(w, http.StatusConflict, "already exists")
		return
	case errors.As(err, &ue):
		writeErr(w, http.StatusBadRequest, ue.msg)
		return
	case errors.As(err, &fb):
		writeErr(w, http.StatusForbidden, fb.msg)
		return
	case errors.Is(err, errLinkScope):
		writeErr(w, http.StatusForbidden, "this Pocket Core link may not change that")
		return
	}
	s.log.Error(context, "err", err)
	writeErr(w, http.StatusInternalServerError, context+": "+err.Error())
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 16<<20))
	return dec.Decode(v)
}

func idParam(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	return n
}

func queryInt(r *http.Request, name string) int {
	n, _ := strconv.Atoi(r.URL.Query().Get(name))
	return n
}

func queryInt64(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(r.URL.Query().Get(name), 10, 64)
	return n
}

func queryBool(r *http.Request, name string) bool {
	v := strings.ToLower(r.URL.Query().Get(name))
	return v == "1" || v == "true" || v == "yes"
}

// queryIDs reads "1,2,3" (0 allowed for "none").
func queryIDs(r *http.Request, name string) []int64 {
	var out []int64
	for _, p := range strings.Split(r.URL.Query().Get(name), ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if p == "none" {
			out = append(out, 0)
			continue
		}
		if n, err := strconv.ParseInt(p, 10, 64); err == nil && n >= 0 {
			out = append(out, n)
		}
	}
	return out
}

func queryList(r *http.Request, name string) []string {
	var out []string
	for _, p := range strings.Split(r.URL.Query().Get(name), ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func me(r *http.Request) int64 { return actorID(r.Context()) }

// ---------- Pocket Core ----------

// coreLoop registers with Core on start (retrying until it answers) and
// re-registers every 30 minutes so version and last-seen stay fresh.
func (s *Server) coreLoop(ctx context.Context) {
	wait := 5 * time.Second
	for {
		err := s.registerWithCore(ctx)
		s.coreMu.Lock()
		s.coreOK, s.coreErr = err == nil, ""
		if err != nil {
			s.coreErr = err.Error()
		}
		s.coreMu.Unlock()
		if err != nil {
			s.log.Warn("pocket core: registration failed, retrying", "err", err, "in", wait.String())
		} else {
			wait = 30 * time.Minute
			s.reportModes(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if err != nil && wait < 5*time.Minute {
			wait *= 2
		}
	}
}

// coreTopics are the events this app publishes.
var coreTopics = []string{"projects.issue_created", "projects.issue_updated", "projects.issue_completed", "projects.comment_added",
	"projects.project_created", "projects.project_updated", "projects.project_update_posted", "projects.cycle_started", "projects.cycle_completed"}

func (s *Server) registerWithCore(ctx context.Context) error {
	m := core.Manifest{
		Slug: "projects", Name: "Pocket Projects",
		Description: "Plan and track work like Linear: teams with their own workflow, issues with identifiers, a kanban board, projects with milestones and status updates, cycles, a roadmap, an inbox, insights and time tracking, with meetings in Pocket Calendar, stakeholders from Pocket Contacts, to-dos in Pocket Tasks and Pocket AI to write, split and summarise.",
		Icon:        "/favicon.svg", Version: Version,
		URL: s.cfg.CoreAppURL, PublicURL: s.cfg.BaseURL, HealthPath: "/healthz", EnvPrefix: "PROJECTS",
		Publishes: coreTopics,
		Offers:    linkOffers,
		Uses:      append(appUses(), core.InferenceUse()),
	}
	if s.cfg.BaseURL != "" {
		m.OIDC = &core.OIDCRequest{RedirectURIs: []string{s.cfg.BaseURL + "/api/v1/auth/oidc/callback"}}
	}
	reg, err := s.core.Register(ctx, s.cfg.CoreRegistrationKey, m)
	if err != nil {
		return err
	}
	if reg.Token != "" {
		s.log.Info("pocket core: registered, app token stored", "core", s.cfg.CoreURL)
	} else {
		s.log.Debug("pocket core: re-registered", "core", s.cfg.CoreURL)
	}
	return nil
}

// coreStatus summarises the Pocket Core link for the About page.
func (s *Server) coreStatus() map[string]any {
	s.coreMu.Lock()
	defer s.coreMu.Unlock()
	return map[string]any{"configured": s.core.Configured(), "url": s.cfg.CoreURL, "registered": s.coreOK, "error": s.coreErr}
}

// handleClientError writes a crash of the web app into the server's log.
func (s *Server) handleClientError(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Message, Stack, Component, URL, Where, Agent string
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	who := ""
	if u, err := s.resolveUser(r); err == nil && u != nil {
		who = u.Email
	}
	s.log.Warn("web app crashed", "user", who, "where", in.Where, "url", in.URL, "message", in.Message, "stack", in.Stack, "component", in.Component, "agent", in.Agent)
	w.WriteHeader(http.StatusNoContent)
}

// ---------- health / metrics ----------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"core_outbox": s.coreOutboxHealth(r.Context()), "status": "ok", "version": Version, "uptime": time.Since(s.started).Round(time.Second).String()})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DB.PingContext(r.Context()); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	counts := s.store.Counts(ctx)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP pocket_projects_info Build info.\n# TYPE pocket_projects_info gauge\npocket_projects_info{version=%q} 1\n", Version)
	fmt.Fprintf(w, "# HELP pocket_projects_uptime_seconds Process uptime.\n# TYPE pocket_projects_uptime_seconds gauge\npocket_projects_uptime_seconds %d\n", int(time.Since(s.started).Seconds()))
	fmt.Fprintf(w, "# HELP pocket_projects_http_requests_total HTTP requests served.\n# TYPE pocket_projects_http_requests_total counter\npocket_projects_http_requests_total %d\n", s.requests.Load())
	fmt.Fprintf(w, "# HELP pocket_projects_http_errors_total HTTP 5xx responses.\n# TYPE pocket_projects_http_errors_total counter\npocket_projects_http_errors_total %d\n", s.errors.Load())
	fmt.Fprintf(w, "# HELP pocket_projects_stream_clients Browsers on the live stream.\n# TYPE pocket_projects_stream_clients gauge\npocket_projects_stream_clients %d\n", s.hub.count())
	fmt.Fprintf(w, "# HELP pocket_projects_rows Rows per table.\n# TYPE pocket_projects_rows gauge\n")
	for k, v := range counts {
		fmt.Fprintf(w, "pocket_projects_rows{table=%q} %d\n", k, v)
	}
}

// ---------- static / SPA ----------

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/feed/") || strings.HasPrefix(r.URL.Path, "/hooks/") || strings.HasPrefix(r.URL.Path, "/files/") {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if s.webFS == nil {
		http.Error(w, "web UI not built (run `make web`)", http.StatusNotFound)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if f, err := s.webFS.Open(path); err == nil {
		f.Close()
		if strings.HasPrefix(path, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.FileServerFS(s.webFS).ServeHTTP(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	r.URL.Path = "/"
	http.FileServerFS(s.webFS).ServeHTTP(w, r)
}

// loc returns the person's time zone (their choice, else the box's).
func (s *Server) loc(ctx context.Context, uid int64) *time.Location {
	if tz := s.store.UserSetting(ctx, uid, "timezone"); tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			return l
		}
	}
	l, err := time.LoadLocation(s.cfg.Timezone)
	if err != nil {
		return time.UTC
	}
	return l
}

// today is the person's local date.
func (s *Server) today(ctx context.Context, uid int64) string {
	return time.Now().In(s.loc(ctx, uid)).Format("2006-01-02")
}

// boxToday is the date in the box's zone (background jobs).
func (s *Server) boxToday() time.Time {
	l, err := time.LoadLocation(s.cfg.Timezone)
	if err != nil {
		l = time.UTC
	}
	n := time.Now().In(l)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

// publicURL is where the browser finds this app ("" when BASE_URL is unset).
func (s *Server) publicURL(r *http.Request) string {
	if s.cfg.BaseURL != "" {
		return s.cfg.BaseURL
	}
	if r == nil {
		return ""
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (s *Server) issueURL(r *http.Request, ident string) string {
	return s.publicURL(r) + "/issue/" + ident
}

func (s *Server) projectURL(r *http.Request, id int64) string {
	return s.publicURL(r) + "/project/" + strconv.FormatInt(id, 10)
}
