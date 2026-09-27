package store

import (
	"encoding/json"
	"garcon/internal/usage"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestSessionsAcrossHarnesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`INSERT INTO sessions(key,session_id,account_id,account,model,created_at,last_seen) VALUES('pin','shared','codex-account','codex@example.test','codex-model',1,2)`); err != nil {
		t.Fatal(err)
	}
	for _, r := range []usage.Record{
		{RequestID: "codex", Harness: "codex", SessionID: "shared", Model: "codex-model", Time: 10, Status: 200},
		{RequestID: "claude", Harness: "claude", SessionID: "shared", Model: "claude-model", Account: "claude@example.test", Time: 20, State: "streaming"},
		{RequestID: "future", Harness: "future", Provider: "openrouter", SessionID: "shared", Model: "future-model", Time: 30, Status: 503, State: "failed", Error: "upstream failed"},
		{RequestID: "unassigned", Harness: "claude", Time: 40, State: "streaming"},
	} {
		if err := s.SaveRecord(r); err != nil {
			t.Fatal(err)
		}
	}
	defer func() { s.Close() }()
	rows, err := s.sessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0]["harness"] != "claude" {
		t.Fatalf("wrong sessions: %+v", rows)
	}
	keys := map[any]bool{}
	for _, row := range rows {
		if row["requests"] != 1 || keys[row["key"]] {
			t.Fatalf("mixed clients: %+v", rows)
		}
		keys[row["key"]] = true
		switch row["harness"] {
		case "codex":
			if row["key"] != "pin" || row["account_id"] != "codex-account" || row["active"] != 0 {
				t.Fatal(row)
			}
		case "claude":
			if row["account"] != "claude@example.test" || row["active"] != 1 || row["model"] != "claude-model" || row["provider"] != "anthropic" {
				t.Fatal(row)
			}
		case "future":
			if row["last_error"] != "upstream failed" || row["provider"] != "openrouter" {
				t.Fatal(row)
			}
		}
	}
	for _, url := range []string{"/api/logs?session_id=shared&harness=claude", "/api/logs?q=future", "/api/logs?q=openrouter"} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
		var result struct {
			Total    int
			Requests []usage.Record
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Total != 1 || len(result.Requests) != 1 {
			t.Fatalf("%s mixed clients: %s", url, w.Body.String())
		}
	}
	var pins int
	if err := s.DB.QueryRow("SELECT count(*) FROM sessions").Scan(&pins); err != nil || pins != 1 {
		t.Fatal("non-Codex traffic changed router pins", err)
	}

	// Reopening retains all clients and marks abandoned streams interrupted.
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rows, err = s.sessions()
	if err != nil || len(rows) != 3 {
		t.Fatalf("lost sessions after restart: %+v %v", rows, err)
	}
	for _, row := range rows {
		if row["harness"] == "claude" && (row["active"] != 0 || row["last_state"] != "interrupted") {
			t.Fatal(row)
		}
	}
}
