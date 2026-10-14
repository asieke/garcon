package claudeimport

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportRecordsToolNamesAndSubagents(t *testing.T) {
	i, path := setup(t)
	line := func(id, extra, content string) string {
		return `{"type":"assistant","sessionId":"parent","timestamp":"2026-10-01T12:00:00Z"` + extra + `,"message":{"id":"msg_` + id + `","model":"claude-test","content":` + content + `,"usage":{"input_tokens":1,"output_tokens":2}}}` + "\n"
	}
	// One message split across lines, one content block each, as Claude Code writes them.
	put(t, path, line("main", "", `[{"type":"text","text":"PRIVATE CONVERSATION"}]`)+
		line("main", "", `[{"type":"tool_use","id":"toolu_1","name":"Task","input":{"prompt":"PRIVATE ARGUMENT"}}]`)+
		line("main", "", `[{"type":"tool_use","id":"toolu_2","name":"mcp__linear__get_issue","input":{"id":"PRIVATE ARGUMENT"}}]`)+
		`{"type":"user","sessionId":"parent","timestamp":"2026-10-01T12:00:01Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"PRIVATE OUTPUT"}]}}`+"\n")
	put(t, filepath.Join(filepath.Dir(path), "parent/subagents/agent-a1.jsonl"),
		line("sub", `,"isSidechain":true,"agentId":"a1"`, `[{"type":"tool_use","id":"toolu_3","name":"Grep","input":{"pattern":"PRIVATE ARGUMENT"}}]`))
	i.Scan(context.Background())
	rows := i.Store.All()
	if len(rows) != 2 {
		t.Fatalf("rows: %+v", rows)
	}
	byID := map[string]int{}
	for n, r := range rows {
		byID[r.MessageID] = n
	}
	main, sub := rows[byID["msg_main"]], rows[byID["msg_sub"]]
	if len(main.Tools) != 2 || main.Tools[0].Name != "Task" || main.Tools[1].Kind != "mcp" || main.Subagent != "" || main.ThreadID != "" {
		t.Fatalf("main: %+v", main)
	}
	if len(sub.Tools) != 1 || sub.Tools[0].Name != "Grep" || sub.Subagent != "sidechain" || sub.AgentName != "a1" || sub.ThreadID != "a1" || sub.SessionID != "parent" {
		t.Fatalf("subagent: %+v", sub)
	}
	r, err := i.Store.DB.Query("SELECT record FROM requests")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for r.Next() {
		var raw string
		r.Scan(&raw)
		if strings.Contains(raw, "PRIVATE") {
			t.Fatal("persisted content", raw)
		}
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://127.0.0.1:4141/api/sessions/timeline?harness=claude&session_id=parent", nil)
	req.RemoteAddr = "127.0.0.1:1"
	i.Store.ServeHTTP(w, req)
	if w.Code != 200 || strings.Contains(w.Body.String(), "PRIVATE") || !strings.Contains(w.Body.String(), `"key":"thread:a1"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}
