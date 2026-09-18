package claude

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Credentials is the claude.ai login Claude Code keeps for itself. Garcon reads it at the moment a
// request needs it. Credentials are never persisted by Garcon.
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return LoadContext(ctx, dir)
}

// KeychainService matches Claude Code's per-configuration-directory macOS login.
func KeychainService(dir, home string) string {
	dir, _ = filepath.Abs(dir)
	if filepath.Clean(dir) == filepath.Join(home, ".claude") {
		return "Claude Code-credentials"
	}
	digest := sha256.Sum256([]byte(filepath.Clean(dir)))
	return fmt.Sprintf("Claude Code-credentials-%x", digest[:4])
}

func LoadContext(ctx context.Context, dir string) (Credentials, error) {
	home, _ := os.UserHomeDir()
	return loadCredentials(ctx, dir, home, runtime.GOOS == "darwin", func(ctx context.Context, service string) ([]byte, error) {
		return exec.CommandContext(ctx, "security", "find-generic-password", "-s", service, "-w").Output()
	})
}

func loadCredentials(ctx context.Context, dir, home string, mac bool, keychain func(context.Context, string) ([]byte, error)) (Credentials, error) {
	raw, err := os.ReadFile(filepath.Join(dir, ".credentials.json"))
	if errors.Is(err, os.ErrNotExist) && mac {
		lookup, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		raw, err = keychain(lookup, KeychainService(dir, home))
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
