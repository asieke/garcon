package claude

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testGateway() *Gateway {
	return &Gateway{keyHash: sha256.Sum256([]byte("test")), profile: "/chosen-profile",
		load: func(context.Context, string) (Credentials, error) {
			return Credentials{AccessToken: "upstream-token"}, nil
		},
		renew: func(context.Context, string) (bool, error) { return false, nil },
	}
}

func TestGatewayCredentialReplacement(t *testing.T) {
	for _, header := range []string{"Authorization", "X-Api-Key"} {
		t.Run(header, func(t *testing.T) {
			g := testGateway()
			g.load = func(_ context.Context, profile string) (Credentials, error) {
				if profile != "/chosen-profile" {
					t.Fatalf("wrong profile: %s", profile)
				}
				return Credentials{AccessToken: "upstream-token"}, nil
			}
			body := `{"stream":true,"messages":[{"role":"user","content":"hello"}]}`
			r := httptest.NewRequest("POST", "http://127.0.0.1:4141/claude-gateway/v1/messages?beta=true", strings.NewReader(body))
			r.RemoteAddr = "127.0.0.1:1234"
			value := "test"
			if header == "Authorization" {
				value = "Bearer test"
			}
			r.Header.Set(header, value)
			r.Header.Set("Anthropic-Beta", "prompt-caching-test")
			called := false
			w := httptest.NewRecorder()
			g.Handler(http.HandlerFunc(func(w http.ResponseWriter, out *http.Request) {
				called = true
				if out.Header.Get("Authorization") != "Bearer upstream-token" || out.Header.Get("X-Api-Key") != "" {
					t.Fatal("local key was not replaced")
				}
				if out.URL.Path != "/claude/v1/messages" || out.URL.RawQuery != "beta=true" {
					t.Fatal("wrong route")
				}
				if out.Header.Get("Anthropic-Beta") != "prompt-caching-test,oauth-2025-04-20" {
					t.Fatal("beta flags not preserved")
				}
				b, _ := io.ReadAll(out.Body)
				if string(b) != body {
					t.Fatal("body changed")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, "event: message_stop\ndata: {}\n\n")
			})).ServeHTTP(w, r)
			if !called || w.Code != 200 || !strings.Contains(w.Body.String(), "message_stop") {
				t.Fatalf("gateway failed: %d", w.Code)
			}
			if r.Header.Get(header) != value || r.URL.Path != "/claude-gateway/v1/messages" {
				t.Fatal("original request mutated")
			}
		})
	}
}

func TestGatewayRejectsBeforeLoadingLogin(t *testing.T) {
	for _, tc := range []struct {
		name, key, peer, host, origin, path string
		want                                int
	}{
		{"missing", "", "127.0.0.1:1", "localhost", "", "/v1/messages", 401},
		{"wrong", "wrong", "127.0.0.1:1", "localhost", "", "/v1/messages", 401},
		{"remote", "test", "192.0.2.1:1", "localhost", "", "/v1/messages", 403},
		{"rebound", "test", "127.0.0.1:1", "evil.example", "", "/v1/messages", 403},
		{"browser", "test", "127.0.0.1:1", "localhost", "http://evil.example", "/v1/messages", 403},
		{"non-inference", "test", "127.0.0.1:1", "localhost", "", "/api/oauth/profile", 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := testGateway()
			g.renew = func(context.Context, string) (bool, error) { t.Fatal("unauthorized login access"); return false, nil }
			r := httptest.NewRequest("POST", "http://localhost/claude-gateway"+tc.path, nil)
			r.RemoteAddr, r.Host = tc.peer, tc.host
			r.Header.Set("X-Api-Key", tc.key)
			r.Header.Set("Origin", tc.origin)
			w := httptest.NewRecorder()
			g.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("forwarded rejected request") })).ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("got %d, want %d", w.Code, tc.want)
			}
		})
	}
}

func TestGatewayUnavailableLogin(t *testing.T) {
	for _, mode := range []string{"renewal", "missing", "expired"} {
		t.Run(mode, func(t *testing.T) {
			g := testGateway()
			if mode == "renewal" {
				g.renew = func(context.Context, string) (bool, error) { return false, errors.New("private-error") }
			}
			if mode == "missing" {
				g.load = func(context.Context, string) (Credentials, error) { return Credentials{}, errors.New("private-error") }
			}
			if mode == "expired" {
				g.load = func(context.Context, string) (Credentials, error) {
					return Credentials{AccessToken: "expired-token", ExpiresAt: time.Now().Add(-time.Hour).UnixMilli()}, nil
				}
			}
			r := httptest.NewRequest("GET", "http://localhost/claude-gateway/v1/models", nil)
			r.RemoteAddr = "[::1]:1234"
			r.Header.Set("Authorization", "Bearer test")
			w := httptest.NewRecorder()
			g.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("forwarded invalid login") })).ServeHTTP(w, r)
			if w.Code != 503 || strings.Contains(w.Body.String(), "private-error") {
				t.Fatalf("unexpected error: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestGatewayOptIn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.json")
	g, err := LoadGateway(path)
	if err != nil || g != nil {
		t.Fatal("missing config must disable gateway")
	}
	w := httptest.NewRecorder()
	g.Handler(nil).ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/claude-gateway/v1/models", nil))
	if w.Code != 404 {
		t.Fatal("disabled gateway is reachable")
	}
	for _, input := range []string{`{`, `{}`, `{"key":"test","profile":"relative"}`} {
		os.WriteFile(path, []byte(input), 0600)
		if _, err := LoadGateway(path); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
	os.WriteFile(path, []byte(`{"key":"test","profile":"/selected/profile"}`), 0600)
	g, err = LoadGateway(path)
	if err != nil || g == nil || g.profile != "/selected/profile" {
		t.Fatal("valid config rejected")
	}
}
