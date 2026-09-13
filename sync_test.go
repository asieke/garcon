package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// resetSync gives every test a clean in-memory state and temp files, as if the
// proxy had just started on a new machine.
func resetSync(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mu.Lock()
	defer mu.Unlock()
	records = []record{}
	remote, seen = []record{}, map[string]struct{}{}
	cfg = settings{DeviceName: "laptop"}
	st = syncState{DeviceID: "dev1"}
	configPath, statePath = filepath.Join(dir, "config.json"), filepath.Join(dir, "sync.json")
	listenAddr = "127.0.0.1:4141"
	lastPushOK, lastPushErr, lastPullOK, lastPullErr = 0, "", 0, ""
	var err error
	if logFile, err = os.Create(filepath.Join(dir, "usage.jsonl")); err != nil {
		t.Fatal(err)
	}
	if remoteFile, err = os.Create(filepath.Join(dir, "remote.jsonl")); err != nil {
		t.Fatal(err)
	}
	return dir
}

func row(i int) record {
	return record{Time: int64(1_700_000_000_000 + i), Harness: "claude", Account: "me@x.com", Model: "claude-sonnet-5", Status: 200, Ms: 10, Input: int64(i), Output: 1}
}

func TestRecordID(t *testing.T) {
	a := row(1)
	id := recordID("dev1", a)
	if len(id) != 32 || id != recordID("dev1", a) {
		t.Fatalf("not stable: %q", id)
	}
	if recordID("dev2", a) == id {
		t.Error("device id must change the id")
	}
	ms := int64(5)
	b := a
	b.ConnectMs = &ms
	if recordID("dev1", b) != id {
		t.Error("latency fields must not affect identity")
	}
	// Field boundaries are delimited: shifting a character between adjacent fields changes the hash.
	c, d := a, a
	c.Harness, c.Account = "ab", "c"
	d.Harness, d.Account = "a", "bc"
	if recordID("dev1", c) == recordID("dev1", d) {
		t.Error("boundary collision")
	}
}

func TestWire(t *testing.T) {
	ms := int64(7)
	rs := []record{row(1), row(2)}
	rs[0].Provider, rs[1].ConnectMs, rs[1].Harness = "openrouter", &ms, "codex"
	var objs []map[string]json.RawMessage
	b, _ := json.Marshal([]wire{toWire(rs[0], "dev1", "laptop"), toWire(rs[1], "dev1", "laptop")})
	json.Unmarshal(b, &objs)
	for _, o := range objs {
		if len(o) != 21 {
			t.Errorf("want 21 keys, got %d", len(o))
		}
		if _, has := o["synced_at"]; has {
			t.Error("synced_at must never be sent")
		}
	}
	if string(objs[0]["connect_ms"]) != "null" || string(objs[1]["connect_ms"]) != "7" {
		t.Errorf("nulls for absent fields: %s %s", objs[0]["connect_ms"], objs[1]["connect_ms"])
	}
	if string(objs[0]["provider"]) != `"openrouter"` || string(objs[1]["provider"]) != `"chatgpt"` {
		t.Errorf("provider: %s %s", objs[0]["provider"], objs[1]["provider"])
	}
	if strings.Count(columns, ",") != 21 {
		t.Errorf("columns should list the 21 wire keys plus synced_at")
	}

	req, _ := http.NewRequest("GET", "http://x", nil)
	authHeaders(req, "sb_secret_abc")
	if req.Header.Get("apikey") != "sb_secret_abc" || req.Header.Get("Authorization") != "" {
		t.Error("new keys go in apikey only")
	}
	jwt := "eyJhbGciOiJIUzI1NiJ9." + strings.TrimRight(base64url(`{"role":"service_role"}`), "=") + ".sig"
	req, _ = http.NewRequest("GET", "http://x", nil)
	authHeaders(req, jwt)
	if req.Header.Get("Authorization") != "Bearer "+jwt {
		t.Error("legacy keys need Authorization too")
	}
	if validKey(jwt) != nil || validKey("sb_secret_abc") != nil {
		t.Error("valid keys rejected")
	}
	anon := "eyJhbGciOiJIUzI1NiJ9." + strings.TrimRight(base64url(`{"role":"anon"}`), "=") + ".sig"
	if validKey(anon) == nil || validKey("sb_publishable_abc") == nil || validKey("nope") == nil {
		t.Error("anon, publishable and junk keys must be rejected")
	}
}

func base64url(s string) string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	var out []byte
	b := []byte(s)
	for i := 0; i < len(b); i += 3 {
		var n uint32
		k := 0
		for j := 0; j < 3; j++ {
			n <<= 8
			if i+j < len(b) {
				n |= uint32(b[i+j])
				k++
			}
		}
		for j := 0; j < k+1; j++ {
			out = append(out, chars[(n>>(18-6*j))&63])
		}
	}
	return string(out)
}

// fakeREST is a minimal PostgREST stand-in: it records upserts and serves pages.
type fakeREST struct {
	posts   []*http.Request
	bodies  [][]wire
	fail    atomic.Int32 // remaining POSTs to answer with 500
	pages   [][]map[string]any
	gets    []string
	handled atomic.Int32
}

func (f *fakeREST) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.handled.Add(1)
	switch r.Method {
	case http.MethodPost:
		var batch []wire
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &batch)
		f.posts = append(f.posts, r)
		f.bodies = append(f.bodies, batch)
		if f.fail.Load() > 0 {
			f.fail.Add(-1)
			w.WriteHeader(500)
			fmt.Fprint(w, `{"message":"boom","code":"XX000"}`)
			return
		}
		w.WriteHeader(201)
	case http.MethodGet:
		f.gets = append(f.gets, r.URL.RawQuery)
		if r.URL.Query().Get("limit") == "0" { // probe
			fmt.Fprint(w, "[]")
			return
		}
		offset := 0
		fmt.Sscan(r.URL.Query().Get("offset"), &offset)
		i := offset / pageSize
		if i < len(f.pages) {
			json.NewEncoder(w).Encode(f.pages[i])
		} else {
			fmt.Fprint(w, "[]")
		}
	}
}

func TestPush(t *testing.T) {
	dir := resetSync(t)
	f := &fakeREST{}
	srv := httptest.NewServer(f)
	defer srv.Close()
	for i := 0; i < 1200; i++ {
		records = append(records, row(i))
	}
	c := settings{SyncEnabled: true, DeviceName: "laptop", URL: srv.URL, Key: "sb_secret_k"}
	f.fail.Store(1)
	if err := push(c); err == nil || !strings.Contains(err.Error(), "XX000: boom") {
		t.Fatalf("want the server's error, got %v", err)
	}
	if st.Pushed != 0 || lastPushErr == "" {
		t.Fatalf("cursor moved on failure: %d %q", st.Pushed, lastPushErr)
	}
	if err := push(c); err != nil {
		t.Fatal(err)
	}
	if st.Pushed != 1200 || lastPushErr != "" || lastPushOK == 0 {
		t.Fatalf("cursor %d err %q ok %d", st.Pushed, lastPushErr, lastPushOK)
	}
	if n := []int{len(f.bodies[0]), len(f.bodies[1]), len(f.bodies[2]), len(f.bodies[3])}; fmt.Sprint(n) != "[500 500 500 200]" {
		t.Errorf("batches: %v (first was the failed one)", n)
	}
	p := f.posts[1]
	if p.URL.Path != "/rest/v1/garcon_usage" || p.Header.Get("Prefer") != "resolution=merge-duplicates,return=minimal" ||
		p.Header.Get("apikey") != "sb_secret_k" || p.Header.Get("Authorization") != "" || p.Header.Get("User-Agent") != "garcon" {
		t.Errorf("headers: %v %v", p.URL.Path, p.Header)
	}
	if f.bodies[1][0].ID != recordID("dev1", row(0)) || f.bodies[1][0].Device != "laptop" {
		t.Errorf("wire row: %+v", f.bodies[1][0])
	}
	var saved syncState
	b, _ := os.ReadFile(filepath.Join(dir, "sync.json"))
	if json.Unmarshal(b, &saved); saved.Pushed != 1200 || saved.DeviceID != "dev1" {
		t.Errorf("state not persisted: %s", b)
	}
}

func TestPull(t *testing.T) {
	dir := resetSync(t)
	mk := func(id, dev string, at string, i int) map[string]any {
		return map[string]any{"id": id, "device_id": dev, "device": "desk", "time": 1_700_000_000_000 + i, "harness": "codex", "account": "me@x.com",
			"provider": "chatgpt", "model": "gpt-6-astra", "status": 200, "ms": 5, "queue_us": nil, "reused": nil, "connect_ms": nil, "dns_ms": nil,
			"tcp_ms": nil, "tls_ms": nil, "first_byte_ms": nil, "input": 1, "cache_read": 0, "cache_write": 0, "output": 2, "synced_at": at}
	}
	f := &fakeREST{}
	first := make([]map[string]any, pageSize)
	for i := range first {
		first[i] = mk(fmt.Sprintf("r%04d", i), "dev2", "2026-09-13T10:00:00.000001+00:00", i)
	}
	f.pages = [][]map[string]any{first, {
		mk("r0000", "dev2", "2026-09-13T10:00:00.000001+00:00", 0), // overlap duplicate
		mk("mine", "dev1", "2026-09-13T10:05:00+00:00", 1),         // own device slipped through: ignored
		mk("late", "dev3", "2026-09-13T10:07:00.5+00:00", 2),
	}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	st.PullCursor = "2026-09-13T09:58:00+00:00"
	c := settings{SyncEnabled: true, DeviceName: "laptop", URL: srv.URL, Key: "sb_secret_k"}
	if err := pull(c); err != nil {
		t.Fatal(err)
	}
	if len(remote) != pageSize+1 || len(seen) != pageSize+1 {
		t.Fatalf("remote %d seen %d", len(remote), len(seen))
	}
	if st.PullCursor != "2026-09-13T10:07:00.5+00:00" {
		t.Errorf("cursor %q", st.PullCursor)
	}
	if len(f.gets) != 2 || !strings.Contains(f.gets[0], "synced_at=gte.2026-09-13T09%3A53%3A00Z") || !strings.Contains(f.gets[0], "device_id=neq.dev1") ||
		!strings.Contains(f.gets[1], "offset=1000") || !strings.Contains(f.gets[0], "order=synced_at.asc%2Cid.asc") {
		t.Errorf("queries: %v", f.gets)
	}
	last := remote[len(remote)-1]
	if last.ID != "late" || last.DeviceID != "dev3" || last.Device != "desk" || last.Output != 2 || last.SyncedAtIgnored() {
		t.Errorf("row: %+v", last)
	}
	// The cache survives a restart and is deduplicated on the way in.
	remoteFile.Close()
	remote, seen = []record{}, map[string]struct{}{}
	loadRemote(filepath.Join(dir, "remote.jsonl"))
	if len(remote) != pageSize+1 {
		t.Errorf("reloaded %d", len(remote))
	}
	if f.pages[1][2]["synced_at"] == nil {
		t.Error("test data")
	}
}

// SyncedAtIgnored proves the pulled struct's synced_at never leaks into the cached record.
func (r record) SyncedAtIgnored() bool {
	b, _ := json.Marshal(r)
	return strings.Contains(string(b), "synced_at")
}

func putSettings(body, host, ct string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, "http://"+host+"/api/settings", strings.NewReader(body))
	req.Host = host
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	handleSettings(w, req)
	return w
}

func TestSettings(t *testing.T) {
	dir := resetSync(t)
	f := &fakeREST{}
	srv := httptest.NewServer(f)
	defer srv.Close()
	records = append(records, row(1))

	if w := putSettings(`{}`, "127.0.0.1:4141", "text/plain"); w.Code != 415 {
		t.Errorf("content type: %d", w.Code)
	}
	if w := putSettings(`{}`, "evil.example", "application/json"); w.Code != 403 {
		t.Errorf("host: %d", w.Code)
	}
	if w := putSettings(`{"device_name":"laptop","url":"`+srv.URL+`","key":"sb_publishable_x"}`, "localhost:4141", "application/json"); w.Code != 400 || !strings.Contains(w.Body.String(), "publishable") {
		t.Errorf("publishable key: %d %s", w.Code, w.Body)
	}
	if w := putSettings(`{"device_name":"laptop","url":"`+srv.URL+`","key":"sb_secret_k","sync_enabled":true}`, "[::1]:4141", "application/json"); w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	if cfg.Key != "sb_secret_k" || !cfg.SyncEnabled || f.handled.Load() != 1 {
		t.Errorf("saved %+v, probes %d", cfg, f.handled.Load())
	}
	b, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	if fi, _ := os.Stat(filepath.Join(dir, "config.json")); fi.Mode().Perm() != 0o600 || !strings.Contains(string(b), "sb_secret_k") {
		t.Errorf("config file: %v %s", fi.Mode(), b)
	}
	// GET never reveals the key; PUT without a key keeps it.
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:4141/api/settings", nil)
	w := httptest.NewRecorder()
	handleSettings(w, req)
	if strings.Contains(w.Body.String(), "sb_secret") || !strings.Contains(w.Body.String(), `"key_set":true`) || !strings.Contains(w.Body.String(), `"pending":1`) {
		t.Errorf("GET: %s", w.Body)
	}
	if w := putSettings(`{"device_name":"desk","url":"`+srv.URL+`","sync_enabled":true}`, "127.0.0.1:4141", "application/json"); w.Code != 200 || cfg.Key != "sb_secret_k" || cfg.DeviceName != "desk" || records[0].Device != "desk" {
		t.Errorf("keep key / rename: %d %+v %q", w.Code, cfg, records[0].Device)
	}
	// A failing probe persists nothing.
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		fmt.Fprint(w, `{"code":"PGRST204","message":"Could not find the 'tls_ms' column"}`)
	}))
	defer dead.Close()
	if w := putSettings(`{"device_name":"desk","url":"`+dead.URL+`","sync_enabled":true}`, "127.0.0.1:4141", "application/json"); w.Code != 400 || !strings.Contains(w.Body.String(), "tls_ms") || cfg.URL != srv.URL {
		t.Errorf("failed probe: %d %s %s", w.Code, w.Body, cfg.URL)
	}
	// Changing the project resets the cursors and the remote cache.
	st.Pushed, st.PullCursor = 1, "x"
	remote = append(remote, row(9))
	if w := putSettings(`{"device_name":"desk","url":"`+dead.URL+`","sync_enabled":false}`, "127.0.0.1:4141", "application/json"); w.Code != 200 || st.Pushed != 0 || st.PullCursor != "" || len(remote) != 0 {
		t.Errorf("url change: %d %+v %d", w.Code, st, len(remote))
	}

	// The master switch: off means no requests at all, across a save, a new row and a due pull.
	before := f.handled.Load()
	if w := putSettings(`{"device_name":"desk","url":"`+srv.URL+`","sync_enabled":false}`, "127.0.0.1:4141", "application/json"); w.Code != 200 {
		t.Fatalf("save off: %d %s", w.Code, w.Body)
	}
	save(row(2))
	lastPull := time.Time{}
	if err := syncOnce(&lastPull); err != nil || f.handled.Load() != before {
		t.Errorf("switch off but the server saw %d requests (err %v)", f.handled.Load()-before, err)
	}
	if w := putSettings(`{"device_name":"desk","url":"`+srv.URL+`","sync_enabled":true}`, "127.0.0.1:4141", "application/json"); w.Code != 200 {
		t.Fatalf("save on: %d %s", w.Code, w.Body)
	}
	if err := syncOnce(&lastPull); err != nil || st.Pushed != 2 || len(f.bodies) != 1 || lastPull.IsZero() {
		t.Errorf("switch on: err %v pushed %d posts %d pulled %v", err, st.Pushed, len(f.bodies), !lastPull.IsZero())
	}
}
