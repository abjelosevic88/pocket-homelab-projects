package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ---------- Pocket PR Review ----------
//
// A pull request linked to an issue opens in Pocket PR Review as a guided
// walkthrough (the changes in the order to read them). Projects asks it for
// each pull request's walkthrough (prs:read: a badge with how far the
// person got) and adds the pull request there on "Walkthrough" (prs:write:
// the walkthrough starts at once). Without the link the button still opens
// Pocket PR Review's own "add" page.

type prReviewState struct {
	OpenURL string `json:"open_url"`
	Status  string `json:"status,omitempty"` // queued | working | ready | failed
	Steps   int    `json:"steps,omitempty"`
	Units   int    `json:"units,omitempty"`
	Viewed  int    `json:"viewed,omitempty"`
	Stale   bool   `json:"stale,omitempty"`
	Review  string `json:"my_review,omitempty"`
}

func (s *Server) prReviewPublic(ctx context.Context) string {
	if !s.core.Configured() {
		return ""
	}
	if res, err := s.core.Resolve(ctx, "pr-review"); err == nil {
		return strings.TrimRight(firstNonEmpty(res.PublicURL, res.URL), "/")
	}
	return ""
}

// handlePRReviewStatus answers, for each pull request address, its walkthrough in Pocket PR Review.
func (s *Server) handlePRReviewStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var urls []string
	for _, u := range strings.Split(r.URL.Query().Get("urls"), ",") {
		if u = strings.TrimSpace(u); u != "" && len(urls) < 20 {
			urls = append(urls, u)
		}
	}
	out := map[string]any{"public_url": s.prReviewPublic(ctx), "pulls": map[string]prReviewState{}}
	app, _ := pocketApp("pr-review")
	sb, err := s.siblingFor(ctx, userFrom(ctx), app.only("prs:read"))
	if err != nil {
		out["error"] = err.Error()
		writeJSON(w, http.StatusOK, out)
		return
	}
	got := map[string]prReviewState{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, u := range urls {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			c, cancel := context.WithTimeout(ctx, siblingTimeout)
			defer cancel()
			var res struct {
				OpenURL string `json:"open_url"`
				Pull    struct {
					MyReview string `json:"my_review"`
					Walk     *struct {
						Status string `json:"status"`
						Steps  int    `json:"steps"`
						Units  int    `json:"units"`
						Viewed int    `json:"viewed"`
						Stale  bool   `json:"stale"`
					} `json:"walkthrough"`
				} `json:"pull"`
			}
			if sb.call(c, http.MethodGet, "/api/v1/lookup?url="+url.QueryEscape(u), nil, &res) != nil {
				return
			}
			st := prReviewState{OpenURL: res.OpenURL, Review: res.Pull.MyReview}
			if wk := res.Pull.Walk; wk != nil {
				st.Status, st.Steps, st.Units, st.Viewed, st.Stale = wk.Status, wk.Steps, wk.Units, wk.Viewed, wk.Stale
			}
			mu.Lock()
			got[u] = st
			mu.Unlock()
		}(u)
	}
	wg.Wait()
	out["pulls"] = got
	writeJSON(w, http.StatusOK, out)
}

// handlePRReviewOpen adds a pull request to the person's Pocket PR Review and
// says where its walkthrough opens.
func (s *Server) handlePRReviewOpen(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		URL string `json:"url"`
	}
	if err := decode(r, &in); err != nil || !strings.Contains(in.URL, "/pull/") {
		writeErr(w, http.StatusBadRequest, "url is a pull request address")
		return
	}
	public := s.prReviewPublic(ctx)
	fallback := ""
	if public != "" {
		fallback = public + "/?add=" + url.QueryEscape(in.URL)
	}
	app, _ := pocketApp("pr-review")
	sb, err := s.siblingFor(ctx, userFrom(ctx), app.only("prs:write"))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"url": fallback, "error": err.Error()})
		return
	}
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var res struct {
		OpenURL string `json:"open_url"`
	}
	if err := sb.call(c, http.MethodPost, "/api/v1/pulls", map[string]any{"url": in.URL}, &res); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"url": fallback, "error": siblingErr("Pocket PR Review", err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": firstNonEmpty(res.OpenURL, fallback)})
}
