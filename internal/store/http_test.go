package store

import (
	"encoding/json"
	"garcon/internal/usage"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestLogsSearchPaginationAndAnalytics(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.Save(usage.Record{RequestID: "a", SessionID: "session-one", Account: "first@example.test", Model: "model", Time: 1000, Status: 200, Input: 10, Output: 2})
	s.Save(usage.Record{RequestID: "b", Account: "second@example.test", Model: "model", Time: 2000, Status: 429, State: "failed"})
	s.Save(usage.Record{RequestID: "c", Time: 3000, State: "streaming", Input: 999})
	s.Save(usage.Record{RequestID: "d", Time: 3000, Kind: "request", Status: 200, Input: 999})
	for _, tc := range []struct {
		path string
		want int
	}{{"/api/logs?limit=1&offset=1", 4}, {"/api/logs?q=session-one", 1}, {"/api/logs?errors=true", 1}, {"/api/logs?q=%27%20OR%201%3D1%20--", 0}} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		var result struct {
			Total    int            `json:"total"`
			Requests []RecentRecord `json:"requests"`
		}
		if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Total != tc.want {
			t.Fatalf("%s: %+v", tc.path, result)
		}
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/api/analytics", nil))
	var groups []struct {
		Requests, Errors int
		Input            int64
	}
	if err = json.Unmarshal(w.Body.Bytes(), &groups); err != nil {
		t.Fatal(err)
	}
	var n, e int
	var input int64
	for _, g := range groups {
		n += g.Requests
		e += g.Errors
		input += g.Input
	}
	if n != 2 || e != 1 || input != 10 {
		t.Fatalf("wrong totals %d %d %d", n, e, input)
	}
}
