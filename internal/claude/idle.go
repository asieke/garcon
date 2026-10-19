package claude

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type renewalAttempt struct {
	fingerprint [32]byte
	next        time.Time
	failures    int
}

// Renewer delegates credential rotation to Claude Code, so its own cross-process
// auth lock and profile-specific credential store remain authoritative. It never
// exchanges or writes a refresh token itself.
type Renewer struct {
	mu         sync.Mutex
	attempts   map[string]renewalAttempt
	renewing   map[string]chan struct{}
	now        func() time.Time
	load       func(context.Context, string) (Credentials, error)
	run        func(context.Context, string, string) error
	executable string
}

func NewRenewer() *Renewer {
	return &Renewer{attempts: map[string]renewalAttempt{}, renewing: map[string]chan struct{}{}, now: time.Now, load: LoadContext, run: runIdleRenewal}
}

// Refresh checks one profile. A changed login bypasses failure backoff. No login,
// no refresh grant, and tokens with unknown expiry are left alone.
func (r *Renewer) Refresh(ctx context.Context, dir string) (bool, error) {
	for {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		// Keychain reads may block. Healthy logins never wait for other reads or
		// CLI renewals, even when they use this same profile.
		creds, err := r.load(ctx, dir)
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if err != nil || creds.RefreshToken == "" || !needsRenewal(creds.ExpiresAt, r.now()) {
			r.clearAttempt(dir)
			return false, nil
		}
		fingerprint := sha256.Sum256([]byte(creds.AccessToken + "\x00" + creds.RefreshToken))
		r.mu.Lock()
		if done := r.renewing[dir]; done != nil {
			r.mu.Unlock()
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case <-done:
				// Reread the CLI's saved login after the in-flight renewal.
				continue
			}
		}
		previous := r.attempts[dir]
		if previous.fingerprint == fingerprint && r.now().Before(previous.next) {
			r.mu.Unlock()
			return false, nil
		}
		done := make(chan struct{})
		r.renewing[dir] = done
		r.mu.Unlock()
		changed, err := r.refresh(ctx, dir, previous)
		r.mu.Lock()
		delete(r.renewing, dir)
		close(done)
		r.mu.Unlock()
		return changed, err
	}
}

func (r *Renewer) clearAttempt(dir string) {
	r.mu.Lock()
	delete(r.attempts, dir)
	r.mu.Unlock()
}

// refresh has one caller per profile, but never holds the state mutex across
// credential reads, the cross-process profile lock, or the CLI subprocess.
func (r *Renewer) refresh(ctx context.Context, dir string, previous renewalAttempt) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	release, ok := lock(dir)
	if !ok {
		return false, nil
	}
	defer release()
	// A CLI may have renewed while we waited for Garcon's per-profile lock.
	latest, err := r.load(ctx, dir)
	if err != nil {
		return false, nil
	}
	if latest.RefreshToken == "" {
		return false, nil
	}
	if !needsRenewal(latest.ExpiresAt, r.now()) {
		r.clearAttempt(dir)
		return true, nil
	}
	fingerprint := sha256.Sum256([]byte(latest.AccessToken + "\x00" + latest.RefreshToken))
	child, cancel := context.WithTimeout(ctx, renewTimeout)
	defer cancel()
	// Empty input intentionally exits nonzero. Only a fresh persisted login proves
	// renewal succeeded; neither process exit status nor stdout is authoritative.
	_ = r.run(child, dir, r.executable)
	after, err := r.load(ctx, dir)
	if err == nil && after.AccessToken != "" && after.ExpiresAt > latest.ExpiresAt && !needsRenewal(after.ExpiresAt, r.now()) {
		r.clearAttempt(dir)
		return true, nil
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	failures := 1
	if previous.fingerprint == fingerprint {
		failures = previous.failures + 1
	}
	if failures > 5 {
		failures = 5
	}
	delay := time.Minute * 2 * time.Duration(1<<(failures-1))
	r.mu.Lock()
	if len(r.attempts) >= 1024 {
		clear(r.attempts)
	}
	r.attempts[dir] = renewalAttempt{fingerprint, r.now().Add(delay), failures}
	r.mu.Unlock()
	return false, errors.New("Claude CLI could not renew its saved login; retrying with backoff (check CLI installation or sign in again if revoked)")
}

func renewalEnv(env []string, dir, base string) []string {
	clean := make([]string, 0, len(env)+4)
	noProxy := "localhost,127.0.0.1,::1"
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		if key == "NO_PROXY" || key == "no_proxy" {
			noProxy += "," + value
			continue
		}
		if strings.HasPrefix(key, "ANTHROPIC_") || strings.HasPrefix(key, "CLAUDE_") || strings.HasPrefix(key, "_CLAUDE_") {
			continue
		}
		clean = append(clean, entry)
	}
	// Claude distinguishes an explicitly configured directory from its default
	// login, even when the directory string is ~/.claude (notably in Keychain).
	home, _ := os.UserHomeDir()
	abs, _ := filepath.Abs(dir)
	if abs != filepath.Join(home, ".claude") {
		clean = append(clean, "CLAUDE_CONFIG_DIR="+dir)
	}
	return append(clean, "ANTHROPIC_BASE_URL="+base,
		"_CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL=1", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "NO_PROXY="+noProxy, "no_proxy="+noProxy)
}

func runIdleRenewal(ctx context.Context, dir, executable string) error {
	if executable == "" {
		// Launch agents often have a minimal PATH without Claude's native install.
		executable, _ = exec.LookPath("claude")
		if executable == "" {
			home, _ := os.UserHomeDir()
			executable = filepath.Join(home, ".local", "bin", "claude")
		}
	}
	cwd, err := os.MkdirTemp("", "garcon-claude-renew-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(cwd)
	// Defense in depth: even if a future CLI version treats empty input differently,
	// the model API has no upstream. OAuth renewal uses Claude's own auth endpoint.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "model requests disabled during login maintenance", http.StatusForbidden)
	})}
	go server.Serve(ln)
	defer server.Close()
	return renewalCommand(ctx, executable, dir, "http://"+ln.Addr().String(), cwd).Run()
}

func renewalCommand(ctx context.Context, executable, dir, base, cwd string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, executable, "-p", "--setting-sources", "", "--settings", `{"disableAllHooks":true}`,
		"--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`, "--no-session-persistence", "--tools", "")
	cmd.Env = renewalEnv(os.Environ(), dir, base)
	cmd.Dir = cwd
	cmd.Stdin = strings.NewReader("")
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	return cmd
}
