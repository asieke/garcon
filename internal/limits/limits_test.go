package limits

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"fmt"
	"math"
	"net"
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

func TestIndependentProfilesSurviveDesktopAccountSwitch(t *testing.T) {
	home := t.TempDir()
	for _, key := range []string{"CODEX_HOME", "CLAUDE_CONFIG_DIR", "HERMES_HOME"} {
		t.Setenv(key, "")
	}
	personal := codexSource(jwt("personal", "personal-space", "personal@example.com"), "")
	work := codexSource(jwt("work", "work-space", "work@example.com"), "")
	dev := codexSource(jwt("dev", "dev-space", "dev@example.com"), "")
	writeLogin := func(dir string, src source) {
		t.Helper()
		path := filepath.Join(home, dir)
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(map[string]any{"tokens": map[string]string{"access_token": src.token, "account_id": src.selected}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "auth.json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeLogin(".codex", personal)
	writeLogin(".codex-dev", dev)
	writeLogin(".codex-work", work)
	polls := map[string]int{}
	s := testService(t, nil, transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == creditsURL {
			return reply(200, `{"available_count":1,"credits":[{"status":"available","expires_at":"2035-01-01T00:00:00Z"}]}`), nil
		}
		polls[r.Header.Get("ChatGPT-Account-Id")]++
		return reply(200, codexFixture), nil
	}))
	s.discover = func(ctx context.Context) []source { return discover(ctx, home) }
	next(s)
	writeLogin(".codex", work)
	next(s)
	for _, a := range s.Snapshot().Accounts {
		if a.ID == personal.account.ID && (a.Status != "needs_login" || len(a.Windows) != 1) {
			t.Fatalf("missing desktop login must retain its stale snapshot: %+v", a)
		}
	}
	writeLogin(".codex-personal", personal)
	for _, desktop := range []source{work, dev, personal} {
		writeLogin(".codex", desktop)
		clear(polls)
		next(s)
		accounts := s.Snapshot().Accounts
		if len(accounts) != 3 {
			t.Fatalf("expected three accounts, got %d", len(accounts))
		}
		for _, a := range accounts {
			if a.Status != "fresh" || a.Error != "" || a.ResetCredits == nil || a.ResetCredits.Status != "fresh" || polls[a.Workspace] != 1 {
				t.Fatalf("independent login did not recover or duplicate was polled: %+v polls=%v", a, polls)
			}
		}
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

func TestStartupRenewsBeforeCollectingLimits(t *testing.T) {
	login := codexSource(jwt("a", "org", "a@example.com"), "")
	s := testService(t, nil, transport(func(r *http.Request) (*http.Response, error) { return reply(200, codexFixture), nil }))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	maintained := false
	s.maintain = func(context.Context) bool { maintained = true; return true }
	s.discover = func(context.Context) []source {
		if !maintained {
			t.Error("quota collection ran before login maintenance")
		}
		cancel()
		return []source{login}
	}
	s.Run(ctx)
	if !maintained {
		t.Fatal("startup did not maintain idle credentials")
	}
}

func TestAGYAndMuseLimits(t *testing.T) {
	agyJSON := `{"command":{"name":"usage","data":{"groups":[{"name":"Gemini Models","buckets":[{"id":"gemini-weekly","window":"weekly","remaining_fraction":0.75,"reset_time":"2030-01-07T00:00:00Z"},{"id":"gemini-5h","window":"5h","remaining_fraction":0.5,"reset_time":"2030-01-01T05:00:00Z"}]},{"name":"Claude and GPT models","buckets":[{"id":"3p-weekly","window":"weekly","remaining_fraction":0.9,"reset_time":"2030-01-07T00:00:00Z"},{"id":"3p-5h","window":"5h","remaining_fraction":0.8,"reset_time":"2030-01-01T05:00:00Z"}]}]}}}`
	ws, err := parseAGY([]byte(agyJSON))
	if err != nil || len(ws) != 4 {
		t.Fatalf("parseAGY: %+v %v", ws, err)
	}
	if ws[0].Label != "Weekly" || *ws[0].UsedPercent != 25 || ws[1].Label != "5-hour" || *ws[1].UsedPercent != 50 {
		t.Fatalf("unexpected primary AGY windows: %+v", ws)
	}
	if ws[2].Label != "Claude & GPT · 5-hour" || math.Abs(*ws[2].UsedPercent-20) > 1e-6 || ws[3].Label != "Claude & GPT · Weekly" || math.Abs(*ws[3].UsedPercent-10) > 1e-6 {
		t.Fatalf("unexpected secondary AGY windows: %+v", ws)
	}

	home := t.TempDir()
	for _, key := range []string{"CODEX_HOME", "CLAUDE_CONFIG_DIR", "HERMES_HOME", "AGY_HOME", "AGY_BIN", "MUSE_AUTH_PATH"} {
		t.Setenv(key, "")
	}
	agyDir := filepath.Join(home, ".gemini", "antigravity-cli")
	if err := os.MkdirAll(agyDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agyDir, "cli.log"), []byte("I1014 10:18:31.015864 1 server_oauth.go:204] applyAuthResult: email=user@example.com, authMethod=consumer\n"), 0600); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0700); err != nil {
		t.Fatal(err)
	}
	fakeAGY := filepath.Join(binDir, "agy")
	script := "#!/bin/sh\nprintf '%s\\n' '" + agyJSON + "'\n"
	if err := os.WriteFile(fakeAGY, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}

	museDir := filepath.Join(home, ".config", "muse")
	if err := os.MkdirAll(museDir, 0700); err != nil {
		t.Fatal(err)
	}
	museAuth := `{"providers":{"meta":{"mechanism":"oauth","storage":"keychain","subs_active":true,"user_full_name":"Test User","user_email":"user@example.com"}}}`
	if err := os.WriteFile(filepath.Join(museDir, "auth.json"), []byte(museAuth), 0600); err != nil {
		t.Fatal(err)
	}
	// A fake running Muse host: newline-delimited JSON-RPC on the session's
	// runtime socket, with a notification before each answer as a real host
	// may send. A lease for an exited process must be skipped.
	data, err := os.MkdirTemp("", "muse") // unix socket paths are short
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(data) })
	t.Setenv("XDG_DATA_HOME", data)
	runtimeDir := museRuntimeDir(home)
	if err := os.MkdirAll(runtimeDir, 0700); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(runtimeDir, "ms-test.sock")
	listener, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				lines := bufio.NewScanner(conn)
				for lines.Scan() {
					switch line := lines.Text(); {
					case strings.Contains(line, `"method":"initialize"`):
						fmt.Fprint(conn, `{"jsonrpc":"2.0","method":"session/listChanged","params":{}}`+"\n"+`{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"muse"}}}`+"\n")
					case strings.Contains(line, `"method":"usage/read"`):
						fmt.Fprint(conn, `{"jsonrpc":"2.0","id":2,"result":{"usage":{"observedAtMs":1893456000000,"tier":"plus","window":{"usedPercent":42,"windowDurationMins":300,"resetsAtMs":1893474000000},"weekly":{"usedPercent":7,"resetsAtMs":1894060800000}}}}`+"\n")
					}
				}
			}()
		}
	}()
	lease := fmt.Sprintf(`{"schema_version":1,"pid":%d}`, os.Getpid())
	if err := os.WriteFile(sock+".lease", []byte(lease), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "ms-gone.sock.lease"), []byte(`{"pid":999999999}`), 0600); err != nil {
		t.Fatal(err)
	}

	s := testService(t, nil, nil)
	s.discover = func(ctx context.Context) []source { return discover(ctx, home) }
	s.Refresh(context.Background())
	snap := s.Snapshot()
	if len(snap.Accounts) != 2 {
		t.Fatalf("expected 2 accounts (agy + muse), got %+v", snap.Accounts)
	}
	if snap.Accounts[0].Provider != "agy" || snap.Accounts[0].Email != "user@example.com" || snap.Accounts[0].Status != "fresh" || len(snap.Accounts[0].Windows) != 4 {
		t.Fatalf("unexpected agy account: %+v", snap.Accounts[0])
	}
	muse := snap.Accounts[1]
	if muse.Provider != "muse" || muse.Email != "user@example.com" || muse.Status != "fresh" || muse.Plan != "plus" || muse.ObservedAt != 1893456000000 || len(muse.Windows) != 2 {
		t.Fatalf("unexpected muse account: %+v", muse)
	}
	if muse.Windows[0].Label != "Weekly" || *muse.Windows[0].UsedPercent != 7 || muse.Windows[1].Label != "5-hour" || *muse.Windows[1].UsedPercent != 42 || muse.Windows[1].ResetsAt != 1893474000000 {
		t.Fatalf("unexpected muse windows: %+v", muse.Windows)
	}

	// Once the session ends, the last reading stays visible as idle, not stale.
	listener.Close()
	os.Remove(sock + ".lease")
	s.discover = func(ctx context.Context) []source { return discoverMuse(ctx, home) }
	next(s)
	if a := s.Snapshot().Accounts; len(a) != 2 || a[1].Status != "idle" || len(a[1].Windows) != 2 || a[1].ObservedAt != 1893456000000 {
		t.Fatalf("an ended Muse session must keep its last reading: %+v", a)
	}

	// With no reading ever, the account is unavailable, never a blank "fresh" row.
	s = testService(t, nil, nil)
	s.discover = func(ctx context.Context) []source { return discoverMuse(ctx, home) }
	s.Refresh(context.Background())
	if a := s.Snapshot().Accounts; len(a) != 1 || a[0].Status != "unavailable" || len(a[0].Windows) != 0 || !strings.Contains(a[0].Error, "No Muse reading yet") {
		t.Fatalf("missing Muse reading must not look fresh: %+v", a)
	}
}

func TestMuseUsageShapes(t *testing.T) {
	var stored museUsage
	if err := json.Unmarshal([]byte(`{"window":{"used_percent":12.5,"window_duration_mins":300,"resets_at":"2030-01-01T05:00:00Z"},"weekly":{"used_percent":101,"resets_at_ms":1893974400000}}`), &stored); err != nil {
		t.Fatal(err)
	}
	ws := parseMuseWindows(&stored)
	if len(ws) != 2 || ws[1].Label != "5-hour" || *ws[1].UsedPercent != 12.5 || ws[1].ResetsAt != 1893474000000 || *ws[0].UsedPercent != 101 || ws[0].ResetsAt != 1893974400000 {
		t.Fatalf("snake_case usage: %+v", ws)
	}
	if ws := parseMuseWindows(&museUsage{Window: &museWindow{}, Weekly: &museWindow{}}); len(ws) != 0 {
		t.Fatalf("empty windows must not be reported: %+v", ws)
	}
	for in, want := range map[int64]int64{1893474000: 1893474000000, 1893474000000: 1893474000000, 1893474000000000: 1893474000000, 1893474000000000000: 1893474000000} {
		if got := epochMillis(in); got != want {
			t.Fatalf("epochMillis(%d) = %d, want %d", in, got, want)
		}
	}
}

