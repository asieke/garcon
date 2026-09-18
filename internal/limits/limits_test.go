package limits

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func reply(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}

const codexFixture = `{"plan_type":"pro","rate_limit":{"primary_window":{"used_percent":32,"limit_window_seconds":604800,"reset_at":1900000000},"secondary_window":null}}`
const claudeFixture = `{"five_hour":{"utilization":99,"resets_at":"2030-01-01T05:00:00Z"},"seven_day":{"utilization":99},"seven_day_fable":{"utilization":99},"limits":[{"kind":"session","group":"session","percent":0,"resets_at":"2030-01-01T05:00:00Z","scope":null},{"kind":"weekly_all","group":"weekly","percent":6,"resets_at":"2030-01-07T00:00:00.150396+00:00","scope":null},{"kind":"weekly_scoped","group":"weekly","percent":12,"resets_at":"2030-01-07T00:00:00Z","scope":{"model":{"display_name":"Fable"}}}]}`

func jwt(person, workspace, email string) string {
	b, _ := json.Marshal(map[string]any{"exp": 2000000000, "email": email, "https://api.openai.com/auth": map[string]string{"chatgpt_user_id": person, "chatgpt_account_id": workspace}})
	return "header." + base64.RawURLEncoding.EncodeToString(b) + ".signature"
}
func testService(t *testing.T, sources []source, rt transport) *Service {
	t.Helper()
	s := New(t.TempDir())
	s.now = func() time.Time { return time.Unix(1800000000, 0) }
	s.discover = func(context.Context) []source { return sources }
	s.client.Transport = rt
	return s
}
func next(s *Service) {
	s.mu.Lock()
	s.lastStart = time.Time{}
	s.mu.Unlock()
	s.Refresh(context.Background())
}

func TestProviderWindows(t *testing.T) {
	plan, ws, err := parseCodex([]byte(codexFixture))
	if err != nil || plan != "pro" || len(ws) != 1 || ws[0].Label != "Weekly" || *ws[0].UsedPercent != 32 || ws[0].ResetsAt != 1900000000000 {
		t.Fatalf("codex: %s %+v %v", plan, ws, err)
	}
	_, ws, err = parseCodex([]byte(`{"rate_limit":{"primary_window":{"used_percent":0,"limit_window_seconds":18000},"secondary_window":{"used_percent":null,"limit_window_seconds":604800}},"additional_rate_limits":[{"limit_name":"Special model","rate_limit":{"primary_window":{"used_percent":45,"limit_window_seconds":3600}}}]}`))
	if err != nil || len(ws) != 3 || ws[0].UsedPercent != nil || ws[1].UsedPercent == nil || *ws[1].UsedPercent != 0 {
		t.Fatalf("missing vs zero: %+v %v", ws, err)
	}
	ws, err = parseClaude([]byte(claudeFixture))
	if err != nil || len(ws) != 3 || ws[0].Label != "Weekly" || ws[1].Label != "5-hour" || ws[2].Label != "Fable · Weekly" || *ws[0].UsedPercent != 6 || *ws[1].UsedPercent != 0 || *ws[2].UsedPercent != 12 {
		t.Fatalf("claude: %+v %v", ws, err)
	}
	ws, err = parseClaude([]byte(`{"five_hour":{"utilization":0},"seven_day":null,"seven_day_sonnet":{"utilization":25,"resets_at":"2030-01-07T00:00:00Z"}}`))
	if err != nil || len(ws) != 2 || ws[0].UsedPercent == nil || *ws[0].UsedPercent != 0 || ws[0].ResetsAt != 0 {
		t.Fatalf("legacy: %+v %v", ws, err)
	}
	for _, body := range []string{`{`, `{"limits":"bad"}`, `{"limits":[{"percent":"oops"}]}`} {
		if _, err := parseClaude([]byte(body)); err == nil {
			t.Fatal("accepted malformed limits")
		}
	}
}

func TestDeduplicationAndCredentialIsolation(t *testing.T) {
	a := codexSource(jwt("user-a", "space-a", "same@example.com"), "")
	rotated := a
	rotated.token += "-rotated"
	b := codexSource(jwt("user-a", "space-b", "same@example.com"), "")
	c := codexSource(jwt("user-b", "space-a", "same@example.com"), "")
	claudeSource := source{token: "claude-token", account: Account{Provider: "claude"}}
	var calls atomic.Int32
	s := testService(t, []source{a, a, rotated, b, c, claudeSource}, transport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.String() {
		case profileURL:
			return reply(200, `{"account":{"uuid":"user-a","email":"same@example.com"},"organization":{"uuid":"space-a","name":"Work"}}`), nil
		case claudeURL:
			calls.Add(1)
			return reply(200, claudeFixture), nil
		case codexURL:
			calls.Add(1)
			if r.Header.Get("ChatGPT-Account-Id") == "" {
				t.Error("missing selected workspace")
			}
			return reply(200, codexFixture), nil
		case creditsURL:
			return reply(200, `{"available_count":0,"credits":[]}`), nil
		default:
			t.Fatalf("unexpected destination: %s", r.URL)
			return nil, nil
		}
	}))
	s.Refresh(context.Background())
	snap := s.Snapshot()
	if len(snap.Accounts) != 4 || calls.Load() != 4 {
		t.Fatalf("accounts=%d polls=%d", len(snap.Accounts), calls.Load())
	}
	for _, account := range snap.Accounts {
		if account.Provider == "claude" && account.ResetCredits != nil {
			t.Fatal("reset credits must be Codex-only")
		}
	}
	bts, _ := os.ReadFile(s.path)
	out, _ := json.Marshal(snap)
	for _, secret := range []string{a.token, claudeSource.token, "access_token", "refresh_token"} {
		if strings.Contains(string(bts), secret) || strings.Contains(string(out), secret) {
			t.Fatal("credentials escaped the collector")
		}
	}
}

func TestSnapshotSurvivesFailuresAndLoginChanges(t *testing.T) {
	a := codexSource(jwt("a", "org", "a@example.com"), "")
	mode := 200
	used := 0
	s := testService(t, []source{a}, transport(func(r *http.Request) (*http.Response, error) {
		used++
		if mode == 0 {
			return nil, errors.New("network error with secret token")
		}
		return reply(mode, codexFixture), nil
	}))
	s.Refresh(context.Background())
	first := s.Snapshot().Accounts[0]
	mode = 0
	next(s)
	if a := s.Snapshot().Accounts[0]; a.Status != "stale" || a.FetchedAt != first.FetchedAt || len(a.Windows) != 1 || strings.Contains(a.Error, "secret") {
		t.Fatalf("failure: %+v", a)
	}
	mode = 401
	next(s)
	if s.Snapshot().Accounts[0].Status != "needs_login" {
		t.Fatal("unauthorized login not marked")
	}
	a.token = "new-token"
	s.discover = func(context.Context) []source { return []source{a} }
	mode = 200
	next(s)
	if s.Snapshot().Accounts[0].Status != "fresh" {
		t.Fatal("rotated credential did not recover")
	}
	a.expires = s.now().Add(-time.Minute).UnixMilli()
	count := used
	next(s)
	if used != count || s.Snapshot().Accounts[0].Status != "needs_login" {
		t.Fatal("expired credential was queried")
	}
	s.discover = func(context.Context) []source { return nil }
	next(s)
	if len(s.Snapshot().Accounts) != 1 {
		t.Fatal("missing credential erased snapshot")
	}
	loaded := New(filepath.Dir(s.path))
	if len(loaded.Snapshot().Accounts) != 1 || loaded.Snapshot().Accounts[0].FetchedAt != first.FetchedAt {
		t.Fatal("snapshot not restored")
	}
}

func TestBackoffAndCoalescing(t *testing.T) {
	a := codexSource(jwt("a", "org", "a@example.com"), "")
	var calls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	s := testService(t, []source{a}, transport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		close(entered)
		<-release
		res := reply(429, "")
		res.Header.Set("Retry-After", "3600")
		return res, nil
	}))
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); s.Refresh(context.Background()) }()
	<-entered
	for range 10 {
		s.Refresh(context.Background())
	}
	if !s.Snapshot().Refreshing {
		t.Fatal("missing refreshing status")
	}
	close(release)
	wg.Wait()
	next(s)
	if calls.Load() != 1 {
		t.Fatal("refresh bypassed backoff or coalescing")
	}
	if s.Snapshot().Accounts[0].Error != "Provider refresh is rate limited" {
		t.Fatal("missing backoff status")
	}
}

func TestDuplicateValidLoginSurvivesRevokedLogin(t *testing.T) {
	a := codexSource(jwt("a", "org", "a@example.com"), "")
	b := a
	b.token = "valid-alternative"
	s := testService(t, []source{a, b}, transport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") == "Bearer "+a.token {
			return reply(401, ""), nil
		}
		return reply(200, codexFixture), nil
	}))
	s.Refresh(context.Background())
	if s.Snapshot().Accounts[0].Status != "fresh" {
		t.Fatal("revoked duplicate hid a valid login")
	}
}

func TestExpiredWindowAndAPIProtection(t *testing.T) {
	s := testService(t, nil, transport(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected network request"); return nil, nil }))
	s.cache.Accounts = []Account{{ID: "a", Status: "fresh", FetchedAt: s.now().Add(-11 * time.Minute).UnixMilli(), Windows: []Window{{ID: "week", ResetsAt: s.now().Add(-time.Second).UnixMilli()}}}}
	a := s.Snapshot().Accounts[0]
	if a.Status != "stale" || !a.Windows[0].Expired {
		t.Fatal("old window is still current")
	}
	for _, tc := range []struct {
		method, path, origin string
		want                 int
	}{{"POST", "/api/limits", "", 405}, {"GET", "/api/limits/refresh", "", 405}, {"POST", "/api/limits/refresh", "https://evil.example", 403}} {
		r := httptest.NewRequest(tc.method, "http://localhost:4141"+tc.path, nil)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("%s got %d", tc.path, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "http://localhost:4141/api/limits", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		t.Fatal("invalid GET response")
	}
}

func TestFetchBoundsAndRedirects(t *testing.T) {
	s := testService(t, nil, transport(func(r *http.Request) (*http.Response, error) {
		res := reply(302, "")
		res.Header.Set("Location", "https://other.example/secret")
		return res, nil
	}))
	if _, err := s.fetch(context.Background(), codexURL, source{token: "secret"}); err == nil {
		t.Fatal("followed redirect")
	}
	s.client.Transport = transport(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 256*1024+1)), nil })
	if _, err := s.fetch(context.Background(), codexURL, source{token: "secret"}); err == nil {
		t.Fatal("accepted oversized response")
	}
}

func TestDiscoverProfilesAndHermes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", "")
	t.Setenv("HERMES_HOME", "")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	token := jwt("person", "workspace", "person@example.com")
	for _, name := range []string{".codex", ".codex-dev", ".hermes", ".claude-work"} {
		if err := os.Mkdir(filepath.Join(home, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path string, v any) {
		b, _ := json.Marshal(v)
		if err := os.WriteFile(filepath.Join(home, path), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(".codex/auth.json", map[string]any{"tokens": map[string]string{"access_token": token, "account_id": "workspace"}})
	write(".codex-dev/auth.json", map[string]any{"tokens": map[string]string{"access_token": jwt("person2", "workspace2", "dev@example.com")}})
	write(".hermes/auth.json", map[string]any{"providers": map[string]any{"openai-codex": map[string]any{"tokens": map[string]string{"access_token": token}}}, "credential_pool": map[string]any{"openai-codex": []map[string]string{{"access_token": token}}}})
	write(".claude-work/.credentials.json", map[string]any{"claudeAiOauth": map[string]string{"accessToken": "claude-token"}})
	sources := discover(context.Background(), home)
	if len(sources) != 5 {
		t.Fatalf("sources=%d", len(sources))
	}
	if sources[0].account.ID != sources[2].account.ID || sources[2].account.ID != sources[3].account.ID {
		t.Fatal("Hermes/Codex identity mismatch")
	}
	if sources[0].account.ID == sources[1].account.ID {
		t.Fatal("different accounts merged")
	}
}
