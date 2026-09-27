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

	"garcon/internal/database"
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

func TestConcurrentRequestsKeepQuotaScore(t *testing.T) {
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
	if first.Header.Get("ChatGPT-Account-Id") != second.Header.Get("ChatGPT-Account-Id") {
		t.Fatal("active requests incorrectly changed the quota-per-hour score")
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
	for _, ids := range [][]string{{"missing"}, {"a", "a"}} {
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

func TestAutomaticPoolAndEmptySelectionSurviveRestart(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	saveLogin(t, filepath.Join(home, ".codex"), "a", time.Now().Add(time.Hour))
	dir := t.TempDir()
	snapshot := func() limits.Snapshot { return limits.Snapshot{} }
	r := New(dir, snapshot)
	if !r.Enabled() || !r.enrolled("a") {
		t.Fatal("initial account pool is not automatic")
	}
	// New logins do not silently join an already selected pool.
	saveLogin(t, filepath.Join(home, ".codex-b"), "b", time.Now().Add(time.Hour))
	r = New(dir, snapshot)
	if !r.enrolled("a") || r.enrolled("b") {
		t.Fatal("restart changed the selected pool")
	}
	// Account-only API updates work without a separate enable switch.
	req := httptest.NewRequest("PUT", "http://127.0.0.1:4141/api/routing/codex", strings.NewReader(`{"accounts":[]}`))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	r = New(dir, snapshot)
	if !r.Enabled() || len(r.config.Accounts) != 0 {
		t.Fatal("empty pool did not survive restart")
	}
	if _, err := r.Prepare(request("no-accounts")); err == nil {
		t.Fatal("empty pool allowed a request")
	}
}

func TestSmartRoutingIgnoresLegacyPriorities(t *testing.T) {
	r, s, _ := setup(t)
	// B has most remaining quota, but A resets sooner and has more percent/hour.
	s.Accounts[0].Windows[0].ResetsAt = r.now().Add(10 * time.Minute).UnixMilli()
	first := request("score")
	release, err := r.Prepare(first)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if first.Header.Get("ChatGPT-Account-Id") != "a" {
		t.Fatal("ignored remaining hours")
	}
	if err = r.ConfigurePriorities(true, nil, map[string]int{"a": 2, "b": 1, "c": 2}); err != nil {
		t.Fatal(err)
	}
	second := request("priority")
	release, err = r.Prepare(second)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if second.Header.Get("ChatGPT-Account-Id") != "a" {
		t.Fatal("legacy priority overrode smart routing score")
	}
	if got := r.Status().Accounts[0].ID; got != "a" {
		t.Fatalf("display order disagrees with smart routing: %s", got)
	}
	exhausted := 100.0
	s.Accounts[0].Windows[0].UsedPercent = &exhausted
	third := request("fallback")
	release, err = r.Prepare(third)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if third.Header.Get("ChatGPT-Account-Id") != "b" {
		t.Fatal("did not fall back to the next eligible score")
	}
	if err = r.ConfigurePriorities(false, []string{}, map[string]int{}); err != nil {
		t.Fatal(err)
	}
	if len(r.config.Accounts) != 0 || !r.config.Enabled {
		t.Fatal("could not remove last account")
	}
}

func TestScoreUsesMostConstrainedWindow(t *testing.T) {
	r, s, _ := setup(t)
	a := s.Accounts[0]
	used := 90.0
	a.Windows = append(a.Windows, limits.Window{ID: "codex:1", UsedPercent: &used, ResetsAt: r.now().Add(100 * time.Hour).UnixMilli()})
	score, hours := quotaScore(a, r.now())
	if score != 0.1 || hours != 100 {
		t.Fatalf("wrong bottleneck score %v %v", score, hours)
	}
	a.Windows[1].ResetsAt = 0
	if score, _ = quotaScore(a, r.now()); score != -1 {
		t.Fatal("scored unknown reset time")
	}
}

func TestSQLiteAssignmentAndConfigurationSurviveRestart(t *testing.T) {
	r, _, _ := setup(t)
	db, err := database.Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r.db = db
	if err = r.ConfigurePriorities(true, nil, map[string]int{"a": 2, "b": 1, "c": 2}); err != nil {
		t.Fatal(err)
	}
	req := request("real-session-id")
	release, err := r.Prepare(req)
	if err != nil {
		t.Fatal(err)
	}
	release()
	var id, account string
	if err = db.QueryRow("SELECT session_id,account_id FROM sessions").Scan(&id, &account); err != nil {
		t.Fatal(err)
	}
	if id != "real-session-id" || account != "b" {
		t.Fatal("session tracker lost exact assignment")
	}
	restarted := New(r.dir, r.snapshot, db)
	restarted.discover = r.discover
	restarted.now = r.now
	restarted.health = r.health
	if len(restarted.pins) != 1 || restarted.config.Priorities["a"] != 2 {
		t.Fatal("SQLite routing state did not survive restart")
	}
	resumed := request("real-session-id")
	release, err = restarted.Prepare(resumed)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if resumed.Header.Get("ChatGPT-Account-Id") != "b" {
		t.Fatal("resumed session moved accounts")
	}
}
