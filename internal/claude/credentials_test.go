package claude

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
)

func TestProfileKeychainIsolation(t *testing.T) {
	home := t.TempDir()
	defaultService := KeychainService(filepath.Join(home, ".claude"), home)
	if defaultService != "Claude Code-credentials" {
		t.Fatal(defaultService)
	}
	services := map[string]bool{}
	for _, name := range []string{".claude-personal", ".claude-dev", ".claude-humanist"} {
		dir := filepath.Join(home, name)
		want := KeychainService(dir, home)
		if want == defaultService || services[want] {
			t.Fatal("profiles share a Keychain entry")
		}
		services[want] = true
		calls := 0
		_, err := loadCredentials(context.Background(), dir, home, true, func(ctx context.Context, service string) ([]byte, error) {
			calls++
			if service != want {
				t.Errorf("lookup %s want %s", service, want)
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Error("unbounded lookup")
			}
			return nil, errors.New("missing")
		})
		if err == nil || calls != 1 {
			t.Fatal("missing profile fell back to another login")
		}
	}
}

func TestParse(t *testing.T) {
	c, err := parse([]byte(`{"claudeAiOauth":{"accessToken":"at","refreshToken":"rt","expiresAt":1789400000000,
		"refreshTokenExpiresAt":1790000000000,"scopes":["user:inference","user:profile"],"subscriptionType":"max","rateLimitTier":"default_claude_max_20x"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.AccessToken != "at" || c.RefreshToken != "rt" || c.ExpiresAt != 1789400000000 || len(c.Scopes) != 2 {
		t.Errorf("parsed %+v", c)
	}
	want := []string{"CLAUDE_CODE_OAUTH_TOKEN=at", "CLAUDE_CODE_OAUTH_SCOPES=user:inference user:profile",
		"CLAUDE_CODE_SUBSCRIPTION_TYPE=max", "CLAUDE_CODE_RATE_LIMIT_TIER=default_claude_max_20x"}
	if got := c.Env(); !slices.Equal(got, want) {
		t.Errorf("Env() = %q", got)
	}
	if got := (Credentials{AccessToken: "at"}).Env(); !slices.Equal(got, []string{"CLAUDE_CODE_OAUTH_TOKEN=at", "CLAUDE_CODE_OAUTH_SCOPES=user:inference"}) {
		t.Errorf("minimal Env() = %q", got)
	}

	if _, err := parse([]byte(`{"claudeAiOauth":{}}`)); !errors.Is(err, errNotLoggedIn) {
		t.Errorf("empty login: %v", err)
	}
	if _, err := parse([]byte(`not json`)); err == nil || errors.Is(err, errNotLoggedIn) {
		t.Errorf("invalid JSON: %v", err)
	}
}
