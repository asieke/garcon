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
	defer s.logFile.Close()
	defer s.remoteFile.Close()
	s.Save(usage.Record{Account: "a@example.com", Input: 5})
	all := s.All()
	var total int64
	for _, r := range all {
		if r.Account != "a@example.com" {
			t.Errorf("unmerged account: %q", r.Account)
		}
		total += r.Input
	}
	if len(all) != 3 || total != 12 {
		t.Fatalf("lost usage: rows=%d, input=%d", len(all), total)
	}
	if s.Batch(0, 1)[0].Account != "a@example.com [chatgpt:workspace-a]" || s.Remote()[0].Account != "a@example.com [chatgpt:workspace-b]" {
		t.Fatal("changed underlying sync records")
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
	defer s.logFile.Close()
	defer s.remoteFile.Close()
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
	if s.Recent()[0].Account != "a@example.com" || s.Batch(0, 1)[0].Account != "a@example.com [chatgpt:workspace]" {
		t.Fatal("recent snapshot changed the underlying log")
	}
}

func TestRecentIncludesNewSyncArrivalsWithoutHistoryOrDuplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	if err := os.WriteFile(path, []byte("{\"time\":1,\"account\":\"old-local\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "remote.jsonl"), []byte("{\"id\":\"old-remote\",\"time\":1}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.logFile.Close()
	defer s.remoteFile.Close()
	if len(s.Recent()) != 0 {
		t.Fatal("loaded history entered the ticker")
	}
	if s.RequestFeed().TotalRequests != 2 {
		t.Fatal("all-machine total omitted stored history")
	}
	s.SetDevice("Laptop")
	s.Save(usage.Record{Time: 100, Account: "local"})
	remote := usage.Record{ID: "new-remote", DeviceID: "desktop-id", Device: "Desktop", Time: 10, Account: "remote"}
	if !s.AddRemote(remote) || s.AddRemote(remote) || s.AddRemote(usage.Record{ID: "old-remote"}) {
		t.Fatal("sync deduplication failed")
	}
	s.Save(usage.Record{Time: 200, Account: "local-again"})
	rows := s.Recent()
	if len(rows) != 3 || rows[0].Sequence != 3 || rows[1].Sequence != 4 || rows[2].Sequence != 5 || !rows[1].Remote || rows[0].Remote || rows[1].Device != "Desktop" {
		t.Fatalf("wrong arrival order or device: %#v", rows)
	}
	if s.RequestFeed().TotalRequests != 5 {
		t.Fatal("all-machine total lost arrivals or counted duplicate sync rows")
	}
	s.SetDevice("Renamed laptop")
	if s.Recent()[0].Device != "Renamed laptop" || s.Recent()[1].Device != "Desktop" {
		t.Fatal("device labels mixed across machines")
	}
	s.ClearRemote()
	if s.RequestFeed().TotalRequests != 3 {
		t.Fatal("cleared remote history remained in total")
	}
	if rows := s.Recent(); len(rows) != 2 || rows[1].Sequence != 5 {
		t.Fatal("clearing remote rows broke the arrival sequence")
	}
}
