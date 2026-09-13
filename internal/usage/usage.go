// Package usage is the data model: one Record per completion call, and how to
// read the provider's usage block out of a response body.
package usage

import (
	"bytes"
	"encoding/json"
)

// Record is one completion call, as stored one per line in usage.jsonl.
type Record struct {
	Time    int64  `json:"time"` // unix milliseconds
	Harness string `json:"harness"`
	Account string `json:"account"`
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

	// Sync fields. Never written to usage.jsonl: Device is set in memory on local
	// rows, and all three are on disk only for rows pulled from other devices.
	ID       string `json:"id,omitempty"`
	DeviceID string `json:"device_id,omitempty"`
	Device   string `json:"device,omitempty"`
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
	// Chat completions naming. Reasoning tokens are billed as, and included in,
	// completion_tokens, so no separate field is needed.
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	PromptDetails    struct {
		Cached int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

// event is one response body or SSE data line. Model and usage sit at the top
// level (Anthropic message_delta, non-streaming replies), under message
// (Anthropic message_start) or under response (OpenAI response.* events).
type event struct {
	Model    string `json:"model"`
	Usage    *block `json:"usage"`
	Message  *event `json:"message"`
	Response *event `json:"response"`
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
				rec.Model = e.Model
			}
			if u := e.Usage; u != nil {
				rec.Input = max(rec.Input, u.Input-u.Details.Cached, u.PromptTokens-u.PromptDetails.Cached)
				rec.Output = max(rec.Output, u.Output, u.CompletionTokens)
				rec.CacheRead = max(rec.CacheRead, u.CacheRead, u.Details.Cached, u.PromptDetails.Cached)
				rec.CacheWrite = max(rec.CacheWrite, u.CacheWrite)
			}
		}
	}
}
