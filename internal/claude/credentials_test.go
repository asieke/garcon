package claude

import (
	"errors"
	"slices"
	"testing"
)

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
