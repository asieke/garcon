package claude

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestProfilePrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if got, err := profileDir(""); err != nil || got != filepath.Join(home, ".claude") {
		t.Fatal("default profile", got, err)
	}
	envProfile := filepath.Join(home, ".claude-work")
	t.Setenv("CLAUDE_CONFIG_DIR", envProfile)
	if got, err := profileDir(""); err != nil || got != envProfile {
		t.Fatal("environment profile", got, err)
	}
	explicit := filepath.Join(home, ".claude-personal")
	if got, err := profileDir(explicit); err != nil || got != explicit {
		t.Fatal("explicit profile", got, err)
	}
}

func TestAccountFlagIsRejected(t *testing.T) {
	for _, args := range [][]string{{"--account", "old@example.com"}, {"--account=old@example.com"}} {
		if code, _ := run(args); code != 2 {
			t.Errorf("run(%v) returned %d, want usage error", args, code)
		}
	}
}

func TestRemoteControlIsExplicit(t *testing.T) {
	args := []string{"--resume", "session-id"}
	if got := launchArgs(false, args); !slices.Equal(got, args) {
		t.Fatal(got)
	}
	if got := launchArgs(true, args); !slices.Equal(got, []string{"--remote-control", "--resume", "session-id"}) {
		t.Fatal(got)
	}
	if !slices.Equal(args, []string{"--resume", "session-id"}) {
		t.Fatal("changed caller arguments")
	}
	if code, err := run([]string{"rc", "--help"}); code != 0 || err != nil {
		t.Fatal(code, err)
	}
}
