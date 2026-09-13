package main

import "testing"

func TestParseRoute(t *testing.T) {
	cases := []struct {
		path                             string
		ok                               bool
		harness, account, provider, rest string
	}{
		{"/claude/me@x.com/v1/messages", true, "claude", "me@x.com", "anthropic", "v1/messages"},
		{"/codex/me@x.com/backend-api/codex/responses", true, "codex", "me@x.com", "chatgpt", "backend-api/codex/responses"},
		{"/hermes/me@x.com/openrouter/api/v1/chat/completions", true, "hermes", "me@x.com", "openrouter", "api/v1/chat/completions"},
		{"/openclaw/me@x.com/anthropic/v1/messages", true, "openclaw", "me@x.com", "anthropic", "v1/messages"},
		{"/openclaw/me@x.com/openai/v1/responses", true, "openclaw", "me@x.com", "openai", "v1/responses"},
		{"/hermes/me@x.com/nowhere/v1/chat/completions", false, "", "", "", ""},
		{"/_app/immutable/nodes/2.js", false, "", "", "", ""},
		{"/api/usage", false, "", "", "", ""},
		{"/robots.txt", false, "", "", "", ""},
		{"/", false, "", "", "", ""},
	}
	for _, c := range cases {
		rt, ok := parseRoute(c.path)
		if ok != c.ok {
			t.Errorf("%s: ok=%v want %v", c.path, ok, c.ok)
			continue
		}
		if ok && (rt.harness != c.harness || rt.account != c.account || rt.provider != c.provider || rt.rest != c.rest) {
			t.Errorf("%s: got %+v", c.path, rt)
		}
	}
}

func TestIsCompletion(t *testing.T) {
	for path, want := range map[string]bool{
		"v1/messages": true, "backend-api/codex/responses": true, "v1/responses": true,
		"api/v1/chat/completions": true, "v1/chat/completions": true,
		"v1/models": false, "api/v1/models": false, "v1/messages/count_tokens": false,
	} {
		if got := isCompletion(path); got != want {
			t.Errorf("%s: %v want %v", path, got, want)
		}
	}
}

func TestFold(t *testing.T) {
	cases := []struct {
		name string
		body string
		want record
	}{
		{"anthropic stream", "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"claude-sonnet-5\",\"usage\":{\"input_tokens\":10,\"cache_read_input_tokens\":500,\"cache_creation_input_tokens\":20,\"output_tokens\":1}}}\n\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":42}}\n",
			record{Model: "claude-sonnet-5", Input: 10, CacheRead: 500, CacheWrite: 20, Output: 42}},
		{"responses stream", "data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-6-astra\",\"usage\":{\"input_tokens\":300,\"input_tokens_details\":{\"cached_tokens\":200},\"output_tokens\":7}}}\n",
			record{Model: "gpt-6-astra", Input: 100, CacheRead: 200, Output: 7}},
		{"chat completions stream (openrouter/hermes)", ": OPENROUTER PROCESSING\n\ndata: {\"id\":\"x\",\"model\":\"anthropic/claude-sonnet-5\",\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: {\"id\":\"x\",\"model\":\"anthropic/claude-sonnet-5\",\"choices\":[],\"usage\":{\"prompt_tokens\":1200,\"completion_tokens\":30,\"prompt_tokens_details\":{\"cached_tokens\":1000},\"completion_tokens_details\":{\"reasoning_tokens\":12}}}\n\ndata: [DONE]\n",
			record{Model: "anthropic/claude-sonnet-5", Input: 200, CacheRead: 1000, Output: 30}},
		{"chat completions non-stream", "{\"model\":\"gpt-5.3-codex\",\"choices\":[],\"usage\":{\"prompt_tokens\":50,\"completion_tokens\":5}}",
			record{Model: "gpt-5.3-codex", Input: 50, Output: 5}},
	}
	for _, c := range cases {
		var rec record
		fold([]byte(c.body), &rec)
		if rec != c.want {
			t.Errorf("%s: got %+v want %+v", c.name, rec, c.want)
		}
	}
}
