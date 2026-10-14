package usage

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func names(tools []Tool) []string {
	out := []string{}
	for _, t := range tools {
		out = append(out, t.Kind+":"+t.Name)
	}
	return out
}

func assertPrivate(t *testing.T, v any) {
	t.Helper()
	b, _ := json.Marshal(v)
	if strings.Contains(string(b), "PRIVATE") {
		t.Fatalf("retained private content: %s", b)
	}
}

func TestFoldResponsesToolCallsOncePerCall(t *testing.T) {
	items := []string{
		`{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"PRIVATE REASONING"}]}`,
		`{"type":"function_call","id":"fc_1","call_id":"call_1","name":"shell","arguments":"{\"cmd\":\"PRIVATE ARGUMENT\"}"}`,
		`{"type":"function_call","id":"fc_2","call_id":"call_2","namespace":"mcp__github","name":"create_issue","arguments":"PRIVATE ARGUMENT"}`,
		`{"type":"custom_tool_call","id":"ctc_1","call_id":"call_3","name":"apply_patch","input":"PRIVATE ARGUMENT"}`,
		`{"type":"local_shell_call","id":"lsh_1","call_id":"call_4","action":{"command":["PRIVATE ARGUMENT"]}}`,
		`{"type":"web_search_call","id":"ws_1","action":{"query":"PRIVATE ARGUMENT"}}`,
		`{"type":"message","id":"msg_1","content":[{"type":"output_text","text":"PRIVATE OUTPUT"}]}`,
	}
	var body strings.Builder
	for _, it := range items {
		fmt.Fprintf(&body, "data: {\"type\":\"response.output_item.added\",\"item\":%s}\n\n", it)
		fmt.Fprintf(&body, "data: {\"type\":\"response.output_item.done\",\"item\":%s}\n\n", it)
	}
	fmt.Fprintf(&body, `data: {"type":"response.completed","response":{"model":"gpt-test","output":[%s],"usage":{"input_tokens":10,"output_tokens":50,"output_tokens_details":{"reasoning_tokens":30}}}}`+"\n", strings.Join(items, ","))
	var rec Record
	Fold([]byte(body.String()), &rec)
	want := []string{"function:shell", "mcp:mcp__github__create_issue", "custom:apply_patch", "shell:local_shell", "web_search:web_search"}
	if !reflect.DeepEqual(names(rec.Tools), want) || rec.ReasoningTokens != 30 || rec.Output != 50 || rec.Model != "gpt-test" {
		t.Fatalf("got %v %+v", names(rec.Tools), rec)
	}
	assertPrivate(t, rec)

	// Without streamed items, the completed output list is the source.
	var only Record
	Fold([]byte(`data: {"type":"response.completed","response":{"output":[`+strings.Join(items, ",")+`]}}`), &only)
	if !reflect.DeepEqual(names(only.Tools), want) {
		t.Fatalf("completed output: %v", names(only.Tools))
	}
}

func TestFoldAnthropicAndChatToolCalls(t *testing.T) {
	claude := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_1","model":"claude-test","content":[],"usage":{"input_tokens":5}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"PRIVATE"}}`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"Bash","input":{}}}`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"PRIVATE ARGUMENT"}}`,
		`data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_2","name":"mcp__linear__get_issue","input":{}}}`,
		`data: {"type":"content_block_start","index":3,"content_block":{"type":"server_tool_use","id":"srvtoolu_1","name":"web_search","input":{"query":"PRIVATE"}}}`,
		`data: {"type":"message_delta","usage":{"output_tokens":9}}`,
	}, "\n")
	var rec Record
	Fold([]byte(claude), &rec)
	if want := []string{"function:Bash", "mcp:mcp__linear__get_issue", "server:web_search"}; !reflect.DeepEqual(names(rec.Tools), want) || rec.Output != 9 {
		t.Fatalf("claude: %v %+v", names(rec.Tools), rec)
	}
	assertPrivate(t, rec)

	var nonStream Record
	Fold([]byte(`{"type":"message","id":"msg_2","model":"claude-test","content":[{"type":"text","text":"PRIVATE OUTPUT"},{"type":"tool_use","id":"toolu_3","name":"Read","input":{"path":"PRIVATE"}}],"usage":{"input_tokens":1,"output_tokens":2}}`), &nonStream)
	if want := []string{"function:Read"}; !reflect.DeepEqual(names(nonStream.Tools), want) {
		t.Fatalf("non-streaming: %v", names(nonStream.Tools))
	}

	chat := `data: {"model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"read_file","arguments":""}}]}}]}` + "\n" +
		`data: {"model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"PRIVATE ARGUMENT"}}]}}]}` + "\n" +
		`data: {"model":"m","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":4}}` + "\n"
	var c Record
	Fold([]byte(chat), &c)
	if want := []string{"function:read_file"}; !reflect.DeepEqual(names(c.Tools), want) || c.Output != 4 {
		t.Fatalf("chat: %v %+v", names(c.Tools), c)
	}
	assertPrivate(t, c)
}

func TestFoldToleratesUnexpectedToolShapes(t *testing.T) {
	var rec Record
	Fold([]byte(`{"type":"response.output_item.done","item":5,"output":"text","content":{"a":1},"choices":"x","model":"m","usage":{"input_tokens":3,"output_tokens":4}}`), &rec)
	if rec.Model != "m" || rec.Input != 3 || rec.Output != 4 || len(rec.Tools) != 0 {
		t.Fatalf("unexpected shapes cost usage: %+v", rec)
	}
	long := strings.Repeat("x", 300)
	Fold([]byte(`{"type":"response.output_item.done","item":{"type":"function_call","id":"a","name":"`+long+`"}}`+"\n"+`{"type":"response.output_item.done","item":{"type":"function_call","id":"b","name":"bad\u0007name"}}`), &rec)
	if want := []string{"function:unknown", "function:unknown"}; !reflect.DeepEqual(names(rec.Tools), want) {
		t.Fatalf("unsafe names kept: %v", names(rec.Tools))
	}
	var capped Record
	var body strings.Builder
	for i := range MaxTools + 10 {
		fmt.Fprintf(&body, `{"type":"response.output_item.done","item":{"type":"function_call","id":"fc_%d","name":"shell"}}`+"\n", i)
	}
	Fold([]byte(body.String()), &capped)
	if len(capped.Tools) != MaxTools {
		t.Fatalf("uncapped: %d", len(capped.Tools))
	}
}

func TestRequestCall(t *testing.T) {
	turn, _ := json.Marshal(map[string]any{"turn_id": "turn-from-blob", "agent_name": "/root/worker", "request_kind": "turn", "subagent_kind": "thread_spawn", "parent_thread_id": "parent-thread", "tool_namespaces_info": map[string]any{"mcp__x": "PRIVATE"}})
	body := map[string]any{
		"model":     "gpt-test",
		"reasoning": map[string]any{"effort": "High", "summary": "auto"},
		"client_metadata": map[string]any{
			"session_id": "root", "thread_id": "child-thread", "turn_id": "turn-1",
			"x-openai-subagent": "collab_spawn", "x-codex-turn-metadata": string(turn),
		},
		"input": []any{
			map[string]any{"type": "message", "role": "user", "content": "PRIVATE PROMPT"},
			map[string]any{"type": "function_call_output", "call_id": "old", "output": "PRIVATE OUTPUT"},
			map[string]any{"type": "reasoning"},
			map[string]any{"type": "function_call", "call_id": "a", "arguments": "PRIVATE ARGUMENT"},
			map[string]any{"type": "function_call_output", "call_id": "a", "output": "PRIVATE OUTPUT"},
			map[string]any{"type": "custom_tool_call", "call_id": "b"},
			map[string]any{"type": "custom_tool_call_output", "call_id": "b", "output": "PRIVATE OUTPUT"},
		},
	}
	b, _ := json.Marshal(body)
	parsed, err := ParseRequest(b)
	if err != nil {
		t.Fatal(err)
	}
	c := parsed.Call(http.Header{})
	want := Call{Effort: "high", Subagent: "thread_spawn", AgentName: "/root/worker", ThreadID: "child-thread", ParentSessionID: "parent-thread", TurnID: "turn-1", RequestKind: "turn", ToolResults: 2}
	if c != want {
		t.Fatalf("got %+v", c)
	}
	assertPrivate(t, c)
	if parsed.Model != "gpt-test" || StringValue(parsed.ClientMetadata, "session_id") != "root" {
		t.Fatalf("routing fields lost: %+v", parsed)
	}

	// Headers (the bounded compatibility projection) win over the body blob.
	h := http.Header{}
	h.Set("X-Codex-Turn-Metadata", `{"turn_id":"t","request_kind":"compaction","subagent_kind":"compact"}`)
	h.Set("X-Codex-Parent-Thread-Id", "header-parent")
	parsed, _ = ParseRequest([]byte(`{"input":"PRIVATE PROMPT","reasoning":"odd","client_metadata":{"x-codex-turn-metadata":"{\"request_kind\":\"turn\"}"}}`))
	c = parsed.Call(h)
	if c != (Call{Subagent: "compact", TurnID: "t", RequestKind: "compaction", ParentSessionID: "header-parent"}) {
		t.Fatalf("header metadata: %+v", c)
	}

	// Main threads are /root and need no label; Anthropic requests report effort and tool results.
	parsed, _ = ParseRequest([]byte(`{"output_config":{"effort":"max"},"client_metadata":{"x-codex-turn-metadata":"{\"agent_name\":\"/root\"}"},"messages":[{"role":"user","content":"PRIVATE"},{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"PRIVATE OUTPUT"},{"type":"tool_result","tool_use_id":"t2","content":"PRIVATE OUTPUT"},{"type":"text","text":"PRIVATE"}]}]}`))
	if c = parsed.Call(http.Header{}); c != (Call{Effort: "max", ToolResults: 2}) {
		t.Fatalf("anthropic: %+v", c)
	}

	// An oversized turn metadata string is not parsed.
	parsed, _ = ParseRequest([]byte(`{"client_metadata":{"x-codex-turn-metadata":"{\"turn_id\":\"x\",\"pad\":\"` + strings.Repeat("a", maxTurnMetadata) + `\"}"}}`))
	if c = parsed.Call(http.Header{}); c.TurnID != "" {
		t.Fatal("parsed oversized metadata")
	}
}

func TestSubagentKinds(t *testing.T) {
	for in, want := range map[string]string{
		"collab_spawn": "thread_spawn", "thread_spawn_0198_d1": "thread_spawn", "review": "review", "Compact": "compact",
		"guardian": "guardian", "memory_consolidation": "memory_consolidation", "subagent_review": "review", "": "", "bad label!": "",
	} {
		if got := Subagent(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestMergeCallJoinsToolsByReference(t *testing.T) {
	proxy := Record{SessionID: "s", Tools: []Tool{{Name: "Bash", Kind: "function", Ref: "a"}}, Effort: "high"}
	local := Record{Tools: []Tool{{Name: "Bash", Kind: "function", Ref: "a"}, {Name: "Read", Kind: "function", Ref: "b"}}, Subagent: "sidechain", AgentName: "agent-1", Effort: "low", ReasoningTokens: 4}
	MergeCall(&proxy, local)
	if !reflect.DeepEqual(names(proxy.Tools), []string{"function:Bash", "function:Read"}) || proxy.Effort != "high" || proxy.Subagent != "sidechain" || proxy.AgentName != "agent-1" || proxy.ReasoningTokens != 4 {
		t.Fatalf("%+v", proxy)
	}
	var r Record
	(Call{ThreadID: "s"}).Apply(&r)
	r.SessionID = "s"
	(Call{ThreadID: "s"}).Apply(&r)
	if r.ThreadID != "" {
		t.Fatal("kept the main thread as a child thread")
	}
}

func TestToolCallsFromTranscriptContent(t *testing.T) {
	var tools ToolCalls
	if err := json.Unmarshal([]byte(`[{"type":"text","text":"PRIVATE"},{"type":"tool_use","id":"toolu_1","name":"Task","input":{"prompt":"PRIVATE ARGUMENT"}},{"type":"tool_use","id":"toolu_1","name":"Task","input":{}}]`), &tools); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names(tools), []string{"function:Task"}) {
		t.Fatalf("%v", names(tools))
	}
	if err := json.Unmarshal([]byte(`"PRIVATE"`), &tools); err != nil || len(tools) != 0 {
		t.Fatal("string content", err, tools)
	}
}
