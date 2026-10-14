package usage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"unicode"
)

// Tool is one tool call a model asked for in a response. Only the tool's name
// and kind are kept. Arguments, inputs, results and outputs are never decoded.
type Tool struct {
	Name string `json:"name"`
	Kind string `json:"kind,omitempty"`
	// Ref is a short hash of the provider's call ID. It deduplicates a call that
	// appears in several stream events or transcript lines; it is not the ID.
	Ref string `json:"ref,omitempty"`
}

// MaxTools bounds the tool calls kept for one model call.
const MaxTools = 64

const maxToolName = 128

// Call is where a model call sits in a conversation: its turn, thread and
// subagent, read from request metadata. It never holds prompts or tool data.
type Call struct {
	Effort          string
	Subagent        string
	AgentName       string
	ThreadID        string
	ParentSessionID string
	TurnID          string
	RequestKind     string
	ToolResults     int
}

// Apply copies the call's known fields into a record.
func (c Call) Apply(r *Record) {
	set := func(dst *string, v string) {
		if v != "" {
			*dst = v
		}
	}
	set(&r.Effort, c.Effort)
	set(&r.Subagent, c.Subagent)
	set(&r.AgentName, c.AgentName)
	set(&r.ThreadID, c.ThreadID)
	set(&r.ParentSessionID, c.ParentSessionID)
	set(&r.TurnID, c.TurnID)
	set(&r.RequestKind, c.RequestKind)
	if c.ToolResults > 0 {
		r.ToolResults = c.ToolResults
	}
	// The main conversation's own thread ID adds nothing; keep only child threads.
	if r.ThreadID == r.SessionID {
		r.ThreadID = ""
	}
}

// MergeCall combines what two observations of one model call recorded, such as
// a proxied request and the same message in a local transcript. Tool calls are
// joined by reference; other fields keep dst's value unless it is empty.
func MergeCall(dst *Record, src Record) {
	dst.Tools = MergeTools(dst.Tools, src.Tools)
	dst.ToolResults = max(dst.ToolResults, src.ToolResults)
	dst.ReasoningTokens = max(dst.ReasoningTokens, src.ReasoningTokens)
	for _, f := range []struct{ dst, src *string }{
		{&dst.Effort, &src.Effort}, {&dst.Subagent, &src.Subagent}, {&dst.AgentName, &src.AgentName},
		{&dst.ThreadID, &src.ThreadID}, {&dst.ParentSessionID, &src.ParentSessionID},
		{&dst.TurnID, &src.TurnID}, {&dst.RequestKind, &src.RequestKind},
	} {
		if *f.dst == "" {
			*f.dst = *f.src
		}
	}
}

// MergeTools joins two lists of tool calls by reference, keeping a's order first.
func MergeTools(a, b []Tool) []Tool {
	if len(b) == 0 {
		return a
	}
	out := append([]Tool(nil), a...)
	for _, t := range b {
		out = appendTool(out, t)
	}
	return out
}

// appendTool adds a call unless its reference was already recorded.
func appendTool(tools []Tool, t Tool) []Tool {
	if len(tools) >= MaxTools {
		return tools
	}
	if t.Ref != "" {
		for _, old := range tools {
			if old.Ref == t.Ref {
				return tools
			}
		}
	}
	return append(tools, t)
}

// RequestBody is the narrow view of a model request body that Garcon reads for
// routing and attribution. Every other field, including prompts, messages and
// tool outputs, is skipped by the decoder without being stored.
type RequestBody struct {
	Model    string `json:"model"`
	Previous string `json:"previous_response_id"`
	CacheKey string `json:"prompt_cache_key"`
	Session  struct {
		Model string `json:"model"`
	} `json:"session"`
	Metadata       map[string]json.RawMessage `json:"metadata"`
	ClientMetadata map[string]json.RawMessage `json:"client_metadata"`
	// The fields below never fail decoding: an unexpected shape just leaves them empty.
	Reasoning    effortField     `json:"reasoning"`
	OutputConfig effortField     `json:"output_config"`
	Input        trailingResults `json:"input"`
	Messages     lastResults     `json:"messages"`
}

// ParseRequest decodes the routing and attribution fields of a request body.
func ParseRequest(b []byte) (RequestBody, error) {
	var body RequestBody
	err := json.Unmarshal(b, &body)
	return body, err
}

// StringValue returns a metadata entry when it is a JSON string.
func StringValue(m map[string]json.RawMessage, key string) string {
	var s string
	if raw, ok := m[key]; ok {
		_ = json.Unmarshal(raw, &s)
	}
	return s
}

// maxTurnMetadata bounds the Codex turn metadata string that is parsed. The
// header form omits Codex's tool inventory and is preferred for that reason.
const maxTurnMetadata = 64 << 10

// Call reads a request's conversation position from Codex client metadata and
// headers, its reasoning effort, and how many tool results it returns.
func (b RequestBody) Call(h http.Header) Call {
	meta := func(key string) string { return identifier(StringValue(b.ClientMetadata, key), 256) }
	var turn struct {
		TurnID       string `json:"turn_id"`
		ThreadID     string `json:"thread_id"`
		AgentName    string `json:"agent_name"`
		RequestKind  string `json:"request_kind"`
		SubagentKind string `json:"subagent_kind"`
		ParentThread string `json:"parent_thread_id"`
	}
	raw := h.Get("X-Codex-Turn-Metadata")
	if raw == "" {
		raw = StringValue(b.ClientMetadata, "x-codex-turn-metadata")
	}
	if raw != "" && len(raw) <= maxTurnMetadata {
		_ = json.Unmarshal([]byte(raw), &turn)
	}
	first := func(values ...string) string {
		for _, v := range values {
			if v = identifier(v, 256); v != "" {
				return v
			}
		}
		return ""
	}
	c := Call{
		TurnID:          first(meta("turn_id"), turn.TurnID),
		ThreadID:        first(meta("thread_id"), turn.ThreadID, h.Get("Thread-Id")),
		ParentSessionID: first(h.Get("X-Codex-Parent-Thread-Id"), meta("x-codex-parent-thread-id"), turn.ParentThread),
		AgentName:       first(turn.AgentName, meta("agent_name")),
		RequestKind:     keyword(turn.RequestKind),
		Subagent:        Subagent(first(turn.SubagentKind, h.Get("X-Openai-Subagent"), meta("x-openai-subagent"))),
		Effort:          keyword(first(string(b.Reasoning), string(b.OutputConfig))),
		ToolResults:     max(int(b.Input), int(b.Messages)),
	}
	if c.AgentName == "/root" {
		c.AgentName = "" // Codex names every main conversation /root.
	}
	return c
}

// Subagent normalizes Codex's subagent labels to a kind: review, compact,
// memory_consolidation, thread_spawn (spawned child agents), guardian (the
// automatic approval reviewer), or another short label.
func Subagent(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case s == "":
		return ""
	case s == "collab_spawn" || strings.HasPrefix(s, "thread_spawn"):
		return "thread_spawn"
	case strings.HasPrefix(s, "subagent_"):
		return Subagent(strings.TrimPrefix(s, "subagent_"))
	case strings.HasPrefix(s, "internal_"):
		return Subagent(strings.TrimPrefix(s, "internal_"))
	}
	return keyword(s)
}

// IsReview reports whether a call is a background review rather than work in
// the conversation itself.
func IsReview(r Record) bool {
	return r.Model == "codex-auto-review" || r.Subagent == "review" || r.Subagent == "guardian"
}

func identifier(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n || strings.ContainsFunc(s, unicode.IsControl) {
		return ""
	}
	return s
}

// keyword accepts a short lowercase label such as an effort or request kind.
func keyword(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) > 64 {
		return ""
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.') {
			return ""
		}
	}
	return s
}

// ToolName accepts a provider tool name for display, or reports it unusable.
func ToolName(s string) string {
	if s == "" || len(s) > maxToolName || strings.ContainsFunc(s, unicode.IsControl) || !strings.ContainsFunc(s, func(r rune) bool { return !unicode.IsSpace(r) }) {
		return ""
	}
	return s
}

func ref(ids ...string) string {
	for _, id := range ids {
		if id != "" {
			sum := sha256.Sum256([]byte(id))
			return hex.EncodeToString(sum[:6])
		}
	}
	return ""
}

// effortField reads {"effort": "..."} and ignores any other shape.
type effortField string

func (e *effortField) UnmarshalJSON(b []byte) error {
	var v struct {
		Effort string `json:"effort"`
	}
	if json.Unmarshal(b, &v) == nil {
		*e = effortField(v.Effort)
	}
	return nil
}

// trailingResults counts tool outputs that a Responses request returns to the
// model. Codex resends the whole history, so only outputs after the model's
// latest calls count: walk back over outputs and calls and stop at anything else.
type trailingResults int

func (t *trailingResults) UnmarshalJSON(b []byte) error {
	var items []struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(b, &items) != nil {
		return nil
	}
	n := 0
	for i := len(items) - 1; i >= 0; i-- {
		kind := items[i].Type
		if strings.HasSuffix(kind, "_output") {
			n++
		} else if !strings.HasSuffix(kind, "_call") {
			break
		}
	}
	*t = trailingResults(n)
	return nil
}

// lastResults counts tool_result blocks in the final user message of an
// Anthropic Messages request.
type lastResults int

func (l *lastResults) UnmarshalJSON(b []byte) error {
	var messages []struct {
		Role    string     `json:"role"`
		Content blockTypes `json:"content"`
	}
	if json.Unmarshal(b, &messages) != nil || len(messages) == 0 {
		return nil
	}
	if last := messages[len(messages)-1]; last.Role == "user" {
		*l = lastResults(last.Content)
	}
	return nil
}

// blockTypes counts tool_result content blocks; string content has none.
type blockTypes int

func (c *blockTypes) UnmarshalJSON(b []byte) error {
	var blocks []struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(b, &blocks) != nil {
		return nil
	}
	for _, block := range blocks {
		if block.Type == "tool_result" {
			*c++
		}
	}
	return nil
}

// toolItem is the identifying part of a Responses output item, an Anthropic
// content block or a chat completions tool call. Payload fields such as
// arguments, input, output and text are deliberately absent.
type toolItem struct {
	Type        string `json:"type"`
	ID          string `json:"id"`
	CallID      string `json:"call_id"`
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	ServerLabel string `json:"server_label"`
	ServerName  string `json:"server_name"`
	Function    *struct {
		Name string `json:"name"`
	} `json:"function"`
}

// tool reports the tool call an item represents, if any.
func (it toolItem) tool() (Tool, bool) {
	name, kind := it.Name, ""
	mcp := func(server string) {
		if server != "" {
			name = "mcp__" + server + "__" + name
		}
		kind = "mcp"
	}
	switch it.Type {
	case "function_call", "custom_tool_call", "tool_use":
		if it.Namespace != "" {
			name = strings.TrimSuffix(it.Namespace, "__") + "__" + name
		}
		kind = map[string]string{"function_call": "function", "custom_tool_call": "custom", "tool_use": "function"}[it.Type]
		if strings.HasPrefix(name, "mcp__") {
			kind = "mcp"
		}
	case "function":
		if it.Function == nil || it.Function.Name == "" {
			return Tool{}, false
		}
		name, kind = it.Function.Name, "function"
		if strings.HasPrefix(name, "mcp__") {
			kind = "mcp"
		}
	case "server_tool_use":
		kind = "server"
	case "mcp_tool_use":
		mcp(it.ServerName)
	case "mcp_call":
		mcp(it.ServerLabel)
	case "local_shell_call":
		name, kind = "local_shell", "shell"
	default:
		if !strings.HasSuffix(it.Type, "_call") {
			return Tool{}, false
		}
		name = strings.TrimSuffix(it.Type, "_call")
		kind = name
	}
	name = ToolName(name)
	if name == "" {
		name = "unknown"
	}
	return Tool{Name: name, Kind: keyword(kind), Ref: ref(it.ID, it.CallID)}, true
}

// toolEntry and toolList decode tool calls without ever failing the enclosing
// event, so a provider shape change cannot cost the usage counts.
type toolEntry struct {
	item toolItem
	ok   bool
}

func (t *toolEntry) UnmarshalJSON(b []byte) error {
	t.ok = json.Unmarshal(b, &t.item) == nil
	return nil
}

type toolList []toolItem

func (l *toolList) UnmarshalJSON(b []byte) error {
	var entries []toolEntry
	if json.Unmarshal(b, &entries) != nil {
		return nil
	}
	for _, e := range entries {
		if e.ok {
			*l = append(*l, e.item)
		}
	}
	return nil
}

// choiceList reads the tool calls of chat completions choices (streamed deltas
// and complete messages).
type choiceList []toolItem

func (c *choiceList) UnmarshalJSON(b []byte) error {
	var choices []struct {
		Delta struct {
			ToolCalls toolList `json:"tool_calls"`
		} `json:"delta"`
		Message struct {
			ToolCalls toolList `json:"tool_calls"`
		} `json:"message"`
	}
	if json.Unmarshal(b, &choices) != nil {
		return nil
	}
	for _, choice := range choices {
		*c = append(*c, choice.Delta.ToolCalls...)
		*c = append(*c, choice.Message.ToolCalls...)
	}
	return nil
}

// ToolCalls decodes the tool calls in an Anthropic message content array, such
// as an assistant line of a local Claude transcript. Text, thinking and tool
// inputs are skipped by the decoder; string content and other shapes yield none.
type ToolCalls []Tool

func (t *ToolCalls) UnmarshalJSON(b []byte) error {
	var items toolList
	_ = items.UnmarshalJSON(b)
	*t = nil
	for _, it := range items {
		if tool, ok := it.tool(); ok {
			*t = appendTool(*t, tool)
		}
	}
	return nil
}
