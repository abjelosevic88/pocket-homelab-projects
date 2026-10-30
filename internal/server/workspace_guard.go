package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// ---------- which workspace an agent means ----------
//
// A tool call from Pocket Core's MCP gateway (X-Pocket-Source: mcp) names its
// workspace by address in X-Pocket-Workspace: this instance's own host
// (projects.example.com) or the host of one of the person's connections.
// Once the person connected another instance:
//
//   - every change must say its workspace (nothing is changed otherwise);
//   - a change must name what it changes so that a wrong workspace finds
//     nothing: issues by identifier (ATH-12, never a bare number), new issues
//     and team memory by team key, new projects by team keys, a project by its
//     number AND its exact name;
//   - the team key must exist in the workspace said, and a key used in two of
//     the person's workspaces is off limits to agents until one is renamed.
//
// Calls for a connection are then answered by /r/{id}/… as if the web app
// had asked. Reads only need a known workspace; people are never affected.

var (
	agentIssuePath   = regexp.MustCompile(`^/api/v1/issues/([^/]+)(/|$)`)
	agentTeamPath    = regexp.MustCompile(`^/api/v1/teams/([^/]+)(/|$)`)
	agentProjectPath = regexp.MustCompile(`^/api/v1/projects/([0-9]+)(/|$)`)
	issueIdentRe     = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]{0,6})-[1-9][0-9]*$`)
	teamKeyRe        = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,6}$`)
)

// ownAddress is the host this instance is known by (its links say it).
func (s *Server) ownAddress(r *http.Request) string {
	if u, err := url.Parse(s.cfg.BaseURL); err == nil && u.Host != "" {
		return strings.ToLower(u.Host)
	}
	return strings.ToLower(r.Host)
}

func (s *Server) agentWorkspace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isAgentCall(r) {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/r/") {
			// only this guard sends an agent to a connection, after its checks
			writeErr(w, http.StatusConflict, "Say the workspace instead (workspace: its address). Nothing was changed.")
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/v1/") {
			next.ServeHTTP(w, r)
			return
		}
		u, err := s.resolveUser(r)
		if err != nil || u == nil {
			next.ServeHTTP(w, r) // the route's own auth answers
			return
		}
		ctx := r.Context()
		said := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Pocket-Workspace")))
		own := s.ownAddress(r)
		remotes, _ := s.store.Remotes(ctx, u.ID)
		write := r.Method != http.MethodGet && r.Method != http.MethodHead
		refuse := func(msg string) { writeErr(w, http.StatusConflict, msg+" Nothing was changed.") }
		if len(remotes) == 0 {
			if said != "" && said != own {
				writeErr(w, http.StatusNotFound, "no workspace "+said+": this is "+own+" and nothing else is connected")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		active := s.agentActive(ctx, u.ID, own, remotes)
		if said == "" {
			if write {
				refuse("You connected other Pocket Projects workspaces, so every change must say which one: pass workspace (" + active + " is the one Pocket AI works in now).")
				return
			}
			said = active
		}
		var rem *store.Remote
		if said != own {
			for i := range remotes {
				if strings.ToLower(remotes[i].Host()) == said {
					rem = &remotes[i]
				}
			}
			if rem == nil {
				writeErr(w, http.StatusNotFound, "no workspace "+said+": pocket_projects_workspaces lists yours")
				return
			}
		}
		if said != active && s.oneWorkspace(ctx, u.ID) {
			// one workspace at a time: the others are closed to agents, reads too
			writeErr(w, http.StatusConflict, "Pocket AI may use only the workspace open in Pocket Projects, "+active+" right now, so "+said+" is closed to it (reads too). "+
				"The person opens "+said+" in Pocket Projects to use it. Nothing was changed.")
			return
		}
		if write {
			if msg := s.agentTarget(r, u, own, said, rem, remotes); msg != "" {
				refuse(msg)
				return
			}
		}
		if rem != nil {
			r.URL.Path = "/r/" + strconv.FormatInt(rem.ID, 10) + r.URL.Path
			r.URL.RawPath = ""
		}
		next.ServeHTTP(w, r)
	})
}

// agentTarget checks that a change names its target so that the workspace it
// said has it (and no other of the person's workspaces does); "" = fine.
func (s *Server) agentTarget(r *http.Request, u *store.User, own, said string, rem *store.Remote, remotes []store.Remote) string {
	ctx := r.Context()
	var body map[string]any
	if r.Body != nil {
		b, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		r.Body = io.NopCloser(bytes.NewReader(b))
		_ = json.Unmarshal(b, &body)
	}
	str := func(k string) string { v, _ := body[k].(string); return strings.TrimSpace(v) }
	keys := []string{}
	p := r.URL.Path
	switch {
	case agentIssuePath.MatchString(p):
		ref := agentIssuePath.FindStringSubmatch(p)[1]
		m := issueIdentRe.FindStringSubmatch(ref)
		if m == nil {
			return "Name the issue by its identifier (like ATH-12), not a number: the same number is another issue in your other workspaces."
		}
		keys = append(keys, m[1])
	case r.Method == http.MethodPost && p == "/api/v1/issues":
		t := str("team")
		if !teamKeyRe.MatchString(t) {
			return "Say the team of the new issue by its key (team, like ATH): you have several workspaces."
		}
		keys = append(keys, t)
	case agentTeamPath.MatchString(p):
		t := agentTeamPath.FindStringSubmatch(p)[1]
		if !teamKeyRe.MatchString(t) {
			return "Name the team by its key (like ATH), not a number."
		}
		keys = append(keys, t)
	case r.Method == http.MethodPost && p == "/api/v1/projects":
		list, _ := body["teams"].([]any)
		if len(list) == 0 || body["team_ids"] != nil {
			return "Say the teams of the new project by their keys (teams, like [\"ATH\"]), not team_ids: you have several workspaces."
		}
		for _, v := range list {
			t, _ := v.(string)
			if !teamKeyRe.MatchString(t) {
				return "Team keys look like ATH."
			}
			keys = append(keys, t)
		}
		// the workspace's own team ids for those keys (any version over there takes team_ids)
		ids, msg := s.teamIDsIn(ctx, u, rem, keys)
		if msg != "" {
			return msg
		}
		delete(body, "teams")
		body["team_ids"] = ids
		b, _ := json.Marshal(body)
		r.Body = io.NopCloser(bytes.NewReader(b))
		r.ContentLength = int64(len(b))
		r.Header.Set("Content-Length", strconv.Itoa(len(b)))
	case agentProjectPath.MatchString(p):
		id := agentProjectPath.FindStringSubmatch(p)[1]
		name := str("project")
		if name == "" {
			return "Add project with the project's exact name: a project number alone can mean another project in your other workspaces."
		}
		got, err := s.projectNameIn(ctx, r, u, rem, id)
		if err != nil || !strings.EqualFold(strings.TrimSpace(got), name) {
			return "Project " + id + " in " + said + " is not \"" + name + "\": check the workspace and the project."
		}
		return ""
	}
	if pid := str("parent_id"); pid != "" {
		m := issueIdentRe.FindStringSubmatch(pid)
		if m == nil {
			return "Name the parent issue by its identifier (like ATH-12), not a number."
		}
		keys = append(keys, m[1])
	}
	if len(keys) == 0 {
		return ""
	}
	// where each key exists among the person's workspaces
	where := map[string][]string{}
	mine, _ := s.store.ListTeams(ctx, u.ID)
	for _, t := range mine {
		k := strings.ToUpper(t.Key)
		where[k] = append(where[k], own)
	}
	for i := range remotes {
		ks := remotes[i].TeamKeys
		if rem != nil && remotes[i].ID == rem.ID {
			// the workspace said: ask it, it may have a team since the last poll
			if fresh := s.remoteTeamKeys(ctx, &remotes[i]); fresh != "" {
				ks = fresh
			}
		}
		for _, k := range strings.Split(ks, ",") {
			if k = strings.ToUpper(strings.TrimSpace(k)); k != "" {
				where[k] = append(where[k], strings.ToLower(remotes[i].Host()))
			}
		}
	}
	for _, k := range keys {
		k = strings.ToUpper(k)
		in := where[k]
		has := false
		for _, h := range in {
			has = has || h == said
		}
		if !has {
			if len(in) > 0 {
				return "There is no team " + k + " in " + said + "; it is in " + strings.Join(in, ", ") + ". Check the workspace."
			}
			return "There is no team " + k + " in " + said + "."
		}
		if len(in) > 1 {
			return "Team " + k + " exists in " + strings.Join(in, " and ") + ", so an agent can't be sure which one it changes. Rename one of them, or make this change yourself."
		}
	}
	return ""
}

// remoteTeamKeys asks the connection for the person's team keys there and
// remembers them; "" when it can't be reached.
func (s *Server) remoteTeamKeys(ctx context.Context, rem *store.Remote) string {
	var there []struct {
		Key string `json:"key"`
	}
	if err := s.remoteCall(ctx, rem, http.MethodGet, "/api/v1/teams", nil, &there); err != nil {
		return ""
	}
	keys := []string{}
	for _, t := range there {
		keys = append(keys, strings.ToUpper(t.Key))
	}
	joined := strings.Join(keys, ",")
	if joined != rem.TeamKeys {
		_ = s.store.SetRemoteTeamKeys(ctx, rem.ID, joined)
		rem.TeamKeys = joined
	}
	return joined
}

// projectNameIn is the name of project id in the workspace (rem nil = here).
func (s *Server) projectNameIn(ctx context.Context, r *http.Request, u *store.User, rem *store.Remote, id string) (string, error) {
	var p struct {
		Name string `json:"name"`
	}
	if rem != nil {
		err := s.remoteCall(ctx, rem, http.MethodGet, "/api/v1/projects/"+id, nil, &p)
		return p.Name, err
	}
	pid, _ := strconv.ParseInt(id, 10, 64)
	pr, err := s.store.GetProject(ctx, pid)
	if err != nil {
		return "", err
	}
	a := s.accessFor(ctx, u.ID)
	if !s.visibleProject(ctx, a, pr.ID) {
		return "", store.ErrNotFound
	}
	return pr.Name, nil
}

// sameTeamKeys: the person's team keys here that a team over there uses too.
func (s *Server) sameTeamKeys(ctx context.Context, rem *store.Remote) []string {
	there := s.remoteTeamKeys(ctx, rem)
	mine, _ := s.store.ListTeams(ctx, rem.UserID)
	keys := map[string]bool{}
	for _, t := range mine {
		keys[strings.ToUpper(t.Key)] = true
	}
	out := []string{}
	for _, k := range strings.Split(there, ",") {
		if k != "" && keys[k] {
			out = append(out, k)
			delete(keys, k)
		}
	}
	return out
}

// ---------- the one workspace Pocket AI works in ----------

const agentWorkspaceKey = "agent.workspace" // a connection's host, "" = this instance
const agentOneKey = "agent.one_workspace"   // "0" = agents may use every workspace; on otherwise

// oneWorkspace: whether agents may use only the workspace open in Projects
// (Settings → Connected instances; on unless the person switched it off).
func (s *Server) oneWorkspace(ctx context.Context, uid int64) bool {
	return s.store.UserSetting(ctx, uid, agentOneKey) != "0"
}

// agentActive is the workspace agents may use: the person's choice while that
// connection exists, else this instance.
func (s *Server) agentActive(ctx context.Context, uid int64, own string, remotes []store.Remote) string {
	v := strings.ToLower(s.store.UserSetting(ctx, uid, agentWorkspaceKey))
	for i := range remotes {
		if v != "" && strings.ToLower(remotes[i].Host()) == v {
			return v
		}
	}
	return own
}

type workspaceOut struct {
	Address string `json:"address"`
	Name    string `json:"name"`
	ID      int64  `json:"id,omitempty"` // the connection (/r/{id}); 0 = this instance
	Own     bool   `json:"own"`
	Active  bool   `json:"active"` // Pocket AI may work here (every workspace when the rule is off)
	Open    bool   `json:"open"`   // the one open in Projects now
	Unread  int    `json:"unread"`
}

// handleWorkspaces: this instance and the person's connections, by address,
// with the one Pocket AI works in (pocket_projects_workspaces, the switcher).
func (s *Server) handleWorkspaces(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	remotes, err := s.store.Remotes(ctx, me(r))
	if err != nil {
		s.fail(w, err, "workspaces")
		return
	}
	own := s.ownAddress(r)
	open := s.agentActive(ctx, me(r), own, remotes)
	all := !s.oneWorkspace(ctx, me(r))
	out := []workspaceOut{{Address: own, Name: "Your own", Own: true, Open: open == own, Active: all || open == own}}
	for _, rem := range remotes {
		h := strings.ToLower(rem.Host())
		out = append(out, workspaceOut{Address: h, Name: rem.Name, ID: rem.ID, Open: open == h, Active: all || open == h, Unread: rem.Unread})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAgentRule: GET/PUT {one_workspace} — whether agents may use only the
// workspace open in Projects. Only the person changes it, never an agent.
func (s *Server) handleAgentRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method == http.MethodPut {
		if isAgentCall(r) || s.linkApp(r) != "" {
			writeErr(w, http.StatusForbidden, "only the person changes this, in Pocket Projects itself")
			return
		}
		var in struct {
			OneWorkspace bool `json:"one_workspace"`
		}
		if err := decode(r, &in); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		v := "0"
		if in.OneWorkspace {
			v = "1"
		}
		if err := s.store.SetUserSetting(ctx, me(r), agentOneKey, v); err != nil {
			s.fail(w, err, "setting")
			return
		}
		s.send(ctx, change{Kind: "remotes", users: []int64{me(r)}})
	}
	writeJSON(w, http.StatusOK, map[string]bool{"one_workspace": s.oneWorkspace(ctx, me(r))})
}

// handleSetAgentWorkspace: PUT {address}, sent by the web app whenever the
// person opens a workspace (the one open in Projects is the one Pocket AI
// works in). Only the person, never an agent: an agent that could pick its
// workspace would make the choice pointless.
func (s *Server) handleSetAgentWorkspace(w http.ResponseWriter, r *http.Request) {
	if isAgentCall(r) || s.linkApp(r) != "" {
		writeErr(w, http.StatusForbidden, "only the person chooses where Pocket AI works, in Pocket Projects itself")
		return
	}
	var in struct {
		Address string `json:"address"`
		ID      *int64 `json:"id"` // the web app's workspace: 0 = this instance, else /r/{id}
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ctx := r.Context()
	addr := strings.ToLower(strings.TrimSpace(in.Address))
	remotes, _ := s.store.Remotes(ctx, me(r))
	if in.ID != nil {
		addr = s.ownAddress(r)
		for _, rem := range remotes {
			if rem.ID == *in.ID {
				addr = strings.ToLower(rem.Host())
			}
		}
		if *in.ID != 0 && addr == s.ownAddress(r) {
			writeErr(w, http.StatusNotFound, "no such connected instance")
			return
		}
	}
	val := ""
	if addr != s.ownAddress(r) {
		for _, rem := range remotes {
			if strings.ToLower(rem.Host()) == addr {
				val = addr
			}
		}
		if val == "" {
			writeErr(w, http.StatusNotFound, "no workspace "+addr)
			return
		}
	}
	if s.store.UserSetting(ctx, me(r), agentWorkspaceKey) != val {
		if err := s.store.SetUserSetting(ctx, me(r), agentWorkspaceKey, val); err != nil {
			s.fail(w, err, "workspace")
			return
		}
		s.send(ctx, change{Kind: "remotes", users: []int64{me(r)}})
	}
	s.handleWorkspaces(w, r)
}

// codeHere refuses Code with Pocket AI on an issue whose workspace Pocket AI
// doesn't work in (its tools couldn't reach the ticket); the web app offers
// to switch (switch_to).
func (s *Server) codeHere(w http.ResponseWriter, r *http.Request, addr string) bool {
	ctx := r.Context()
	remotes, _ := s.store.Remotes(ctx, me(r))
	if len(remotes) == 0 {
		return true
	}
	active := s.agentActive(ctx, me(r), s.ownAddress(r), remotes)
	if strings.EqualFold(addr, active) || !s.oneWorkspace(ctx, me(r)) {
		return true
	}
	writeJSON(w, http.StatusConflict, map[string]any{"error": "Pocket AI works in " + active + " right now; switch it to " + addr + " to code on this issue.", "switch_to": addr})
	return false
}

// isAgentCall: a tool call through Pocket Core's MCP gateway (it says so, and
// its delegated tokens are pcu_mcp_…, whatever header is sent with them).
func isAgentCall(r *http.Request) bool {
	h := r.Header.Get("Authorization")
	return r.Header.Get("X-Pocket-Source") == "mcp" || (len(h) > 7 && strings.HasPrefix(strings.TrimSpace(h[7:]), "pcu_mcp_"))
}

// teamIDsIn maps team keys to the team ids of the workspace (rem nil = here).
func (s *Server) teamIDsIn(ctx context.Context, u *store.User, rem *store.Remote, keys []string) ([]int64, string) {
	byKey := map[string]int64{}
	if rem != nil {
		var there []struct {
			ID  int64  `json:"id"`
			Key string `json:"key"`
		}
		if err := s.remoteCall(ctx, rem, http.MethodGet, "/api/v1/teams", nil, &there); err != nil {
			return nil, rem.Host() + " can't be reached right now."
		}
		for _, t := range there {
			byKey[strings.ToUpper(t.Key)] = t.ID
		}
	} else {
		mine, _ := s.store.ListTeams(ctx, u.ID)
		for _, t := range mine {
			byKey[strings.ToUpper(t.Key)] = t.ID
		}
	}
	ids := []int64{}
	for _, k := range keys {
		id, ok := byKey[strings.ToUpper(k)]
		if !ok {
			return nil, "There is no team " + strings.ToUpper(k) + " in that workspace."
		}
		ids = append(ids, id)
	}
	return ids, ""
}

// workspaceLink sends /r/<address>/… (links Pocket Core builds from a tool
// call's workspace) to the person's /r/{id}/… or, for this instance, to /….
func (s *Server) workspaceLink(w http.ResponseWriter, r *http.Request) bool {
	m := workspaceLinkRe.FindStringSubmatch(r.URL.Path)
	if m == nil {
		return false
	}
	addr, rest := strings.ToLower(m[1]), m[2]
	if rest == "" {
		rest = "/"
	}
	if addr == s.ownAddress(r) {
		http.Redirect(w, r, rest, http.StatusFound)
		return true
	}
	u, err := s.resolveUser(r)
	if err != nil || u == nil {
		return false // the web app signs in first, then comes back here
	}
	remotes, _ := s.store.Remotes(r.Context(), u.ID)
	for _, rem := range remotes {
		if strings.ToLower(rem.Host()) == addr {
			http.Redirect(w, r, "/r/"+strconv.FormatInt(rem.ID, 10)+rest, http.StatusFound)
			return true
		}
	}
	return false
}

var workspaceLinkRe = regexp.MustCompile(`^/r/([a-zA-Z0-9.-]*[a-zA-Z.-][a-zA-Z0-9.-]*(?::[0-9]{1,5})?)(/.*)?$`)
