package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	st, err := s.store.GetSettings(ctx, u.ID)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	writeJSON(w, http.StatusOK, s.publicSettings(ctx, r, u, st))
}

func (s *Server) publicSettings(ctx context.Context, r *http.Request, u *store.User, st store.Settings) map[string]any {
	feed := ""
	if u.FeedToken != "" {
		feed = s.publicURL(r) + "/feed/" + u.FeedToken + ".ics"
	}
	return map[string]any{
		"settings": st, "has_notify_token": st.NotifyToken != "", "box_timezone": s.cfg.Timezone,
		"box_notify": s.cfg.NotifyURL != "" && s.isOwner(ctx, u.ID), "feed_url": feed,
		"feed_webcal": strings.Replace(strings.Replace(feed, "https://", "webcal://", 1), "http://", "webcal://", 1),
		"push_kinds":  store.PushKinds,
	}
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	st, err := s.store.GetSettings(ctx, u.ID)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	var in struct {
		Title         *string         `json:"title"`
		Timezone      *string         `json:"timezone"`
		WeekStart     *int            `json:"week_start"`
		Density       *string         `json:"density"`
		DefaultTeamID *int64          `json:"default_team_id"`
		HomeView      *string         `json:"home_view"`
		AIModel       *string         `json:"ai_model"`
		NotifyKind    *string         `json:"notify_kind"`
		NotifyURL     *string         `json:"notify_url"`
		NotifyToken   *string         `json:"notify_token"`
		Push          map[string]bool `json:"push"`
		DigestTime    *string         `json:"digest_time"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Title != nil {
		if u.Role != "admin" {
			writeErr(w, http.StatusForbidden, "only an admin can rename the app")
			return
		}
		st.Title = strings.TrimSpace(*in.Title)
		if st.Title == "" {
			st.Title = store.DefaultTitle
		}
		if err := s.store.SaveBoxSettings(ctx, st); err != nil {
			s.fail(w, err, "settings")
			return
		}
	}
	if in.Timezone != nil {
		if *in.Timezone != "" {
			if _, err := time.LoadLocation(*in.Timezone); err != nil {
				writeErr(w, http.StatusBadRequest, "unknown time zone")
				return
			}
		}
		st.Timezone = *in.Timezone
	}
	if in.WeekStart != nil && (*in.WeekStart == 0 || *in.WeekStart == 1 || *in.WeekStart == 6) {
		st.WeekStart = *in.WeekStart
	}
	if in.Density != nil && (*in.Density == "comfortable" || *in.Density == "compact") {
		st.Density = *in.Density
	}
	if in.DefaultTeamID != nil {
		st.DefaultTeamID = *in.DefaultTeamID
	}
	if in.HomeView != nil {
		st.HomeView = *in.HomeView
	}
	if in.AIModel != nil {
		st.AIModel = strings.TrimSpace(*in.AIModel)
	}
	if in.NotifyKind != nil {
		switch *in.NotifyKind {
		case "apprise", "ntfy", "webhook":
			st.NotifyKind = *in.NotifyKind
		default:
			writeErr(w, http.StatusBadRequest, "notify_kind must be apprise, ntfy or webhook")
			return
		}
	}
	if in.NotifyURL != nil {
		st.NotifyURL = strings.TrimSpace(*in.NotifyURL)
	}
	for k, v := range in.Push {
		st.Push[k] = v
	}
	if in.DigestTime != nil {
		if h, m := clockOf(*in.DigestTime); fmt.Sprintf("%02d:%02d", h, m) != "" {
			st.DigestTime = fmt.Sprintf("%02d:%02d", h, m)
		}
	}
	if err := s.store.SaveSettings(ctx, u.ID, st); err != nil {
		s.fail(w, err, "settings")
		return
	}
	if in.NotifyToken != nil {
		_ = s.store.SetUserSetting(ctx, u.ID, "notify_token", strings.TrimSpace(*in.NotifyToken))
	}
	st, _ = s.store.GetSettings(ctx, u.ID)
	writeJSON(w, http.StatusOK, s.publicSettings(ctx, r, u, st))
}

func (s *Server) handleRotateFeed(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	u.FeedToken = store.RandomToken(16)
	if err := s.store.UpdateUser(ctx, u); err != nil {
		s.fail(w, err, "rotate feed")
		return
	}
	s.handleGetSettings(w, r)
}

// notifyRoute is where a person's notifications go: their own route, else
// (for the owner) the box route from the environment.
func (s *Server) notifyRoute(ctx context.Context, uid int64, st store.Settings) (kind, url, token string) {
	if st.NotifyURL != "" {
		return st.NotifyKind, st.NotifyURL, st.NotifyToken
	}
	if s.cfg.NotifyURL != "" && s.isOwner(ctx, uid) {
		return s.cfg.NotifyKind, s.cfg.NotifyURL, ""
	}
	return "", "", ""
}

func (s *Server) handleTestNotify(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	st, _ := s.store.GetSettings(ctx, u.ID)
	kind, url, token := s.notifyRoute(ctx, u.ID, st)
	if url == "" {
		writeErr(w, http.StatusBadRequest, "set a notification address first")
		return
	}
	if err := notify.Send(ctx, kind, url, token, "Pocket Projects", "Notifications work. Assignments, mentions and due dates will look like this.", s.publicURL(r)); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	info := map[string]any{
		"version": Version, "go": runtime.Version(), "uptime": time.Since(s.started).Round(time.Second).String(),
		"counts": s.store.UserCounts(ctx, u.ID), "core": s.coreStatus(), "auth_mode": s.cfg.AuthMode,
		"base_url": s.cfg.BaseURL, "timezone": s.cfg.Timezone,
	}
	if u.Role == "admin" {
		if fi, err := os.Stat(s.cfg.DBPath); err == nil {
			info["db_size"] = fi.Size()
		}
		info["users"], _ = s.store.CountUsers(ctx)
	}
	writeJSON(w, http.StatusOK, info)
}

// handleBackup streams a consistent copy of the whole database; only an
// admin, and only while they are the only person on the box.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	if u.Role != "admin" {
		writeErr(w, http.StatusForbidden, "only an admin can download the database")
		return
	}
	if n, _ := s.store.CountUsers(ctx); n > 1 {
		writeErr(w, http.StatusForbidden, "the database holds other people's work; back it up on the server (pocket-projects backup) or export a team as JSON")
		return
	}
	tmp := fmt.Sprintf("%s/backups/download-%d.db", s.cfg.DataDir, time.Now().UnixNano())
	if err := s.store.Backup(ctx, tmp); err != nil {
		s.fail(w, err, "backup")
		return
	}
	defer os.Remove(tmp)
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-projects-`+time.Now().Format("20060102")+`.db"`)
	http.ServeFile(w, r, tmp)
}

// clockOf reads "HH:MM" (08:00 when unreadable).
func clockOf(v string) (int, int) {
	var h, m int
	if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d:%d", &h, &m); err != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 8, 0
	}
	return h, m
}
