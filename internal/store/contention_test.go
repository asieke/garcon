package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"garcon/internal/proxy"
	"garcon/internal/usage"
)

func assertRequestTotal(t *testing.T, s *Store, want int) {
	t.Helper()
	var actual, persisted int
	if err := s.DB.QueryRow("SELECT count(*) FROM requests").Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow("SELECT total FROM request_totals WHERE id=1").Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if got := s.RequestFeed().TotalRequests; actual != want || persisted != actual || got != actual || s.Len() != actual {
		t.Fatalf("total: rows=%d persisted=%d feed=%d len=%d want=%d", actual, persisted, got, s.Len(), want)
	}
}

func TestRequestTotalTracksLifecycleReconciliationRollbackAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	if err := os.WriteFile(path, []byte("{\"time\":1}\n{\"time\":2}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	save := func(r usage.Record, want int) {
		t.Helper()
		if err := s.SaveRecord(r); err != nil {
			t.Fatal(err)
		}
		assertRequestTotal(t, s, want)
	}
	assertRequestTotal(t, s, 2)
	save(usage.Record{RequestID: "stream", State: "streaming"}, 3)
	save(usage.Record{RequestID: "stream", State: "complete"}, 3)
	save(usage.Record{}, 4)
	save(usage.Record{}, 5) // anonymous completions stay distinct
	local := usage.Record{RequestID: "local", Harness: "claude", Source: "claude-local", MessageID: "msg-one"}
	save(local, 6)
	save(local, 6) // repeated transcript blocks
	remote := usage.Record{RequestID: "proxy", Harness: "claude", MessageID: "msg-one"}
	save(remote, 6) // delete imported row, insert observed row
	save(local, 6)  // reconciliation skips the insert
	local.RequestID, local.MessageID = "pending", ""
	save(local, 7)
	local.MessageID = "msg-one"
	save(local, 6) // reconciliation skip also deletes the earlier imported row
	local.RequestID, local.MessageID = "rollback-local", "msg-rollback"
	save(local, 7)
	if _, err := s.DB.Exec(`CREATE TRIGGER reject_record BEFORE INSERT ON requests
 WHEN NEW.request_id='rollback-proxy' BEGIN SELECT RAISE(ABORT,'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	remote.RequestID, remote.MessageID = "rollback-proxy", "msg-rollback"
	if err := s.SaveRecord(remote); err == nil {
		t.Fatal("expected insert failure after reconciliation deleted a row")
	}
	assertRequestTotal(t, s, 7)
	if _, err := s.DB.Exec(`CREATE TABLE test_parent(id INTEGER PRIMARY KEY);
 CREATE TABLE test_child(id INTEGER REFERENCES test_parent(id) DEFERRABLE INITIALLY DEFERRED);
 CREATE TRIGGER reject_commit AFTER INSERT ON requests WHEN NEW.request_id='commit-failure'
 BEGIN INSERT INTO test_child(id) VALUES(1); END;`); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRecord(usage.Record{RequestID: "commit-failure"}); err == nil {
		t.Fatal("expected deferred constraint failure on commit")
	}
	assertRequestTotal(t, s, 7)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s = reopened
	assertRequestTotal(t, s, 7)
	if len(s.Recent()) != 0 {
		t.Fatal("restart must not put historical requests in the ticker")
	}
}

func TestProxyRecordingProceedsWithAllReadersHoldingSnapshots(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var readers []*sql.Tx
	for range s.DB.Readers.Stats().MaxOpenConnections {
		tx, err := s.DB.Readers.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		var n int
		if err := tx.QueryRow("SELECT count(*) FROM requests").Scan(&n); err != nil {
			t.Fatal(err)
		}
		readers = append(readers, tx)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := s.RequestFeed(); got.TotalRequests != 1 || len(got.Requests) != 1 || got.Requests[0].State != "streaming" {
			t.Errorf("start not durable/visible before upstream: %+v", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"model":"test","usage":{"input_tokens":5,"output_tokens":7}}`)
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)
	route, _ := proxy.ParseRoute("/claude/v1/messages")
	route.Target = target
	p := proxy.Proxy{Start: s.Save, Save: s.Save}
	req := httptest.NewRequest("POST", "/claude/v1/messages", strings.NewReader(`{"model":"test"}`))
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); p.Serve(response, req, route) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		for _, tx := range readers {
			tx.Rollback()
		}
		<-done
		t.Fatal("proxy start or completion waited for dashboard readers")
	}
	if response.Code != http.StatusOK || s.RequestFeed().TotalRequests != 1 || s.Recent()[0].State != "complete" {
		t.Fatalf("proxy did not finish recording: status=%d feed=%+v", response.Code, s.RequestFeed())
	}
	for _, tx := range readers {
		var n int
		if err := tx.QueryRow("SELECT count(*) FROM requests").Scan(&n); err != nil || n != 0 {
			t.Fatalf("reader did not retain its snapshot: count=%d err=%v", n, err)
		}
		tx.Rollback()
	}
	assertRequestTotal(t, s, 1)
}

func TestRequestFeedDoesNotWaitForWriterAdmission(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tx, err := s.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	written := make(chan error, 1)
	go func() { written <- s.SaveRecord(usage.Record{RequestID: "waiting"}) }()
	deadline := time.Now().Add(3 * time.Second)
	for s.DB.Stats().WaitCount == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s.DB.Stats().WaitCount == 0 {
		tx.Rollback()
		<-written
		t.Fatal("writer did not reach database admission")
	}
	feed := make(chan RequestFeed, 1)
	go func() { feed <- s.RequestFeed() }()
	select {
	case got := <-feed:
		if got.TotalRequests != 0 {
			t.Errorf("uncommitted request became visible: %+v", got)
		}
	case <-time.After(time.Second):
		t.Error("feed waited on a writer's database admission")
	}
	tx.Rollback()
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	assertRequestTotal(t, s, 1)
}

// The polling case deliberately polls continuously to expose contention; the
// real dashboard polls every few seconds. Report pool admission separately
// from total lifecycle time (SQL execution plus store/writer serialization).
func BenchmarkRecordingWithDashboard(b *testing.B) {
	for _, polling := range []bool{false, true} {
		b.Run(fmt.Sprintf("polling=%t", polling), func(b *testing.B) {
			s, err := Open(filepath.Join(b.TempDir(), "usage.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer s.Close()
			tx, err := s.DB.Begin()
			if err != nil {
				b.Fatal(err)
			}
			for i := range 10000 {
				if _, err := tx.Exec(`INSERT INTO requests(time,session_id,model,record) VALUES(?,?,?,?)`, i, fmt.Sprint(i%100), "test", `{"harness":"claude","input":5,"output":7}`); err != nil {
					b.Fatal(err)
				}
			}
			if err := tx.Commit(); err != nil {
				b.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			var wg sync.WaitGroup
			if polling {
				for _, path := range []string{"/api/sessions", "/api/logs", "/api/analytics", "/api/events"} {
					wg.Go(func() {
						for ctx.Err() == nil {
							s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil).WithContext(ctx))
							s.RequestFeed()
						}
					})
				}
			}
			before := s.DB.Stats()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				r := usage.Record{RequestID: fmt.Sprint(i), Harness: "claude", SessionID: "active", Model: "test", State: "streaming", Time: time.Now().UnixMilli()}
				if err := s.SaveRecord(r); err != nil {
					b.Fatal(err)
				}
				r.State = "complete"
				if err := s.SaveRecord(r); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64((s.DB.Stats().WaitDuration-before.WaitDuration).Nanoseconds())/float64(b.N), "writer-pool-wait-ns/op")
			cancel()
			wg.Wait()
		})
	}
}
