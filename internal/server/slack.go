package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// Slack: a team's channels (incoming webhooks, one per channel) and people
// (member IDs, for @mentions). "Send to Slack" on a pull request drafts a
// short summary with Pocket AI; the person edits it and presses Send — nothing
// is posted unseen. Webhook URLs are secrets: only their last characters are
// ever sent back to the browser.

func (s *Server) slackRoutes(r chi.Router) {
	r.Get("/teams/{ref}/slack", s.handleSlackList)
	r.Post("/teams/{ref}/slack", s.handleSlackAdd)
	r.Delete("/teams/{ref}/slack/{id}", s.handleSlackDelete)
	r.Post("/issues/{ref}/slack/draft", s.handleSlackDraft)
	r.Post("/issues/{ref}/slack", s.handleSlackSend)
}

var (
	slackHookRe   = regexp.MustCompile(`^https://hooks\.slack\.com/(services|workflows|triggers)/[A-Za-z0-9_/-]{10,200}$`)
	slackMemberRe = regexp.MustCompile(`^[UW][A-Z0-9]{6,15}$`)
	slackHTTP     = &http.Client{Timeout: 10 * time.Second}
	slackHookOK   = slackHookRe.MatchString // tests point it at a local server
)

type slackEntry struct {
	ID    int64  `json:"id"`
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Value string `json:"value"` // channel: masked webhook; person: member ID
}

func (s *Server) slackEntries(ctx context.Context, teamID int64) ([]slackEntry, map[int64]string, error) {
	rows, err := s.store.DB.QueryContext(ctx, `SELECT id, kind, name, value FROM team_slack WHERE team_id = ? ORDER BY kind, name COLLATE NOCASE`, teamID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	out, secret := []slackEntry{}, map[int64]string{}
	for rows.Next() {
		var e slackEntry
		if err := rows.Scan(&e.ID, &e.Kind, &e.Name, &e.Value); err != nil {
			return nil, nil, err
		}
		if e.Kind == "channel" {
			secret[e.ID] = e.Value
			e.Value = "hooks.slack.com/…" + e.Value[max(0, len(e.Value)-4):]
		}
		out = append(out, e)
	}
	return out, secret, rows.Err()
}

func (s *Server) handleSlackList(w http.ResponseWriter, r *http.Request) {
	t, err := s.loadTeam(r, s.accessOf(r), chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	list, _, err := s.slackEntries(r.Context(), t.ID)
	if err != nil {
		s.fail(w, err, "slack")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleSlackAdd(w http.ResponseWriter, r *http.Request) {
	var in slackEntry
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	t, err := s.loadTeam(r, s.accessOf(r), chi.URLParam(r, "ref"), needAdmin)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	in.Name, in.Value = strings.TrimSpace(in.Name), strings.TrimSpace(in.Value)
	switch {
	case in.Name == "" || len(in.Name) > 80:
		writeErr(w, http.StatusBadRequest, "give it a name (up to 80 characters)")
		return
	case in.Kind == "channel" && !slackHookOK(in.Value):
		writeErr(w, http.StatusBadRequest, "that is not a Slack incoming-webhook URL (https://hooks.slack.com/services/…)")
		return
	case in.Kind == "person" && !slackMemberRe.MatchString(strings.ToUpper(in.Value)):
		writeErr(w, http.StatusBadRequest, "that is not a Slack member ID (like U01AB2CD3EF: in Slack, open the profile → ⋮ → Copy member ID)")
		return
	case in.Kind != "channel" && in.Kind != "person":
		writeErr(w, http.StatusBadRequest, "kind is channel or person")
		return
	}
	if in.Kind == "person" {
		in.Value = strings.ToUpper(in.Value)
	}
	if _, err := s.store.DB.ExecContext(r.Context(), `INSERT INTO team_slack (team_id, kind, name, value) VALUES (?, ?, ?, ?)`, t.ID, in.Kind, in.Name, in.Value); err != nil {
		s.fail(w, err, "slack")
		return
	}
	list, _, _ := s.slackEntries(r.Context(), t.ID)
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleSlackDelete(w http.ResponseWriter, r *http.Request) {
	t, err := s.loadTeam(r, s.accessOf(r), chi.URLParam(r, "ref"), needAdmin)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if _, err := s.store.DB.ExecContext(r.Context(), `DELETE FROM team_slack WHERE id = ? AND team_id = ?`, id, t.ID); err != nil {
		s.fail(w, err, "slack")
		return
	}
	list, _, _ := s.slackEntries(r.Context(), t.ID)
	writeJSON(w, http.StatusOK, list)
}

// the pull request being announced (or the issue's latest one)
func (s *Server) slackPR(ctx context.Context, issueID, attID int64) *store.Attachment {
	list, _ := s.store.Attachments(ctx, issueID, 0)
	var pick *store.Attachment
	for i := range list {
		a := &list[i]
		if a.Kind != "pull_request" {
			continue
		}
		if attID != 0 && a.ID == attID {
			return a
		}
		if pick == nil || a.CreatedAt > pick.CreatedAt {
			pick = a
		}
	}
	if attID != 0 {
		return nil
	}
	return pick
}

// POST /issues/{ref}/slack/draft {attachment_id}: a short message to edit.
func (s *Server) handleSlackDraft(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AttachmentID int64 `json:"attachment_id"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ctx := r.Context()
	is, err := s.loadIssue(r, s.accessOf(r), chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	pr := s.slackPR(ctx, is.ID, in.AttachmentID)
	link, title := "", ""
	if pr != nil {
		link, title = pr.URL, pr.Title
	}
	fallback := strings.TrimSpace(firstNonEmpty(title, is.Identifier+" "+is.Title))
	comments, _ := s.store.Comments(ctx, is.ID, 0)
	var b strings.Builder
	for i := max(0, len(comments)-4); i < len(comments); i++ {
		fmt.Fprintf(&b, "[%s] %s\n\n", comments[i].CreatedAt[:10], comments[i].Body)
	}
	prompt := fmt.Sprintf(`Write a short Slack message announcing this pull request to teammates who will review or test it.
2 or 3 sentences, plain text (Slack *bold* at most once): what changed and why, and how to check it if the input says so.
No greeting, no sign-off, no link (it is added after your text), no headings, no bullet lists, no emoji.

Pull request: %s
Issue %s: %s
Issue description:
%s

Latest comments (the newest usually summarises the work):
%s`, firstNonEmpty(title, "(none linked)"), is.Identifier, is.Title, clip(is.Description, 3000), clip(b.String(), 6000))
	c, st := s.aiClient(ctx, me(r))
	text := fallback
	note := ""
	if c != nil && st.Ready {
		actx, cancel := context.WithTimeout(ctx, 90*time.Second)
		reply, err := c.Complete(actx, aiSystem, prompt, false)
		cancel()
		if err == nil && strings.TrimSpace(reply.Text) != "" {
			text = strings.TrimSpace(reply.Text)
		} else {
			note = "Pocket AI could not write a summary just now; write one or send the title."
		}
	} else {
		note = "Pocket AI is not available (" + st.Detail + "); write the summary yourself."
	}
	writeJSON(w, http.StatusOK, map[string]any{"text": text, "link": link, "pr_title": title, "note": note})
}

// POST /issues/{ref}/slack {channel_id, people: [ids], text, link}: posts it.
func (s *Server) handleSlackSend(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ChannelID int64   `json:"channel_id"`
		People    []int64 `json:"people"`
		Text      string  `json:"text"`
		Link      string  `json:"link"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ctx := r.Context()
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, chi.URLParam(r, "ref"), needWrite)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	text := strings.TrimSpace(in.Text)
	if text == "" || len(text) > 3000 {
		writeErr(w, http.StatusBadRequest, "write the message (up to 3000 characters)")
		return
	}
	entries, secret, err := s.slackEntries(ctx, is.TeamID)
	if err != nil {
		s.fail(w, err, "slack")
		return
	}
	hook, channel := secret[in.ChannelID], ""
	mentions := []string{}
	for _, e := range entries {
		if e.ID == in.ChannelID {
			channel = e.Name
		}
		for _, p := range in.People {
			if e.Kind == "person" && e.ID == p {
				mentions = append(mentions, "<@"+e.Value+">")
			}
		}
	}
	if hook == "" {
		writeErr(w, http.StatusBadRequest, "choose one of the team's Slack channels")
		return
	}
	msg := text
	if len(mentions) > 0 {
		msg = strings.Join(mentions, " ") + " " + msg
	}
	if l := strings.TrimSpace(in.Link); strings.HasPrefix(l, "https://") && !strings.Contains(msg, l) {
		msg += "\n" + l
	}
	body, _ := json.Marshal(map[string]any{"text": msg, "unfurl_links": true})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, hook, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := slackHTTP.Do(req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "Slack could not be reached: "+err.Error())
		return
	}
	defer res.Body.Close()
	reply, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
	if res.StatusCode != http.StatusOK {
		writeErr(w, http.StatusBadGateway, "Slack refused it: "+firstNonEmpty(strings.TrimSpace(string(reply)), res.Status)+" (is the webhook still active?)")
		return
	}
	_ = s.store.AddHistory(ctx, store.History{IssueID: &is.ID, ActorID: ptr(me(r)), Field: "slack_sent", ToLabel: channel})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "channel": channel})
}
