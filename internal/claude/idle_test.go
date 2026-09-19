package claude

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestIdleRenewalAndBackoff(t *testing.T) {
	now := time.Unix(1800000000, 0)
	dir := t.TempDir()
	creds := Credentials{AccessToken: "old", RefreshToken: "refresh", ExpiresAt: now.Add(-time.Minute).UnixMilli()}
	r := NewRenewer()
	r.now = func() time.Time { return now }
	r.load = func(context.Context, string) (Credentials, error) { return creds, nil }
	calls := 0
	succeed := false
	r.run = func(ctx context.Context, got, executable string) error {
		calls++
		if got != dir {
			t.Fatal("wrong profile")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("unbounded renewal")
		}
		if succeed {
			creds.AccessToken = "new"
			creds.ExpiresAt = now.Add(8 * time.Hour).UnixMilli()
		}
		return errors.New("private child output must not escape")
	}
	if changed, err := r.Refresh(context.Background(), dir); changed || err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal("failed renewal not sanitized")
	}
	if changed, err := r.Refresh(context.Background(), dir); changed || err != nil || calls != 1 {
		t.Fatal("backoff not honored")
	}
	now = now.Add(2 * time.Minute)
	r.Refresh(context.Background(), dir)
	if calls != 2 {
		t.Fatal("retry not attempted after backoff")
	}
	// A user signing in again must bypass even an active failure backoff.
	creds.AccessToken = "relogin"
	succeed = true
	if changed, err := r.Refresh(context.Background(), dir); !changed || err != nil || calls != 3 {
		t.Fatalf("rotated login did not recover: %v %v", changed, err)
	}
	if changed, err := r.Refresh(context.Background(), dir); changed || err != nil || calls != 3 {
		t.Fatal("fresh credential renewed unnecessarily")
	}
}

func TestIdleRenewalSkipsUnusableCredentials(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name  string
		creds Credentials
		err   error
	}{
		{name: "missing", err: errors.New("missing")},
		{name: "no grant", creds: Credentials{AccessToken: "a", ExpiresAt: now.Add(-time.Hour).UnixMilli()}},
		{name: "unknown expiry", creds: Credentials{AccessToken: "a", RefreshToken: "r"}},
		{name: "fresh", creds: Credentials{AccessToken: "a", RefreshToken: "r", ExpiresAt: now.Add(time.Hour).UnixMilli()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRenewer()
			r.load = func(context.Context, string) (Credentials, error) { return tc.creds, tc.err }
			r.run = func(context.Context, string, string) error { t.Fatal("unexpected child"); return nil }
			changed, err := r.Refresh(context.Background(), t.TempDir())
			if changed || err != nil {
				t.Fatal(changed, err)
			}
		})
	}
}

func TestIdleRenewalRereadsUnderProfileLock(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	reads := 0
	r := NewRenewer()
	r.load = func(context.Context, string) (Credentials, error) {
		reads++
		expiry := now.Add(-time.Minute)
		if reads > 1 {
			expiry = now.Add(time.Hour)
		}
		return Credentials{AccessToken: "a", RefreshToken: "r", ExpiresAt: expiry.UnixMilli()}, nil
	}
	r.run = func(context.Context, string, string) error { t.Fatal("CLI already renewed"); return nil }
	if changed, err := r.Refresh(context.Background(), dir); !changed || err != nil {
		t.Fatal(changed, err)
	}
}

func TestIdleRenewalSerializesSeparateKeepers(t *testing.T) {
	dir := t.TempDir()
	started := make(chan struct{})
	finish := make(chan struct{})
	var calls atomic.Int32
	makeRenewer := func() *Renewer {
		r := NewRenewer()
		r.load = func(context.Context, string) (Credentials, error) {
			return Credentials{AccessToken: "a", RefreshToken: "r", ExpiresAt: time.Now().Add(-time.Minute).UnixMilli()}, nil
		}
		r.run = func(context.Context, string, string) error { calls.Add(1); close(started); <-finish; return nil }
		return r
	}
	first, second := makeRenewer(), makeRenewer()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); first.Refresh(context.Background(), dir) }()
	<-started
	changed, err := second.Refresh(context.Background(), dir)
	close(finish)
	wg.Wait()
	if changed || err != nil || calls.Load() != 1 {
		t.Fatal("concurrent renewal escaped the profile lock")
	}
}

func TestIdleRenewalCancellation(t *testing.T) {
	r := NewRenewer()
	r.load = func(context.Context, string) (Credentials, error) {
		return Credentials{AccessToken: "a", RefreshToken: "r", ExpiresAt: 1}, nil
	}
	r.run = func(ctx context.Context, _, _ string) error { <-ctx.Done(); return ctx.Err() }
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := r.Refresh(ctx, t.TempDir()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestRenewalCommandCannotSubmitAPrompt(t *testing.T) {
	cmd := renewalCommand(context.Background(), "claude", "/profile", "http://127.0.0.1:1234", "/empty")
	b, err := io.ReadAll(cmd.Stdin)
	if err != nil || len(b) != 0 || cmd.Dir != "/empty" || cmd.Stdout != io.Discard || cmd.Stderr != io.Discard {
		t.Fatal("unsafe child IO or directory")
	}
	got := strings.Join(cmd.Args, "|")
	want := `claude|-p|--setting-sources||--settings|{"disableAllHooks":true}|--strict-mcp-config|--mcp-config|{"mcpServers":{}}|--no-session-persistence|--tools|`
	if got != want {
		t.Fatalf("unexpected renewal arguments: %s", got)
	}
	env := renewalEnv([]string{"PATH=/bin", "CLAUDE_CODE_OAUTH_TOKEN=secret", "ANTHROPIC_API_KEY=secret", "CLAUDE_CODE_USE_BEDROCK=1", "_CLAUDE_CODE_OAUTH_CLIENT_ID=override", "CLAUDE_CONFIG_DIR=/wrong", "ANTHROPIC_BASE_URL=https://wrong", "NO_PROXY=internal.test"}, "/profile", "http://127.0.0.1:1234")
	flat := strings.Join(env, "\n")
	for _, bad := range []string{"secret", "/wrong", "override", "BEDROCK"} {
		if strings.Contains(flat, bad) {
			t.Fatal("inherited credential override")
		}
	}
	for _, want := range []string{"CLAUDE_CONFIG_DIR=/profile", "ANTHROPIC_BASE_URL=http://127.0.0.1:1234", "NO_PROXY=localhost,127.0.0.1,::1,internal.test"} {
		if !strings.Contains(flat, want) {
			t.Fatal("missing child setting", want)
		}
	}
}

func TestIdleRenewalKeepsProfilesIndependent(t *testing.T) {
	now := time.Now()
	first, second := t.TempDir(), t.TempDir()
	profiles := map[string]Credentials{
		first:  {AccessToken: "first", RefreshToken: "grant-one", ExpiresAt: now.Add(-time.Minute).UnixMilli()},
		second: {AccessToken: "second", RefreshToken: "grant-two", ExpiresAt: now.Add(-time.Minute).UnixMilli()},
	}
	r := NewRenewer()
	r.load = func(_ context.Context, dir string) (Credentials, error) { return profiles[dir], nil }
	var seen []string
	r.run = func(_ context.Context, dir, _ string) error {
		seen = append(seen, dir)
		c := profiles[dir]
		c.ExpiresAt = now.Add(time.Hour).UnixMilli()
		profiles[dir] = c
		return errors.New("expected empty-input exit")
	}
	for _, dir := range []string{first, second} {
		if changed, err := r.Refresh(context.Background(), dir); !changed || err != nil {
			t.Fatal(changed, err)
		}
	}
	if len(seen) != 2 || profiles[first].RefreshToken != "grant-one" || profiles[second].RefreshToken != "grant-two" {
		t.Fatal("profiles were combined")
	}
}

// Run the real child harness against a fake CLI, without touching credentials.
func TestIdleChildBlocksModelTraffic(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(t.TempDir(), "claude")
	body := "#!/bin/sh\nexec '" + strings.ReplaceAll(executable, "'", "'\\''") + "' -test.run=^TestIdleChildHelper$ -- \"$@\"\n"
	if err := os.WriteFile(fake, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GARCON_RENEWAL_TEST_CHILD", "1")
	t.Setenv("ANTHROPIC_API_KEY", "must-not-inherit")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = runIdleRenewal(ctx, "/isolated-profile", fake)
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("child safety verification failed: %v", err)
	}
}

func TestIdleChildHelper(t *testing.T) {
	if os.Getenv("GARCON_RENEWAL_TEST_CHILD") != "1" {
		return
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil || len(b) != 0 || os.Getenv("ANTHROPIC_API_KEY") != "" || os.Getenv("CLAUDE_CONFIG_DIR") != "/isolated-profile" {
		os.Exit(2)
	}
	cwd, _ := os.Getwd()
	if !strings.HasPrefix(filepath.Base(cwd), "garcon-claude-renew-") {
		os.Exit(3)
	}
	client := &http.Client{Timeout: time.Second}
	response, err := client.Post(os.Getenv("ANTHROPIC_BASE_URL")+"/v1/messages", "application/json", strings.NewReader(`{}`))
	if err != nil {
		os.Exit(4)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		os.Exit(5)
	}
	os.Exit(7)
}

func TestDefaultRenewalUsesDefaultKeychainIdentity(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	env := renewalEnv([]string{"CLAUDE_CONFIG_DIR=/some-other-profile"}, filepath.Join(home, ".claude"), "http://127.0.0.1:1234")
	for _, entry := range env {
		if strings.HasPrefix(entry, "CLAUDE_CONFIG_DIR=") {
			t.Fatal("default login would use a different Keychain service")
		}
	}
}
