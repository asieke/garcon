package store

import (
	"os"
	"path/filepath"
	"testing"

	"garcon/internal/usage"
)

func TestAllGroupsExistingAccountLabelsWithoutRewritingLogs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	local := []byte("{\"account\":\"a@example.com [chatgpt:workspace-a]\",\"input\":3}\n")
	remote := []byte("{\"id\":\"remote-row\",\"account\":\"a@example.com [chatgpt:workspace-b]\",\"input\":4}\n")
	if err := os.WriteFile(path, local, 0o600); err != nil {
		t.Fatal(err)
	}
	remotePath := filepath.Join(filepath.Dir(path), "remote.jsonl")
	if err := os.WriteFile(remotePath, remote, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.Save(usage.Record{Account: "a@example.com", Input: 5})
	all := s.All()
	var total int64
	for _, r := range all {
		if r.Account != "a@example.com" {
			t.Errorf("unmerged account: %q", r.Account)
		}
		total += r.Input
	}
	if len(all) != 2 || total != 8 {
		t.Fatalf("lost usage: rows=%d, input=%d", len(all), total)
	}
	var original string
	s.DB.QueryRow("SELECT json_extract(record, '$.account') FROM requests ORDER BY sequence LIMIT 1").Scan(&original)
	if original != "a@example.com [chatgpt:workspace-a]" {
		t.Fatal("changed underlying local records")
	}
	got, err := os.ReadFile(remotePath)
	if err != nil || string(got) != string(remote) {
		t.Fatal("changed remote log")
	}
}

func TestRecentIsBoundedAndKeepsIdenticalCompletions(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "usage.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := s.Recent(); got == nil || len(got) != 0 {
		t.Fatalf("empty response: %#v", got)
	}
	for i := 0; i < 35; i++ {
		s.Save(usage.Record{Time: 123, Account: "a@example.com [chatgpt:workspace]", Input: 45000})
	}
	rows := s.Recent()
	if len(rows) != 30 || rows[0].Sequence != 6 || rows[29].Sequence != 35 {
		t.Fatalf("unexpected recent range: %#v", rows)
	}
	for i, row := range rows {
		if row.Sequence != i+6 || row.Account != "a@example.com" || row.Input != 45000 {
			t.Fatalf("lost or remote completion: %#v", row)
		}
	}
	rows[0].Account = "changed"
	if s.Recent()[0].Account != "a@example.com" {
		t.Fatal("recent snapshot changed the underlying log")
	}
}

func TestRecentExcludesHistoryAndLegacyRemoteCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	if err := os.WriteFile(path, []byte("{\"time\":1}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// A cache left by an earlier version must not affect local totals or arrivals.
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "remote.jsonl"), []byte("{\"id\":\"old-remote\",\"time\":1}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if len(s.Recent()) != 0 || s.RequestFeed().TotalRequests != 1 {
		t.Fatal("history entered arrivals or remote cache entered total")
	}
	s.Save(usage.Record{Time: 100})
	s.Save(usage.Record{Time: 200})
	rows := s.Recent()
	if len(rows) != 2 || rows[0].Sequence != 2 || rows[1].Sequence != 3 || s.RequestFeed().TotalRequests != 3 {
		t.Fatalf("wrong local arrivals: %#v", rows)
	}
}

func TestOpenDoesNotCreateRemoteCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "remote.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("remote cache created: %v", err)
	}
}

func TestSQLiteLifecycleAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rec := usage.Record{RequestID: "request-1", SessionID: "session", AccountID: "account", Time: 100, State: "streaming"}
	if err = s.SaveRecord(rec); err != nil {
		t.Fatal(err)
	}
	rec.State = "complete"
	rec.Status = 200
	rec.Input = 7
	s.Save(rec)
	if s.Len() != 1 || len(s.Recent()) != 1 || s.Recent()[0].Input != 7 {
		t.Fatal("request lifecycle created duplicate usage")
	}
	s.Save(usage.Record{RequestID: "interrupted", Time: 101, State: "streaming"})
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var state string
	s.DB.QueryRow("SELECT state FROM requests WHERE request_id='interrupted'").Scan(&state)
	if state != "interrupted" || len(s.All()) != 2 {
		t.Fatalf("restart recovery: %s / %d", state, len(s.All()))
	}
}
