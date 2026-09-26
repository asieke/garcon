package proxy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"garcon/internal/codexrouting"
	"garcon/internal/limits"
	"garcon/internal/usage"
)

func TestCodexRoutingForwardsSelectedIdentityAndDoesNotReplay(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	now := time.Now()
	var snapshot limits.Snapshot
	for i, id := range []string{"a", "b", "c"} {
		dir := filepath.Join(home, ".codex-"+id)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		claims, _ := json.Marshal(map[string]any{"exp": now.Add(time.Hour).Unix(), "email": id + "@example.com", "https://api.openai.com/auth": map[string]string{"chatgpt_account_id": id}})
		b, _ := json.Marshal(map[string]any{"tokens": map[string]string{"account_id": id, "access_token": "header." + base64.RawURLEncoding.EncodeToString(claims) + ".signature"}})
		if err := os.WriteFile(filepath.Join(dir, "auth.json"), b, 0600); err != nil {
			t.Fatal(err)
		}
		used := []float64{60, 20, 90}[i]
		snapshot.Accounts = append(snapshot.Accounts, limits.Account{Provider: "codex", Workspace: id, Status: "fresh", FetchedAt: now.UnixMilli(), Windows: []limits.Window{{ID: "codex:0", UsedPercent: &used, ResetsAt: now.Add(time.Hour).UnixMilli()}}})
	}
	original := http.DefaultTransport
	http.DefaultTransport = transportFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "chatgpt.com" {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"models":[{"slug":"test"}]}`))}, nil
		}
		return original.RoundTrip(req)
	})
	defer func() { http.DefaultTransport = original }()
	router := codexrouting.New(t.TempDir(), func() limits.Snapshot { return snapshot })
	if err := router.Configure(true); err != nil {
		t.Fatal(err)
	}
	router.Refresh(context.Background())
	calls := 0
	upstreamStatus := 200
	const payload = `{"model":"test","input":"unchanged","stream":true}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls++
		b, _ := io.ReadAll(req.Body)
		if string(b) != payload || req.Header.Get("ChatGPT-Account-Id") != "b" || req.Header.Get("Authorization") == "Bearer original" || req.URL.Path != "/backend-api/codex/responses" {
			t.Error("wrong upstream identity or altered request")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(upstreamStatus)
		io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"model\":\"test\",\"usage\":{\"input_tokens\":1,\"output_tokens\":2}}}\n\n")
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)
	var record, started usage.Record
	p := Proxy{Codex: router, Start: func(r usage.Record) { started = r }, Save: func(r usage.Record) { record = r }}
	route, _ := ParseRoute("/codex/backend-api/codex/responses")
	route.Target = target
	call := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "http://127.0.0.1:4141/codex/backend-api/codex/responses", strings.NewReader(payload))
		req.RemoteAddr = "127.0.0.1:1111"
		req.Header.Set("Session-Id", "conversation")
		req.Header.Set("Authorization", "Bearer original")
		req.Header.Set("ChatGPT-Account-Id", "a")
		w := httptest.NewRecorder()
		p.Serve(w, req, route)
		if req.Header.Get("Authorization") != "Bearer original" {
			t.Fatal("mutated caller headers")
		}
		return w
	}
	w := call()
	if w.Code != 200 || record.Account != "b@example.com" || record.Input != 1 || record.Output != 2 || w.Header().Get("X-Garcon-Account-Id") != "b" {
		t.Fatalf("incorrect attribution or stream: %d %+v", w.Code, record)
	}
	if started.State != "streaming" || record.State != "complete" || started.RequestID == "" || started.RequestID != record.RequestID || record.SessionID != "conversation" || record.AccountID != "b" {
		t.Fatalf("lost request lifecycle %+v %+v", started, record)
	}
	upstreamStatus = 429
	w = call()
	if w.Code != 429 || calls != 2 {
		t.Fatal("replayed a rejected request")
	}
	w = call()
	if w.Code != 503 || calls != 2 || record.Status != 503 || record.State != "failed" {
		t.Fatal("rerouted an unavailable pinned conversation")
	}
}
