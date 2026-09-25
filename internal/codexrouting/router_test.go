package codexrouting

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"garcon/internal/limits"
)

func saveLogin(t *testing.T, dir, id string, expires time.Time) string {
	t.Helper()
	claims, _ := json.Marshal(map[string]any{"email": id + "@example.com", "exp": expires.Unix(), "https://api.openai.com/auth": map[string]string{"chatgpt_account_id": id, "chatgpt_plan_type": "pro"}})
	token := "header." + base64.RawURLEncoding.EncodeToString(claims) + ".signature"
	if err := writeJSON(filepath.Join(dir, "auth.json"), map[string]any{"auth_mode": "chatgpt", "tokens": map[string]string{"access_token": token, "refresh_token": "private-refresh-" + id, "account_id": id}}); err != nil {
		t.Fatal(err)
	}
	return token
}
func setup(t *testing.T) (*Router, *limits.Snapshot, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	now := time.Now()
	snapshot := &limits.Snapshot{}
	for i, id := range []string{"a", "b", "c"} {
		saveLogin(t, filepath.Join(home, ".codex-"+id), id, now.Add(time.Hour))
		used := []float64{60, 20, 90}[i]
		snapshot.Accounts = append(snapshot.Accounts, limits.Account{Provider: "codex", Workspace: id, Status: "fresh", FetchedAt: now.UnixMilli(), Windows: []limits.Window{{ID: "codex:0", UsedPercent: &used, ResetsAt: now.Add(time.Hour).UnixMilli()}}})
	}
	r := New(t.TempDir(), func() limits.Snapshot { return *snapshot })
	r.now = func() time.Time { return now }
	if err := r.Configure(true); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "c"} {
		r.health[id] = health{models: map[string]bool{"model": true}, checked: now}
	}
	return r, snapshot, home
}
func request(session string) *http.Request {
	r := httptest.NewRequest("POST", "http://127.0.0.1:4141/codex/backend-api/codex/responses", strings.NewReader(`{"model":"model","input":"hello"}`))
	r.RemoteAddr = "127.0.0.1:12345"
	r.Header.Set("Session_id", session)
	r.Header.Set("Authorization", "Bearer original")
	r.Header.Set("ChatGPT-Account-Id", "a")
	return r
}

func TestThreeAccountsScorePinsAndActualIdentity(t *testing.T) {
	r, snapshot, _ := setup(t)
	first := request("conversation-1")
	first.Header.Set("OpenAI-Organization", "original-org")
	first.Header.Set("Cookie", "private")
	done, err := r.Prepare(first)
	if err != nil {
		t.Fatal(err)
	}
	if first.Header.Get("ChatGPT-Account-Id") != "b" || first.Header.Get("Authorization") == "Bearer original" || first.Header.Get("Cookie") != "" || first.Header.Get("OpenAI-Organization") != "" {
		t.Fatal("wrong outbound identity")
	}
	done()
	done() // idempotent completion
	if r.active["b"] != 0 {
		t.Fatal("active request leaked")
	}
	used := 99.0
	snapshot.Accounts[1].Windows[0].UsedPercent = &used
	same := request("conversation-1")
	release, err := r.Prepare(same)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if same.Header.Get("ChatGPT-Account-Id") != "b" {
		t.Fatal("conversation moved accounts")
	}
	newReq := request("conversation-2")
	release, err = r.Prepare(newReq)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if newReq.Header.Get("ChatGPT-Account-Id") != "a" {
		t.Fatal("new conversation ignored headroom")
	}
	usedA := 99.0
	snapshot.Accounts[0].Windows[0].UsedPercent = &usedA
	third := request("conversation-3")
	release, err = r.Prepare(third)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if third.Header.Get("ChatGPT-Account-Id") != "c" {
		t.Fatal("third account cannot be selected")
	}
	// No raw conversation identifiers, OAuth credentials, or prompts on disk.
	b, err := os.ReadFile(filepath.Join(r.dir, "codex-routing-pins.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"conversation-1", "header.", "private-refresh", "hello"} {
		if strings.Contains(string(b), secret) {
			t.Fatal("sensitive material persisted")
		}
	}
	restarted := New(r.dir, r.snapshot)
	restarted.health = r.health
	restarted.now = r.now
	resumed := request("conversation-1")
	release, err = restarted.Prepare(resumed)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if resumed.Header.Get("ChatGPT-Account-Id") != "b" {
		t.Fatal("lost account across restart")
	}
}

func TestEligibilityAndNoSilentFailover(t *testing.T) {
	for _, condition := range []string{"exhausted", "stale", "unknown", "reset", "missing-model", "expired-login", "cooldown"} {
		t.Run(condition, func(t *testing.T) {
			r, s, home := setup(t)
			first := request("pinned")
			done, err := r.Prepare(first)
			if err != nil {
				t.Fatal(err)
			}
			done()
			switch condition {
			case "exhausted":
				used := 100.0
				s.Accounts[1].Windows[0].UsedPercent = &used
			case "stale":
				s.Accounts[1].Status = "stale"
			case "unknown":
				s.Accounts[1].Windows[0].UsedPercent = nil
			case "reset":
				s.Accounts[1].Windows[0].ResetsAt = r.now().Add(-time.Second).UnixMilli()
			case "missing-model":
				r.health["b"] = health{models: map[string]bool{"other": true}, checked: r.now()}
			case "expired-login":
				saveLogin(t, filepath.Join(home, ".codex-b"), "b", r.now().Add(-time.Second))
			case "cooldown":
				r.Observe("b", &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"120"}}})
			}
			if _, err := r.Prepare(request("pinned")); err == nil {
				t.Fatal("unavailable pinned account silently rerouted")
			}
			fresh := request("new")
			done, err = r.Prepare(fresh)
			if err != nil {
				t.Fatal(err)
			}
			done()
			if fresh.Header.Get("ChatGPT-Account-Id") == "b" {
				t.Fatal("selected unavailable account")
			}
		})
	}
}

func TestConcurrentReservations(t *testing.T) {
	r, _, _ := setup(t)
	first := request("first")
	done, err := r.Prepare(first)
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	second := request("second")
	done2, err := r.Prepare(second)
	if err != nil {
		t.Fatal(err)
	}
	defer done2()
	if first.Header.Get("ChatGPT-Account-Id") == second.Header.Get("ChatGPT-Account-Id") {
		t.Fatal("ignored in-flight reservation")
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			req := request("shared")
			release, err := r.Prepare(req)
			if err != nil {
				t.Error(err)
				return
			}
			release()
		})
	}
	wg.Wait()
}

func TestDiscoveryDeduplicatesAndDoesNotEnrollNewAccounts(t *testing.T) {
	r, _, home := setup(t)
	saveLogin(t, filepath.Join(home, ".codex"), "b", r.now().Add(2*time.Hour))
	creds := r.discover()
	if len(creds) != 3 {
		t.Fatalf("got %d accounts", len(creds))
	}
	for _, c := range creds {
		if c.id == "b" && c.dir != filepath.Join(home, ".codex") {
			t.Fatal("did not prefer fresh login")
		}
	}
	saveLogin(t, filepath.Join(home, ".codex-new"), "new", r.now().Add(time.Hour))
	for _, a := range r.Status().Accounts {
		if a.ID == "new" && a.Enrolled {
			t.Fatal("new workspace enrolled without consent")
		}
	}
	encoded, _ := json.Marshal(r.Status())
	for _, secret := range []string{"header.", "refresh_token", home} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("status leaks credential data")
		}
	}
}

func TestLocalOnlyAndContinuationRules(t *testing.T) {
	for _, kind := range []string{"remote", "origin", "no-session", "websocket", "state-without-owner"} {
		t.Run(kind, func(t *testing.T) {
			r, _, _ := setup(t)
			req := request("session")
			switch kind {
			case "remote":
				req.RemoteAddr = "192.0.2.1:1234"
			case "origin":
				req.Header.Set("Origin", "https://evil.example")
			case "no-session":
				req.Header.Del("Session_id")
			case "websocket":
				req.Header.Set("Upgrade", "websocket")
			case "state-without-owner":
				req.Header.Set("X-Codex-Turn-State", "old")
				req.Header.Del("ChatGPT-Account-Id")
			}
			if _, err := r.Prepare(req); err == nil {
				t.Fatal("accepted unsafe request")
			}
		})
	}
	r, _, _ := setup(t)
	req := request("existing")
	req.Header.Set("X-Codex-Turn-State", "old")
	done, err := r.Prepare(req)
	if err != nil {
		t.Fatal(err)
	}
	done()
	if req.Header.Get("ChatGPT-Account-Id") != "a" {
		t.Fatal("existing state switched account")
	}
}

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
func TestCatalogAndRenewal(t *testing.T) {
	r, _, home := setup(t)
	saveLogin(t, filepath.Join(home, ".codex-b"), "b", r.now().Add(-time.Minute))
	renewed := 0
	r.renew = func(_ context.Context, c credential) error {
		renewed++
		saveLogin(t, c.dir, c.id, r.now().Add(time.Hour))
		return nil
	}
	requests := 0
	r.client.Transport = transport(func(req *http.Request) (*http.Response, error) {
		requests++
		if req.URL.Host != "chatgpt.com" || req.URL.Path != "/backend-api/codex/models" || req.Header.Get("Authorization") == "" || req.Header.Get("ChatGPT-Account-Id") == "" {
			t.Fatal("wrong catalog request")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"models":[{"slug":"model"}]}`)), Header: http.Header{}}, nil
	})
	r.Refresh(context.Background())
	if renewed != 1 || requests != 3 {
		t.Fatalf("renewals %d requests %d", renewed, requests)
	}
	for _, a := range r.Status().Accounts {
		if a.Status != "Ready" {
			t.Fatalf("%s: %s", a.ID, a.Status)
		}
	}
	r.Observe("a", &http.Response{StatusCode: 401, Header: http.Header{}})
	r.Refresh(context.Background())
	if renewed != 2 {
		t.Fatal("401 did not request credential renewal")
	}
}

func TestCorruptPinsFailClosed(t *testing.T) {
	r, _, _ := setup(t)
	if err := os.WriteFile(filepath.Join(r.dir, "codex-routing-pins.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	restarted := New(r.dir, r.snapshot)
	restarted.health = r.health
	if _, err := restarted.Prepare(request("session")); err == nil {
		t.Fatal("silently replaced invalid assignments")
	}
}

func TestExplicitEnrollment(t *testing.T) {
	r, _, _ := setup(t)
	for _, ids := range [][]string{{}, {"missing"}, {"a", "a"}} {
		if err := r.ConfigureAccounts(true, ids); err == nil {
			t.Fatal("accepted invalid selection")
		}
	}
	if err := r.ConfigureAccounts(true, []string{"c"}); err != nil {
		t.Fatal(err)
	}
	req := request("only-c")
	release, err := r.Prepare(req)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if req.Header.Get("ChatGPT-Account-Id") != "c" {
		t.Fatal("routed outside selected pool")
	}
}
