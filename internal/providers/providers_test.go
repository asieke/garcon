package providers

import (
	"garcon/internal/database"
	"path/filepath"
	"testing"
)

func TestProviderPersistenceAndBoundaries(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	registry, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"openai", "https://example.com"}, {"chatgpt", "https://example.com"}, {"anthropic", ""}, {"../bad", "https://example.com"}, {"bad", "http://example.com"}, {"bad", "https://key@example.com"}, {"bad", "https://example.com/path"}, {"bad", "https://example.com?key=secret"}} {
		if err := registry.Set(pair[0], pair[1]); err == nil {
			t.Fatalf("accepted %v", pair)
		}
	}
	if err := registry.Set("custom", "https://example.com"); err != nil {
		t.Fatal(err)
	}
	loaded, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	route, ok := loaded.ParseRoute("/pi/custom/v1/chat/completions")
	if !ok || route.Target.Host != "example.com" || route.Rest != "v1/chat/completions" {
		t.Fatal(route, ok)
	}
	if _, ok := loaded.ParseRoute("/codex/custom/v1/responses"); ok {
		t.Fatal("Codex escaped its provider")
	}
	if err := loaded.Set("custom", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.ParseRoute("/pi/custom/v1/chat/completions"); ok {
		t.Fatal("removed provider remained active")
	}
}
