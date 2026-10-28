package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// What an issue's GitHub pull requests say now: their CI checks (check runs
// and commit statuses on the head commit) and their comments (conversation,
// reviews and inline review comments). Read with the team's token when the
// issue page asks, kept a minute per pull request.

type prCheck struct {
	Name    string `json:"name"`
	State   string `json:"state"` // passed | failed | pending | skipped
	URL     string `json:"url,omitempty"`
	Summary string `json:"summary,omitempty"` // check runs: their output title ("2 tests failed")
}

type prComment struct {
	Author   string `json:"author"`
	Bot      bool   `json:"bot,omitempty"`
	Kind     string `json:"kind"`             // comment | review | inline
	Review   string `json:"review,omitempty"` // approved | changes_requested | commented (reviews)
	Path     string `json:"path,omitempty"`   // inline: the file
	Line     int    `json:"line,omitempty"`   // inline: the line (in the newest diff, else the original)
	ID       int64  `json:"id,omitempty"`
	ReplyTo  int64  `json:"reply_to,omitempty"` // inline: the thread's first comment
	Resolved bool   `json:"resolved,omitempty"` // inline: its thread is resolved
	Outdated bool   `json:"outdated,omitempty"` // inline: the code it was on has changed
	Body     string `json:"body"`
	URL      string `json:"url"`
	At       string `json:"at"`
}

type prDetails struct {
	AttachmentID int64       `json:"attachment_id"`
	URL          string      `json:"url"`
	Title        string      `json:"title,omitempty"`
	Branch       string      `json:"branch,omitempty"`
	Checks       []prCheck   `json:"checks"`
	ChecksState  string      `json:"checks_state"` // passed | failed | pending | none
	Comments     []prComment `json:"comments"`
	Error        string      `json:"error,omitempty"`
}

type prDetailsCached struct {
	at time.Time
	d  prDetails
}

var prDetailsCache sync.Map // token|url → prDetailsCached

const prDetailsTTL = time.Minute

func (s *Server) handlePRDetails(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	is, err := s.loadIssue(r, s.accessOf(r), chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	atts, err := s.store.Attachments(ctx, is.ID, 0)
	if err != nil {
		s.fail(w, err, "attachments")
		return
	}
	tok := s.store.TeamGitHubToken(ctx, is.TeamID)
	out := []prDetails{}
	for _, a := range atts {
		ref, ok := parsePRURL(a.URL)
		if a.Kind != "pull_request" || !ok || ref.Host != "github.com" {
			continue
		}
		if tok == "" {
			out = append(out, prDetails{AttachmentID: a.ID, URL: a.URL, Checks: []prCheck{}, Comments: []prComment{}, ChecksState: "none",
				Error: "Add a GitHub token in Team settings to see checks and comments"})
			continue
		}
		key := tok + "|" + a.URL
		if c, ok := prDetailsCache.Load(key); ok && time.Since(c.(prDetailsCached).at) < prDetailsTTL {
			d := c.(prDetailsCached).d
			d.AttachmentID = a.ID
			out = append(out, d)
			continue
		}
		d := githubPRDetails(ctx, tok, ref)
		d.AttachmentID, d.URL = a.ID, a.URL
		prDetailsCache.Store(key, prDetailsCached{at: time.Now(), d: d})
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, out)
}

// githubGet reads one GitHub API path into v.
func githubGet(ctx context.Context, token, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubAPI+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "PocketProjects/"+Version)
	res, err := prHTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
		return json.NewDecoder(res.Body).Decode(v)
	case http.StatusUnauthorized:
		return fmt.Errorf("GitHub refused the team's token (401)")
	case http.StatusForbidden, http.StatusNotFound:
		return fmt.Errorf("GitHub answered %d: the token cannot read this", res.StatusCode)
	default:
		return fmt.Errorf("GitHub answered %d", res.StatusCode)
	}
}

type ghUser struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

func githubPRDetails(ctx context.Context, token string, ref prRef) prDetails {
	d := prDetails{Checks: []prCheck{}, Comments: []prComment{}, ChecksState: "none"}
	repo := "/repos/" + url.PathEscape(ref.Owner) + "/" + url.PathEscape(ref.Repo)
	var pr struct {
		Title string `json:"title"`
		Head  struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"head"`
	}
	if err := githubGet(ctx, token, fmt.Sprintf("%s/pulls/%d", repo, ref.Number), &pr); err != nil {
		d.Error = err.Error()
		return d
	}
	d.Title, d.Branch = pr.Title, pr.Head.Ref
	var errs []string
	if pr.Head.SHA != "" {
		var runs struct {
			CheckRuns []struct {
				Name       string `json:"name"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
				HTMLURL    string `json:"html_url"`
				DetailsURL string `json:"details_url"`
				Output     struct {
					Title string `json:"title"`
				} `json:"output"`
			} `json:"check_runs"`
		}
		if err := githubGet(ctx, token, repo+"/commits/"+pr.Head.SHA+"/check-runs?per_page=100", &runs); err != nil {
			errs = append(errs, "checks: "+err.Error())
		}
		for _, c := range runs.CheckRuns {
			st := "pending"
			if c.Status == "completed" {
				st = checkConclusion(c.Conclusion)
			}
			d.Checks = append(d.Checks, prCheck{Name: c.Name, State: st, URL: firstNonEmpty(c.HTMLURL, c.DetailsURL), Summary: c.Output.Title})
		}
		var status struct {
			Statuses []struct {
				Context     string `json:"context"`
				State       string `json:"state"`
				TargetURL   string `json:"target_url"`
				Description string `json:"description"`
			} `json:"statuses"`
		}
		if err := githubGet(ctx, token, repo+"/commits/"+pr.Head.SHA+"/status", &status); err != nil {
			errs = append(errs, "statuses: "+err.Error())
		}
		for _, c := range status.Statuses { // newest per context
			st := map[string]string{"success": "passed", "failure": "failed", "error": "failed"}[c.State]
			if st == "" {
				st = "pending"
			}
			d.Checks = append(d.Checks, prCheck{Name: c.Context, State: st, URL: c.TargetURL, Summary: c.Description})
		}
	}
	rank := map[string]int{"failed": 0, "pending": 1, "passed": 2, "skipped": 3}
	sort.SliceStable(d.Checks, func(i, j int) bool {
		if rank[d.Checks[i].State] != rank[d.Checks[j].State] {
			return rank[d.Checks[i].State] < rank[d.Checks[j].State]
		}
		return strings.ToLower(d.Checks[i].Name) < strings.ToLower(d.Checks[j].Name)
	})
	d.ChecksState = checksRollup(d.Checks)

	var issueComments []struct {
		User      ghUser `json:"user"`
		Body      string `json:"body"`
		HTMLURL   string `json:"html_url"`
		CreatedAt string `json:"created_at"`
	}
	if err := githubGet(ctx, token, fmt.Sprintf("%s/issues/%d/comments?per_page=100", repo, ref.Number), &issueComments); err != nil {
		errs = append(errs, "comments: "+err.Error())
	}
	for _, c := range issueComments {
		d.Comments = append(d.Comments, prComment{Author: c.User.Login, Bot: c.User.Type == "Bot", Kind: "comment", Body: c.Body, URL: c.HTMLURL, At: c.CreatedAt})
	}
	var reviews []struct {
		User        ghUser `json:"user"`
		Body        string `json:"body"`
		State       string `json:"state"`
		HTMLURL     string `json:"html_url"`
		SubmittedAt string `json:"submitted_at"`
	}
	if err := githubGet(ctx, token, fmt.Sprintf("%s/pulls/%d/reviews?per_page=100", repo, ref.Number), &reviews); err != nil {
		errs = append(errs, "reviews: "+err.Error())
	}
	for _, c := range reviews {
		st := strings.ToLower(c.State)
		if st == "pending" || st == "dismissed" || (st == "commented" && strings.TrimSpace(c.Body) == "") {
			continue // drafts, dismissed ones, and the empty shell around inline comments
		}
		d.Comments = append(d.Comments, prComment{Author: c.User.Login, Bot: c.User.Type == "Bot", Kind: "review", Review: st, Body: c.Body, URL: c.HTMLURL, At: c.SubmittedAt})
	}
	var inline []struct {
		ID           int64  `json:"id"`
		InReplyTo    int64  `json:"in_reply_to_id"`
		User         ghUser `json:"user"`
		Body         string `json:"body"`
		Path         string `json:"path"`
		Line         int    `json:"line"`
		OriginalLine int    `json:"original_line"`
		HTMLURL      string `json:"html_url"`
		CreatedAt    string `json:"created_at"`
	}
	if err := githubGet(ctx, token, fmt.Sprintf("%s/pulls/%d/comments?per_page=100", repo, ref.Number), &inline); err != nil {
		errs = append(errs, "review comments: "+err.Error())
	}
	threads := githubThreadStates(ctx, token, ref) // nil when GraphQL can't be read: nothing shows as resolved
	for _, c := range inline {
		line := c.Line
		if line == 0 {
			line = c.OriginalLine
		}
		t := threads[c.ID]
		d.Comments = append(d.Comments, prComment{Author: c.User.Login, Bot: c.User.Type == "Bot", Kind: "inline", Path: c.Path, Line: line,
			ID: c.ID, ReplyTo: c.InReplyTo, Resolved: t.resolved, Outdated: t.outdated, Body: c.Body, URL: c.HTMLURL, At: c.CreatedAt})
	}
	sort.SliceStable(d.Comments, func(i, j int) bool { return d.Comments[i].At < d.Comments[j].At })
	if len(errs) > 0 {
		d.Error = strings.Join(errs, "; ")
	}
	return d
}

type threadState struct{ resolved, outdated bool }

// githubThreadStates maps each inline review comment id to its thread's
// resolved/outdated state. The REST API doesn't say; GraphQL does.
func githubThreadStates(ctx context.Context, token string, ref prRef) map[int64]threadState {
	const q = `query($owner:String!,$repo:String!,$n:Int!){repository(owner:$owner,name:$repo){pullRequest(number:$n){
reviewThreads(first:100){nodes{isResolved isOutdated comments(first:100){nodes{databaseId}}}}}}}`
	body, _ := json.Marshal(map[string]any{"query": q, "variables": map[string]any{"owner": ref.Owner, "repo": ref.Repo, "n": ref.Number}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, githubAPI+"/graphql", bytes.NewReader(body))
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "PocketProjects/"+Version)
	res, err := prHTTP.Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	var out struct {
		Data struct {
			Repository struct {
				PullRequest struct {
					ReviewThreads struct {
						Nodes []struct {
							IsResolved bool `json:"isResolved"`
							IsOutdated bool `json:"isOutdated"`
							Comments   struct {
								Nodes []struct {
									DatabaseID int64 `json:"databaseId"`
								} `json:"nodes"`
							} `json:"comments"`
						} `json:"nodes"`
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(res.Body).Decode(&out) != nil {
		return nil
	}
	m := map[int64]threadState{}
	for _, t := range out.Data.Repository.PullRequest.ReviewThreads.Nodes {
		for _, c := range t.Comments.Nodes {
			m[c.DatabaseID] = threadState{t.IsResolved, t.IsOutdated}
		}
	}
	return m
}

func checkConclusion(c string) string {
	switch c {
	case "success":
		return "passed"
	case "neutral", "skipped", "stale":
		return "skipped"
	case "":
		return "pending"
	default: // failure, cancelled, timed_out, action_required, startup_failure
		return "failed"
	}
}

func checksRollup(cs []prCheck) string {
	if len(cs) == 0 {
		return "none"
	}
	pending, passed := false, false
	for _, c := range cs {
		switch c.State {
		case "failed":
			return "failed"
		case "pending":
			pending = true
		case "passed":
			passed = true
		}
	}
	if pending {
		return "pending"
	}
	if passed {
		return "passed"
	}
	return "none"
}
