package server

import (
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// What links to a thing in another app: Pocket Draw asks
// GET /api/v1/mentions?url=<drawing page> (Core link scope projects:read) to
// show the issues and projects a drawing is used in. An issue mentions an
// address when its description or one of its comments links there; a project
// when its description, an update or a comment on one does. The matching is
// Pocket Notes' (docs/notes-from-apps.md there): the same address, one below
// it (a drawing's /d/12/picture.svg is under /d/12) or with a query of its
// own; host case, scheme, fragment and a trailing slash don't matter.

var mentionURLRe = regexp.MustCompile(`https?://[^\s<>()\[\]"'` + "`" + `]+`)

// itemKey is an address as compared here: host in lower case, then path,
// then query; no scheme, fragment or trailing slash ("" if it is no web address).
func itemKey(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	k := strings.ToLower(u.Host) + strings.TrimRight(u.EscapedPath(), "/")
	if u.RawQuery != "" {
		k += "?" + u.RawQuery
	}
	return k
}

// linksTo reports whether text links to the item: the same address, or one
// below it or with a query of its own.
func linksTo(text string, keys []string) bool {
	for _, m := range mentionURLRe.FindAllString(text, -1) {
		k := itemKey(strings.TrimRight(m, ".,;:!?"))
		if k == "" {
			continue
		}
		for _, want := range keys {
			if k == want || strings.HasPrefix(k, want+"?") || strings.HasPrefix(k, want+"/") {
				return true
			}
		}
	}
	return false
}

func (s *Server) handleMentions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var keys, needles []string
	for _, raw := range r.URL.Query()["url"] {
		k := itemKey(raw)
		if k == "" {
			continue
		}
		keys = append(keys, k)
		// The text is searched for host and path; the exact match is made above.
		needles = append(needles, strings.SplitN(k, "?", 2)[0])
	}
	if len(keys) == 0 || len(keys) > 20 {
		writeErr(w, http.StatusBadRequest, "give one to 20 web addresses as url=")
		return
	}
	a := s.accessOf(r)
	texts, err := s.store.LinkTexts(ctx, a.ids(), needles, 1000)
	if err != nil {
		s.fail(w, err, "mentions")
		return
	}
	issues, projects := map[int64]bool{}, map[int64]bool{}
	for _, t := range texts {
		if (t.Kind == "issue" && issues[t.ID]) || (t.Kind == "project" && projects[t.ID]) || !linksTo(t.Text, keys) {
			continue
		}
		if t.Kind == "issue" {
			issues[t.ID] = true
		} else {
			projects[t.ID] = true
		}
	}
	teamName := map[int64]string{}
	if teams, err := s.store.ListTeams(ctx, a.uid); err == nil {
		for _, t := range teams {
			teamName[t.ID] = t.Name
		}
	}
	out := []map[string]any{}
	for id := range issues {
		is, err := s.store.GetIssue(ctx, id, a.uid)
		if err != nil || !a.can(is.TeamID, needRead) || is.DeletedAt != nil {
			continue
		}
		out = append(out, map[string]any{"kind": "issue", "id": is.ID, "identifier": is.Identifier, "title": is.Title,
			"url": s.issueURL(r, is.Identifier), "team": teamName[is.TeamID], "updated_at": is.UpdatedAt})
	}
	for id := range projects {
		p, err := s.store.GetProject(ctx, id)
		if err != nil || !a.canAny(p.TeamIDs, needRead) || p.DeletedAt != nil {
			continue
		}
		var names []string
		for _, t := range p.TeamIDs {
			if n := teamName[t]; n != "" && a.can(t, needRead) {
				names = append(names, n)
			}
		}
		out = append(out, map[string]any{"kind": "project", "id": p.ID, "title": p.Name,
			"url": s.projectURL(r, p.ID), "team": strings.Join(names, ", "), "updated_at": p.UpdatedAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["updated_at"].(string) > out[j]["updated_at"].(string) })
	if len(out) > 50 {
		out = out[:50]
	}
	writeJSON(w, http.StatusOK, out)
}
