package clauderouting

import (
	"context"
	"crypto/sha256"
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

	"garcon/internal/claude"
	"garcon/internal/database"
	"garcon/internal/limits"
)

func setup(t *testing.T) (*Router, *limits.Snapshot) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	now := time.Now()
	s := &limits.Snapshot{}
	r, err := New(db, func() limits.Snapshot { return *s }, "")
	if err != nil {
		t.Fatal(err)
	}
	r.now = func() time.Time { return now }
	for i, id := range []string{"a", "b", "c"} {
		used := []float64{60, 20, 90}[i]
		s.Accounts = append(s.Accounts, limits.Account{ID: id, Provider: "claude", Status: "fresh", FetchedAt: now.UnixMilli(), Windows: []limits.Window{{ID: "seven_day", UsedPercent: &used, ResetsAt: now.Add(time.Hour).UnixMilli()}}})
		r.candidates = append(r.candidates, candidate{ID: id, Email: id + "@example.com", Profile: "/profile-" + id, TokenHash: sha256.Sum256([]byte("token-" + id)), Expires: now.Add(time.Hour).UnixMilli(), Models: map[string]bool{"claude-opus": true, "claude-sonnet": true}, Checked: now})
		r.config.Accounts = append(r.config.Accounts, id)
	}
	if err := db.Put("claude-routing", r.config); err != nil {
		t.Fatal(err)
	}
	r.load = func(_ context.Context, dir string) (claude.Credentials, error) {
		return claude.Credentials{AccessToken: "token-" + strings.TrimPrefix(dir, "/profile-"), ExpiresAt: now.Add(time.Hour).UnixMilli()}, nil
	}
	return r, s
}
func request(session string) *http.Request {
	body := `{"model":"claude-opus","messages":[{"role":"user","content":"hello"}],"metadata":{"user_id":"{\"session_id\":\"` + session + `\"}"}}`
	req := httptest.NewRequest("POST", "http://localhost/claude/v1/messages", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set("X-Api-Key", "test")
	return req
}
func choose(t *testing.T, r *Router, session string) string {
	t.Helper()
	req := request(session)
	done, err := r.Prepare(req)
	if err != nil {
		t.Fatal(err)
	}
	done()
	done()
	return claude.Details(req).AccountID
}
func TestScorePinsAndRestart(t *testing.T) {
	r, s := setup(t)
	if got := choose(t, r, "one"); got != "b" {
		t.Fatal(got)
	}
	used := 99.0
	s.Accounts[1].Windows[0].UsedPercent = &used
	if got := choose(t, r, "one"); got != "b" {
		t.Fatal("session moved", got)
	}
	if got := choose(t, r, "two"); got != "a" {
		t.Fatal("new session not rescored", got)
	}
	restarted, err := New(r.db, r.snapshot, "")
	if err != nil {
		t.Fatal(err)
	}
	restarted.candidates = r.candidates
	restarted.now = r.now
	restarted.load = r.load
	if got := choose(t, restarted, "one"); got != "b" {
		t.Fatal("pin lost after restart", got)
	}
	if r.active["b"] != 0 {
		t.Fatal("release leaked")
	}
}
func TestPinnedExhaustedNeverMoves(t *testing.T) {
	r, s := setup(t)
	choose(t, r, "one")
	used := 100.0
	s.Accounts[1].Windows[0].UsedPercent = &used
	if _, err := r.Prepare(request("one")); err == nil || !strings.Contains(err.Error(), "session's Claude account") {
		t.Fatal(err)
	}
	if got := choose(t, r, "new"); got != "a" {
		t.Fatal(got)
	}
}
func TestQuotaPerHourNotPercent(t *testing.T) {
	r, s := setup(t)
	s.Accounts[0].Windows[0].ResetsAt = r.now().Add(10 * time.Minute).UnixMilli()
	if got := choose(t, r, "soon-reset"); got != "a" {
		t.Fatal("did not use Codex quota per hour", got)
	}
}
func TestRelevantModelWindowsAndDormantSession(t *testing.T) {
	r, s := setup(t)
	used := 100.0
	zero := 0.0
	ninety := 90.0
	s.Accounts[0].Windows[0].UsedPercent = &ninety
	s.Accounts[1].Windows = append(s.Accounts[1].Windows, limits.Window{ID: "seven_day_opus", Label: "Opus · Weekly", UsedPercent: &used, ResetsAt: r.now().Add(time.Hour).UnixMilli()}, limits.Window{ID: "session:5-hour", WindowSeconds: 18000, UsedPercent: &zero})
	if got := choose(t, r, "opus"); got != "a" {
		t.Fatal("ignored Opus exhaustion", got)
	}
	req := request("sonnet")
	b, _ := io.ReadAll(req.Body)
	req.Body = io.NopCloser(strings.NewReader(strings.ReplaceAll(string(b), "claude-opus", "claude-sonnet")))
	done, err := r.Prepare(req)
	if err != nil {
		t.Fatal(err)
	}
	done()
	if claude.Details(req).AccountID != "b" {
		t.Fatal("unrelated Opus limit excluded Sonnet")
	}
	if s.Accounts[1].Windows[2].ResetsAt != 0 {
		t.Fatal("snapshot mutated")
	}
}
func TestStaleMissingResetAndUnavailableModels(t *testing.T) {
	for _, mode := range []string{"stale", "missing-used", "missing-reset", "model", "cooldown", "missing-login", "identity-changed", "empty-pool"} {
		t.Run(mode, func(t *testing.T) {
			r, s := setup(t)
			switch mode {
			case "stale":
				s.Accounts[1].FetchedAt = r.now().Add(-11 * time.Minute).UnixMilli()
			case "missing-used":
				s.Accounts[1].Windows[0].UsedPercent = nil
			case "missing-reset":
				s.Accounts[1].Windows[0].ResetsAt = 0
			case "model":
				r.candidates[1].Models = map[string]bool{"other": true}
			case "cooldown":
				r.observe("b", &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"300"}}})
			case "missing-login":
				r.candidates[1].Problem = "Login needs refresh"
			case "identity-changed":
				r.load = func(context.Context, string) (claude.Credentials, error) {
					return claude.Credentials{AccessToken: "other-token", ExpiresAt: r.now().Add(time.Hour).UnixMilli()}, nil
				}
			case "empty-pool":
				r.config.Accounts = []string{}
			}
			if mode == "identity-changed" || mode == "empty-pool" {
				if _, err := r.Prepare(request("one")); err == nil {
					t.Fatal("expected failure")
				}
				return
			}
			if got := choose(t, r, "one"); got != "a" {
				t.Fatal(got)
			}
		})
	}
}
func TestExistingSessionPreservesOriginalAccount(t *testing.T) {
	r, _ := setup(t)
	raw := `{"harness":"claude","account":"a@example.com"}`
	_, err := r.db.Exec("INSERT INTO requests(time,session_id,status,record) VALUES(1,'old',200,?)", raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := choose(t, r, "old"); got != "a" {
		t.Fatal("moved legacy session", got)
	}
	// A same-named Codex conversation is unrelated.
	_, err = r.db.Exec("INSERT INTO sessions(key,session_id,account_id,created_at,last_seen) VALUES('codex','new','a',1,1)")
	if err != nil {
		t.Fatal(err)
	}
	if got := choose(t, r, "new"); got != "b" {
		t.Fatal("cross-harness collision", got)
	}
}
func TestRejectUnidentifiedAndUnknownContinuations(t *testing.T) {
	r, _ := setup(t)
	for _, body := range []string{`{`, `{"model":"claude-opus"}`, `{"model":"claude-opus","messages":[{"role":"assistant"}],"metadata":{"session_id":"unknown"}}`} {
		req := httptest.NewRequest("POST", "http://localhost/claude/v1/messages", strings.NewReader(body))
		if _, err := r.Prepare(req); err == nil {
			t.Fatal("accepted unpinnable request")
		}
	}
}
func TestConcurrentRequestsPinOnceAndDontPersistSecrets(t *testing.T) {
	r, _ := setup(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := request("shared")
			done, err := r.Prepare(req)
			if err != nil {
				t.Error(err)
				return
			}
			if claude.Details(req).AccountID != "b" {
				t.Error("inconsistent account")
			}
			done()
		}()
	}
	wg.Wait()
	var count int
	r.db.QueryRow("SELECT count(*) FROM claude_routing_sessions").Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
	var id, email, model string
	r.db.QueryRow("SELECT account_id,account,model FROM claude_routing_sessions").Scan(&id, &email, &model)
	if strings.Contains(id+email+model, "token-") {
		t.Fatal("persisted token")
	}
}
func TestModelDiscoveryDoesNotPin(t *testing.T) {
	r, _ := setup(t)
	req := httptest.NewRequest("GET", "http://localhost/claude/v1/models", nil)
	done, err := r.Prepare(req)
	if err != nil {
		t.Fatal(err)
	}
	done()
	var n int
	r.db.QueryRow("SELECT count(*) FROM claude_routing_sessions").Scan(&n)
	if n != 0 {
		t.Fatal("model discovery created a session")
	}
}
func TestGatewayIntegrationPreservesStreamingAndLogsIdentity(t *testing.T) {
	r, _ := setup(t)
	// Exercise the actual gateway branch, not merely the router's header mutation.
	p := filepath.Join(t.TempDir(), "gateway.json")
	data, _ := json.Marshal(map[string]string{"key": "test", "profile": "/unused"})
	if err := os.WriteFile(p, data, 0600); err != nil {
		t.Fatal(err)
	}
	g, err := claude.LoadGateway(p)
	if err != nil {
		t.Fatal(err)
	}
	g.Prepare = r.Prepare
	req := request("gateway")
	req.URL.Path = "/claude-gateway/v1/messages"
	req.RemoteAddr = "127.0.0.1:5"
	req.Header.Del("X-Api-Key")
	w := httptest.NewRecorder()
	g.Handler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer token-b" || req.Header.Get("X-Api-Key") != "" || claude.Details(req).AccountID != "b" {
			t.Fatal("wrong authenticated request")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: message_stop\n\n")
	})).ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "message_stop") {
		t.Fatal(w.Code, w.Body.String())
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRefreshVerifiesIdentityDeduplicatesAndPreservesPool(t *testing.T) {
	r, _ := setup(t)
	r.home = t.TempDir()
	r.extra = ""
	r.candidates = nil
	r.config = pool{}
	for _, name := range []string{".claude-a", ".claude-b"} {
		if err := os.Mkdir(filepath.Join(r.home, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	r.renew = func(context.Context, string) (bool, error) { return false, nil }
	r.load = func(context.Context, string) (claude.Credentials, error) {
		return claude.Credentials{AccessToken: "same-account", ExpiresAt: r.now().Add(time.Hour).UnixMilli(), SubscriptionType: "max"}, nil
	}
	r.client = &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") != "Bearer same-account" {
			t.Fatal("wrong probe token")
		}
		body := `{"account":{"uuid":"person","email":"person@example.com"},"organization":{"uuid":"org"}}`
		if req.URL.Path == "/v1/models" {
			body = `{"data":[{"id":"claude-opus"}]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}
	r.Refresh(context.Background())
	if len(r.candidates) != 1 || len(r.config.Accounts) != 1 || r.config.Accounts[0] != limits.AccountID("claude", "person", "org") {
		t.Fatal("identity deduplication failed", r.config)
	}
	// Explicitly empty persisted enrollment must never silently repopulate.
	r.config.Accounts = []string{}
	r.Refresh(context.Background())
	if len(r.config.Accounts) != 0 {
		t.Fatal("repopulated empty pool")
	}
}
func TestTokenCountingWithoutSessionDoesNotPin(t *testing.T) {
	r, _ := setup(t)
	req := httptest.NewRequest("POST", "http://localhost/claude/v1/messages/count_tokens", strings.NewReader(`{"model":"claude-opus","messages":[{"role":"assistant","content":"old content"}]}`))
	done, err := r.Prepare(req)
	if err != nil {
		t.Fatal(err)
	}
	done()
	var count int
	r.db.QueryRow("SELECT count(*) FROM claude_routing_sessions").Scan(&count)
	if count != 0 {
		t.Fatal("token counter created a pin")
	}
}
