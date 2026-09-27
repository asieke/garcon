package connections

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSavedCodexConnection(t *testing.T) {
	base := "http://127.0.0.1:4141"
	for _, tc := range []struct {
		name, config string
		want         bool
		unknown      bool
	}{
		{"configured", `model_provider = "garcon"
[model_providers.garcon]
base_url = "http://127.0.0.1:4141/codex/backend-api/codex"
wire_api = "responses"`, true, false},
		{"defined but not selected", `[model_providers.garcon]
base_url = "http://127.0.0.1:4141/codex/backend-api/codex"`, false, false},
		{"other provider selected", `model_provider = "openai"
[model_providers.garcon]
base_url = "http://127.0.0.1:4141/codex/backend-api/codex"`, false, false},
		{"local alias and quoted keys", `model_provider = 'proxy'
[model_providers."proxy"]
base_url = 'http://localhost:4141/codex/backend-api/codex/' # local route`, true, false},
		{"wrong port", `model_provider = "garcon"
[model_providers.garcon]
base_url = "http://127.0.0.1:9999/codex/backend-api/codex"`, false, false},
		{"wrong path", `model_provider = "garcon"
[model_providers.garcon]
base_url = "http://127.0.0.1:4141/other"`, false, false},
		{"malformed", `model_provider = "unterminated`, false, true},
		{"empty", ``, false, false},
		{"profile override", `model_provider = "garcon"
profile = "direct"
[profiles.direct]
model_provider = "openai"
[model_providers.garcon]
base_url = "http://127.0.0.1:4141/codex/backend-api/codex"`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(tc.config), 0600); err != nil {
				t.Fatal(err)
			}
			got := Codex(path, base)
			if tc.unknown {
				if got.Connected != nil {
					t.Fatal("malformed config should be unknown")
				}
				return
			}
			if got.Connected == nil || *got.Connected != tc.want {
				t.Fatalf("wrong saved connection: %+v", got)
			}
			// Re-read the file on every check; disconnecting clears the indicator
			// even if previous requests were recorded in the usage ledger.
			if err := os.WriteFile(path, []byte(`model_provider = "openai"`), 0600); err != nil {
				t.Fatal(err)
			}
			if got := Codex(path, base); got.Connected == nil || *got.Connected {
				t.Fatal("stale connected state")
			}
		})
	}
	if got := Codex(filepath.Join(t.TempDir(), "missing.toml"), base); got.Connected == nil || *got.Connected {
		t.Fatal("missing config should be disconnected")
	}
}
