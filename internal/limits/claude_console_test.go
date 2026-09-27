package limits

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"garcon/internal/database"
)

func TestConsoleRetainsAndDiscoversClaudeAccounts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	dir := filepath.Join(home, ".claude")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(`{"claudeAiOauth":{"accessToken":"test-only","expiresAt":2000000000000}}`), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Put("limits", Snapshot{Accounts: []Account{{ID: "saved-claude", Provider: "claude", Status: "fresh"}, {ID: "saved-codex", Provider: "codex", Status: "fresh"}}}); err != nil {
		t.Fatal(err)
	}
	s := New(filepath.Dir(db.Path), db)
	got := s.Snapshot()
	if len(got.Accounts) != 2 || got.Accounts[0].Provider != "claude" || got.Accounts[0].Status != "stale" {
		t.Fatalf("lost cached Claude account: %+v", got)
	}
	found := false
	for _, src := range s.discover(context.Background()) {
		if src.account.Provider == "claude" {
			found = true
		}
	}
	if !found {
		t.Fatal("console skipped Claude discovery")
	}
}
