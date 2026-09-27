package connections

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeSavedConnection(t *testing.T) {
	for _, tc := range []struct {
		body      string
		connected bool
		known     bool
	}{
		{`{"env":{"ANTHROPIC_BASE_URL":"http://localhost:4141/claude"}}`, true, true},
		{`{"env":{"ANTHROPIC_BASE_URL":"https://api.anthropic.com"}}`, false, true},
		{`{"env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:4242/claude"}}`, false, true},
		{`{"env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:4141/codex"}}`, false, true},
		{`{}`, false, true},
		{`broken`, false, false},
	} {
		p := filepath.Join(t.TempDir(), "settings.json")
		if err := os.WriteFile(p, []byte(tc.body), 0600); err != nil {
			t.Fatal(err)
		}
		got := Claude(p, "http://127.0.0.1:4141")
		if (got.Connected != nil) != tc.known || (tc.known && *got.Connected != tc.connected) {
			t.Fatalf("unexpected status for %s: %+v", tc.body, got)
		}
	}
}
