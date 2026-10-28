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
	s.linkPR(w, r, is, in.URL, in.Title, in.Branch, in.Repo, in.Number)
}

// linkPR attaches a pull request to an issue as the git webhook would (and
// reads its status now when the team has a token). Also used when a PR
// address is pasted with Attachments → Link.
func (s *Server) linkPR(w http.ResponseWriter, r *http.Request, is *store.Issue, prURL, title, branch, repo string, number int) {
	saved := s.attachPR(r.Context(), is, s.actorOf(r), me(r), prURL, title, branch, repo, number)
	if saved == nil {
		writeErr(w, http.StatusInternalServerError, "could not save the pull request")
		return
	}
	writeJSON(w, http.StatusCreated, saved)
}

// attachPR saves a pull request on an issue as the git webhook would, moves
// the issue to review while it is open and reads its real status when the
// team has a token. Used by link_pr, pasted links and PRs found in Pocket AI
// conversations.
func (s *Server) attachPR(ctx context.Context, is *store.Issue, who actor, uid int64, prURL, title, branch, repo string, number int) *store.Attachment {
	if ref, ok := parsePRURL(prURL); ok {
		if number == 0 {
			number = ref.Number
		}
		if repo == "" {
			repo = ref.fullName()
		}
	}
	name := firstNonEmpty(strings.TrimSpace(title), fmt.Sprintf("%s #%d", firstNonEmpty(repo, "Pull request"), number))
	sub := firstNonEmpty(repo, "Pull request")
	if number > 0 {
		sub += " #" + strconv.Itoa(number)
	}
	meta, _ := json.Marshal(map[string]any{"number": number, "branch": branch, "repo": repo, "linked": true})
	att := &store.Attachment{Kind: "pull_request", Title: name, Subtitle: sub + " · open", URL: prURL, ExtApp: "git", ExtID: prURL,
		State: "open", Meta: meta, CreatedBy: ptr(uid)}
	if old, err := s.store.FindAttachment(ctx, is.ID, "git", prURL); err == nil && old.State != "" && old.State != "open" {
		att.State, att.Subtitle = old.State, sub+" · "+old.State // already known as merged / closed / draft
	}
	s.attachGit(ctx, is, att)
	if att.State == "open" {
		s.gitMove(ctx, who, is, "review")
	}
	if s.store.TeamGitHubToken(ctx, is.TeamID) != "" {
		s.checkPRs(ctx, 0, is.ID)
	}
	saved, err := s.store.FindAttachment(ctx, is.ID, "git", prURL)
	if err != nil {
		return nil
	}
	return saved
}

// adoptAgentPRs attaches the pull requests a person's Pocket AI conversations
// opened to their issues (the agent may never have called link_pr).
func (s *Server) adoptAgentPRs(ctx context.Context, u *store.User) (n int) {
	sb, err := s.siblingFor(ctx, u, aiApp)
	if err != nil {
		return 0
	}
	var prs []struct {
		Ref string `json:"ref"`
		URL string `json:"url"`
	}
	if err := sb.call(ctx, http.MethodGet, "/api/v1/link/prs?source=projects", nil, &prs); err != nil {
		return 0
	}
	for _, p := range prs {
		if s.adoptPR(ctx, u, p.Ref, p.URL) {
			n++
		}
	}
	return n
}

// adoptPR attaches one PR found in a conversation unless the issue has it already.
func (s *Server) adoptPR(ctx context.Context, u *store.User, ident, prURL string) bool {
	if _, ok := parsePRURL(prURL); !ok {
		return false
	}
	id, err := s.store.IssueIDByIdentifier(ctx, ident)
	if err != nil {
		return false
	}
	if _, err := s.store.FindAttachment(ctx, id, "git", prURL); err == nil {
		return false
	}
	is, err := s.store.GetIssue(ctx, id, u.ID)
	if err != nil {
		return false
	}
	return s.attachPR(ctx, is, actor{id: u.ID, app: "ai", name: "Pocket AI"}, u.ID, prURL, "", "", "", 0) != nil
}

// agentPRJob: every few minutes, for everyone who started a code conversation
// in the last 30 days, attach the PRs those conversations opened.
func (s *Server) agentPRJob(ctx context.Context) {
	rows, err := s.store.DB.QueryContext(ctx, `SELECT DISTINCT created_by FROM attachments WHERE kind = 'ai_thread' AND created_by IS NOT NULL AND created_at > ?`,
		time.Now().AddDate(0, 0, -30).UTC().Format(time.RFC3339))
	if err != nil {
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if u, err := s.store.GetUser(ctx, id); err == nil {
			if n := s.adoptAgentPRs(ctx, u); n > 0 {
				s.log.Info("attached pull requests from Pocket AI conversations", "user", id, "count", n)
			}
		}
	}
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
func (s *Server) prJob(ctx context.Context) {
	s.agentPRJob(ctx)
	s.checkPRs(ctx, 0, 0)
}

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
		state, title, head, err := githubPRState(ctx, tok, ref)
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
		noBranch := false
		if b, _ := meta["branch"].(string); b == "" && head != "" {
			meta["branch"], noBranch = head, true // pasted links and adopted PRs come without one
		}
		if state == p.State {
			if hadErr || noBranch {
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

// githubPRState asks GitHub: draft | open | merged | closed, the title and the head branch.
func githubPRState(ctx context.Context, token string, ref prRef) (state, title, head string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/repos/%s/%s/pulls/%d", githubAPI, url.PathEscape(ref.Owner), url.PathEscape(ref.Repo), ref.Number), nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "PocketProjects/"+Version)
	res, err := prHTTP.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return "", "", "", fmt.Errorf("GitHub refused the team's token (401)")
	case http.StatusForbidden, http.StatusNotFound:
		return "", "", "", fmt.Errorf("GitHub answered %d: the token cannot read %s", res.StatusCode, ref.fullName())
	default:
		return "", "", "", fmt.Errorf("GitHub answered %d", res.StatusCode)
	}
	var pr struct {
		Title  string `json:"title"`
		State  string `json:"state"`
		Merged bool   `json:"merged"`
		Draft  bool   `json:"draft"`
		Head   struct {
			Ref string `json:"ref"`
		} `json:"head"`
	}
	if err := json.NewDecoder(res.Body).Decode(&pr); err != nil {
		return "", "", "", err
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
	return state, pr.Title, pr.Head.Ref, nil
}
