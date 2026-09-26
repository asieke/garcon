package openrouter

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestKeyLifecycleAndAuthorization(t *testing.T) {
	s := New(t.TempDir())
	call := func(method, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:4141/api/providers/openrouter", strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	const key = "test-only-secret"
	w := call("PUT", `{"key":"`+key+`"}`)
	if w.Code != 200 || strings.Contains(w.Body.String(), key) {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	info, err := os.Stat(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal("key file is not private")
	}
	if w = call("GET", ""); w.Code != 200 || strings.Contains(w.Body.String(), key) || !strings.Contains(w.Body.String(), "true") {
		t.Fatal("unsafe or incorrect status")
	}
	r := httptest.NewRequest("POST", "http://localhost:4141/pi/openrouter/api/v1/chat/completions", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Authorization", "Bearer garcon-local")
	r.Header.Set("Cookie", "session=test")
	r.Header.Set("ChatGPT-Account-Id", "test")
	if status, err := s.Authorize(r); status != 0 || err != nil || r.Header.Get("Authorization") != "Bearer "+key || r.Header.Get("Cookie") != "" || r.Header.Get("ChatGPT-Account-Id") != "" {
		t.Fatal("incorrect upstream credentials")
	}
	if w = call("DELETE", ""); w.Code != 200 {
		t.Fatal("delete failed")
	}
	r.Header.Set("Authorization", "Bearer garcon-local")
	if status, _ := s.Authorize(r); status != 401 {
		t.Fatal("accepted placeholder without saved key")
	}
	for _, body := range []string{`{"key":"short"}`, `{"key":"valid-key","extra":true}`, `{"key":"valid-key"}{}`, `{"key":"space in key"}`} {
		if call("PUT", body).Code != 400 {
			t.Fatal("accepted invalid key body")
		}
	}
}

func TestRejectNonLocalOrCrossOrigin(t *testing.T) {
	s := New(t.TempDir())
	for _, tc := range []struct{ host, remote, origin string }{{"localhost:4141", "192.0.2.1:1234", ""}, {"evil.example", "127.0.0.1:1234", ""}, {"localhost:4141", "127.0.0.1:1234", "https://evil.example"}} {
		r := httptest.NewRequest("PUT", "http://"+tc.host+"/api/providers/openrouter", strings.NewReader(`{"key":"test-only-secret"}`))
		r.RemoteAddr = tc.remote
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("accepted unsafe key management request")
		}
		if status, _ := s.Authorize(r); status != 403 {
			t.Fatal("accepted unsafe proxy request")
		}
	}
}
