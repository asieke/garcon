package usage

import (
	"strings"
	"testing"
)

func TestFold(t *testing.T) {
	cases := []struct {
		name string
		body string
		want Record
	}{
		{"anthropic stream", "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"claude-sonnet-5\",\"usage\":{\"input_tokens\":10,\"cache_read_input_tokens\":500,\"cache_creation_input_tokens\":20,\"output_tokens\":1}}}\n\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":42}}\n",
			Record{Model: "claude-sonnet-5", Input: 10, CacheRead: 500, CacheWrite: 20, Output: 42}},
		{"responses stream", "data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-6-astra\",\"usage\":{\"input_tokens\":300,\"input_tokens_details\":{\"cached_tokens\":200},\"output_tokens\":7}}}\n",
			Record{Model: "gpt-6-astra", Input: 100, CacheRead: 200, Output: 7}},
		{"chat completions stream (openrouter/hermes)", ": OPENROUTER PROCESSING\n\ndata: {\"id\":\"x\",\"model\":\"anthropic/claude-sonnet-5\",\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: {\"id\":\"x\",\"model\":\"anthropic/claude-sonnet-5\",\"choices\":[],\"usage\":{\"prompt_tokens\":1200,\"completion_tokens\":30,\"prompt_tokens_details\":{\"cached_tokens\":1000},\"completion_tokens_details\":{\"reasoning_tokens\":12}}}\n\ndata: [DONE]\n",
			Record{Model: "anthropic/claude-sonnet-5", Input: 200, CacheRead: 1000, Output: 30}},
		{"chat completions non-stream", "{\"model\":\"gpt-5.3-codex\",\"choices\":[],\"usage\":{\"prompt_tokens\":50,\"completion_tokens\":5}}",
			Record{Model: "gpt-5.3-codex", Input: 50, Output: 5}},
	}
	for _, c := range cases {
		var rec Record
		Fold([]byte(c.body), &rec)
		if rec != c.want {
			t.Errorf("%s: got %+v want %+v", c.name, rec, c.want)
		}
	}
}

func TestFoldClipsModel(t *testing.T) {
	var rec Record
	Fold([]byte(`{"model":"`+strings.Repeat("x", 500)+`","usage":{"input_tokens":1}}`), &rec)
	if len(rec.Model) != maxModel || rec.Input != 1 {
		t.Fatalf("model length %d, input %d", len(rec.Model), rec.Input)
	}
}
