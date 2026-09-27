package connections

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegisteredClaudeLaunchers(t *testing.T) {
	home := t.TempDir()
	base := "http://127.0.0.1:4141"
	settings := filepath.Join(home, ".claude", "settings.json")
	profile := filepath.Join(home, ".claude-personal")
	os.MkdirAll(profile, 0700)
	os.WriteFile(filepath.Join(profile, "settings.json"), []byte(`{"remoteControlAtStartup":true}`), 0600)
	launcher := filepath.Join(home, "claude-personal")
	body := []byte("#!/bin/sh\nexec garcon claude --config-dir " + profile + " -- \"$@\"\n")
	os.WriteFile(launcher, body, 0700)
	// A login or RC setting alone must never be called a Garcon connection.
	if status := ClaudeConfigured(home, settings, base); status.Connected == nil || *status.Connected {
		t.Fatal(status)
	}
	for i := 0; i < 2; i++ {
		if err := RegisterClaudeLauncher(home, launcher, profile, base); err != nil {
			t.Fatal(err)
		}
	}
	status := ClaudeConfigured(home, settings, base)
	if status.Connected == nil || !*status.Connected || status.Mode != "socket" || status.Profiles != 1 || !status.RemoteControl {
		t.Fatal(status)
	}
	if status := ClaudeConfigured(home, settings, "http://127.0.0.1:4242"); *status.Connected {
		t.Fatal("wrong endpoint reported configured")
	}
	// RC can be disabled while the launcher still routes through Garcon.
	os.WriteFile(filepath.Join(profile, "settings.json"), []byte(`{"remoteControlAtStartup":false}`), 0600)
	if status := ClaudeConfigured(home, settings, base); !*status.Connected || status.RemoteControl {
		t.Fatal(status)
	}
	// A launcher must not hide the separately configured default HTTP gateway.
	os.MkdirAll(filepath.Dir(settings), 0700)
	os.WriteFile(settings, []byte(`{"env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:4141/claude"}}`), 0600)
	if status := ClaudeConfigured(home, settings, base); !*status.Connected || status.Mode != "http+socket" || status.Profiles != 1 {
		t.Fatal(status)
	}
	os.Remove(settings)
	// Changed and removed launchers invalidate the registration, without execution.
	os.WriteFile(launcher, []byte("#!/bin/sh\nexec claude \"$@\"\n"), 0700)
	if status := ClaudeConfigured(home, settings, base); *status.Connected {
		t.Fatal("stale launcher reported configured")
	}
	os.Remove(launcher)
	if status := ClaudeConfigured(home, settings, base); *status.Connected {
		t.Fatal("missing launcher reported configured")
	}
	info, err := os.Stat(launcherRegistry(home))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("registry permissions", err)
	}
}
