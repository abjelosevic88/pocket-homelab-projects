package server

import "testing"

// The chat key is the person's: ⌘I unless they chose another (or none);
// a key without ⌘/Ctrl or Alt, or in the wrong order, is refused.
func TestChatKeySetting(t *testing.T) {
	s, h := apiServer(t)
	alex := person(t, s, h, "alex@example.com", "Alex", "admin")
	var got struct {
		Settings struct {
			ChatKey string `json:"chat_key"`
		} `json:"settings"`
	}
	alex.do("GET", "/api/v1/settings", nil, 200, &got)
	if got.Settings.ChatKey != "mod+i" {
		t.Fatalf("default: %q", got.Settings.ChatKey)
	}
	for _, k := range []string{"i", "shift+i", "shift+mod+i", "mod+mod+i", "mod+ii", "mod+", "ctrl+i"} {
		if c := alex.status("PUT", "/api/v1/settings", map[string]any{"chat_key": k}); c != 400 {
			t.Fatalf("%q: %d", k, c)
		}
	}
	for _, k := range []string{"mod+shift+l", "alt+/", "OFF", "mod+alt+shift+9"} {
		alex.do("PUT", "/api/v1/settings", map[string]any{"chat_key": k}, 200, nil)
	}
	alex.do("GET", "/api/v1/settings", nil, 200, &got)
	if got.Settings.ChatKey != "mod+alt+shift+9" {
		t.Fatalf("saved: %q", got.Settings.ChatKey)
	}
	alex.do("PUT", "/api/v1/settings", map[string]any{"chat_key": ""}, 200, nil)
	alex.do("GET", "/api/v1/settings", nil, 200, &got)
	if got.Settings.ChatKey != "mod+i" {
		t.Fatalf("reset: %q", got.Settings.ChatKey)
	}
}
