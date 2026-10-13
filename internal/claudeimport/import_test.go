package claudeimport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"garcon/internal/store"
)

func fixture(id, session string, output int, timestamp string) string {
	return fmt.Sprintf(`{"type":"assistant","sessionId":%q,"requestId":%q,"timestamp":%q,"cwd":"/projects/example","entrypoint":"claude-desktop","message":{"id":%q,"model":"claude-test","content":[{"text":"PRIVATE CONVERSATION"}],"usage":{"input_tokens":10,"cache_read_input_tokens":20,"cache_creation_input_tokens":30,"output_tokens":%d}}}`+"\n", session, "req_"+id, timestamp, "msg_"+id, output)
}
func put(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func setup(t *testing.T) (*Importer, string) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	i := &Importer{Store: s, Home: t.TempDir()}
	return i, filepath.Join(i.Home, ".claude-personal/projects/example/session.jsonl")
}

func TestBackfillAppendRestartPrivacyAndDedup(t *testing.T) {
	i, path := setup(t)
	ctx := context.Background()
	meta := filepath.Join(i.Home, ".claude-personal/.claude.json")
	put(t, meta, `{"oauthAccount":{"emailAddress":"personal@example.com"}}`)
	first := fixture("one", "session", 5, "2026-10-01T12:00:00Z")
	final := fixture("one", "session", 9, "2026-10-01T12:00:01Z")
	second := fixture("two", "session", 7, "2026-10-01T12:00:02Z")
	put(t, path, first+final+strings.TrimSuffix(second, "\n"))
	i.Scan(ctx)
	rows := i.Store.All()
	if len(rows) != 1 || rows[0].Output != 9 || rows[0].Account != "personal@example.com" || rows[0].Status != 0 || rows[0].Ms != 0 {
		t.Fatalf("bad backfill: %+v", rows)
	}
	if len(i.Store.Recent()) != 0 {
		t.Fatal("backfill entered ticker")
	}
	i.Scan(ctx) // Partial last line must neither be discarded nor counted early.
	if i.Store.Len() != 1 {
		t.Fatal("partial line counted")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("\nnot-json\n")
	f.Close()
	restarted := &Importer{Store: i.Store, Home: i.Home}
	restarted.Scan(ctx)
	if restarted.Store.Len() != 2 || restarted.Snapshot().SkippedLines != 1 {
		t.Fatalf("append/restart: %+v", restarted.Snapshot())
	}
	put(t, meta, `{"oauthAccount":{"emailAddress":"changed@example.com"}}`)
	put(t, filepath.Join(i.Home, ".claude-dev/projects/copied/session.jsonl"), strings.ReplaceAll(first+final+second, `"sessionId":"session"`, `"sessionId":"copied-session"`))
	restarted.Scan(ctx)
	if i.Store.Len() != 2 {
		t.Fatal("copied transcript counted twice")
	}
	for _, r := range i.Store.All() {
		if r.SessionID != "session" {
			t.Fatal("copied history moved sessions", r)
		}
		if r.Account != "personal@example.com" {
			t.Fatal("history reattributed", r)
		}
	}
	// API-level checks: usage and analytics include imports, sessions identify the
	// project locally, and remote clients never receive filesystem metadata.
	for _, endpoint := range []string{"/api/analytics", "/api/sessions", "/api/logs"} {
		for _, remote := range []string{"127.0.0.1:1234", "192.0.2.1:1234"} {
			r := httptest.NewRequest("GET", "http://127.0.0.1:4141"+endpoint, nil)
			r.RemoteAddr = remote
			w := httptest.NewRecorder()
			i.Store.ServeHTTP(w, r)
			if w.Code != 200 || strings.Contains(w.Body.String(), "PRIVATE CONVERSATION") {
				t.Fatal(endpoint, w.Body.String())
			}
			if endpoint == "/api/analytics" && !strings.Contains(w.Body.String(), `"output":16`) {
				t.Fatal("analytics omitted imports", w.Body.String())
			}
			if endpoint == "/api/sessions" {
				contains := strings.Contains(w.Body.String(), "/projects/example")
				if contains != (remote == "127.0.0.1:1234") {
					t.Fatal("metadata privacy", w.Body.String())
				}
				if !strings.Contains(w.Body.String(), `"active":0`) {
					t.Fatal("invented live activity")
				}
			}
		}
	}
	// Ensure neither the persisted ledger nor checkpoints contain conversation text.
	for _, query := range []string{"SELECT record FROM requests", "SELECT checkpoint FROM claude_import_files"} {
		r, err := i.Store.DB.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		for r.Next() {
			var raw string
			r.Scan(&raw)
			if strings.Contains(raw, "PRIVATE CONVERSATION") {
				t.Fatal("persisted content")
			}
		}
		r.Close()
	}
	before, _ := os.ReadFile(path)
	restarted.Scan(ctx)
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("import changed transcript")
	}
}

func TestRotationOversizeAndSyntheticRows(t *testing.T) {
	i, path := setup(t)
	ctx := context.Background()
	first := fixture("one", "s", 1, "2026-10-01T00:00:00Z")
	put(t, path, first+strings.Repeat("x", maxLine+10)+"\n"+strings.Replace(first, `"type":"assistant"`, `"type":"assistant","isApiErrorMessage":true`, 1))
	i.Scan(ctx)
	if i.Store.Len() != 1 || i.Snapshot().SkippedLines != 1 {
		t.Fatalf("oversize/synthetic handling: %+v", i.Snapshot())
	}
	// Replacing/truncating the file must reset its offset, without deleting history.
	put(t, path, fixture("two", "s", 2, "2026-10-02T00:00:00Z"))
	i.Scan(ctx)
	if i.Store.Len() != 2 {
		t.Fatal("truncation lost new usage")
	}
	put(t, path, fixture("six", "s", 6, "2026-10-03T00:00:00Z"))
	i.Scan(ctx)
	if i.Store.Len() != 3 {
		t.Fatal("same-size replacement missed")
	}
	status := i.Snapshot()
	b, _ := json.Marshal(status)
	if strings.Contains(string(b), i.Home) {
		t.Fatal("status exposes paths")
	}
}

func TestRegisteredCustomProfileAndMissingHomes(t *testing.T) {
	i, _ := setup(t)
	custom := t.TempDir()
	put(t, filepath.Join(custom, "projects/example/subagents/child.jsonl"), fixture("child", "parent", 3, "2026-10-01T00:00:00Z"))
	b, _ := json.Marshal([]map[string]string{{"profile": custom}})
	put(t, filepath.Join(i.Home, ".config/garcon/claude-launchers.json"), string(b))
	i.ExtraHomes = []string{custom, custom}
	i.Scan(context.Background())
	if i.Store.Len() != 1 || i.Snapshot().Files != 1 || i.Snapshot().FailedFiles != 0 {
		t.Fatalf("custom/duplicate roots: %+v", i.Snapshot())
	}
}
