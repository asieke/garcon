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
	"garcon/internal/usage"
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

func TestMuseAndAGYLocalImport(t *testing.T) {
	i, _ := setup(t)
	ctx := context.Background()

	// Muse setup
	put(t, filepath.Join(i.Home, ".config/muse/auth.json"), `{"providers":{"meta":{"user_email":"muse@example.com"}}}`)
	museLog := filepath.Join(i.Home, ".local/share/muse/sessions/2026/10/14/sess-muse-1/session.jsonl")
	put(t, museLog, `{"id":"evt-1","stream":{"kind":"session","id":"sess-muse-1"},"recorded_at":1791903248148000,"payload_type":"runtime.session","payload":{"kind":"run","event":{"kind":"model_completed","model":"muse-wonder-1.0","duration_ms":3688,"usage":{"input_tokens":2000,"output_tokens":50,"cached_tokens":500,"cache_write_tokens":100,"cache_read_tokens":500}}}}`+"\n")

	// AGY setup
	put(t, filepath.Join(i.Home, ".gemini/antigravity-cli/settings.json"), `{"model":"Claude Opus 5.5 (Medium)"}`)
	put(t, filepath.Join(i.Home, ".gemini/antigravity-cli/cli.log"), "I1014 10:18:31.015864 1 server_oauth.go:204] applyAuthResult: email=agy@example.com, authMethod=consumer\n")
	agyLog := filepath.Join(i.Home, ".gemini/antigravity-cli/brain/conv-agy-1/.system_generated/logs/transcript.jsonl")
	put(t, agyLog, `{"type":"PLANNER_RESPONSE","step_index":3,"created_at":"2026-10-14T13:19:01Z","status":"DONE","input_tokens":3355,"cache_read_tokens":22222,"output_tokens":485}`+"\n")

	i.Scan(ctx)
	rows := i.Store.All()
	if len(rows) != 2 {
		t.Fatalf("expected 2 imported rows, got %+v", rows)
	}
	if rows[0].Harness != "muse" || rows[0].Provider != "meta" || rows[0].Account != "muse@example.com" || rows[0].Input != 1500 || rows[0].CacheRead != 500 || rows[0].CacheWrite != 100 || rows[0].Output != 50 {
		t.Fatalf("unexpected muse row: %+v", rows[0])
	}
	if rows[1].Harness != "agy" || rows[1].Provider != "google" || rows[1].Account != "agy@example.com" || rows[1].Model != "Claude Opus 5.5 (Medium)" || rows[1].Input != 3355 || rows[1].CacheRead != 22222 || rows[1].Output != 485 {
		t.Fatalf("unexpected agy row: %+v", rows[1])
	}
	if len(i.Store.Recent()) != 0 {
		t.Fatal("muse/agy backfill entered live ticker")
	}
}


func TestMuseHomeTimestampsAndReimport(t *testing.T) {
	i, _ := setup(t)
	t.Setenv("MUSE_HOME", "")
	ctx := context.Background()
	put(t, filepath.Join(i.Home, ".config/muse/auth.json"), `{"providers":{"meta":{"user_email":"muse@example.com"}}}`)
	session := filepath.Join(i.Home, ".muse/sessions/2026/10/14/sess-root")
	// Millisecond timestamps, float counters, camelCase usage and a subagent
	// log; content fields must never be stored.
	put(t, filepath.Join(session, "session.jsonl"),
		`{"id":"evt-ms","stream":{"kind":"session","id":"sess-root"},"recorded_at":1791903248148,"payload_type":"runtime.session","payload":{"kind":"run","event":{"kind":"model_completed","model":"muse-large-2","duration_ms":1200.0,"usage":{"input_tokens":1000.0,"output_tokens":20,"cached_tokens":400}}}}`+"\n"+
			`{"id":"evt-text","stream":{"kind":"session","id":"sess-root"},"recorded_at":1791903248149,"payload_type":"runtime.session","payload":{"kind":"run","event":{"kind":"text_delta","text":"PRIVATE CONVERSATION"}}}`+"\n")
	put(t, filepath.Join(session, "subagent/child-1/session.jsonl"),
		`{"id":"evt-child","stream":{"kind":"session","id":"child-1"},"recorded_at":1791903249000000,"payload_type":"runtime.session","payload":{"kind":"run","event":{"kind":"model_completed","model_id":"muse-small-1","usage":{"inputTokens":300,"outputTokens":5,"cacheReadTokens":100}}}}`+"\n")

	i.Scan(ctx)
	rows := i.Store.All()
	if len(rows) != 2 {
		t.Fatalf("expected 2 Muse rows from ~/.muse, got %+v", rows)
	}
	byModel := map[string]usage.Record{}
	for _, r := range rows {
		byModel[r.Model] = r
		if r.SessionID != "sess-root" || r.Harness != "muse" || r.Time < 1791903248000 || r.Time > 1791903250000 {
			t.Fatalf("unexpected Muse row: %+v", r)
		}
	}
	if r := byModel["muse-large-2"]; r.Input != 600 || r.CacheRead != 400 || r.Output != 20 || r.Ms != 1200 {
		t.Fatalf("root usage: %+v", r)
	}
	if r := byModel["muse-small-1"]; r.Input != 200 || r.CacheRead != 100 || r.Output != 5 {
		t.Fatalf("subagent usage: %+v", r)
	}
	var raw string
	if err := i.Store.DB.QueryRow(`SELECT group_concat(record) FROM requests`).Scan(&raw); err != nil || strings.Contains(raw, "PRIVATE") {
		t.Fatalf("Muse content retained: %v", err)
	}

	// A file consumed by the previous reader (parser 0) is read again, and a
	// corrected time replaces the implausible one stored before.
	bad := byModel["muse-large-2"]
	bad.Time = 1791903248
	if err := i.Store.SaveRecord(bad); err != nil {
		t.Fatal(err)
	}
	if _, err := i.Store.DB.Exec(`UPDATE claude_import_files SET checkpoint=json_remove(checkpoint,'$.Parser')`); err != nil {
		t.Fatal(err)
	}
	i.Scan(ctx)
	for _, r := range i.Store.All() {
		if r.Model == "muse-large-2" && r.Time != 1791903248148 {
			t.Fatalf("re-import did not correct time: %+v", r)
		}
	}
}
