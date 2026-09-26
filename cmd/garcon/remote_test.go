package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"garcon/internal/codexrouting"
	"garcon/internal/control"
	"garcon/internal/limits"
	"garcon/internal/store"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testHandlers(t *testing.T, remote bool) (http.Handler, http.Handler, *store.Store) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	dir, err := os.MkdirTemp("/tmp", "gv-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	st, err := store.Open(filepath.Join(dir, "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	li := limits.New(st.Dir(), st.DB)
	routing := codexrouting.New(st.Dir(), li.Snapshot, st.DB)
	web, admin, err := newHandlers(st, li, routing, "0.0.0.0:4141", remote, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return web, admin, st
}
func request(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://localhost"+path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestRemoteReads(t *testing.T) {
	web, _, st := testHandlers(t, true)
	for _, path := range []string{"/api/config", "/api/accounts", "/api/providers", "/api/routing/codex", "/api/sessions", "/api/usage", "/api/usage/recent", "/api/logs", "/api/events", "/api/analytics", "/api/limits", "/api/prices", "/api/nickname?account=test@example.com"} {
		for _, method := range []string{"GET", "HEAD"} {
			t.Run(method+path, func(t *testing.T) {
				r := httptest.NewRequest(method, "http://remote.example"+path, nil)
				r.RemoteAddr = "198.51.100.2:4567"
				w := httptest.NewRecorder()
				web.ServeHTTP(w, r)
				if w.Code != 200 {
					t.Fatalf("%d: %s", w.Code, w.Body)
				}
				if method == "GET" && !json.Valid(w.Body.Bytes()) {
					t.Fatalf("not JSON: %s", w.Body)
				}
			})
		}
	}
	// GET prices must not lazily fetch or persist anything in remote mode.
	var n int
	if err := st.DB.QueryRow("SELECT count(*) FROM state WHERE key='prices'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("remote read persisted prices: %d %v", n, err)
	}
	w := request(web, "GET", "/api/config", "")
	if !strings.Contains(w.Body.String(), `"remote_read_only":true`) {
		t.Fatal(w.Body)
	}
}

func TestRemoteMutationMatrix(t *testing.T) {
	web, _, st := testHandlers(t, true)
	paths := []string{"/api/routing/codex", "/api/nickname?account=test@example.com", "/api/config", "/api/providers", "/api/providers/openrouter", "/api/accounts", "/api/accounts/login", "/api/accounts/openrouter", "/api/limits/refresh", "/api/prices", "/api/settings", "/api/future-action", "/api/openai/v1/settings"}
	for _, path := range paths {
		for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CONNECT", "TRACE"} {
			for _, peer := range []string{"127.0.0.1:9876", "[::1]:9876", "198.51.100.2:9876"} {
				t.Run(method+path+peer, func(t *testing.T) {
					r := httptest.NewRequest(method, "http://localhost"+path, strings.NewReader(`{"enabled":true,"nickname":"changed","key":"secret-not-saved"}`))
					r.RemoteAddr = peer
					for k, v := range map[string]string{"Origin": "http://localhost", "Forwarded": "for=127.0.0.1;host=localhost;proto=http", "X-Forwarded-For": "127.0.0.1", "X-Forwarded-Host": "localhost", "X-Real-IP": "127.0.0.1", "X-HTTP-Method-Override": "GET", "X-Garcon-Local": "true"} {
						r.Header.Set(k, v)
					}
					w := httptest.NewRecorder()
					web.ServeHTTP(w, r)
					if w.Code != 403 || !strings.Contains(w.Body.String(), "Remote mode: view-only") {
						t.Fatalf("%d: %s", w.Code, w.Body)
					}
				})
			}
		}
	}
	// Actions remain forbidden even when sent with nominally safe methods.
	for _, path := range []string{"/api/limits/refresh", "/api/accounts/login", "/api/accounts/openrouter", "/api/future-action", "/api/providers/add", "/api/%6cimits/refresh"} {
		for _, method := range []string{"GET", "HEAD"} {
			if w := request(web, method, path, ""); w.Code != 403 {
				t.Fatalf("%s %s: %d", method, path, w.Code)
			}
		}
	}
	var n int
	st.DB.QueryRow("SELECT count(*) FROM state WHERE key IN ('providers','codex-routing','nickname:test@example.com')").Scan(&n)
	if n != 0 {
		t.Fatal("mutation reached storage")
	}
	if _, err := os.Stat(filepath.Join(st.Dir(), "openrouter-key")); !os.IsNotExist(err) {
		t.Fatal("mutation saved credentials")
	}
}

func TestReverseProxyCannotGrantWrites(t *testing.T) {
	web, _, _ := testHandlers(t, true)
	backend := httptest.NewServer(web)
	defer backend.Close()
	target, _ := url.Parse(backend.URL)
	reverse := httputil.NewSingleHostReverseProxy(target)
	original := reverse.Director
	reverse.Director = func(r *http.Request) {
		original(r)
		r.Host = "localhost"
		r.Header.Set("Origin", "http://localhost")
		r.Header.Set("Forwarded", "for=127.0.0.1;host=localhost")
		r.Header.Set("X-Forwarded-For", "127.0.0.1")
	}
	front := httptest.NewServer(reverse)
	defer front.Close()
	for _, tc := range []struct {
		method, path string
		status       int
	}{{"PUT", "/api/routing/codex", 403}, {"POST", "/api/limits/refresh", 403}, {"GET", "/api/limits/refresh", 403}, {"GET", "/api/accounts", 200}, {"GET", "/api/logs", 200}} {
		r, _ := http.NewRequest(tc.method, front.URL+tc.path, strings.NewReader(`{"enabled":false}`))
		res, err := front.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, res.StatusCode, b)
		}
		if tc.status == 403 && !bytes.Contains(b, []byte("Remote mode: view-only")) {
			t.Fatal(string(b))
		}
	}
}

func TestLocalCLIWorksWhileTCPRemainsViewOnly(t *testing.T) {
	web, admin, st := testHandlers(t, true)
	server, listener, err := control.Listen(control.SocketPath(st.Path()), admin)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	go server.Serve(listener)
	run := func(input string, args ...string) string {
		t.Helper()
		args = append(args, "--data", st.Path())
		var out, stderr bytes.Buffer
		if err := manage(args, strings.NewReader(input), &out, &stderr); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	run("", "providers", "add", "example", "https://example.com")
	if got := run("", "providers", "list"); !strings.Contains(got, "example.com") {
		t.Fatal(got)
	}
	if w := request(web, "GET", "/api/providers", ""); !strings.Contains(w.Body.String(), "example.com") {
		t.Fatal(w.Body)
	}
	run("", "providers", "remove", "example")
	run(`{"enabled":false,"accounts":[],"priorities":{}}`, "routing", "set", "--json-stdin")
	if got := run("", "routing", "show"); !strings.Contains(got, `"enabled":false`) {
		t.Fatal(got)
	}
	run("", "accounts", "nickname", "test@example.com", "Test")
	if w := request(web, "GET", "/api/nickname?account=test@example.com", ""); !strings.Contains(w.Body.String(), `"nickname":"Test"`) {
		t.Fatal(w.Body)
	}
	key := "sk-or-test-private-value"
	if got := run(key, "accounts", "key", "openrouter", "--key-stdin"); strings.Contains(got, key) {
		t.Fatal("key exposed")
	}
	if got := run("", "accounts", "list"); !strings.Contains(got, `"openrouter_configured":true`) || strings.Contains(got, key) {
		t.Fatal(got)
	}
	info, err := os.Stat(filepath.Join(st.Dir(), "openrouter-key"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("key permissions", err)
	}
	run("", "accounts", "remove-key", "openrouter")
	for _, path := range []string{"/api/providers", "/api/accounts/openrouter", "/api/routing/codex"} {
		if w := request(web, "PUT", path, `{}`); w.Code != 403 {
			t.Fatal(fmt.Sprint(w.Code, w.Body))
		}
	}
}
func TestLocalDashboardStillEditable(t *testing.T) {
	web, _, _ := testHandlers(t, false)
	if w := request(web, "PUT", "/api/nickname?account=test@example.com", `{"nickname":"Test"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if w := request(web, "GET", "/api/config", ""); !strings.Contains(w.Body.String(), `"remote_read_only":false`) {
		t.Fatal(w.Body)
	}
}

func TestLoginUsesSeparateProfiles(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	bin := filepath.Join(dir, "bin")
	os.MkdirAll(bin, 0700)
	for _, provider := range []string{"codex", "claude"} {
		script := "#!/bin/sh\nprintf '%s\\n' \"$CODEX_HOME\" \"$CLAUDE_CONFIG_DIR\" \"$@\"\n"
		if err := os.WriteFile(filepath.Join(bin, provider), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("CODEX_HOME", "existing-codex")
	t.Setenv("CLAUDE_CONFIG_DIR", "existing-claude")
	for _, provider := range []string{"codex", "claude"} {
		var out bytes.Buffer
		if err := accountLogin("login", []string{provider, "--profile", "work"}, home, strings.NewReader(""), &out, &out); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), filepath.Join(home, "."+provider+"-garcon-work")) {
			t.Fatal(out.String())
		}
		if !strings.Contains(out.String(), "login") {
			t.Fatal(out.String())
		}
		if err := accountLogin("login", []string{provider, "--profile", "../escape"}, home, strings.NewReader(""), &out, &out); err == nil {
			t.Fatal("accepted path traversal")
		}
	}
}

func TestAccountReadsPreserveIndependentProviderWindows(t *testing.T) {
	_, _, st := testHandlers(t, true)
	zero, weekly, fable, codex := 0.0, 14.0, 5.0, 2.0
	snapshot := limits.Snapshot{Accounts: []limits.Account{
		{ID: "claude:one", Provider: "claude", Email: "same@example.com", Status: "fresh", FetchedAt: time.Now().UnixMilli(), Windows: []limits.Window{
			{ID: "session", Label: "5-hour", UsedPercent: &zero, WindowSeconds: 18000},
			{ID: "week", Label: "Weekly", UsedPercent: &weekly, WindowSeconds: 604800, ResetsAt: time.Now().Add(48 * time.Hour).UnixMilli()},
			{ID: "fable", Label: "Fable · Weekly", UsedPercent: &fable, WindowSeconds: 604800, ResetsAt: time.Now().Add(48 * time.Hour).UnixMilli()},
		}},
		{ID: "codex:one", Provider: "codex", Email: "same@example.com", Workspace: "codex-workspace", Status: "fresh", FetchedAt: time.Now().UnixMilli(), Windows: []limits.Window{
			{ID: "codex:0", Label: "Weekly", UsedPercent: &codex, WindowSeconds: 604800},
		}},
	}}
	if err := st.DB.Put("limits", snapshot); err != nil {
		t.Fatal(err)
	}
	li := limits.New(st.Dir(), st.DB)
	routing := codexrouting.New(st.Dir(), li.Snapshot, st.DB)
	web, _, err := newHandlers(st, li, routing, "0.0.0.0:4141", true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/accounts", "/api/limits"} {
		w := request(web, "GET", path, "")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body)
		}
		var got limits.Snapshot
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Accounts) != 2 {
			t.Fatal("accounts combined", got)
		}
		for _, a := range got.Accounts {
			if a.Provider == "claude" {
				if len(a.Windows) != 3 || *a.Windows[0].UsedPercent != 0 || a.Windows[0].ResetsAt != 0 || *a.Windows[1].UsedPercent != 14 || *a.Windows[2].UsedPercent != 5 {
					t.Fatalf("lost Claude limits: %+v", a.Windows)
				}
			} else if len(a.Windows) != 1 || *a.Windows[0].UsedPercent != 2 {
				t.Fatalf("invented or altered Codex limits: %+v", a.Windows)
			}
		}
	}
}
