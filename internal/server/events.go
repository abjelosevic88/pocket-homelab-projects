package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// ---------- the live stream ----------

// change tells browsers what to refresh: the teams it concerns (0 = only
// the listed people), the kind of thing and its ids.
type change struct {
	Kind    string  `json:"kind"` // issue | project | team | cycle | comment | notification | view
	TeamIDs []int64 `json:"team_ids,omitempty"`
	IDs     []int64 `json:"ids,omitempty"`
	ActorID int64   `json:"actor_id,omitempty"`
	users   []int64
}

type client struct {
	uid int64
	ch  chan change
}

type hub struct {
	mu      sync.Mutex
	clients map[*client]bool
}

func newHub() *hub { return &hub{clients: map[*client]bool{}} }

func (h *hub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

func (h *hub) add(uid int64) *client {
	c := &client{uid: uid, ch: make(chan change, 64)}
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
	return c
}

func (h *hub) remove(c *client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

// send delivers a change to the browsers of the people it concerns.
func (s *Server) send(ctx context.Context, ch change) {
	want := map[int64]bool{}
	for _, u := range ch.users {
		want[u] = true
	}
	for _, t := range ch.TeamIDs {
		for _, u := range s.store.TeamMemberIDs(ctx, t) {
			want[u] = true
		}
	}
	if len(want) == 0 {
		return
	}
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()
	for c := range s.hub.clients {
		if want[c.uid] {
			select {
			case c.ch <- ch:
			default: // a slow browser refetches on its next event
			}
		}
	}
}

// changed announces a change to everyone on the teams.
func (s *Server) changed(r *http.Request, kind string, teamIDs []int64, ids ...int64) {
	actor := int64(0)
	if r != nil {
		actor = me(r)
	}
	s.send(context.WithoutCancel(ctxOf(r)), change{Kind: kind, TeamIDs: teamIDs, IDs: ids, ActorID: actor})
}

func ctxOf(r *http.Request) context.Context {
	if r == nil {
		return context.Background()
	}
	return r.Context()
}

// handleStream is a server-sent event stream of changes for the person.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	c := s.hub.add(me(r))
	defer s.hub.remove(c)
	fmt.Fprintf(w, "event: hello\ndata: {\"version\":%q}\n\n", Version)
	fl.Flush()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		case ch := <-c.ch:
			b, _ := json.Marshal(ch)
			fmt.Fprintf(w, "event: change\ndata: %s\n\n", b)
			fl.Flush()
		}
	}
}

// ---------- Pocket Core events ----------

// coreEvent queues an event for Pocket Core inside the saving transaction.
func (s *Server) coreEvent(ctx context.Context, topic, subject string, payload map[string]any) store.TxHook {
	return func(tx *sql.Tx) error {
		if !s.core.Configured() {
			return nil
		}
		return core.QueueEvent(ctx, tx, topic, subject, payload)
	}
}

// teamEmails lists the emails of a team's people (the audience of an event).
func (s *Server) teamEmails(ctx context.Context, teamIDs ...int64) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range teamIDs {
		members, _ := s.store.TeamMembers(ctx, t)
		for _, m := range members {
			if !seen[m.Email] {
				seen[m.Email] = true
				out = append(out, m.Email)
			}
		}
	}
	return out
}

// issueEventPayload is what Core and outgoing webhooks get for an issue.
func (s *Server) issueEventPayload(r *http.Request, is *store.Issue) map[string]any {
	u := userFrom(r.Context())
	p := map[string]any{
		"id": is.ID, "identifier": is.Identifier, "title": is.Title, "team_id": is.TeamID, "state_id": is.StateID, "state_type": is.StateType,
		"priority": is.Priority, "due_date": is.DueDate, "project_id": is.ProjectID, "assignee_id": is.AssigneeID, "url": s.issueURL(r, is.Identifier),
		"audience_emails": s.teamEmails(r.Context(), is.TeamID),
	}
	if st, err := s.store.GetState(r.Context(), is.StateID); err == nil {
		p["state"] = st.Name
	}
	if is.AssigneeID != nil {
		p["assignee_email"] = s.userEmail(r.Context(), *is.AssigneeID)
	}
	if u != nil {
		p["user_email"] = u.Email
	}
	return p
}

// ---------- outgoing webhooks ----------

var webhookHTTP = &http.Client{Timeout: 10 * time.Second}

// webhook delivers an event to a team's outgoing webhooks (in the background, three tries).
func (s *Server) webhook(teamID int64, event string, data any) {
	go func() {
		ctx := context.Background()
		hooks, err := s.store.Webhooks(ctx, teamID)
		if err != nil {
			return
		}
		for _, h := range hooks {
			if !h.Active || !webhookWants(h.Events, event) {
				continue
			}
			s.deliverWebhook(ctx, h, event, data)
		}
	}()
}

func webhookWants(events, event string) bool {
	if events == "" || events == "*" {
		return true
	}
	for _, e := range strings.Split(events, ",") {
		e = strings.TrimSpace(e)
		if e == event || (strings.HasSuffix(e, "*") && strings.HasPrefix(event, strings.TrimSuffix(e, "*"))) {
			return true
		}
	}
	return false
}

func (s *Server) deliverWebhook(ctx context.Context, h store.Webhook, event string, data any) (int, error) {
	body, _ := json.Marshal(map[string]any{"event": event, "app": "projects", "sent_at": time.Now().UTC().Format(time.RFC3339), "data": data})
	var status int
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*attempt) * 2 * time.Second)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.URL, bytes.NewReader(body))
		if err != nil {
			lastErr = err
			break
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "PocketProjects/"+Version)
		req.Header.Set("X-Pocket-Event", event)
		if h.Secret != "" {
			mac := hmac.New(sha256.New, []byte(h.Secret))
			mac.Write(body)
			req.Header.Set("X-Pocket-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		}
		res, err := webhookHTTP.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		res.Body.Close()
		status = res.StatusCode
		if status < 300 {
			lastErr = nil
			break
		}
		lastErr = fmt.Errorf("answered %d", status)
		if status < 500 && status != 429 {
			break
		}
	}
	msg := ""
	if lastErr != nil {
		msg = lastErr.Error()
	}
	s.store.WebhookResult(ctx, h.ID, status, msg)
	return status, lastErr
}
