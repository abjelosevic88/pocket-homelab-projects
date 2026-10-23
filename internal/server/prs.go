package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// Pull requests linked to issues by hand or by an agent (link_pr), and their
// status read from GitHub every few minutes with the team's token: Pocket
// Projects is only reachable privately, so GitHub cannot send it webhooks.
// Rows are the same as the git webhook's (ext_app "git", ext_id = the PR's
// address), so whichever source speaks next updates the same attachment.

// githubAPI is GitHub's REST address (tests point it at a fake).
var githubAPI = "https://api.github.com"

var prHTTP = &http.Client{Timeout: 10 * time.Second}

// prURLRe matches GitHub (/pull/N) and Forgejo / Gitea (/pulls/N) pull request addresses.
var prURLRe = regexp.MustCompile(`^https?://([^/]+)/([^/]+)/([^/]+)/pulls?/([0-9]+)/?(?:[?#].*)?$`)

type prRef struct {
	Host, Owner, Repo string
	Number            int
}

func parsePRURL(u string) (prRef, bool) {
	m := prURLRe.FindStringSubmatch(strings.TrimSpace(u))
	if m == nil {
		return prRef{}, false
	}
	n, _ := strconv.Atoi(m[4])
	return prRef{Host: strings.ToLower(m[1]), Owner: m[2], Repo: m[3], Number: n}, true
}

func (p prRef) fullName() string { return p.Owner + "/" + p.Repo }

func (s *Server) prRoutes(r chi.Router) {
	r.Post("/issues/{ref}/pull-requests", s.handleLinkPR)
	r.Get("/teams/{ref}/git-token", s.handleGetGitToken)
	r.Put("/teams/{ref}/git-token", s.handlePutGitToken)
	r.Post("/teams/{ref}/pr-status/refresh", s.handleRefreshPRStatus)
}

// handleLinkPR attaches a pull request to an issue (open, linked explicitly:
// merging it completes the issue) and moves the issue to review.
func (s *Server) handleLinkPR(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		URL    string `json:"url"`
		Title  string `json:"title"`
		Number int    `json:"number"`
		Branch string `json:"branch"`
		Repo   string `json:"repo"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	in.URL = strings.TrimSpace(in.URL)
	if u, err := url.Parse(in.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		writeErr(w, http.StatusBadRequest, "url must be the pull request's http(s) address")
		return
	}
	is, err := s.loadIssue(r, s.accessOf(r), chi.URLParam(r, "ref"), needWrite)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	ref, ok := parsePRURL(in.URL)
	if ok {
		if in.Number == 0 {
			in.Number = ref.Number
		}
		if in.Repo == "" {
			in.Repo = ref.fullName()
		}
	}
	title := firstNonEmpty(strings.TrimSpace(in.Title), fmt.Sprintf("%s #%d", firstNonEmpty(in.Repo, "Pull request"), in.Number))
	sub := firstNonEmpty(in.Repo, "Pull request")
	if in.Number > 0 {
		sub += " #" + strconv.Itoa(in.Number)
	}
	meta, _ := json.Marshal(map[string]any{"number": in.Number, "branch": in.Branch, "repo": in.Repo, "linked": true})
	att := &store.Attachment{Kind: "pull_request", Title: title, Subtitle: sub + " · open", URL: in.URL, ExtApp: "git", ExtID: in.URL,
		State: "open", Meta: meta, CreatedBy: ptr(me(r))}
	if old, err := s.store.FindAttachment(ctx, is.ID, "git", in.URL); err == nil && old.State != "" && old.State != "open" {
		att.State, att.Subtitle = old.State, sub+" · "+old.State // already known as merged / closed / draft
	}
	s.attachGit(ctx, is, att)
	if att.State == "open" {
		s.gitMove(ctx, s.actorOf(r), is, "review")
	}
	// read its real status now when the team has a token
	if s.store.TeamGitHubToken(ctx, is.TeamID) != "" {
		s.checkPRs(ctx, 0, is.ID)
	}
	saved, err := s.store.FindAttachment(ctx, is.ID, "git", in.URL)
	if err != nil {
		s.fail(w, err, "attachment")
		return
	}
	writeJSON(w, http.StatusCreated, saved)
}

func (s *Server) handleGetGitToken(w http.ResponseWriter, r *http.Request) {
	t, err := s.loadTeam(r, s.accessOf(r), chi.URLParam(r, "ref"), needAdmin)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	writeJSON(w, http.StatusOK, gitTokenView(s.store.TeamGitHubToken(r.Context(), t.ID)))
}

func gitTokenView(tok string) map[string]any {
	last := ""
	if len(tok) >= 4 {
		last = tok[len(tok)-4:]
	}
	return map[string]any{"has_token": tok != "", "last4": last}
}

func (s *Server) handlePutGitToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		GitHubToken string `json:"github_token"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	t, err := s.loadTeam(r, s.accessOf(r), chi.URLParam(r, "ref"), needAdmin)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	tok := strings.TrimSpace(in.GitHubToken)
	if len(tok) > 400 || strings.ContainsAny(tok, " \t\r\n") {
		writeErr(w, http.StatusBadRequest, "that does not look like a GitHub token")
		return
	}
	if err := s.store.SetTeamGitHubToken(r.Context(), t.ID, tok); err != nil {
		s.fail(w, err, "git token")
		return
	}
	writeJSON(w, http.StatusOK, gitTokenView(tok))
}

// handleRefreshPRStatus checks the team's open pull requests now.
func (s *Server) handleRefreshPRStatus(w http.ResponseWriter, r *http.Request) {
	t, err := s.loadTeam(r, s.accessOf(r), chi.URLParam(r, "ref"), needAdmin)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	if s.store.TeamGitHubToken(r.Context(), t.ID) == "" {
		writeErr(w, http.StatusBadRequest, "add a GitHub token first")
		return
	}
	checked, changed := s.checkPRs(r.Context(), t.ID, 0)
	writeJSON(w, http.StatusOK, map[string]int{"checked": checked, "changed": changed})
}

// prJob runs from jobs every five minutes.
func (s *Server) prJob(ctx context.Context) { s.checkPRs(ctx, 0, 0) }

var prCheckMu sync.Mutex

// prDenied remembers tokens GitHub refused, so a bad token is logged once.
var prDenied sync.Map

// checkPRs reads the status of open / draft GitHub pull requests (all teams,
// one team or one issue) and updates them; merged ones complete issues they
// were linked to explicitly or that they close ("fixes AS-2", the issue's branch).
func (s *Server) checkPRs(ctx context.Context, teamID, issueID int64) (checked, changed int) {
	prCheckMu.Lock()
	defer prCheckMu.Unlock()
	list, err := s.store.OpenPullRequests(ctx, teamID, issueID)
	if err != nil {
		s.log.Warn("pull requests", "err", err)
		return 0, 0
	}
	tokens := map[int64]string{}
	for _, p := range list {
		ref, ok := parsePRURL(p.URL)
		if !ok || ref.Host != "github.com" {
			continue
		}
		tok, ok := tokens[p.TeamID]
		if !ok {
			tok = s.store.TeamGitHubToken(ctx, p.TeamID)
			tokens[p.TeamID] = tok
		}
		if tok == "" {
			continue
		}
		state, title, err := githubPRState(ctx, tok, ref)
		checked++
		var meta map[string]any
		_ = json.Unmarshal(p.Meta, &meta)
		if meta == nil {
			meta = map[string]any{}
		}
		if err != nil {
			if _, seen := prDenied.LoadOrStore(fmt.Sprint(p.TeamID, err), true); !seen {
				s.log.Warn("pull request status", "pr", p.URL, "err", err)
			}
			if meta["status_error"] != err.Error() {
				meta["status_error"] = err.Error()
				p.Meta, _ = json.Marshal(meta)
				_ = s.store.SaveAttachment(ctx, &p.Attachment)
			}
			continue
		}
		_, hadErr := meta["status_error"]
		delete(meta, "status_error")
		if state == p.State {
			if hadErr {
				p.Meta, _ = json.Marshal(meta)
				_ = s.store.SaveAttachment(ctx, &p.Attachment)
			}
			continue
		}
		changed++
		p.State = state
		p.Subtitle = ref.fullName() + " #" + strconv.Itoa(ref.Number) + " · " + state
		if title != "" {
			p.Title = title
		}
		p.Meta, _ = json.Marshal(meta)
		_ = s.store.SaveAttachment(ctx, &p.Attachment)
		is, err := s.store.GetIssue(ctx, *p.IssueID, 0)
		if err != nil {
			continue
		}
		s.send(ctx, change{Kind: "issue", TeamIDs: []int64{is.TeamID}, IDs: []int64{is.ID}})
		if state == "merged" && (meta["linked"] == true || prClosesIssue(p.Attachment, is.Identifier)) {
			who := s.systemActor("git")
			who.name = "Git"
			s.gitMove(ctx, who, is, "completed")
		}
	}
	return checked, changed
}

// prClosesIssue: a webhook-attached pull request completes the issue when its
// branch carries the identifier (the same rule as the merge webhook, minus the
// body text it no longer has).
func prClosesIssue(a store.Attachment, ident string) bool {
	var meta struct {
		Branch string `json:"branch"`
	}
	_ = json.Unmarshal(a.Meta, &meta)
	return ident != "" && strings.Contains(strings.ToLower(meta.Branch), strings.ToLower(ident))
}

// githubPRState asks GitHub: draft | open | merged | closed.
func githubPRState(ctx context.Context, token string, ref prRef) (state, title string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/repos/%s/%s/pulls/%d", githubAPI, url.PathEscape(ref.Owner), url.PathEscape(ref.Repo), ref.Number), nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "PocketProjects/"+Version)
	res, err := prHTTP.Do(req)
	if err != nil {
		return "", "", err
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return "", "", fmt.Errorf("GitHub refused the team's token (401)")
	case http.StatusForbidden, http.StatusNotFound:
		return "", "", fmt.Errorf("GitHub answered %d: the token cannot read %s", res.StatusCode, ref.fullName())
	default:
		return "", "", fmt.Errorf("GitHub answered %d", res.StatusCode)
	}
	var pr struct {
		Title  string `json:"title"`
		State  string `json:"state"`
		Merged bool   `json:"merged"`
		Draft  bool   `json:"draft"`
	}
	if err := json.NewDecoder(res.Body).Decode(&pr); err != nil {
		return "", "", err
	}
	switch {
	case pr.Merged:
		state = "merged"
	case pr.State == "closed":
		state = "closed"
	case pr.Draft:
		state = "draft"
	default:
		state = "open"
	}
	return state, pr.Title, nil
}
