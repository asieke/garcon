package claude

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// A socket session never renews the login it was given, and re-reads the credentials file only when a
// request fails with 401. Claude Code does renew a stored login when a normal run starts inside the
// last five minutes before expiry, so that is what the loop triggers: one short, cheap request through
// the base-URL route, recorded like any other row.
const (
	renewMargin  = 5 * time.Minute
	renewPeriod  = time.Minute
	renewTimeout = 2 * time.Minute
	renewModel   = "claude-haiku-4-5-20251001"
	lockName     = ".garcon-renew.lock"
)

// sessionVars are what this process gives its child; the renewal run must not inherit them, and the
// user's shell may export some of them too.
var sessionVars = []string{"ANTHROPIC_UNIX_SOCKET", "CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_OAUTH_SCOPES", "CLAUDE_CODE_SUBSCRIPTION_TYPE", "CLAUDE_CODE_RATE_LIMIT_TIER"}

func needsRenewal(expiresAt int64, now time.Time) bool {
	return expiresAt > 0 && time.UnixMilli(expiresAt).Sub(now) < renewMargin
}

// lock serialises renewals for one configuration directory across concurrent sessions. A lock older
// than the renewal timeout was left by a crash and is taken over.
func lock(dir string) (release func(), ok bool) {
	path := filepath.Join(dir, lockName)
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			fmt.Fprint(f, os.Getpid())
			f.Close()
			return func() { os.Remove(path) }, true
		}
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > renewMargin {
			os.Remove(path)
			continue
		}
		return nil, false
	}
	return nil, false
}

// without drops the named variables from an environment.
func without(env []string, names ...string) []string {
	kept := env[:0:0]
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		drop := false
		for _, name := range names {
			if key == name {
				drop = true
				break
			}
		}
		if !drop {
			kept = append(kept, kv)
		}
	}
	return kept
}

type session struct {
	claude string // path to the claude executable
	dir    string // Claude Code configuration directory
	base   string // Garcon's URL
}

func (s session) route() string { return s.base + "/claude" }

// renewLoop checks at once and then every minute, until the session ends.
func (s session) renewLoop(ctx context.Context) {
	ticker := time.NewTicker(renewPeriod)
	defer ticker.Stop()
	for {
		if err := s.renewIfDue(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "garcon claude: login renewal failed:", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s session) renewIfDue(ctx context.Context) error {
	creds, err := Load(s.dir)
	if err != nil || creds.RefreshToken == "" || !needsRenewal(creds.ExpiresAt, time.Now()) {
		return nil
	}
	release, ok := lock(s.dir)
	if !ok {
		return nil // another session is renewing
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, renewTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.claude, "-p", "ok", "--model", renewModel, "--no-session-persistence",
		"--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`)
	cmd.Env = append(without(os.Environ(), sessionVars...),
		"ANTHROPIC_BASE_URL="+s.route(), "_CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL=1", "CLAUDE_CONFIG_DIR="+s.dir)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Run(); err != nil {
		return err
	}
	if after, err := Load(s.dir); err == nil && after.ExpiresAt <= creds.ExpiresAt {
		return fmt.Errorf("claude ran but the login in %s did not move forward", s.dir)
	}
	return nil
}
