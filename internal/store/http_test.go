package store

import (
	"database/sql"
	"encoding/json"
	"garcon/internal/codexmetadata"
	"garcon/internal/usage"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestSessionTaskMetadataAndActivity(t *testing.T) {
	home := t.TempDir()
	index, err := sql.Open("sqlite", filepath.Join(home, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	if _, err = index.Exec(`CREATE TABLE threads(id TEXT,title TEXT,cwd TEXT,archived INTEGER); INSERT INTO threads VALUES('task','Task title','/project/garcon',0)`); err != nil {
		t.Fatal(err)
	}
	s, err := Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.Tasks = &codexmetadata.Reader{Homes: []string{home}}
	if _, err = s.DB.Exec(`INSERT INTO sessions(key,session_id,account_id,model,created_at,last_seen) VALUES('a','task','account','codex-auto-review',1,2),('b','idle','account','main-model',1,999),('legacy','','account','',0,0)`); err != nil {
		t.Fatal(err)
	}
	for _, r := range []usage.Record{
		{RequestID: "main", SessionID: "task", Model: "main-model", Time: 1, Status: 503, State: "failed", Error: "Quota unavailable"},
		{RequestID: "review", SessionID: "task", Model: "codex-auto-review", Time: 2, State: "streaming"},
		{RequestID: "unassigned", Time: 3, State: "streaming"},
	} {
		if err = s.SaveRecord(r); err != nil {
			t.Fatal(err)
		}
	}
	for _, remote := range []string{"127.0.0.1:1234", "192.0.2.1:1234"} {
		r := httptest.NewRequest("GET", "http://127.0.0.1:4141/api/sessions", nil)
		r.RemoteAddr = remote
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		var rows []struct {
			ID      string              `json:"session_id"`
			Model   string              `json:"model"`
			Active  int                 `json:"active"`
			Reviews int                 `json:"active_reviews"`
			Since   int                 `json:"active_since"`
			State   string              `json:"last_state"`
			Error   string              `json:"last_error"`
			Task    *codexmetadata.Task `json:"task"`
		}
		if err = json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
			t.Fatal(w.Body.String(), err)
		}
		if len(rows) != 3 || rows[0].ID != "task" || rows[0].Model != "main-model" || rows[0].Active != 1 || rows[0].Reviews != 1 || rows[0].Since != 2 || rows[0].State != "failed" || rows[0].Error != "Quota unavailable" {
			t.Fatalf("wrong activity: %+v", rows)
		}
		if remote == "127.0.0.1:1234" {
			if rows[0].Task == nil || rows[0].Task.Title != "Task title" {
				t.Fatal("missing title", rows)
			}
		} else if rows[0].Task != nil {
			t.Fatal("remote client received local metadata")
		}
		if rows[2].Active != 0 || rows[2].Task != nil {
			t.Fatal("legacy session incorrectly joined unassigned traffic")
		}
	}
}

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
