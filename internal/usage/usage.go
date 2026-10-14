// Package usage is the data model: one Record per completion call, and how to
// read the provider's usage block out of a response body.
package usage

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Record is one request observation in the local SQLite ledger.
type Record struct {
	Source string `json:"source,omitempty"`
	// Provider IDs correlate local transcripts with proxied calls. Content is never saved.
	UpstreamRequestID  string `json:"upstream_request_id,omitempty"`
	MessageID          string `json:"message_id,omitempty"`
	AccountAttribution string `json:"account_attribution,omitempty"`
	RequestID          string `json:"request_id,omitempty"`
	SessionID          string `json:"session_id,omitempty"`
	AccountID          string `json:"account_id,omitempty"`
	Method             string `json:"method,omitempty"`
	Path               string `json:"path,omitempty"`
	Kind               string `json:"kind,omitempty"`
	State              string `json:"state,omitempty"`
	Error              string `json:"error,omitempty"`
	Time               int64  `json:"time"` // unix milliseconds
	Harness            string `json:"harness"`
	Account            string `json:"account"`
	// Provider is omitted on rows recorded before harnesses other than Claude Code
	// and Codex were supported; those imply anthropic and chatgpt respectively.
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model"`
	Status   int    `json:"status"`
	Ms       int64  `json:"ms"`

	// The fields below isolate every part of Ms that garcon itself controls,
	// via net/http/httptrace, so total latency (almost entirely the provider
	// generating a response) is never confused with what the proxy adds.
	// All are omitted (never a measured zero) on requests recorded before this
	// instrumentation shipped.

	// QueueUs is request start to the transport asking for a connection: pure
	// proxy-side Go work with no network involved yet. Microseconds, because it
	// is expected to be tiny; reported to prove that, not just assume it.
	QueueUs *int64 `json:"queue_us,omitempty"`
	// Reused is true when an already-open pooled connection served this
	// request, false when a fresh connection had to be established.
	Reused *bool `json:"reused,omitempty"`
	// ConnectMs is the time spent acquiring the upstream connection: near-zero
	// when Reused, or the full DNS+TCP+TLS span otherwise.
	ConnectMs *int64 `json:"connect_ms,omitempty"`
	// DnsMs, TcpMs and TlsMs break ConnectMs down by phase; present only when
	// Reused is false, since none of them run on a reused connection.
	DnsMs *int64 `json:"dns_ms,omitempty"`
	TcpMs *int64 `json:"tcp_ms,omitempty"`
	TlsMs *int64 `json:"tls_ms,omitempty"`
	// FirstByteMs is request start to the first response byte from upstream:
	// dominated by the provider's own time-to-first-token.
	FirstByteMs *int64 `json:"first_byte_ms,omitempty"`

	Input      int64 `json:"input"` // uncached input tokens
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
	Output     int64 `json:"output"`

	// Call details place a model call in its session. They are metadata only:
	// tool names and kinds, never tool arguments, inputs or outputs. All are
	// omitted on rows recorded before they were collected.

	// Tools are the tool calls the model requested in this response (at most MaxTools).
	Tools []Tool `json:"tools,omitempty"`
	// ToolResults counts tool results the request returned to the model.
	ToolResults int `json:"tool_results,omitempty"`
	// Effort is the requested reasoning effort, such as low, medium or high.
	Effort string `json:"effort,omitempty"`
	// ReasoningTokens is the part of Output the provider reports as reasoning.
	ReasoningTokens int64 `json:"reasoning_tokens,omitempty"`
	// Subagent is the kind of agent that made the call: thread_spawn, review,
	// compact, guardian, memory_consolidation, sidechain (Claude) or another label.
	Subagent  string `json:"subagent,omitempty"`
	AgentName string `json:"agent_name,omitempty"`
	// ThreadID identifies a child agent thread within the session; omitted for
	// the session's own thread.
	ThreadID string `json:"thread_id,omitempty"`
	// ParentSessionID is the conversation (thread) that started this one.
	ParentSessionID string `json:"parent_session_id,omitempty"`
	TurnID          string `json:"turn_id,omitempty"`
	// RequestKind is Codex's request purpose: turn, prewarm, compaction or memory.
	RequestKind string `json:"request_kind,omitempty"`
}

// block matches the Anthropic Messages, OpenAI Responses and OpenAI chat
// completions usage objects (OpenRouter and other compatible servers use the
// chat completions shape).
type block struct {
	Input      int64 `json:"input_tokens"`
	Output     int64 `json:"output_tokens"`
	CacheRead  int64 `json:"cache_read_input_tokens"`
	CacheWrite int64 `json:"cache_creation_input_tokens"`
	Details    struct {
		Cached int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputDetails struct {
		Reasoning int64 `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
	// Chat completions naming. Reasoning tokens are billed as, and included in,
	// completion_tokens, so no separate field is needed.
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	PromptDetails    struct {
		Cached int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionDetails struct {
		Reasoning int64 `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

// event is one response body or SSE data line. Model and usage sit at the top
// level (Anthropic message_delta, non-streaming replies), under message
// (Anthropic message_start) or under response (OpenAI response.* events).
//
// Tool calls come from completed Responses output items, Anthropic content
// blocks and chat completions tool calls. Only their type, name and ID are
// decoded; the tolerant list types never fail the event's usage decoding.
type event struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Model    string `json:"model"`
	Usage    *block `json:"usage"`
	Message  *event `json:"message"`
	Response *event `json:"response"`

	Item         toolEntry  `json:"item"`
	ContentBlock toolEntry  `json:"content_block"`
	Output       toolList   `json:"output"`
	Content      toolList   `json:"content"`
	Choices      choiceList `json:"choices"`
}

// Fold reads every event in a body into rec, keeping the largest value seen per
// usage field so partial streaming counts never win.
func Fold(body []byte, rec *Record) {
	for _, line := range bytes.Split(body, []byte("\n")) {
		line = bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(line), []byte("data:")))
		var ev event
		if !bytes.HasPrefix(line, []byte("{")) || json.Unmarshal(line, &ev) != nil {
			continue
		}
		for _, e := range []*event{&ev, ev.Message, ev.Response} {
			if e == nil {
				continue
			}
			if e.Model != "" {
				rec.Model = clip(e.Model, maxModel)
			}
			if strings.HasPrefix(e.ID, "msg_") {
				rec.MessageID = clip(e.ID, 256)
			}
			if u := e.Usage; u != nil {
				rec.Input = max(rec.Input, u.Input-u.Details.Cached, u.PromptTokens-u.PromptDetails.Cached)
				rec.Output = max(rec.Output, u.Output, u.CompletionTokens)
				rec.CacheRead = max(rec.CacheRead, u.CacheRead, u.Details.Cached, u.PromptDetails.Cached)
				rec.CacheWrite = max(rec.CacheWrite, u.CacheWrite)
				rec.ReasoningTokens = max(rec.ReasoningTokens, u.OutputDetails.Reasoning, u.CompletionDetails.Reasoning)
			}
		}
		foldTools(&ev, rec)
	}
}

// foldTools records the tool calls one event reports. Streamed items count when
// they finish. Complete output lists (response.completed, non-streaming bodies)
// repeat those items, so they are used only when no streamed call was seen.
func foldTools(ev *event, rec *Record) {
	add := func(items ...toolItem) {
		for _, it := range items {
			if t, ok := it.tool(); ok {
				rec.Tools = appendTool(rec.Tools, t)
			}
		}
	}
	switch ev.Type {
	case "response.output_item.done":
		if ev.Item.ok {
			add(ev.Item.item)
		}
	case "content_block_start":
		if ev.ContentBlock.ok {
			add(ev.ContentBlock.item)
		}
	}
	add(ev.Choices...)
	if len(rec.Tools) == 0 {
		add(ev.Output...)
		if ev.Type == "" || ev.Type == "message" {
			add(ev.Content...)
		}
		if ev.Response != nil {
			add(ev.Response.Output...)
		}
	}
}

// maxModel bounds the one string a provider writes into a record; a model id is
// a few dozen bytes.
const maxModel = 200

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}
