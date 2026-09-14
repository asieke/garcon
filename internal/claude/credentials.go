package claude

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Credentials is the claude.ai login Claude Code keeps for itself. Garcon reads it at the moment a
// request needs it and never stores or forwards it anywhere but to Garcon's own proxy route.
type Credentials struct {
	AccessToken      string   `json:"accessToken"`
	RefreshToken     string   `json:"refreshToken"`
	ExpiresAt        int64    `json:"expiresAt"` // unix milliseconds
	Scopes           []string `json:"scopes"`
	SubscriptionType string   `json:"subscriptionType"`
	RateLimitTier    string   `json:"rateLimitTier"`
}

var errNotLoggedIn = errors.New("no claude.ai login found")

// Load reads the login for one Claude Code configuration directory: the credentials file on Linux
// and Windows, the Keychain item on macOS when the file is absent.
func Load(dir string) (Credentials, error) {
	raw, err := os.ReadFile(filepath.Join(dir, ".credentials.json"))
	if errors.Is(err, os.ErrNotExist) && runtime.GOOS == "darwin" {
		// Unverified on a real Mac: Claude Code stores the same JSON under this Keychain service name.
		raw, err = exec.Command("security", "find-generic-password", "-s", "Claude Code-credentials", "-w").Output()
	}
	if err != nil {
		return Credentials{}, fmt.Errorf("%w in %s", errNotLoggedIn, dir)
	}
	return parse(raw)
}

func parse(raw []byte) (Credentials, error) {
	var file struct {
		OAuth Credentials `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(raw), &file); err != nil {
		return Credentials{}, fmt.Errorf("credentials file is not Claude Code JSON: %w", err)
	}
	if file.OAuth.AccessToken == "" {
		return Credentials{}, errNotLoggedIn
	}
	return file.OAuth, nil
}

// Env is what a socket session needs to know it is a claude.ai login: Claude Code reads these instead
// of its credentials file when ANTHROPIC_UNIX_SOCKET is set.
func (c Credentials) Env() []string {
	scopes := c.Scopes
	if len(scopes) == 0 {
		scopes = []string{"user:inference"}
	}
	env := []string{"CLAUDE_CODE_OAUTH_TOKEN=" + c.AccessToken, "CLAUDE_CODE_OAUTH_SCOPES=" + strings.Join(scopes, " ")}
	if c.SubscriptionType != "" {
		env = append(env, "CLAUDE_CODE_SUBSCRIPTION_TYPE="+c.SubscriptionType)
	}
	if c.RateLimitTier != "" {
		env = append(env, "CLAUDE_CODE_RATE_LIMIT_TIER="+c.RateLimitTier)
	}
	return env
}
