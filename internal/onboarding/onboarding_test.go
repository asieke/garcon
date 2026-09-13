package onboarding

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalURL(t *testing.T) {
	for _, raw := range []string{"https://example.com", "http://example.com", "http://user@localhost", "http://localhost/path", "http://localhost?key=x", ":bad"} {
		if _, err := LocalURL(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	for _, raw := range []string{DefaultURL, "http://localhost:4141/", "http://[::1]:4141"} {
		if _, err := LocalURL(raw); err != nil {
			t.Errorf("rejected %q: %v", raw, err)
		}
	}
}

func TestDoctor(t *testing.T) {
	for _, tc := range []struct{ name, version, settings, want string }{
		{"local", "test", `{"settings":{"device_name":"laptop"},"status":{}}`, ""},
		{"stale", "old", `{}`, "version mismatch"},
		{"sync failure", "test", `{"settings":{"sync_enabled":true},"status":{"last_pull_error":"missing table"}}`, "missing table"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/config" {
					w.Write([]byte(`{"version":"` + tc.version + `","started":1,"data":"usage.jsonl","rows":0}`))
				} else {
					w.Write([]byte(tc.settings))
				}
			}))
			defer server.Close()
			var out bytes.Buffer
			err := Doctor(server.URL, "test", &out)
			if tc.want == "" {
				if err != nil || !strings.Contains(out.String(), "still waiting") {
					t.Fatalf("%v: %s", err, &out)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestRejectWrongServerAndRedirect(t *testing.T) {
	for _, body := range []string{`{}`, `<html>other app</html>`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(body)) }))
		if _, err := Health(s.URL); err == nil {
			t.Fatal("accepted unrelated server")
		}
		s.Close()
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "http://example.com", 302) }))
	defer s.Close()
	if _, err := Health(s.URL); err == nil {
		t.Fatal("followed redirect")
	}
}
