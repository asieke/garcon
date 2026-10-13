package store

import (
	"garcon/internal/usage"
	"path/filepath"
	"testing"
)

func TestClaudeImportProxyReconciliation(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		for _, importFirst := range []bool{false, true} {
			t.Run(fmtName(legacy, importFirst), func(t *testing.T) {
				s, err := Open(filepath.Join(t.TempDir(), "usage.db"))
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close()
				proxy := usage.Record{RequestID: "proxy", Harness: "claude", Kind: "completion", SessionID: "session", Account: "observed@example.com", Model: "claude-test", Time: 10000, Ms: 500, Input: 10, Output: 20, Status: 200, State: "complete", MessageID: "msg_one", UpstreamRequestID: "req_one"}
				local := proxy
				local.RequestID = "claude-local:msg_one"
				local.Source = "claude-local"
				local.Account = "profile@example.com"
				local.AccountAttribution = "profile"
				local.State = "recorded"
				local.Status = 0
				local.Ms = 0
				local.Time = 10200
				if legacy {
					proxy.MessageID = ""
					proxy.UpstreamRequestID = ""
				}
				order := []usage.Record{proxy, local}
				if importFirst {
					order = []usage.Record{local, proxy}
				}
				for _, r := range order {
					if err = s.SaveRecord(r); err != nil {
						t.Fatal(err)
					}
				}
				if err = s.SaveRecord(local); err != nil {
					t.Fatal(err)
				}
				rows := s.All()
				if len(rows) != 1 || rows[0].Account != "observed@example.com" || rows[0].Output != 20 || rows[0].Ms != 500 || rows[0].Source != "" {
					t.Fatalf("wrong reconciliation: %+v", rows)
				}
				if len(s.Recent()) != 1 || s.Recent()[0].Source != "" {
					t.Fatal("import in ticker")
				}
				// A different request in the same session must remain distinct.
				local.RequestID = "claude-local:msg_two"
				local.MessageID = "msg_two"
				local.UpstreamRequestID = "req_two"
				local.Output = 21
				if err = s.SaveRecord(local); err != nil {
					t.Fatal(err)
				}
				if s.Len() != 2 {
					t.Fatal("different request collapsed")
				}
			})
		}
	}
}
func fmtName(legacy, first bool) string {
	if legacy {
		if first {
			return "legacy-import-first"
		}
		return "legacy-proxy-first"
	}
	if first {
		return "ids-import-first"
	}
	return "ids-proxy-first"
}

func TestClaudeImportedBlocksAndSessionChronology(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r := usage.Record{RequestID: "claude-local:msg_new", MessageID: "msg_new", Harness: "claude", SessionID: "s", Source: "claude-local", Model: "new-model", Time: 20000, Output: 50, State: "recorded"}
	if err = s.SaveRecord(r); err != nil {
		t.Fatal(err)
	}
	r.Output = 10
	if err = s.SaveRecord(r); err != nil {
		t.Fatal(err)
	}
	if s.All()[0].Output != 50 {
		t.Fatal("repeated block regressed usage")
	}
	r.RequestID = "claude-local:msg_old"
	r.MessageID = "msg_old"
	r.Model = "old-model"
	r.Time = 10000
	if err = s.SaveRecord(r); err != nil {
		t.Fatal(err)
	}
	rows, err := s.sessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["model"] != "new-model" || rows[0]["last_seen"] != int64(20000) || rows[0]["requests"] != 2 || rows[0]["active"] != 0 {
		t.Fatalf("backfill reordered session: %+v", rows)
	}
	if len(s.Recent()) != 0 {
		t.Fatal("history in ticker")
	}
}

func TestClaudeLegacyWithoutRequestID(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r := usage.Record{Harness: "claude", SessionID: "s", Model: "claude-test", Time: 10000, Ms: 1000, Input: 10, Output: 10, Status: 200}
	if err = s.SaveRecord(r); err != nil {
		t.Fatal(err)
	}
	r.RequestID = "claude-local:msg_legacy"
	r.MessageID = "msg_legacy"
	r.Source = "claude-local"
	r.Time = 10500
	r.Ms = 0
	r.Status = 0
	if err = s.SaveRecord(r); err != nil {
		t.Fatal(err)
	}
	if s.Len() != 1 || s.All()[0].MessageID != "msg_legacy" {
		t.Fatalf("legacy import duplicated: %+v", s.All())
	}
}
