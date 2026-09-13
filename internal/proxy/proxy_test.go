package proxy

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
		rt, ok := ParseRoute(c.path)
		if ok != c.ok {
			t.Errorf("%s: ok=%v want %v", c.path, ok, c.ok)
			continue
		}
		if ok && (rt.Harness != c.harness || rt.Account != c.account || rt.Provider != c.provider || rt.Rest != c.rest) {
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
		if got := IsCompletion(path); got != want {
			t.Errorf("%s: %v want %v", path, got, want)
		}
	}
}
