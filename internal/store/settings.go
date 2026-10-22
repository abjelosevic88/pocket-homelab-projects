package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

// DefaultTitle is the app title until someone changes it.
const DefaultTitle = "Pocket Projects"

// PushKinds are the notification kinds a person may also get pushed.
var PushKinds = []string{"assigned", "mentioned", "comment", "status", "due", "project_update", "update_due", "added"}

// Settings are the box settings plus one person's preferences.
type Settings struct {
	Title         string `json:"title"`
	SetupComplete bool   `json:"setup_complete"`

	Timezone      string `json:"timezone"`   // "" = the box's
	WeekStart     int    `json:"week_start"` // 0 Sunday, 1 Monday
	Density       string `json:"density"`    // comfortable | compact
	DefaultTeamID int64  `json:"default_team_id"`
	HomeView      string `json:"home_view"` // my-issues | inbox | team | projects
	AIModel       string `json:"ai_model"`  // Pocket AI model override ("" = Pocket AI's default)

	NotifyKind  string          `json:"notify_kind"` // apprise | ntfy | webhook
	NotifyURL   string          `json:"notify_url"`
	NotifyToken string          `json:"-"`
	Push        map[string]bool `json:"push"`        // which kinds are also pushed
	DigestTime  string          `json:"digest_time"` // "08:00": due reminders go out then

	// Other Pocket apps: app → "core" | "direct" | "off".
	Integrations     map[string]string `json:"integrations"`
	IntegrationURLs  map[string]string `json:"integration_urls"`
	IntegrationToken map[string]bool   `json:"integration_tokens"`
}

func defaults() Settings {
	push := map[string]bool{}
	for _, k := range PushKinds {
		push[k] = k == "assigned" || k == "mentioned" || k == "due" || k == "update_due"
	}
	return Settings{
		Title: DefaultTitle, WeekStart: 1, Density: "comfortable", HomeView: "my-issues",
		NotifyKind: "apprise", Push: push, DigestTime: "08:00",
		Integrations: map[string]string{}, IntegrationURLs: map[string]string{}, IntegrationToken: map[string]bool{},
	}
}

// GetSettings loads the box settings plus the preferences of userID, with
// defaults applied. userID 0 gives the box settings with default preferences.
func (s *Store) GetSettings(ctx context.Context, userID int64) (Settings, error) {
	st := defaults()
	rows, err := s.DB.QueryContext(ctx, `SELECT key, value FROM settings WHERE key IN ('title', 'setup_complete')
		UNION ALL SELECT key, value FROM user_settings WHERE user_id = ?`, userID)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return st, err
		}
		switch {
		case k == "title":
			if v != "" {
				st.Title = v
			}
		case k == "setup_complete":
			st.SetupComplete = v == "1"
		case k == "timezone":
			st.Timezone = v
		case k == "week_start":
			st.WeekStart, _ = strconv.Atoi(v)
		case k == "density":
			st.Density = v
		case k == "default_team_id":
			st.DefaultTeamID, _ = strconv.ParseInt(v, 10, 64)
		case k == "home_view":
			st.HomeView = v
		case k == "ai_model":
			st.AIModel = v
		case k == "notify_kind":
			st.NotifyKind = v
		case k == "notify_url":
			st.NotifyURL = v
		case k == "notify_token":
			st.NotifyToken = v
		case k == "digest_time":
			st.DigestTime = v
		case strings.HasPrefix(k, "push."):
			st.Push[strings.TrimPrefix(k, "push.")] = v == "1"
		case strings.HasPrefix(k, "app.mode."):
			st.Integrations[strings.TrimPrefix(k, "app.mode.")] = v
		case strings.HasPrefix(k, "app.url."):
			st.IntegrationURLs[strings.TrimPrefix(k, "app.url.")] = v
		case strings.HasPrefix(k, "app.token."):
			st.IntegrationToken[strings.TrimPrefix(k, "app.token.")] = v != ""
		}
	}
	return st, rows.Err()
}

func b2s(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// SaveSettings stores a person's preferences (not the notify token, not integrations).
func (s *Store) SaveSettings(ctx context.Context, userID int64, st Settings) error {
	kv := map[string]string{
		"timezone": st.Timezone, "week_start": strconv.Itoa(st.WeekStart), "density": st.Density,
		"default_team_id": strconv.FormatInt(st.DefaultTeamID, 10), "home_view": st.HomeView, "ai_model": st.AIModel,
		"notify_kind": st.NotifyKind, "notify_url": st.NotifyURL, "digest_time": st.DigestTime,
	}
	for k, v := range st.Push {
		kv["push."+k] = b2s(v)
	}
	return s.putUserSettings(ctx, userID, kv)
}

// UserSetting reads one raw key of a person's settings ("" when missing).
func (s *Store) UserSetting(ctx context.Context, userID int64, key string) string {
	var v string
	_ = s.DB.QueryRowContext(ctx, `SELECT value FROM user_settings WHERE user_id = ? AND key = ?`, userID, key).Scan(&v)
	return v
}

// SetUserSetting stores one key for a person.
func (s *Store) SetUserSetting(ctx context.Context, userID int64, key, value string) error {
	return s.putUserSettings(ctx, userID, map[string]string{key: value})
}

// SaveBoxSettings stores the settings of the box itself: title and setup state.
func (s *Store) SaveBoxSettings(ctx context.Context, st Settings) error {
	kv := map[string]string{"title": st.Title, "setup_complete": b2s(st.SetupComplete)}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for k, v := range kv {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// BoxSetting reads one box-wide key ("" when missing).
func (s *Store) BoxSetting(ctx context.Context, key string) string {
	var v string
	_ = s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	return v
}

// SetBoxSetting stores one box-wide key.
func (s *Store) SetBoxSetting(ctx context.Context, key, value string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (s *Store) putUserSettings(ctx context.Context, userID int64, kv map[string]string) error {
	if userID <= 0 {
		return ErrNotFound
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for k, v := range kv {
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_settings (user_id, key, value) VALUES (?, ?, ?)
			ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value`, userID, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// OwnerID is the account the box was set up with (the first user): keys
// given to the whole box through the environment are theirs.
func (s *Store) OwnerID(ctx context.Context) int64 {
	var id sql.NullInt64
	_ = s.DB.QueryRowContext(ctx, `SELECT MIN(id) FROM users`).Scan(&id)
	return id.Int64
}

// UserIDs lists every user id.
func (s *Store) UserIDs(ctx context.Context) []int64 {
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM users ORDER BY id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}

// ReminderOnce records key for a person and reports whether it is new.
func (s *Store) ReminderOnce(ctx context.Context, userID int64, key string) bool {
	res, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO reminder_log (user_id, key) VALUES (?, ?)`, userID, key)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n == 1
}

// PruneReminders forgets markers older than days.
func (s *Store) PruneReminders(ctx context.Context, days int) {
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM reminder_log WHERE sent_at < strftime('%Y-%m-%dT%H:%M:%fZ', 'now', ?)`, "-"+strconv.Itoa(days)+" days")
}
