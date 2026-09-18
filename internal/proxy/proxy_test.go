package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"garcon/internal/usage"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClaudeAccountFollowsRequestCredentials(t *testing.T) {
	// Stub the profile endpoint without reading local credentials or contacting
	// Anthropic. All other traffic still goes through a real local upstream.
	original := http.DefaultTransport
	http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.anthropic.com" {
			return original.RoundTrip(r)
		}
		if r.URL.Path != "/api/oauth/profile" {
			t.Fatalf("unexpected lookup: %s", r.URL.Path)
		}
		body := `{"account":{"email":"work@example.com"}}`
		status := 200
		switch r.Header.Get("Authorization") {
		case "Bearer sk-ant-oat-personal":
			body = `{"account":{"email":"personal@example.com"}}`
		case "Bearer sk-ant-oat-failed":
			status, body = 403, `{}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	defer func() { http.DefaultTransport = original }()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		io.WriteString(w, `{"usage":{"input_tokens":1,"output_tokens":2}}`)
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)
	var got usage.Record
	p := Proxy{Save: func(r usage.Record) { got = r }}
	for _, tc := range []struct{ token, want string }{
		{"sk-ant-oat-work", "work@example.com"},
		{"sk-ant-oat-personal", "personal@example.com"},
		{"sk-ant-oat-failed", "anthropic:credential:"},
		{"", "anthropic:unknown"},
	} {
		rt, _ := ParseRoute("/claude/v1/messages")
		rt.Target = target
		r := httptest.NewRequest("POST", "/claude/v1/messages", strings.NewReader(`{}`))
		if tc.token != "" {
			r.Header.Set("Authorization", "Bearer "+tc.token)
		}
		w := httptest.NewRecorder()
		p.Serve(w, r, rt)
		if w.Code != 200 || !strings.HasPrefix(got.Account, tc.want) {
			t.Fatalf("status %d, account %q; want %q", w.Code, got.Account, tc.want)
		}
	}
}

func TestAutomaticAccountsPreserveTraffic(t *testing.T) {
	const body = `{"model":"test","messages":[{"content":"do not modify"}]}`
	const response = "data: {\"usage\":{\"input_tokens\":7,\"output_tokens\":3}}\n\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if string(b) != body || r.URL.Path != "/backend-api/codex/responses" || r.URL.RawQuery != "test=1" || r.Header.Get("Authorization") != "Bearer original" {
			t.Error("proxy changed request")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, response)
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)
	var records []usage.Record
	p := Proxy{Save: func(r usage.Record) { records = append(records, r) }}
	for _, tc := range []struct{ path, id, want string }{
		{"/codex/backend-api/codex/responses", "one", "chatgpt:one"},
		{"/codex/backend-api/codex/responses", "two", "chatgpt:two"},
		{"/hermes/chatgpt/backend-api/codex/responses", "one", "chatgpt:one"},
		{"/openclaw/chatgpt/backend-api/codex/responses", "two", "chatgpt:two"},
	} {
		rt, ok := ParseRoute(tc.path)
		if !ok {
			t.Fatal("route rejected")
		}
		rt.Target = target
		r := httptest.NewRequest("POST", tc.path+"?test=1", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer original")
		r.Header.Set("ChatGPT-Account-Id", tc.id)
		w := httptest.NewRecorder()
		p.Serve(w, r, rt)
		if w.Code != 200 || w.Body.String() != response {
			t.Fatal("proxy changed response")
		}
		if got := records[len(records)-1]; got.Account != tc.want || got.Input != 7 || got.Output != 3 {
			t.Fatalf("record: %+v", got)
		}
	}
}

func TestParseRoute(t *testing.T) {
	cases := []struct{ path, harness, provider, rest string }{
		{"/claude/v1/messages", "claude", "anthropic", "v1/messages"},
		{"/claude/api/claude_code/policy_limits", "claude", "anthropic", "api/claude_code/policy_limits"},
		{"/codex/backend-api/codex/responses", "codex", "chatgpt", "backend-api/codex/responses"},
		{"/hermes/openrouter/api/v1/chat/completions", "hermes", "openrouter", "api/v1/chat/completions"},
		{"/hermes/anthropic/v1/messages", "hermes", "anthropic", "v1/messages"},
		{"/hermes/openai/v1/responses", "hermes", "openai", "v1/responses"},
		{"/hermes/chatgpt/backend-api/codex/responses", "hermes", "chatgpt", "backend-api/codex/responses"},
		{"/openclaw/anthropic/v1/messages", "openclaw", "anthropic", "v1/messages"},
		{"/myagent/openai/v1/responses", "myagent", "openai", "v1/responses"},
	}
	for _, c := range cases {
		rt, ok := ParseRoute(c.path)
		if !ok || rt.Harness != c.harness || rt.Provider != c.provider || rt.Rest != c.rest {
			t.Errorf("%s: got %+v, ok=%v", c.path, rt, ok)
		}
	}
}

func TestRejectAccountURLs(t *testing.T) {
	for _, path := range []string{
		"/claude/me@x.com/v1/messages", "/codex/me@x.com/backend-api/codex/responses",
		"/hermes/me@x.com/openrouter/api/v1/chat/completions", "/openclaw/team/anthropic/v1/messages",
		"/myagent/team/openai/v1/responses", "/hermes/nowhere/v1/chat/completions",
		"/_app/immutable/nodes/2.js", "/api/usage", "/robots.txt", "/",
	} {
		if rt, ok := ParseRoute(path); ok {
			t.Errorf("accepted %s: %+v", path, rt)
		}
	}
}

func TestIsCompletion(t *testing.T) {
	for path, want := range map[string]bool{
		"v1/messages": true, "backend-api/codex/responses": true, "v1/responses": true,
		"api/v1/chat/completions": true, "v1/chat/completions": true,
		"v1/models": false, "api/v1/models": false, "v1/messages/count_tokens": false,
	} {
		if got := IsCompletion(path); got != want {
			t.Errorf("%s: %v want %v", path, got, want)
		}
	}
}
