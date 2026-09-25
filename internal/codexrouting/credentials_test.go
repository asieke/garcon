package codexrouting

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRenewUsesAccountReadAndVerifiesSavedIdentity(t *testing.T) {
	for _, wrongIdentity := range []bool{false, true} {
		t.Run(map[bool]string{false: "same-account", true: "different-account"}[wrongIdentity], func(t *testing.T) {
			dir := t.TempDir()
			profile := filepath.Join(dir, "profile")
			saveLogin(t, profile, "a", time.Now().Add(-time.Minute))
			old, err := loadCredential(profile)
			if err != nil {
				t.Fatal(err)
			}
			fresh := filepath.Join(dir, "fresh")
			id := "a"
			if wrongIdentity {
				id = "b"
			}
			saveLogin(t, fresh, id, time.Now().Add(time.Hour))
			// A fake CLI records the RPC methods and mimics Codex saving a login.
			script := "#!/bin/sh\nread init\nprintf '%s\\n' '{\"id\":1,\"result\":{}}'\nread initialized\nread request\nprintf '%s\\n' \"$init\" \"$initialized\" \"$request\" > \"$CODEX_HOME/rpc.txt\"\ncp '" + filepath.Join(fresh, "auth.json") + "' \"$CODEX_HOME/auth.json\"\nprintf '%s\\n' '{\"id\":2,\"result\":{}}'\n"
			if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			err = renew(context.Background(), old)
			if (err != nil) != wrongIdentity {
				t.Fatalf("unexpected renewal result: %v", err)
			}
			rpc, err := os.ReadFile(filepath.Join(profile, "rpc.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(rpc), `"account/read"`) || !strings.Contains(string(rpc), `"refreshToken":true`) || strings.Contains(string(rpc), "thread/") || strings.Contains(string(rpc), "private-refresh") {
				t.Fatal("unexpected RPC content")
			}
		})
	}
}

// Opt-in integration check; never runs as part of the regular test suite.
// The caller explicitly chooses a real profile whose credentials Codex may renew.
func TestLiveCodexRenewal(t *testing.T) {
	profile := os.Getenv("GARCON_TEST_CODEX_PROFILE")
	if profile == "" {
		t.Skip("set GARCON_TEST_CODEX_PROFILE to verify a real managed login renewal")
	}
	c, err := loadCredential(profile)
	if err != nil {
		t.Fatal("could not load selected test profile")
	}
	if err := renew(context.Background(), c); err != nil {
		t.Fatal(err)
	}
}
