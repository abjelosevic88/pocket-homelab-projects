package server

// Connected instances: another Pocket Projects (a friend's, a client's) shows
// here as a workspace of its own at /r/{id}/…. This server passes the web
// app's API calls on with the person's own API token there, so everything
// works as it does over there, and answers the per-person parts itself:
// Pocket AI conversations and Pocket Notes are the person's, here, and
// nothing about them is stored over there.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

type remoteCtxKey struct{}

// remoteFrom is the connected instance a request is for (nil: this instance).
func remoteFrom(ctx context.Context) *store.Remote {
	rem, _ := ctx.Value(remoteCtxKey{}).(*store.Remote)
	return rem
}

func (s *Server) remoteRoutes(r chi.Router) {
	r.Get("/remotes", s.handleListRemotes)
	r.Post("/remotes", s.handleCreateRemote)
	r.Patch("/remotes/{id}", s.handlePatchRemote)
	r.Delete("/remotes/{id}", s.handleDeleteRemote)
}

// remoteAPI is /r/{rid}/api/v1/…: the per-person parts answered here, the
// rest passed on to the connected instance.
func (s *Server) remoteAPI() http.Handler {
	r := chi.NewRouter()
	r.Use(s.requireAuth, s.withRemote)
	// the person's own Pocket Notes
	r.Get("/issues/{ref}/notes", s.handleIssueNotes)
	r.Post("/issues/{ref}/notes", s.handleIssueLinkNote)
	r.Delete("/issues/{ref}/notes/{note}", s.handleIssueUnlinkNote)
	r.Get("/issues/{ref}/notes/{note}/content", s.handleIssueNoteContent)
	r.Post("/issues/{ref}/notes/{note}/share", s.handleShareNote)
	r.Get("/notes/search", s.handleNoteSearch)
	r.Get("/notes/folders", s.handleNoteFolders)
	r.Get("/teams/{ref}/notes", s.handleTeamNotes)
	// the person's own Pocket AI
	r.Get("/ai/agent-options", s.handleAgentOptions)
	r.Get("/ai/dirs", s.handleAIDirs)
	r.Get("/ai/active", s.handleRemoteAIActive)
	r.Get("/teams/{ref}/ai/mine", s.handleRemoteGetMyAI)
	r.Put("/teams/{ref}/ai/mine", s.handleRemotePutMyAI)
	r.Put("/teams/{ref}/ai", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusForbidden, "the team's own Pocket AI setup is changed on "+remoteFrom(r.Context()).Name+" itself; here you set up your own (Just for you)")
	})
	r.Get("/issues/{ref}/code", s.handleRemoteIssueCode)
	r.Post("/issues/{ref}/code", s.handleRemoteStartCode)
	r.Post("/issues/{ref}/code/{draft}/unlink", s.handleRemoteCodeUnlink)
	r.Delete("/issues/{ref}/code/{draft}", s.handleRemoteCodeDelete)
	notHere := func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusNotImplemented, "adding issues of a connected instance to your Pocket Tasks or Calendar is not there yet")
	}
	r.Post("/issues/{ref}/task", notHere)
	r.Post("/issues/{ref}/event", notHere)
	// this instance's session and connections, not the other one's
	r.Post("/auth/logout", s.handleLogout)
	r.Get("/remotes", s.handleListRemotes)
	r.NotFound(s.proxyRemote)
	r.MethodNotAllowed(s.proxyRemote)
	return r
}

// withRemote loads the person's connection named in the address.
func (s *Server) withRemote(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(chi.URLParam(r, "rid"), 10, 64)
		rem, err := s.store.GetRemote(r.Context(), id, me(r))
		if err != nil {
			writeErr(w, http.StatusNotFound, "no such connected instance")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), remoteCtxKey{}, rem)))
	})
}

// ---------- connections ----------

type remoteOut struct {
	store.Remote
	Host string `json:"host"`
}

func outRemote(rem store.Remote) remoteOut {
	u, _ := url.Parse(rem.URL)
	h := rem.URL
	if u != nil {
		h = u.Host
	}
	return remoteOut{Remote: rem, Host: h}
}

func (s *Server) handleListRemotes(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.Remotes(r.Context(), me(r))
	if err != nil {
		s.fail(w, err, "connected instances")
		return
	}
	out := make([]remoteOut, 0, len(list))
	for _, rem := range list {
		out = append(out, outRemote(rem))
	}
	writeJSON(w, http.StatusOK, out)
}

type remoteIn struct {
	Name  *string `json:"name"`
	URL   *string `json:"url"`
	Token *string `json:"token"`
	Proxy *string `json:"proxy"`
}

// cleanRemoteURL: https://host[/base], without the API path or a trailing slash.
func cleanRemoteURL(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v != "" && !strings.Contains(v, "://") {
		v = "https://" + v
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", badRequest("the address is the other instance's web address, like https://projects.example.com")
	}
	p := strings.TrimRight(u.Path, "/")
	for _, cut := range []string{"/api/v1", "/api"} {
		p = strings.TrimSuffix(p, cut)
	}
	return u.Scheme + "://" + u.Host + p, nil
}

func cleanProxy(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	u, err := url.Parse(v)
	if err != nil || u.Host == "" || (u.Scheme != "socks5" && u.Scheme != "socks5h" && u.Scheme != "http" && u.Scheme != "https") {
		return "", badRequest("the proxy is socks5://host:port or http://host:port")
	}
	return v, nil
}

// checkRemote asks the other instance who the token is; the connection is
// only saved when it answers and the token is a person's there.
func (s *Server) checkRemote(ctx context.Context, rem *store.Remote) error {
	var st struct {
		Authenticated bool `json:"authenticated"`
		User          *struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"user"`
		Version string `json:"version"`
	}
	if err := s.remoteCall(ctx, rem, http.MethodGet, "/api/v1/auth/status", nil, &st); err != nil {
		return badRequest(err.Error())
	}
	if !st.Authenticated || st.User == nil {
		return badRequest(rem.Host() + " did not accept the token: make one there in Settings → API tokens")
	}
	rem.RemoteUser = strings.TrimSpace(st.User.Name + " <" + st.User.Email + ">")
	return nil
}

func (s *Server) handleCreateRemote(w http.ResponseWriter, r *http.Request) {
	var in remoteIn
	if err := decode(r, &in); err != nil || in.URL == nil || in.Token == nil {
		writeErr(w, http.StatusBadRequest, "url and token are required")
		return
	}
	rem := &store.Remote{UserID: me(r)}
	if err := s.applyRemote(rem, in); err != nil {
		s.fail(w, err, "connection")
		return
	}
	if err := s.checkRemote(r.Context(), rem); err != nil {
		s.fail(w, err, "connection")
		return
	}
	if err := s.store.SaveRemote(r.Context(), rem); err != nil {
		s.fail(w, err, "connection")
		return
	}
	writeJSON(w, http.StatusCreated, struct {
		remoteOut
		SameKeys []string `json:"same_keys"`
	}{outRemote(*rem), s.sameTeamKeys(r.Context(), rem)})
}

func (s *Server) applyRemote(rem *store.Remote, in remoteIn) error {
	if in.URL != nil {
		u, err := cleanRemoteURL(*in.URL)
		if err != nil {
			return err
		}
		rem.URL = u
	}
	if in.Token != nil && strings.TrimSpace(*in.Token) != "" {
		rem.Token = strings.TrimSpace(*in.Token)
	}
	if in.Proxy != nil {
		p, err := cleanProxy(*in.Proxy)
		if err != nil {
			return err
		}
		rem.Proxy = p
	}
	if in.Name != nil {
		rem.Name = strings.TrimSpace(*in.Name)
	}
	if rem.Token == "" {
		return badRequest("the token is required")
	}
	if rem.Name == "" {
		rem.Name = rem.Host()
	}
	if len(rem.Name) > 60 {
		return badRequest("keep the name under 60 characters")
	}
	return nil
}

func (s *Server) handlePatchRemote(w http.ResponseWriter, r *http.Request) {
	var in remoteIn
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	rem, err := s.store.GetRemote(r.Context(), idParam(r, "id"), me(r))
	if err != nil {
		s.fail(w, err, "connection")
		return
	}
	if err := s.applyRemote(rem, in); err != nil {
		s.fail(w, err, "connection")
		return
	}
	if err := s.checkRemote(r.Context(), rem); err != nil {
		s.fail(w, err, "connection")
		return
	}
	if err := s.store.SaveRemote(r.Context(), rem); err != nil {
		s.fail(w, err, "connection")
		return
	}
	writeJSON(w, http.StatusOK, outRemote(*rem))
}

func (s *Server) handleDeleteRemote(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteRemote(r.Context(), idParam(r, "id"), me(r)); err != nil {
		s.fail(w, err, "connection")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- talking to the other instance ----------

var remoteTransports sync.Map // proxy address → *http.Transport

// remoteTransport keeps connections to the other instances alive; a proxy
// (socks5://…) reaches one this server has no route to.
func remoteTransport(proxy string) http.RoundTripper {
	if t, ok := remoteTransports.Load(proxy); ok {
		return t.(*http.Transport)
	}
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.ResponseHeaderTimeout = 60 * time.Second
	if proxy != "" {
		if u, err := url.Parse(proxy); err == nil {
			t.Proxy = http.ProxyURL(u)
		}
	}
	got, _ := remoteTransports.LoadOrStore(proxy, t)
	return got.(*http.Transport)
}

// remoteCall is a JSON request to the other instance's API as the person.
func (s *Server) remoteCall(ctx context.Context, rem *store.Remote, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, rem.URL+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+rem.Token)
	req.Header.Set("X-Pocket-Source", "projects-remote")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := (&http.Client{Transport: remoteTransport(rem.Proxy)}).Do(req)
	if err != nil {
		return fmt.Errorf("%s can't be reached from this server (%v)", rem.Host(), errors.Unwrap(err))
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		var e apiError
		_ = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&e)
		return remoteError{status: res.StatusCode, msg: rem.Host() + ": " + firstNonEmpty(e.Error, res.Status)}
	}
	if out == nil || res.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// remoteError keeps the other instance's answer (404 stays a 404 here).
type remoteError struct {
	status int
	msg    string
}

func (e remoteError) Error() string { return e.msg }

// proxyRemote passes a web app call on to the other instance as the person.
// Addresses of its files (/api/v1/attachments/…) are rewritten both ways so
// pictures load through here and saved text keeps the other instance's own.
func (s *Server) proxyRemote(w http.ResponseWriter, r *http.Request) {
	rem := remoteFrom(r.Context())
	target, err := url.Parse(rem.URL)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "the connection's address is broken")
		return
	}
	prefix := "/r/" + strconv.FormatInt(rem.ID, 10)
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme, pr.Out.URL.Host = target.Scheme, target.Host
			pr.Out.URL.Path = strings.TrimRight(target.Path, "/") + strings.TrimPrefix(pr.In.URL.Path, prefix)
			pr.Out.URL.RawPath = ""
			pr.Out.Host = target.Host
			for _, h := range []string{"Cookie", "Authorization", "X-Api-Key", "Accept-Encoding", "Origin", "Referer"} {
				pr.Out.Header.Del(h)
			}
			pr.Out.Header.Set("Authorization", "Bearer "+rem.Token)
			if pr.In.Header.Get("X-Pocket-Source") != "" {
				pr.Out.Header.Set("X-Pocket-Workspace", "0") // the workspace was said: this one, over there
			}
			if isJSON(pr.Out.Header.Get("Content-Type")) && pr.Out.Body != nil {
				b, err := io.ReadAll(io.LimitReader(pr.Out.Body, 32<<20))
				if err == nil {
					b = unprefixRemote(b, prefix)
					pr.Out.Body = io.NopCloser(bytes.NewReader(b))
					pr.Out.ContentLength = int64(len(b))
					pr.Out.Header.Set("Content-Length", strconv.Itoa(len(b)))
				}
			}
		},
		Transport:     remoteTransport(rem.Proxy),
		FlushInterval: -1,
		ModifyResponse: func(res *http.Response) error {
			res.Header.Del("Set-Cookie") // its session cookies are not this server's
			if res.StatusCode == http.StatusUnauthorized {
				// the web app reads 401 as "signed out here": it is the token over there
				res.Body.Close()
				b, _ := json.Marshal(apiError{Error: rem.Host() + " no longer accepts your token: put a new one in Settings → Connected instances"})
				res.StatusCode, res.Status = http.StatusBadGateway, "502 Bad Gateway"
				res.Body = io.NopCloser(bytes.NewReader(b))
				res.ContentLength = int64(len(b))
				res.Header.Set("Content-Type", "application/json; charset=utf-8")
				res.Header.Set("Content-Length", strconv.Itoa(len(b)))
				return nil
			}
			if !isJSON(res.Header.Get("Content-Type")) {
				return nil
			}
			b, err := io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil {
				return err
			}
			b = prefixRemote(b, prefix)
			res.Body = io.NopCloser(bytes.NewReader(b))
			res.ContentLength = int64(len(b))
			res.Header.Set("Content-Length", strconv.Itoa(len(b)))
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			s.log.Warn("connected instance", "host", rem.Host(), "path", r.URL.Path, "err", err)
			writeErr(w, http.StatusBadGateway, rem.Host()+" can't be reached from this server right now")
		},
	}
	rp.ServeHTTP(w, r)
}

func isJSON(ct string) bool { return strings.HasPrefix(strings.ToLower(ct), "application/json") }

// the other instance's file addresses, as the web app here must ask for them
var remotePaths = []string{"/api/v1/attachments/", "/api/v1/draw/"}

func prefixRemote(b []byte, prefix string) []byte {
	for _, p := range remotePaths {
		b = bytes.ReplaceAll(b, []byte(p), []byte(prefix+p))
	}
	// a prefixed one that came back prefixed again (text saved through here before the fix) stays single
	return bytes.ReplaceAll(b, []byte(prefix+prefix), []byte(prefix))
}

func unprefixRemote(b []byte, prefix string) []byte {
	for _, p := range remotePaths {
		b = bytes.ReplaceAll(b, []byte(prefix+p), []byte(p))
	}
	return b
}

// ---------- an issue, here or there ----------

// issueInfo is what the person's own parts (Pocket Notes, Pocket AI) need of
// an issue: one of this instance's, or one on a connected instance.
type issueInfo struct {
	Ident, Title, URL, NotesFolder string
	TeamID                         int64 // here
	Local                          *store.Issue
	Remote                         *remoteIssue
}

// remoteIssue is the part of the other instance's issue page that is read here.
type remoteIssue struct {
	ID          int64  `json:"id"`
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Priority    int    `json:"priority"`
	StateType   string `json:"state_type"`
	BranchName  string `json:"branch_name"`
	Team        struct {
		Key         string `json:"key"`
		Name        string `json:"name"`
		NotesFolder string `json:"notes_folder"`
	} `json:"team"`
	Comments []struct {
		Body      string `json:"body"`
		CreatedAt string `json:"created_at"`
		UserID    *int64 `json:"user_id"`
	} `json:"comment_list"`
	Attachments []struct {
		Kind     string `json:"kind"`
		Title    string `json:"title"`
		Subtitle string `json:"subtitle"`
		URL      string `json:"url"`
		State    string `json:"state"`
	} `json:"attachment_list"`
	SharedNotes []struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
	} `json:"shared_notes"`
}

func (s *Server) issueInfoOf(r *http.Request, need int) (*issueInfo, error) {
	ref := chi.URLParam(r, "ref")
	if rem := remoteFrom(r.Context()); rem != nil {
		var ri remoteIssue
		if err := s.remoteCall(r.Context(), rem, http.MethodGet, "/api/v1/issues/"+url.PathEscape(ref), nil, &ri); err != nil {
			return nil, err
		}
		return &issueInfo{Ident: ri.Identifier, Title: ri.Title, URL: firstNonEmpty(ri.URL, rem.URL+"/issue/"+ri.Identifier),
			NotesFolder: firstNonEmpty(ri.Team.NotesFolder, defaultNotesFolder), Remote: &ri}, nil
	}
	is, err := s.loadIssue(r, s.accessOf(r), ref, need)
	if err != nil {
		return nil, err
	}
	return &issueInfo{Ident: is.Identifier, Title: is.Title, URL: s.issueURL(r, is.Identifier),
		NotesFolder: s.teamNotesFolder(r.Context(), is.TeamID), TeamID: is.TeamID, Local: is}, nil
}

// remoteRef is how Pocket AI knows a connected instance's issue: host/ATH-12,
// never mixed up with this instance's ATH-12.
func remoteRef(rem *store.Remote, ident string) string { return rem.Host() + "/" + ident }

// ---------- which workspace an agent means ----------

// Once the person connected another instance, every change a tool call makes
// here (Pocket Core's MCP gateway, X-Pocket-Source: mcp) must say which
// workspace it means: X-Pocket-Workspace, set by Core when the tool got
// workspace (0 = this instance). Without it nothing is changed: a number, a
// team key or even an identifier like WEB-12 can name something over there
// too, and an agent that forgot the workspace would create or change it here.
// Calls through /r/{id}/… are explicit (that router isn't behind this guard).
func (s *Server) workspaceGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-Pocket-Source") == "mcp" &&
			r.Header.Get("X-Pocket-Workspace") == "" && s.store.HasRemotes(r.Context(), me(r)) {
			writeErr(w, http.StatusConflict, "You connected other Pocket Projects workspaces, so every change must say which one; nothing was changed. "+
				"Pass workspace: 0 for your own instance, or an id from pocket_projects_workspaces.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sameTeamKeys: the person's team keys here that a team over there uses too
// (WEB-12 then names an issue in both; worth knowing when connecting).
func (s *Server) sameTeamKeys(ctx context.Context, rem *store.Remote) []string {
	var there []struct {
		Key string `json:"key"`
	}
	if err := s.remoteCall(ctx, rem, http.MethodGet, "/api/v1/teams", nil, &there); err != nil {
		return nil
	}
	mine, _ := s.store.ListTeams(ctx, rem.UserID)
	keys := map[string]bool{}
	for _, t := range mine {
		keys[strings.ToUpper(t.Key)] = true
	}
	out := []string{}
	for _, t := range there {
		if k := strings.ToUpper(t.Key); keys[k] {
			out = append(out, k)
			delete(keys, k)
		}
	}
	return out
}
