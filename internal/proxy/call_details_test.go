package proxy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"garcon/internal/codexrouting"
	"garcon/internal/limits"
	"garcon/internal/usage"
)

// A Codex subagent request: the turn metadata blob, tool outputs being
// returned, and a response that calls tools. Arguments, outputs and prompts
// carry PRIVATE markers that must never reach a record.
func codexCallFixture(t *testing.T) (body string, sse string) {
	t.Helper()
	turn, _ := json.Marshal(map[string]any{"turn_id": "turn-7", "agent_name": "/root/explorer", "request_kind": "turn", "subagent_kind": "thread_spawn", "parent_thread_id": "root-session"})
	b, _ := json.Marshal(map[string]any{
		"model":     "gpt-test",
		"reasoning": map[string]any{"effort": "high"},
		"client_metadata": map[string]any{
			"session_id": "root-session", "thread_id": "child-thread", "turn_id": "turn-7",
			"x-openai-subagent": "collab_spawn", "x-codex-parent-thread-id": "root-session", "x-codex-turn-metadata": string(turn),
		},
		"input": []any{
			map[string]any{"type": "message", "role": "user", "content": "PRIVATE PROMPT"},
			map[string]any{"type": "function_call", "call_id": "c1", "name": "shell", "arguments": "PRIVATE ARGUMENT"},
			map[string]any{"type": "function_call_output", "call_id": "c1", "output": "PRIVATE OUTPUT"},
		},
	})
	items := []string{
		`{"type":"function_call","id":"fc_1","call_id":"c2","name":"shell","arguments":"PRIVATE ARGUMENT"}`,
		`{"type":"custom_tool_call","id":"ctc_1","call_id":"c3","name":"apply_patch","input":"PRIVATE ARGUMENT"}`,
		`{"type":"message","id":"m","content":[{"type":"output_text","text":"PRIVATE OUTPUT"}]}`,
	}
	for _, it := range items {
		sse += "data: {\"type\":\"response.output_item.done\",\"item\":" + it + "}\n\n"
	}
	sse += `data: {"type":"response.completed","response":{"model":"gpt-test","output":[` + strings.Join(items, ",") + `],"usage":{"input_tokens":10,"output_tokens":40,"output_tokens_details":{"reasoning_tokens":25}}}}` + "\n\n"
	return string(b), sse
}

func assertCodexCall(t *testing.T, rec usage.Record) {
	t.Helper()
	got := []string{}
	for _, tool := range rec.Tools {
		got = append(got, tool.Kind+":"+tool.Name)
	}
	if !reflect.DeepEqual(got, []string{"function:shell", "custom:apply_patch"}) || rec.ToolResults != 1 || rec.Effort != "high" || rec.ReasoningTokens != 25 ||
		rec.Subagent != "thread_spawn" || rec.AgentName != "/root/explorer" || rec.ThreadID != "child-thread" || rec.ParentSessionID != "root-session" ||
		rec.TurnID != "turn-7" || rec.RequestKind != "turn" || rec.SessionID != "root-session" || rec.Output != 40 {
		t.Fatalf("call details: %v %+v", got, rec)
	}
	b, _ := json.Marshal(rec)
	if strings.Contains(string(b), "PRIVATE") {
		t.Fatalf("record retained content: %s", b)
	}
}

func TestUnroutedCodexRecordsCallDetails(t *testing.T) {
	body, sse := codexCallFixture(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b, _ := io.ReadAll(r.Body); string(b) != body {
			t.Error("request body changed")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse)
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)
	var saved, started usage.Record
	p := Proxy{Start: func(r usage.Record) { started = r }, Save: func(r usage.Record) { saved = r }}
	rt, _ := ParseRoute("/codex/backend-api/codex/responses")
	rt.Target = target
	w := httptest.NewRecorder()
	p.Serve(w, httptest.NewRequest("POST", "/codex/backend-api/codex/responses", strings.NewReader(body)), rt)
	if w.Body.String() != sse {
		t.Fatal("response changed")
	}
	if started.TurnID != "turn-7" || started.Subagent != "thread_spawn" || len(started.Tools) != 0 {
		t.Fatalf("in-flight row lacks request details: %+v", started)
	}
	assertCodexCall(t, saved)
}

func TestRoutedCodexRecordsCallDetails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	now := time.Now()
	dir := filepath.Join(home, ".codex-a")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	claims, _ := json.Marshal(map[string]any{"exp": now.Add(time.Hour).Unix(), "email": "a@example.com", "https://api.openai.com/auth": map[string]string{"chatgpt_account_id": "a"}})
	auth, _ := json.Marshal(map[string]any{"tokens": map[string]string{"account_id": "a", "access_token": "header." + base64.RawURLEncoding.EncodeToString(claims) + ".signature"}})
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), auth, 0600); err != nil {
		t.Fatal(err)
	}
	used := 10.0
	snapshot := limits.Snapshot{Accounts: []limits.Account{{Provider: "codex", Workspace: "a", Status: "fresh", FetchedAt: now.UnixMilli(), Windows: []limits.Window{{ID: "codex:0", UsedPercent: &used, ResetsAt: now.Add(time.Hour).UnixMilli()}}}}}
	original := http.DefaultTransport
	http.DefaultTransport = transportFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "chatgpt.com" {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"models":[{"slug":"gpt-test"}]}`))}, nil
		}
		return original.RoundTrip(req)
	})
	defer func() { http.DefaultTransport = original }()
	router := codexrouting.New(t.TempDir(), func() limits.Snapshot { return snapshot })
	if err := router.Configure(true); err != nil {
		t.Fatal(err)
	}
	router.Refresh(context.Background())

	body, sse := codexCallFixture(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b, _ := io.ReadAll(r.Body); string(b) != body || r.Header.Get("ChatGPT-Account-Id") != "a" {
			t.Error("request changed or not routed")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse)
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)
	var saved usage.Record
	p := Proxy{Codex: router, Save: func(r usage.Record) { saved = r }}
	rt, _ := ParseRoute("/codex/backend-api/codex/responses")
	rt.Target = target
	req := httptest.NewRequest("POST", "http://127.0.0.1:4141/codex/backend-api/codex/responses", strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:1111"
	// Codex also projects subagent identity into headers; the header wins for the parent.
	req.Header.Set("X-Codex-Parent-Thread-Id", "root-session")
	w := httptest.NewRecorder()
	p.Serve(w, req, rt)
	if w.Code != 200 || saved.AccountID != "a" {
		t.Fatalf("routing failed: %d %+v", w.Code, saved)
	}
	assertCodexCall(t, saved)
}

func TestClaudeProxyRecordsToolNames(t *testing.T) {
	const body = `{"model":"claude-test","output_config":{"effort":"high"},"metadata":{"user_id":"{\"session_id\":\"conversation\"}"},"messages":[{"role":"user","content":"PRIVATE PROMPT"},{"role":"assistant","content":[{"type":"tool_use","id":"toolu_0","name":"Grep","input":{"pattern":"PRIVATE ARGUMENT"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_0","content":"PRIVATE OUTPUT"}]}]}`
	const sse = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-test\",\"content\":[],\"usage\":{\"input_tokens\":3}}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"PRIVATE OUTPUT\"}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"Bash\",\"input\":{}}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"PRIVATE ARGUMENT\"}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":12}}\n\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse)
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)
	var saved usage.Record
	p := Proxy{Save: func(r usage.Record) { saved = r }}
	rt, _ := ParseRoute("/claude/v1/messages")
	rt.Target = target
	p.Serve(httptest.NewRecorder(), httptest.NewRequest("POST", "/claude/v1/messages", strings.NewReader(body)), rt)
	if saved.SessionID != "conversation" || len(saved.Tools) != 1 || saved.Tools[0].Name != "Bash" || saved.ToolResults != 1 || saved.Effort != "high" || saved.Output != 12 {
		t.Fatalf("%+v", saved)
	}
	if b, _ := json.Marshal(saved); strings.Contains(string(b), "PRIVATE") {
		t.Fatalf("record retained content: %s", b)
	}
}
