package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// Incoming Forgejo / Gitea / GitHub webhooks (push and pull_request, JSON).
// The secret in the address identifies the team; commits and pull requests
// mentioning one of its identifiers are attached to the issue; "fixes WEB-12"
// completes it when pushed to the default branch or merged; an opened pull
// request moves an issue that has not started to the team's review state.

var closeWordRe = regexp.MustCompile(`(?i)\b(?:fix|fixes|fixed|close|closes|closed|resolve|resolves|resolved|complete|completes|completed)[\s:]+((?:[A-Za-z][A-Za-z0-9]{0,6}-[0-9]+[\s,]*(?:and\s+)?)+)`)

type gitCommit struct {
	ID      string `json:"id"`
	Message string `json:"message"`
	URL     string `json:"url"`
	Author  struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Username string `json:"username"`
	} `json:"author"`
}

type gitPayload struct {
	Ref        string      `json:"ref"`
	Commits    []gitCommit `json:"commits"`
	Action     string      `json:"action"`
	Number     int         `json:"number"`
	Repository struct {
		FullName      string `json:"full_name"`
		HTMLURL       string `json:"html_url"`
		DefaultBranch string `json:"default_branch"`
	} `json:"repository"`
	PullRequest *struct {
		Title   string `json:"title"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		Merged  bool   `json:"merged"`
		Draft   bool   `json:"draft"`
		State   string `json:"state"`
		Head    struct {
			Ref string `json:"ref"`
		} `json:"head"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"pull_request"`
}

func (s *Server) handleGitHook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	team, err := s.store.GetTeamByGitSecret(ctx, chi.URLParam(r, "secret"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	event := firstNonEmpty(r.Header.Get("X-Forgejo-Event"), r.Header.Get("X-Gitea-Event"), r.Header.Get("X-GitHub-Event"))
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "unreadable body")
		return
	}
	var p gitPayload
	if err := json.Unmarshal(body, &p); err != nil {
		writeErr(w, http.StatusBadRequest, "expected a JSON webhook payload")
		return
	}
	who := s.systemActor("git")
	who.name = "Git"
	linked := 0
	switch event {
	case "ping":
		writeJSON(w, http.StatusOK, map[string]string{"ok": "pong"})
		return
	case "push":
		branch := strings.TrimPrefix(p.Ref, "refs/heads/")
		def := firstNonEmpty(p.Repository.DefaultBranch, "main")
		onDefault := branch == def || (p.Repository.DefaultBranch == "" && branch == "master")
		for _, c := range p.Commits {
			first := strings.SplitN(c.Message, "\n", 2)[0]
			closes := closingIDs(c.Message)
			for _, ident := range s.teamIdents(team, c.Message+" "+branch) {
				is := s.issueOfTeam(ctx, team, ident)
				if is == nil {
					continue
				}
				short := c.ID
				if len(short) > 7 {
					short = short[:7]
				}
				meta, _ := json.Marshal(map[string]any{"sha": c.ID, "branch": branch, "repo": p.Repository.FullName, "author": firstNonEmpty(c.Author.Name, c.Author.Username)})
				s.attachGit(ctx, is, &store.Attachment{Kind: "commit", Title: first, Subtitle: p.Repository.FullName + " · " + short + " · " + firstNonEmpty(c.Author.Name, c.Author.Username),
					URL: c.URL, ExtApp: "git", ExtID: c.ID, State: "pushed", Meta: meta})
				linked++
				if onDefault && closes[strings.ToUpper(ident)] {
					s.gitMove(ctx, who, is, "completed")
				}
			}
		}
	case "pull_request":
		pr := p.PullRequest
		if pr == nil {
			break
		}
		text := pr.Title + "\n" + pr.Body + "\n" + pr.Head.Ref
		closes := closingIDs(text)
		state := "open"
		switch {
		case pr.Merged:
			state = "merged"
		case p.Action == "closed" || pr.State == "closed":
			state = "closed"
		case pr.Draft:
			state = "draft"
		}
		for _, ident := range s.teamIdents(team, text) {
			is := s.issueOfTeam(ctx, team, ident)
			if is == nil {
				continue
			}
			meta, _ := json.Marshal(map[string]any{"number": p.Number, "branch": pr.Head.Ref, "repo": p.Repository.FullName, "author": pr.User.Login})
			s.attachGit(ctx, is, &store.Attachment{Kind: "pull_request", Title: pr.Title, Subtitle: p.Repository.FullName + " #" + strconv.Itoa(p.Number) + " · " + state,
				URL: pr.HTMLURL, ExtApp: "git", ExtID: pr.HTMLURL, State: state, Meta: meta})
			linked++
			switch {
			case state == "merged" && (closes[strings.ToUpper(ident)] || strings.Contains(strings.ToLower(pr.Head.Ref), strings.ToLower(ident))):
				s.gitMove(ctx, who, is, "completed")
			case state == "open" && (p.Action == "opened" || p.Action == "reopened" || p.Action == "ready_for_review"):
				s.gitMove(ctx, who, is, "review")
			}
		}
	default:
		writeJSON(w, http.StatusOK, map[string]any{"ignored": event})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"linked": linked})
}

// teamIdents finds the identifiers of the team's issues in text.
func (s *Server) teamIdents(team *store.Team, text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range store.IdentifierRe.FindAllStringSubmatch(text, -1) {
		if !strings.EqualFold(m[1], team.Key) {
			continue
		}
		id := strings.ToUpper(m[1]) + "-" + m[2]
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func closingIDs(text string) map[string]bool {
	out := map[string]bool{}
	for _, m := range closeWordRe.FindAllStringSubmatch(text, -1) {
		for _, id := range store.IdentifierRe.FindAllString(m[1], -1) {
			out[strings.ToUpper(id)] = true
		}
	}
	return out
}

func (s *Server) issueOfTeam(ctx context.Context, team *store.Team, ident string) *store.Issue {
	id, err := s.store.IssueIDByIdentifier(ctx, ident)
	if err != nil {
		return nil
	}
	is, err := s.store.GetIssue(ctx, id, 0)
	if err != nil || is.TeamID != team.ID || is.DeletedAt != nil {
		return nil
	}
	return is
}

// attachGit adds or refreshes a commit / pull request on an issue.
func (s *Server) attachGit(ctx context.Context, is *store.Issue, att *store.Attachment) {
	if old, err := s.store.FindAttachment(ctx, is.ID, att.ExtApp, att.ExtID); err == nil {
		old.Title, old.Subtitle, old.State, old.Meta = att.Title, att.Subtitle, att.State, att.Meta
		if att.URL != "" {
			old.URL = att.URL
		}
		_ = s.store.SaveAttachment(ctx, old)
	} else {
		att.IssueID = &is.ID
		if err := s.store.SaveAttachment(ctx, att); err != nil {
			return
		}
		_ = s.store.AddHistory(ctx, store.History{IssueID: &is.ID, ActorApp: "git", Field: "git_" + att.Kind, ToLabel: att.Title})
	}
	s.send(ctx, change{Kind: "issue", TeamIDs: []int64{is.TeamID}, IDs: []int64{is.ID}})
}

// gitMove completes an issue or moves it to review (only forward, never back).
func (s *Server) gitMove(ctx context.Context, who actor, is *store.Issue, to string) {
	states, _ := s.store.TeamStates(ctx, is.TeamID)
	var target *store.State
	switch to {
	case "completed":
		if is.StateType == "completed" || is.StateType == "canceled" {
			return
		}
		for i, st := range states {
			if st.Type == "completed" {
				target = &states[i]
				break
			}
		}
	case "review":
		if is.StateType == "completed" || is.StateType == "canceled" {
			return
		}
		cur, _ := s.store.GetState(ctx, is.StateID)
		for i, st := range states {
			if st.Type == "started" && strings.Contains(strings.ToLower(st.Name), "review") {
				target = &states[i]
			}
		}
		if target == nil && is.StateType != "started" {
			for i, st := range states {
				if st.Type == "started" {
					target = &states[i]
					break
				}
			}
		}
		if target != nil && cur != nil && cur.Type == "started" && cur.Position >= target.Position {
			return
		}
	}
	if target == nil || target.ID == is.StateID {
		return
	}
	a := &access{teams: map[int64]string{is.TeamID: "admin"}}
	if _, err := s.updateIssue(ctx, a, who, is, issueInput{StateID: &target.ID}); err != nil {
		s.log.Warn("git: move issue", "issue", is.Identifier, "err", err)
	}
}
