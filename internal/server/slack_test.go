package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

func TestSlackChannelsPeopleAndSend(t *testing.T) {
	var mu sync.Mutex
	got := map[string]any{}
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		w.Write([]byte("ok"))
	}))
	defer hook.Close()
	old := slackHookOK
	slackHookOK = func(u string) bool { return strings.HasPrefix(u, hook.URL) || slackHookRe.MatchString(u) }
	defer func() { slackHookOK = old }()

	s, h := apiServer(t)
	alex := person(t, s, h, "alex@example.com", "Alex", "admin")
	ben := person(t, s, h, "ben@example.com", "Ben", "member")
	var team store.Team
	alex.do("POST", "/api/v1/teams", map[string]any{"name": "Web", "key": "web", "preset": "software"}, 201, &team)
	alex.do("POST", "/api/v1/teams/WEB/members", map[string]any{"user_id": ben.user.ID, "role": "member"}, 200, nil)
	var is store.Issue
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": team.ID, "title": "Fix login"}, 201, &is)

	// only real webhooks / member IDs; members cannot add
	if c := alex.status("POST", "/api/v1/teams/WEB/slack", map[string]any{"kind": "channel", "name": "dev", "value": "https://evil.example.com/x"}); c != 400 {
		t.Fatalf("bad webhook accepted: %d", c)
	}
	if c := ben.status("POST", "/api/v1/teams/WEB/slack", map[string]any{"kind": "person", "name": "Ben", "value": "U01AB2CD3EF"}); c != 403 {
		t.Fatalf("member added a Slack entry: %d", c)
	}
	secretURL := hook.URL + "/services/T000/B000/SECRETSECRET"
	alex.do("POST", "/api/v1/teams/WEB/slack", map[string]any{"kind": "channel", "name": "#athena-dev", "value": secretURL}, 200, nil)
	var list []slackEntry
	alex.do("POST", "/api/v1/teams/WEB/slack", map[string]any{"kind": "person", "name": "Jasenko", "value": "u01ab2cd3ef"}, 200, &list)
	raw, _ := json.Marshal(list)
	if strings.Contains(string(raw), "SECRET") || len(list) != 2 {
		t.Fatalf("webhook URL leaked or list wrong: %s", raw)
	}
	var ch, jp slackEntry
	for _, e := range list {
		if e.Kind == "channel" {
			ch = e
		} else {
			jp = e
		}
	}
	if jp.Value != "U01AB2CD3EF" {
		t.Fatalf("member id not normalised: %q", jp.Value)
	}
	// members can list (to pick) and send
	ben.do("GET", "/api/v1/teams/WEB/slack", nil, 200, &list)
	ben.do("POST", "/api/v1/issues/WEB-1/slack", map[string]any{"channel_id": ch.ID, "people": []int64{jp.ID}, "text": "Login works on Safari again.", "link": "https://github.com/acme/web/pull/12"}, 200, nil)
	mu.Lock()
	text, _ := got["text"].(string)
	mu.Unlock()
	if text != "<@U01AB2CD3EF> Login works on Safari again.\nhttps://github.com/acme/web/pull/12" {
		t.Fatalf("posted %q", text)
	}
	if c := ben.status("POST", "/api/v1/issues/WEB-1/slack", map[string]any{"channel_id": 999, "text": "x"}); c != 400 {
		t.Fatalf("unknown channel: %d", c)
	}
	// the draft works without Pocket AI: the issue title, and says so
	var d struct{ Text, Note string }
	ben.do("POST", "/api/v1/issues/WEB-1/slack/draft", map[string]any{}, 200, &d)
	if d.Text == "" || d.Note == "" {
		t.Fatalf("draft without AI: %+v", d)
	}
}
