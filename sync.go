// Cross-device sync: every device upserts its rows into one table in a Supabase
// project the user owns and pulls the other devices' rows into a local cache.
// Recording never waits on any of this; the local log stays the source of truth.
package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	table     = "garcon_usage"
	pullEvery = 60 * time.Second
	batchSize = 500
	pageSize  = 1000 // Supabase's default max rows per request
	// A row's synced_at is its transaction start, so a push that began before a
	// pull can commit after it. Re-reading this much lets id dedupe pick those up.
	overlap = 5 * time.Minute
)

// columns is every column the table must have, in wire order plus synced_at;
// probing with it makes a missing column fail loudly on Save.
const columns = "id,device_id,device,time,harness,account,provider,model,status,ms,queue_us,reused,connect_ms,dns_ms,tcp_ms,tls_ms,first_byte_ms,input,cache_read,cache_write,output,synced_at"

// settings is what the user chose in Settings > Sync (~/.config/garcon/config.json).
type settings struct {
	SyncEnabled bool   `json:"sync_enabled"` // master switch: off means no network calls at all
	DeviceName  string `json:"device_name"`
	URL         string `json:"url"`           // https://<ref>.supabase.co
	Key         string `json:"key,omitempty"` // secret key; never returned by the API
}

// syncState is what this device has done so far (~/.local/share/garcon/sync.json).
// It lives next to the data it describes, so recreating the config never changes
// the device's identity.
type syncState struct {
	DeviceID   string `json:"device_id"`
	Pushed     int    `json:"pushed"`      // local records delivered
	PullCursor string `json:"pull_cursor"` // raw synced_at string from PostgREST
}

// wire is one row as sent to the table: a fixed set of keys (nulls for absent
// latency fields) because PostgREST bulk inserts need uniform objects, and no
// synced_at so the column default applies.
type wire struct {
	ID          string `json:"id"`
	DeviceID    string `json:"device_id"`
	Device      string `json:"device"`
	Time        int64  `json:"time"`
	Harness     string `json:"harness"`
	Account     string `json:"account"`
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	Status      int    `json:"status"`
	Ms          int64  `json:"ms"`
	QueueUs     *int64 `json:"queue_us"`
	Reused      *bool  `json:"reused"`
	ConnectMs   *int64 `json:"connect_ms"`
	DnsMs       *int64 `json:"dns_ms"`
	TcpMs       *int64 `json:"tcp_ms"`
	TlsMs       *int64 `json:"tls_ms"`
	FirstByteMs *int64 `json:"first_byte_ms"`
	Input       int64  `json:"input"`
	CacheRead   int64  `json:"cache_read"`
	CacheWrite  int64  `json:"cache_write"`
	Output      int64  `json:"output"`
}

// pulled is a row as read back from the table.
type pulled struct {
	record
	SyncedAt string `json:"synced_at"`
}

// All guarded by mu (main.go), which is never held across a network call.
var (
	cfg        settings
	st         syncState
	remote     = []record{}
	seen       = map[string]struct{}{}
	remoteFile *os.File
	configPath string
	statePath  string
	listenAddr string

	lastPushOK, lastPushErrAt, lastPullOK, lastPullErrAt int64 // unix ms, 0 = never
	lastPushErr, lastPullErr                             string
)

var (
	poke   = make(chan struct{}, 1)
	client = &http.Client{Timeout: 30 * time.Second}
)

// providerOf mirrors the dashboard: rows from before the provider segment
// existed imply it from the harness.
func providerOf(r record) string {
	if r.Provider != "" {
		return r.Provider
	}
	if p, ok := implicitProvider[r.Harness]; ok {
		return p
	}
	return "anthropic"
}

// recordID is deterministic so the existing history backfills with stable ids
// and a re-push is an idempotent upsert. Latency fields are left out: they never
// affect identity. Two byte-identical rows in the same millisecond collapse to one.
func recordID(deviceID string, r record) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%d\x00%s\x00%s\x00%s\x00%s\x00%d\x00%d\x00%d\x00%d\x00%d\x00%d",
		deviceID, r.Time, r.Harness, r.Account, providerOf(r), r.Model, r.Status, r.Ms, r.Input, r.CacheRead, r.CacheWrite, r.Output)
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func toWire(r record, deviceID, device string) wire {
	return wire{ID: recordID(deviceID, r), DeviceID: deviceID, Device: device, Time: r.Time, Harness: r.Harness,
		Account: r.Account, Provider: providerOf(r), Model: r.Model, Status: r.Status, Ms: r.Ms,
		QueueUs: r.QueueUs, Reused: r.Reused, ConnectMs: r.ConnectMs, DnsMs: r.DnsMs, TcpMs: r.TcpMs, TlsMs: r.TlsMs,
		FirstByteMs: r.FirstByteMs, Input: r.Input, CacheRead: r.CacheRead, CacheWrite: r.CacheWrite, Output: r.Output}
}

// writeJSON writes atomically with owner-only permissions: the config holds the secret.
func writeJSON(path string, v any) error {
	os.MkdirAll(filepath.Dir(path), 0o700)
	data, _ := json.MarshalIndent(v, "", "  ")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func loadSettings(path string) {
	configPath = path
	data, err := os.ReadFile(path)
	if err == nil {
		json.Unmarshal(data, &cfg)
	}
	if cfg.DeviceName == "" {
		if cfg.DeviceName, _ = os.Hostname(); cfg.DeviceName == "" {
			cfg.DeviceName = "this device"
		}
	}
	if err != nil {
		if err := writeJSON(path, cfg); err != nil {
			log.Print(err)
		}
	}
}

// loadState runs after load(): the cursor can never exceed the log it indexes.
func loadState(path string) {
	statePath = path
	data, err := os.ReadFile(path)
	if err == nil {
		json.Unmarshal(data, &st)
	}
	if st.DeviceID == "" {
		b := make([]byte, 16)
		rand.Read(b)
		st.DeviceID = hex.EncodeToString(b)
		err = errors.New("new device")
	}
	st.Pushed = min(st.Pushed, len(records))
	if err != nil {
		if err := writeJSON(path, st); err != nil {
			log.Print(err)
		}
	}
}

func loadRemote(path string) {
	for _, rec := range readRecords(path) {
		if _, dup := seen[rec.ID]; rec.ID == "" || dup {
			continue
		}
		seen[rec.ID] = struct{}{}
		remote = append(remote, rec)
	}
	var err error
	if remoteFile, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err != nil {
		log.Fatal(err)
	}
}

func restURL(base string) string {
	return strings.TrimRight(base, "/") + "/rest/v1/" + table
}

// authHeaders: new-format secret keys go in apikey only (Supabase parses anything
// in Authorization as a JWT and rejects them with "Invalid JWT"); legacy
// service_role JWTs need both, since RLS bypass is decided from Authorization.
// The User-Agent matters too: Supabase refuses secret keys from browser-like agents.
func authHeaders(req *http.Request, key string) {
	req.Header.Set("apikey", key)
	if !strings.HasPrefix(key, "sb_secret_") {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("User-Agent", "garcon")
}

// jwtRole reads the role claim of a legacy key without verifying it: enough to
// tell a service_role key from an anon key, which would silently see nothing.
func jwtRole(key string) string {
	parts := strings.Split(key, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return ""
	}
	var claims struct {
		Role string `json:"role"`
	}
	json.Unmarshal(payload, &claims)
	return claims.Role
}

func validKey(key string) error {
	switch {
	case strings.HasPrefix(key, "sb_secret_"):
		return nil
	case strings.HasPrefix(key, "sb_publishable_"):
		return errors.New("that is a publishable key; sync needs the project's secret key (sb_secret_…), which is the only one that can read or write the table")
	case strings.HasPrefix(key, "eyJ"):
		if role := jwtRole(key); role != "service_role" {
			return fmt.Errorf("that legacy key has role %q; sync needs the service_role key (or a new sb_secret_… key)", role)
		}
		return nil
	default:
		return errors.New("key must be a Supabase secret key (sb_secret_…) or a legacy service_role key")
	}
}

// apiError turns a PostgREST or gateway error body ({"message": …, "code": …}) into a readable error.
func apiError(res *http.Response, body []byte) error {
	var e struct{ Message, Code, Hint string }
	if json.Unmarshal(body, &e) == nil && e.Message != "" {
		msg := e.Message
		if e.Code != "" {
			msg = e.Code + ": " + msg
		}
		if e.Hint != "" {
			msg += " (" + e.Hint + ")"
		}
		return errors.New(msg)
	}
	return fmt.Errorf("%s: %s", res.Status, strings.TrimSpace(string(body)))
}

func do(req *http.Request) ([]byte, error) {
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		return nil, apiError(res, body)
	}
	return body, nil
}

// probe checks that the table exists with every column, using the exact
// credentials that are about to be saved.
func probe(base, key string) error {
	req, err := http.NewRequest("GET", restURL(base)+"?select="+columns+"&limit=0", nil)
	if err != nil {
		return err
	}
	authHeaders(req, key)
	_, err = do(req)
	return err
}

// push delivers every local record past the cursor in batches, advancing the
// persisted cursor after each successful upsert.
func push(c settings) error {
	for {
		mu.Lock()
		start := st.Pushed
		if start >= len(records) {
			mu.Unlock()
			return nil
		}
		end := min(start+batchSize, len(records))
		batch := make([]wire, 0, end-start)
		for _, r := range records[start:end] {
			batch = append(batch, toWire(r, st.DeviceID, c.DeviceName))
		}
		mu.Unlock()

		body, _ := json.Marshal(batch)
		req, err := http.NewRequest("POST", restURL(c.URL), bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Prefer", "resolution=merge-duplicates,return=minimal")
		authHeaders(req, c.Key)
		_, err = do(req)

		mu.Lock()
		if err != nil {
			lastPushErr, lastPushErrAt = err.Error(), time.Now().UnixMilli()
			mu.Unlock()
			return fmt.Errorf("push: %w", err)
		}
		if st.Pushed == start { // unchanged by a URL change while the request was in flight
			st.Pushed = end
			writeJSON(statePath, st)
		}
		lastPushOK, lastPushErr = time.Now().UnixMilli(), ""
		mu.Unlock()
	}
}

// pull reads every other device's rows since the cursor (minus the overlap),
// page by page, and only moves the cursor once the whole pass has succeeded.
func pull(c settings) error {
	mu.Lock()
	cursor, me := st.PullCursor, st.DeviceID
	mu.Unlock()
	since := "1970-01-01T00:00:00Z"
	if t, err := time.Parse(time.RFC3339Nano, cursor); err == nil {
		since = t.Add(-overlap).UTC().Format(time.RFC3339Nano)
	}
	last := cursor
	for offset := 0; ; offset += pageSize {
		q := url.Values{}
		q.Set("select", "*")
		q.Set("device_id", "neq."+me)
		q.Set("synced_at", "gte."+since)
		q.Set("order", "synced_at.asc,id.asc")
		q.Set("limit", strconv.Itoa(pageSize))
		q.Set("offset", strconv.Itoa(offset))
		req, err := http.NewRequest("GET", restURL(c.URL)+"?"+q.Encode(), nil)
		if err != nil {
			return err
		}
		authHeaders(req, c.Key)
		body, err := do(req)
		var page []pulled
		if err == nil {
			err = json.Unmarshal(body, &page)
		}
		if err != nil {
			mu.Lock()
			lastPullErr, lastPullErrAt = err.Error(), time.Now().UnixMilli()
			mu.Unlock()
			return fmt.Errorf("pull: %w", err)
		}
		mu.Lock()
		for _, p := range page {
			last = p.SyncedAt
			if _, dup := seen[p.ID]; dup || p.ID == "" || p.DeviceID == me {
				continue
			}
			seen[p.ID] = struct{}{}
			remote = append(remote, p.record)
			line, _ := json.Marshal(p.record)
			if _, err := remoteFile.Write(append(line, '\n')); err != nil {
				log.Print(err)
			}
		}
		mu.Unlock()
		if len(page) < pageSize {
			break
		}
	}
	mu.Lock()
	st.PullCursor = last
	writeJSON(statePath, st)
	lastPullOK, lastPullErr = time.Now().UnixMilli(), ""
	mu.Unlock()
	return nil
}

// syncOnce is one turn of the loop: nothing at all happens while the switch is off.
func syncOnce(lastPull *time.Time) error {
	mu.Lock()
	c := cfg
	mu.Unlock()
	if !c.SyncEnabled {
		return nil
	}
	if err := push(c); err != nil {
		return err
	}
	if time.Since(*lastPull) < pullEvery {
		return nil
	}
	if err := pull(c); err != nil {
		return err
	}
	*lastPull = time.Now()
	return nil
}

// syncLoop runs forever: woken by save() and Save in Settings, otherwise ticking.
// Failures back off up to five minutes but a poke always wakes it early.
func syncLoop() {
	backoff := 5 * time.Second
	var lastPull time.Time
	for {
		select {
		case <-poke:
		case <-time.After(5 * time.Second):
		}
		if err := syncOnce(&lastPull); err != nil {
			log.Print("sync: ", err)
			select {
			case <-poke:
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 5*time.Minute)
		} else {
			backoff = 5 * time.Second
		}
	}
}

func wake() {
	select {
	case poke <- struct{}{}:
	default:
	}
}

// loopback reports whether a Host header names this machine: the only defence
// the settings endpoint has against DNS rebinding, since the server has no auth.
func loopback(host string) bool {
	if host == listenAddr {
		return true
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

type deviceStat struct {
	DeviceID string `json:"device_id"`
	Device   string `json:"device"`
	Rows     int    `json:"rows"`
	LastTime int64  `json:"last_time"`
}

// settingsView is the GET /api/settings body: the settings with the key redacted
// to a boolean, plus everything the Sync section shows about progress.
func settingsView() map[string]any {
	byDevice := map[string]*deviceStat{}
	var devices []deviceStat
	for _, r := range remote {
		d := byDevice[r.DeviceID]
		if d == nil {
			d = &deviceStat{DeviceID: r.DeviceID}
			byDevice[r.DeviceID] = d
		}
		d.Rows++
		if r.Time >= d.LastTime {
			d.LastTime, d.Device = r.Time, r.Device
		}
	}
	for _, d := range byDevice {
		devices = append(devices, *d)
	}
	if devices == nil {
		devices = []deviceStat{}
	}
	return map[string]any{
		"settings": map[string]any{"sync_enabled": cfg.SyncEnabled, "device_name": cfg.DeviceName, "url": cfg.URL, "key_set": cfg.Key != ""},
		"status": map[string]any{
			"device_id": st.DeviceID, "pushed": st.Pushed, "pending": len(records) - st.Pushed,
			"last_push_ok": lastPushOK, "last_push_error": lastPushErr, "last_push_error_at": lastPushErrAt,
			"last_pull_ok": lastPullOK, "last_pull_error": lastPullErr, "last_pull_error_at": lastPullErrAt,
			"remote_rows": len(remote), "devices": devices,
		},
	}
}

func handleSettings(w http.ResponseWriter, r *http.Request) {
	if !loopback(r.Host) {
		http.Error(w, "settings can only be changed from this machine", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
	case http.MethodPut:
		if ct := r.Header.Get("Content-Type"); ct != "application/json" && !strings.HasPrefix(ct, "application/json;") {
			http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
			return
		}
		var in struct {
			SyncEnabled bool    `json:"sync_enabled"`
			DeviceName  string  `json:"device_name"`
			URL         string  `json:"url"`
			Key         *string `json:"key"` // absent keeps the stored key; "" clears it
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil {
			http.Error(w, "bad JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		next := cfg
		mu.Unlock()
		next.SyncEnabled, next.DeviceName, next.URL = in.SyncEnabled, strings.TrimSpace(in.DeviceName), strings.TrimRight(strings.TrimSpace(in.URL), "/")
		if in.Key != nil {
			next.Key = strings.TrimSpace(*in.Key)
		}
		if err := validate(next); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if next.SyncEnabled {
			if err := probe(next.URL, next.Key); err != nil {
				http.Error(w, "could not reach the table: "+err.Error(), http.StatusBadRequest)
				return
			}
		}
		mu.Lock()
		if next.URL != cfg.URL { // a different project: start over, and forget the old project's rows
			st.Pushed, st.PullCursor = 0, ""
			remote, seen = []record{}, map[string]struct{}{}
			remoteFile.Truncate(0)
			writeJSON(statePath, st)
		}
		if next.DeviceName != cfg.DeviceName {
			for i := range records {
				records[i].Device = next.DeviceName
			}
		}
		cfg = next
		err := writeJSON(configPath, cfg)
		mu.Unlock()
		if err != nil {
			http.Error(w, "could not write "+configPath+": "+err.Error(), http.StatusInternalServerError)
			return
		}
		wake()
	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	mu.Lock()
	defer mu.Unlock()
	json.NewEncoder(w).Encode(settingsView())
}

func validate(s settings) error {
	if s.DeviceName == "" {
		return errors.New("device name is required")
	}
	if s.URL != "" {
		u, err := url.Parse(s.URL)
		if err != nil || u.Host == "" || u.Path != "" || (u.Scheme != "https" && !(u.Scheme == "http" && loopback(u.Host))) {
			return errors.New("project URL must look like https://<ref>.supabase.co")
		}
	}
	if s.Key != "" {
		if err := validKey(s.Key); err != nil {
			return err
		}
	}
	if s.SyncEnabled && (s.URL == "" || s.Key == "") {
		return errors.New("a project URL and secret key are needed before sync can be enabled")
	}
	return nil
}
