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
