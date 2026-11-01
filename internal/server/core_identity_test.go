package server

import (
	"context"
	"testing"
)

// A name the person picks in Projects survives later sign-ins through Pocket
// Core, which would otherwise write the Core account name back.
func TestCoreSignInKeepsChosenName(t *testing.T) {
	s, h := apiServer(t)
	ctx := context.Background()
	me := person(t, s, h, "ana@example.com", "Ana", "admin")
	sync := func(coreName string) string {
		t.Helper()
		u, err := s.identityUser(ctx, "https://core.example", "sub-ana", "ana@example.com", coreName, true)
		if err != nil {
			t.Fatal(err)
		}
		return u.Name
	}
	if got := sync("Admin"); got != "Admin" {
		t.Fatalf("before a local rename the Core name should apply, got %q", got)
	}
	me.do("PUT", "/api/v1/auth/me", map[string]string{"name": "Ana Petrović"}, 200, nil)
	if got := sync("Admin"); got != "Ana Petrović" {
		t.Fatalf("sign-in overwrote the chosen name: %q", got)
	}
	if got := sync(""); got != "Ana Petrović" {
		t.Fatalf("empty Core name changed it: %q", got)
	}
}
