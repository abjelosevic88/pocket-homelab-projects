package server

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// The person's inbox on each connected instance, read every two minutes with
// their token there: new entries are pushed like their own (same kinds, same
// address, linking to the workspace here) and the unread count shows in the
// workspace switcher. The other instance can't reach this one, so it is
// asked, never told. The first poll of a connection only takes note.

const remoteInboxEvery = 2 * time.Minute
const remoteInboxPushes = 5 // per connection and poll; the rest is summed up

type remoteNotification struct {
	ID     int64   `json:"id"`
	Kind   string  `json:"kind"`
	Title  string  `json:"title"`
	Body   string  `json:"body"`
	ReadAt *string `json:"read_at"`
	Issue  *struct {
		Identifier string `json:"identifier"`
	} `json:"issue"`
}

func (s *Server) remoteInboxLoop(ctx context.Context) {
	tick := time.NewTicker(remoteInboxEvery)
	defer tick.Stop()
	for {
		s.remoteInboxJob(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (s *Server) remoteInboxJob(ctx context.Context) {
	list, err := s.store.AllRemotes(ctx)
	if err != nil {
		s.log.Warn("connected inboxes", "err", err)
		return
	}
	for i := range list {
		s.pollRemoteInbox(ctx, &list[i])
	}
}

func (s *Server) pollRemoteInbox(ctx context.Context, rem *store.Remote) {
	if rem.Token == "" {
		return // sealed under a lost key: waits for a new token
	}
	var in struct {
		Notifications []remoteNotification `json:"notifications"`
		Unread        int                  `json:"unread"`
	}
	if err := s.remoteCall(ctx, rem, "GET", "/api/v1/notifications?limit=50", nil, &in); err != nil {
		s.log.Debug("connected inbox", "host", rem.Host(), "err", err)
		return
	}
	seen := rem.InboxSeen
	fresh := []remoteNotification{}
	for _, n := range in.Notifications {
		if n.ID > seen && rem.InboxSeen >= 0 && n.ReadAt == nil {
			fresh = append(fresh, n)
		}
	}
	for _, n := range in.Notifications {
		seen = max(seen, n.ID)
	}
	seen = max(seen, 0)
	if seen == rem.InboxSeen && in.Unread == rem.Unread {
		return
	}
	if err := s.store.SetRemoteInbox(ctx, rem.ID, seen, in.Unread); err != nil {
		s.log.Warn("connected inbox", "host", rem.Host(), "err", err)
		return
	}
	s.send(ctx, change{Kind: "remotes", users: []int64{rem.UserID}})
	sort.Slice(fresh, func(i, j int) bool { return fresh[i].ID < fresh[j].ID })
	s.pushRemote(ctx, rem, fresh)
}

// pushRemote sends the new entries the person wants pushed, oldest first.
func (s *Server) pushRemote(ctx context.Context, rem *store.Remote, fresh []remoteNotification) {
	st, err := s.store.GetSettings(ctx, rem.UserID)
	if err != nil {
		return
	}
	kind, addr, token := s.notifyRoute(ctx, rem.UserID, st)
	if addr == "" {
		return
	}
	wanted := fresh[:0]
	for _, n := range fresh {
		if st.Push[n.Kind] {
			wanted = append(wanted, n)
		}
	}
	base := strings.TrimRight(s.cfg.BaseURL, "/") + "/r/" + strconv.FormatInt(rem.ID, 10)
	name := firstNonEmpty(rem.Name, rem.Host())
	for i, n := range wanted {
		title, body, link := name+" · "+n.Title, n.Body, base+"/inbox"
		if n.Issue != nil && n.Issue.Identifier != "" {
			link = base + "/issue/" + url.PathEscape(n.Issue.Identifier)
		}
		if i == remoteInboxPushes-1 && len(wanted) > remoteInboxPushes {
			title, body, link = name+" · "+fmt.Sprintf("%d new in your inbox", len(wanted)-i), "Open the inbox there to see them all.", base+"/inbox"
		}
		c, cancel := context.WithTimeout(ctx, 20*time.Second)
		if err := notify.Send(c, kind, addr, token, title, body, link); err != nil {
			s.log.Warn("push failed", "user", rem.UserID, "host", rem.Host(), "err", err)
		}
		cancel()
		if i == remoteInboxPushes-1 {
			return
		}
	}
}
