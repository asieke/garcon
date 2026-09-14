package claude

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"garcon/internal/local"
)

func TestRouted(t *testing.T) {
	cases := []struct {
		path, want string
		added      bool
	}{
		{"/api/claude_code/policy_limits", "/claude/me@x/api/claude_code/policy_limits", true},
		{"/claude/me@x/v1/messages", "/claude/me@x/v1/messages", false},
		{"/claude/me@x", "/claude/me@x", false},
		{"/claude/me@x-evil/v1/messages", "/claude/me@x/claude/me@x-evil/v1/messages", true},
		{"/", "/claude/me@x/", true},
	}
	for _, c := range cases {
		got, added := routed(c.path, "me@x")
		if got != c.want || added != c.added {
			t.Errorf("routed(%q) = %q, %v; want %q, %v", c.path, got, added, c.want, c.added)
		}
	}
}

// TestRelay stands a Garcon look-alike behind the real loopback guard and sends what Claude Code
// sends over the socket, including a Host header the guard would refuse if it were forwarded.
func TestRelay(t *testing.T) {
	type seen struct{ path, query, auth, host string }
	var got seen
	upstream := httptest.NewServer(local.Guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = seen{r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"), r.Host}
	}), true))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)
	h := handler("me@x", target, func() string { return "tok" })

	req := httptest.NewRequest("GET", "http://localhost/api/claude_code/policy_limits?x=1", nil)
	req.Host = "api.anthropic.com"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bare policy poll: status %d, body %s", rec.Code, rec.Body)
	}
	if got.path != "/claude/me@x/api/claude_code/policy_limits" || got.query != "x=1" || got.auth != "Bearer tok" || !strings.HasPrefix(got.host, "127.0.0.1:") {
		t.Errorf("bare policy poll reached upstream as %+v", got)
	}

	req = httptest.NewRequest("POST", "http://localhost/claude/me@x/v1/messages", strings.NewReader("{}"))
	req.Host = "api.anthropic.com"
	req.Header.Set("Authorization", "Bearer own")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("routed call: status %d, body %s", rec.Code, rec.Body)
	}
	if got.path != "/claude/me@x/v1/messages" || got.auth != "Bearer own" {
		t.Errorf("routed call reached upstream as %+v", got)
	}
}
