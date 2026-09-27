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

func TestRequestMetadataPreservesBody(t *testing.T) {
	for _, tc := range []struct{ name, harness, body, header, want, model string }{
		{"claude header", "claude", `{"model":"claude-test","metadata":{"session_id":"body"}}`, "header", "header", "claude-test"},
		{"claude structured identity", "claude", `{"model":"claude-test","metadata":{"user_id":"{\"device_id\":\"private-device\",\"session_id\":\"conversation\"}"}}`, "", "conversation", "claude-test"},
		{"claude legacy identity", "claude", `{"metadata":{"user_id":"user_private_account_private_session_conversation"}}`, "", "conversation", ""},
		{"unrecognized identity", "claude", `{"metadata":{"user_id":"private-device"}}`, "", "", ""},
		{"codex without routing", "codex", `{"model":"test","client_metadata":{"session_id":"conversation"}}`, "", "conversation", "test"},
		{"codex cache key", "codex", `{"prompt_cache_key":"conversation"}`, "", "conversation", ""},
		{"future client", "future", `{"model":"claude-test","metadata":{"session_id":"conversation"}}`, "", "conversation", "claude-test"},
		{"invalid json", "claude", `{bad`, "header", "header", ""},
		{"invalid session", "future", `{"metadata":{"session_id":"bad\nvalue"}}`, "", "", ""},
		{"oversized", "claude", strings.Repeat("x", (32<<20)+2), "header", "header", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
			r.Header.Set("X-Claude-Code-Session-Id", tc.header)
			id, model := requestMetadata(r, tc.harness)
			if id != tc.want || model != tc.model {
				t.Fatalf("got %q %q", id, model)
			}
			b, err := io.ReadAll(r.Body)
			if err != nil || string(b) != tc.body {
				t.Fatal("body changed", err)
			}
		})
	}
}

func TestClaudeSessionIsPresentWhileStreamingAndOnFailure(t *testing.T) {
	const body = `{"model":"claude-test","messages":[{"role":"user","content":"unchanged"}]}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if string(b) != body || r.Header.Get("X-Claude-Code-Session-Id") != "conversation" {
			t.Error("request changed")
		}
		http.Error(w, "unavailable", 503)
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)
	var records []usage.Record
	record := func(r usage.Record) { records = append(records, r) }
	p := Proxy{Start: record, Save: record}
	rt, _ := ParseRoute("/claude/v1/messages")
	rt.Target = target
	r := httptest.NewRequest("POST", "/claude/v1/messages", strings.NewReader(body))
	r.Header.Set("X-Claude-Code-Session-Id", "conversation")
	p.Serve(httptest.NewRecorder(), r, rt)
	if len(records) != 2 || records[0].State != "streaming" || records[1].State != "failed" {
		t.Fatalf("%+v", records)
	}
	for _, rec := range records {
		if rec.Harness != "claude" || rec.SessionID != "conversation" || rec.Model != "claude-test" {
			t.Fatalf("lost attribution: %+v", rec)
		}
	}
}
