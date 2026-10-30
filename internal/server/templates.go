package server

import (
	"net/http"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
	"github.com/go-chi/chi/v5"
)

// A person's own wording for Code with Pocket AI's actions in one team
// (Team settings → Pocket AI → Just for you): it replaces the built-in ask at
// the top of the first message; the issue itself always follows. Placeholders:
// {{id}} WEB-12, {{title}}, {{branch}}, {{url}}.

var templateActions = []string{"investigate", "fix", "chat"}

const maxTemplate = 8000

func fillTemplate(t string, ci codeIssue, branch string) string {
	return strings.NewReplacer("{{id}}", ci.Identifier, "{{title}}", ci.Title, "{{branch}}", branch, "{{url}}", ci.URL).Replace(t)
}

// templateDefaults: the built-in wording, with placeholders, to start from.
func templateDefaults(linkRefs bool) map[string]string {
	out := map[string]string{}
	for _, a := range templateActions {
		t := codeAsk[a]
		if a == "fix" && !linkRefs {
			t = codeAsk["fix-plain"]
		}
		out[a] = strings.NewReplacer("%[1]s", "{{id}}", "%[2]s", "{{branch}}").Replace(t)
	}
	return out
}

func templatesView(own map[string]string, linkRefs bool) map[string]any {
	return map[string]any{"templates": own, "defaults": templateDefaults(linkRefs), "placeholders": []string{"{{id}}", "{{title}}", "{{branch}}", "{{url}}"}}
}

// readTemplates takes {investigate, fix, chat}: text, "" = the built-in one.
func readTemplates(w http.ResponseWriter, r *http.Request) (map[string]string, bool) {
	var in map[string]*string
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return nil, false
	}
	out := map[string]string{}
	for _, a := range templateActions {
		if v := in[a]; v != nil {
			if len(*v) > maxTemplate {
				writeErr(w, http.StatusBadRequest, "keep each template under 8000 characters")
				return nil, false
			}
			out[a] = *v
		}
	}
	for k := range in {
		if _, ok := codeActions[k]; !ok {
			writeErr(w, http.StatusBadRequest, "templates are for investigate, fix and chat")
			return nil, false
		}
	}
	return out, true
}

func (s *Server) handleGetTemplates(w http.ResponseWriter, r *http.Request) {
	t, err := s.loadTeam(r, s.accessOf(r), chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	tai, err := s.store.GetTeamAI(r.Context(), t.ID)
	if err != nil {
		s.fail(w, err, "team ai")
		return
	}
	own, err := s.store.ActionTemplates(r.Context(), me(r), store.TemplateScope(t.ID))
	if err != nil {
		s.fail(w, err, "templates")
		return
	}
	writeJSON(w, http.StatusOK, templatesView(own, tai.LinkRefs))
}

// handlePutTemplates: any member, for themselves only.
func (s *Server) handlePutTemplates(w http.ResponseWriter, r *http.Request) {
	t, err := s.loadTeam(r, s.accessOf(r), chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	in, ok := readTemplates(w, r)
	if !ok {
		return
	}
	if err := s.store.SaveActionTemplates(r.Context(), me(r), store.TemplateScope(t.ID), in); err != nil {
		s.fail(w, err, "templates")
		return
	}
	s.handleGetTemplates(w, r)
}

func (s *Server) handleRemoteGetTemplates(w http.ResponseWriter, r *http.Request) {
	rem := remoteFrom(r.Context())
	key := strings.ToUpper(chi.URLParam(r, "ref"))
	tai, _, err := s.remoteTeamAI(r, rem, key) // a team the person sees over there
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	own, err := s.store.ActionTemplates(r.Context(), me(r), store.RemoteTemplateScope(rem.ID, key))
	if err != nil {
		s.fail(w, err, "templates")
		return
	}
	writeJSON(w, http.StatusOK, templatesView(own, tai.LinkRefs))
}

func (s *Server) handleRemotePutTemplates(w http.ResponseWriter, r *http.Request) {
	rem := remoteFrom(r.Context())
	key := strings.ToUpper(chi.URLParam(r, "ref"))
	if _, _, err := s.remoteTeamAI(r, rem, key); err != nil {
		s.fail(w, err, "team")
		return
	}
	in, ok := readTemplates(w, r)
	if !ok {
		return
	}
	if err := s.store.SaveActionTemplates(r.Context(), me(r), store.RemoteTemplateScope(rem.ID, key), in); err != nil {
		s.fail(w, err, "templates")
		return
	}
	s.handleRemoteGetTemplates(w, r)
}
