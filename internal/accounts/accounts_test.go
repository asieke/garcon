package accounts

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jwt(claims string) string {
	return "header." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".signature"
}

func TestChatGPTIdentity(t *testing.T) {
	for _, tc := range []struct{ name, selected, claims, want string }{
		{"profile email", "acct-a", `{"https://api.openai.com/auth":{"chatgpt_account_id":"acct-a"},"https://api.openai.com/profile":{"email":"a@example.com"}}`, "a@example.com"},
		{"token without header", "", `{"https://api.openai.com/auth":{"chatgpt_account_id":"acct-b"}}`, "chatgpt:acct-b"},
		{"selected workspace wins", "acct-b", `{"email":"a@example.com","https://api.openai.com/auth":{"chatgpt_account_id":"acct-a"}}`, "chatgpt:acct-b"},
		{"malformed token", "acct-a", `{`, "chatgpt:acct-a"},
		{"malformed claim", "acct-a", `{"email":"wrong@example.com","https://api.openai.com/auth":true}`, "chatgpt:acct-a"},
		{"no credentials", "", `{}`, "chatgpt:unknown"},
		{"unsafe label", "bad\nlabel", `{}`, "chatgpt:unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			h.Set("ChatGPT-Account-Id", tc.selected)
			if tc.claims != `{}` {
				h.Set("Authorization", "Bearer "+jwt(tc.claims))
			}
			var r Resolver
			if got := r.Resolve("chatgpt", h); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCredentialIsolation(t *testing.T) {
	var r Resolver
	h := http.Header{"Authorization": {"Bearer secret-one"}}
	a := r.Resolve("openai", h)
	if a != r.Resolve("openai", h) || !strings.HasPrefix(a, "openai:key:") || strings.Contains(a, "secret") {
		t.Fatal("unstable or exposed credential")
	}
	h.Set("Authorization", "Bearer secret-two")
	if a == r.Resolve("openai", h) {
		t.Fatal("different credentials merged")
	}
	h.Set("Authorization", "Bearer secret-one")
	if a == r.Resolve("openrouter", h) {
		t.Fatal("different providers merged")
	}
	h.Set("X-Api-Key", "api-key")
	h.Set("Authorization", "Bearer sk-ant-oat-not-the-key-owner")
	if got := r.Resolve("anthropic", h); got != fingerprint("anthropic", "key", "api-key") {
		t.Fatalf("API key precedence: %q", got)
	}
}

func TestDisplayLabel(t *testing.T) {
	for input, want := range map[string]string{
		"a@example.com [chatgpt:workspace-a]":       "a@example.com",
		"a@example.com":                             "a@example.com",
		"chatgpt:workspace-a":                       "chatgpt:workspace-a",
		"team [chatgpt:workspace-a]":                "team [chatgpt:workspace-a]",
		"a@example.com [chatgpt:]":                  "a@example.com [chatgpt:]",
		"a@example.com [chatgpt:workspace-a] extra": "a@example.com [chatgpt:workspace-a] extra",
	} {
		if got := DisplayLabel(input); got != want {
			t.Errorf("DisplayLabel(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestAnthropicConcurrentSwitchAndRefresh(t *testing.T) {
	var calls atomic.Int32
	r := Resolver{client: &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		if req.URL.String() != profileURL || req.Method != "GET" {
			t.Error("unexpected credential destination")
		}
		email := "a@example.com"
		if req.Header.Get("Authorization") == "Bearer sk-ant-oat-b" {
			email = "b@example.com"
		}
		time.Sleep(10 * time.Millisecond)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"account":{"email":%q}}`, email)))}, nil
	})}}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := r.Resolve("anthropic", http.Header{"Authorization": {"Bearer sk-ant-oat-a"}}); got != "a@example.com" {
				t.Errorf("got %q", got)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("concurrent requests made %d lookups", calls.Load())
	}
	for _, tc := range []struct{ token, want string }{
		{"sk-ant-oat-b", "b@example.com"},
		{"sk-ant-oat-refreshed-a", "a@example.com"},
		{"sk-ant-oat-a", "a@example.com"},
	} {
		if got := r.Resolve("anthropic", http.Header{"Authorization": {"Bearer " + tc.token}}); got != tc.want {
			t.Fatalf("got %q, want %q", got, tc.want)
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("got %d lookups", calls.Load())
	}
}

func TestAnthropicFailureAndRetry(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"unauthorized", `{"error":"secret-token"}`, 401},
		{"malformed", `{`, 200},
		{"missing identity", `{}`, 200},
		{"oversize", strings.Repeat(" ", 64*1024) + `{"account":{"email":"a@example.com"}}`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			r := Resolver{client: &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}}
			h := http.Header{"Authorization": {"Bearer sk-ant-oat-test"}}
			want := fingerprint("anthropic", "credential", "sk-ant-oat-test")
			for range 2 {
				if got := r.Resolve("anthropic", h); got != want {
					t.Fatalf("got %q", got)
				}
			}
			if calls != 1 {
				t.Fatal("failed lookup was not cached")
			}
			for _, e := range r.cache {
				e.expires = time.Now().Add(-time.Second)
			}
			r.Resolve("anthropic", h)
			if calls != 2 {
				t.Fatal("expired failure was not retried")
			}
		})
	}
}
