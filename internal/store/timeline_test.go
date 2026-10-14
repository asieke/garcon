package store

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"garcon/internal/usage"
)

func TestSessionTimelineIncludesSubagents(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	shell := []usage.Tool{{Name: "shell", Kind: "function", Ref: "1"}, {Name: "shell", Kind: "function", Ref: "2"}}
	for _, r := range []usage.Record{
		// Saved out of order; the timeline is ascending by time.
		{RequestID: "main-2", Harness: "codex", SessionID: "root", Model: "gpt-main", Time: 3000, Ms: 400, Output: 5, TurnID: "t1", Tools: []usage.Tool{{Name: "apply_patch", Kind: "custom"}}},
		{RequestID: "main-1", Harness: "codex", SessionID: "root", Model: "gpt-main", Time: 1000, Ms: 900, Input: 10, Output: 7, TurnID: "t1", Effort: "high", Tools: shell},
		// A spawned agent sharing the session (current Codex), and a review.
		{RequestID: "child-1", Harness: "codex", SessionID: "root", ThreadID: "worker", ParentSessionID: "root", Subagent: "thread_spawn", AgentName: "/root/worker", Model: "gpt-mini", Time: 2000, Ms: 100, Input: 4, Tools: shell},
		{RequestID: "child-2", Harness: "codex", SessionID: "root", ThreadID: "worker", ParentSessionID: "root", Subagent: "thread_spawn", Model: "gpt-mini", Time: 2500, Ms: 100, Output: 3, State: "streaming"},
		{RequestID: "guard", Harness: "codex", SessionID: "root", Subagent: "guardian", Model: "gpt-review", Time: 3500, State: "streaming"},
		// A child conversation recorded under its own session (older Codex).
		{RequestID: "own-1", Harness: "codex", SessionID: "own-child", ParentSessionID: "root", Subagent: "thread_spawn", Model: "gpt-mini", Time: 4000, Ms: 50, Output: 2},
		// Not part of this session: another client with the same ID and an unrelated session.
		{RequestID: "claude", Harness: "claude", SessionID: "root", Model: "claude-test", Time: 1500},
		{RequestID: "other", Harness: "codex", SessionID: "other", Model: "gpt-main", Time: 1200},
	} {
		if err = s.SaveRecord(r); err != nil {
			t.Fatal(err)
		}
	}
	get := func(url string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", url, nil)
		r.RemoteAddr = "127.0.0.1:1234"
		s.ServeHTTP(w, r)
		return w
	}
	w := get("http://127.0.0.1:4141/api/sessions/timeline?harness=codex&session_id=root")
	var tl Timeline
	if err = json.Unmarshal(w.Body.Bytes(), &tl); err != nil || w.Code != 200 {
		t.Fatal(w.Code, w.Body.String(), err)
	}
	ids := []string{}
	for _, r := range tl.Requests {
		ids = append(ids, r.RequestID)
	}
	if strings.Join(ids, ",") != "main-1,child-1,child-2,main-2,guard,own-1" || tl.Total != 6 || tl.Truncated || tl.ParentSessionID != "" {
		t.Fatalf("timeline: %v %+v", ids, tl)
	}
	if len(tl.Children) != 2 {
		t.Fatalf("children: %+v", tl.Children)
	}
	worker, own := tl.Children[0], tl.Children[1]
	if worker.Key != "thread:worker" || worker.Subagent != "thread_spawn" || worker.AgentName != "/root/worker" || worker.Calls != 2 || worker.ToolCalls != 2 || worker.Input != 4 || worker.Output != 3 || worker.First != 2000 || worker.Last != 2600 || strings.Join(worker.Models, ",") != "gpt-mini" {
		t.Fatalf("thread child: %+v", worker)
	}
	if own.Key != "session:own-child" || own.SessionID != "own-child" || own.Calls != 1 || own.ParentSessionID != "root" {
		t.Fatalf("session child: %+v", own)
	}

	// The child session links back to its parent.
	w = get("http://127.0.0.1:4141/api/sessions/timeline?harness=codex&session_id=own-child")
	if err = json.Unmarshal(w.Body.Bytes(), &tl); err != nil || tl.ParentSessionID != "root" || len(tl.Requests) != 1 {
		t.Fatalf("child timeline: %s", w.Body.String())
	}
	if w = get("http://127.0.0.1:4141/api/sessions/timeline"); w.Code != 400 {
		t.Fatal("missing session accepted", w.Code)
	}

	// The inventory reports tool calls, subagents and the parent link; reviews
	// and child threads never replace the conversation's model.
	w = get("http://127.0.0.1:4141/api/sessions")
	var rows []struct {
		Harness   string `json:"harness"`
		ID        string `json:"session_id"`
		Model     string `json:"model"`
		Active    int    `json:"active"`
		Reviews   int    `json:"active_reviews"`
		ToolCalls int    `json:"tool_calls"`
		Subagents int    `json:"subagents"`
		Parent    string `json:"parent_session_id"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, row := range rows {
		switch {
		case row.Harness == "codex" && row.ID == "root":
			found++
			if row.Model != "gpt-main" || row.ToolCalls != 5 || row.Subagents != 2 || row.Active != 2 || row.Reviews != 1 || row.Parent != "" {
				t.Fatalf("root inventory: %+v", row)
			}
		case row.ID == "own-child":
			found++
			if row.Parent != "root" {
				t.Fatalf("child inventory: %+v", row)
			}
		}
	}
	if found != 2 {
		t.Fatalf("missing sessions: %+v", rows)
	}
}

func TestTimelineKeepsNewestCalls(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tx, _ := s.DB.Begin()
	for i := range maxTimeline + 3 {
		b, _ := json.Marshal(usage.Record{Harness: "codex", SessionID: "long", Time: int64(i)})
		if _, err = tx.Exec("INSERT INTO requests(time,session_id,record) VALUES(?,?,?)", i, "long", string(b)); err != nil {
			t.Fatal(err)
		}
	}
	tx.Commit()
	tl, err := s.timeline(httptest.NewRequest("GET", "/api/sessions/timeline?session_id=long", nil))
	if err != nil || len(tl.Requests) != maxTimeline || !tl.Truncated || tl.Total != maxTimeline+3 || tl.Requests[0].Time != 3 {
		t.Fatalf("%v %d %v %d", err, len(tl.Requests), tl.Truncated, tl.Requests[0].Time)
	}
}

func TestReconcileKeepsCallDetails(t *testing.T) {
	for _, importFirst := range []bool{false, true} {
		s, err := Open(filepath.Join(t.TempDir(), "usage.db"))
		if err != nil {
			t.Fatal(err)
		}
		proxy := usage.Record{RequestID: "proxy", Harness: "claude", Kind: "completion", SessionID: "session", Model: "claude-test", Time: 10000, Ms: 500, Output: 20, Status: 200, MessageID: "msg_one", Effort: "high", Tools: []usage.Tool{{Name: "Bash", Kind: "function", Ref: "a"}}}
		// The transcript writes one line per content block of the same message.
		line1 := usage.Record{RequestID: "claude-local:msg_one", Source: "claude-local", Harness: "claude", Kind: "completion", SessionID: "session", Model: "claude-test", Time: 10100, Output: 20, MessageID: "msg_one", Subagent: "sidechain", AgentName: "agent-7", ThreadID: "agent-7", Tools: []usage.Tool{{Name: "Bash", Kind: "function", Ref: "a"}}}
		line2 := line1
		line2.Subagent, line2.AgentName, line2.ThreadID = "", "", ""
		line2.Tools = []usage.Tool{{Name: "Read", Kind: "function", Ref: "b"}}
		order := []usage.Record{proxy, line1, line2}
		if importFirst {
			order = []usage.Record{line1, line2, proxy}
		}
		for _, r := range order {
			if err = s.SaveRecord(r); err != nil {
				t.Fatal(err)
			}
		}
		rows := s.All()
		if len(rows) != 1 {
			t.Fatalf("not reconciled: %+v", rows)
		}
		got := rows[0]
		if len(got.Tools) != 2 || got.Tools[0].Name != "Bash" || got.Tools[1].Name != "Read" || got.Subagent != "sidechain" || got.ThreadID != "agent-7" || got.Effort != "high" || got.Ms != 500 {
			t.Fatalf("importFirst=%v lost call details: %+v", importFirst, got)
		}
		s.Close()
	}
}
