// Package syncer exchanges usage rows with a table in a Supabase project the
// user owns: every device upserts its rows and pulls the other devices' rows into
// the local store. Recording never waits on any of this; the local log stays the
// source of truth. Nothing at all happens while the switch is off.
package syncer

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
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"garcon/internal/local"
	"garcon/internal/proxy"
	"garcon/internal/store"
	"garcon/internal/usage"
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

// columns is every column the table must have; probing with it makes a missing
// column fail loudly on Save.
const columns = "id,device_id,device,time,harness,account,provider,model,status,ms,queue_us,reused,connect_ms,dns_ms,tcp_ms,tls_ms,first_byte_ms,input,cache_read,cache_write,output,synced_at"

// settings is what the user chose in Settings > Sync (~/.config/garcon/config.json).
type settings struct {
	SyncEnabled bool   `json:"sync_enabled"` // master switch: off means no network calls at all
	DeviceName  string `json:"device_name"`
	URL         string `json:"url"`           // https://<ref>.supabase.co
	Key         string `json:"key,omitempty"` // secret key; never returned by the API
}

// state is what this device has done so far (sync.json next to the usage log).
// It lives with the data it describes, so recreating the config never changes
// the device's identity.
type state struct {
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
	usage.Record
	SyncedAt string `json:"synced_at"`
}

// Syncer owns the settings, the sync state and the loop.
type Syncer struct {
	store      *store.Store
	configPath string
	statePath  string
	client     *http.Client
	poke       chan struct{}

	mu  sync.Mutex // guards everything below; never held across a network call
	cfg settings
	st  state

	lastPushOK, lastPushErrAt, lastPullOK, lastPullErrAt int64 // unix ms, 0 = never
	lastPushErr, lastPullErr                             string
}

// New loads the settings and state, labels the store's rows with the device
// name, and arranges to be woken on every Save. Run must be called to sync.
func New(st *store.Store, configPath string) *Syncer {
	s := &Syncer{store: st, configPath: configPath, statePath: filepath.Join(st.Dir(), "sync.json"),
		// Never follow a redirect: Go keeps the apikey header across hosts, so a
		// redirecting project URL would hand the key to whatever it points at.
		client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		poke:   make(chan struct{}, 1)}
	s.loadSettings()
	s.loadState()
	st.SetDevice(s.cfg.DeviceName)
	st.OnSave = s.Wake
	return s
}

// DeviceName is this machine's label.
func (s *Syncer) DeviceName() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.DeviceName
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

func (s *Syncer) loadSettings() {
	data, err := os.ReadFile(s.configPath)
	if err == nil {
		json.Unmarshal(data, &s.cfg)
	}
	if s.cfg.DeviceName == "" {
		if s.cfg.DeviceName, _ = os.Hostname(); s.cfg.DeviceName == "" {
			s.cfg.DeviceName = "this device"
		}
	}
	if err != nil {
		if err := writeJSON(s.configPath, s.cfg); err != nil {
			log.Print(err)
		}
	}
}

// loadState clamps the cursor to the log it indexes.
func (s *Syncer) loadState() {
	data, err := os.ReadFile(s.statePath)
	if err == nil {
		json.Unmarshal(data, &s.st)
	}
	if s.st.DeviceID == "" {
		b := make([]byte, 16)
		rand.Read(b)
		s.st.DeviceID = hex.EncodeToString(b)
		err = errors.New("new device")
	}
	s.st.Pushed = min(s.st.Pushed, s.store.Len())
	if err != nil {
		if err := writeJSON(s.statePath, s.st); err != nil {
			log.Print(err)
		}
	}
}

// recordID is deterministic so the existing history backfills with stable ids
// and a re-push is an idempotent upsert. Latency fields are left out: they never
// affect identity. Two byte-identical rows in the same millisecond collapse to one.
func recordID(deviceID string, r usage.Record) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%d\x00%s\x00%s\x00%s\x00%s\x00%d\x00%d\x00%d\x00%d\x00%d\x00%d",
		deviceID, r.Time, r.Harness, r.Account, proxy.ProviderOf(r), r.Model, r.Status, r.Ms, r.Input, r.CacheRead, r.CacheWrite, r.Output)
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func toWire(r usage.Record, deviceID, device string) wire {
	return wire{ID: recordID(deviceID, r), DeviceID: deviceID, Device: device, Time: r.Time, Harness: r.Harness,
		Account: r.Account, Provider: proxy.ProviderOf(r), Model: r.Model, Status: r.Status, Ms: r.Ms,
		QueueUs: r.QueueUs, Reused: r.Reused, ConnectMs: r.ConnectMs, DnsMs: r.DnsMs, TcpMs: r.TcpMs, TlsMs: r.TlsMs,
		FirstByteMs: r.FirstByteMs, Input: r.Input, CacheRead: r.CacheRead, CacheWrite: r.CacheWrite, Output: r.Output}
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

func (s *Syncer) do(req *http.Request) ([]byte, error) {
	res, err := s.client.Do(req)
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
func (s *Syncer) probe(base, key string) error {
	req, err := http.NewRequest("GET", restURL(base)+"?select="+columns+"&limit=0", nil)
	if err != nil {
		return err
	}
	authHeaders(req, key)
	_, err = s.do(req)
	return err
}

// push delivers every local record past the cursor in batches, advancing the
// persisted cursor after each successful upsert.
func (s *Syncer) push(c settings) error {
	for {
		s.mu.Lock()
		start, deviceID := s.st.Pushed, s.st.DeviceID
		s.mu.Unlock()
		rows := s.store.Batch(start, batchSize)
		if len(rows) == 0 {
			return nil
		}
		batch := make([]wire, 0, len(rows))
		for _, r := range rows {
			batch = append(batch, toWire(r, deviceID, c.DeviceName))
		}
		body, _ := json.Marshal(batch)
		req, err := http.NewRequest("POST", restURL(c.URL), bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Prefer", "resolution=merge-duplicates,return=minimal")
		authHeaders(req, c.Key)
		_, err = s.do(req)

		s.mu.Lock()
		if err != nil {
			s.lastPushErr, s.lastPushErrAt = err.Error(), time.Now().UnixMilli()
			s.mu.Unlock()
			return fmt.Errorf("push: %w", err)
		}
		if s.st.Pushed == start { // unchanged by a URL change while the request was in flight
			s.st.Pushed = start + len(rows)
			writeJSON(s.statePath, s.st)
		}
		s.lastPushOK, s.lastPushErr = time.Now().UnixMilli(), ""
		s.mu.Unlock()
	}
}

// pull reads every other device's rows since the cursor (minus the overlap),
// page by page, and only moves the cursor once the whole pass has succeeded.
func (s *Syncer) pull(c settings) error {
	s.mu.Lock()
	cursor, me := s.st.PullCursor, s.st.DeviceID
	s.mu.Unlock()
	since := "1970-01-01T00:00:00Z"
	if t, err := time.Parse(time.RFC3339Nano, cursor); err == nil {
		since = t.Add(-overlap).UTC().Format(time.RFC3339Nano)
	}
	last := cursor
	skipped := 0
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
		body, err := s.do(req)
		var page []pulled
		if err == nil {
			err = json.Unmarshal(body, &page)
		}
		if err != nil {
			s.mu.Lock()
			s.lastPullErr, s.lastPullErrAt = err.Error(), time.Now().UnixMilli()
			s.mu.Unlock()
			return fmt.Errorf("pull: %w", err)
		}
		for _, p := range page {
			if !acceptable(p) {
				skipped++
				continue
			}
			last = p.SyncedAt
			if p.DeviceID != me {
				s.store.AddRemote(p.Record)
			}
		}
		if len(page) < pageSize {
			break
		}
	}
	if skipped > 0 {
		log.Printf("sync: pull skipped %d malformed rows", skipped)
	}
	s.mu.Lock()
	s.st.PullCursor = last
	writeJSON(s.statePath, s.st)
	s.lastPullOK, s.lastPullErr = time.Now().UnixMilli(), ""
	s.mu.Unlock()
	return nil
}

// maxField bounds every string in a pulled row. Whoever holds the project key
// can write anything into the table; this keeps a hostile or broken writer from
// bloating the cache or, through synced_at, moving the cursor past real rows.
const maxField = 512

func acceptable(p pulled) bool {
	for _, s := range []string{p.ID, p.DeviceID, p.Device, p.Harness, p.Account, p.Provider, p.Model, p.SyncedAt} {
		if len(s) > maxField {
			return false
		}
	}
	if p.ID == "" || p.DeviceID == "" {
		return false
	}
	horizon := time.Now().Add(24 * time.Hour)
	if p.Time < 0 || p.Time > horizon.UnixMilli() {
		return false
	}
	if t, err := time.Parse(time.RFC3339Nano, p.SyncedAt); err == nil && t.After(horizon) {
		return false
	}
	return true
}

// once is one turn of the loop: nothing at all happens while the switch is off.
func (s *Syncer) once(lastPull *time.Time) error {
	s.mu.Lock()
	c := s.cfg
	s.mu.Unlock()
	if !c.SyncEnabled {
		return nil
	}
	if err := s.push(c); err != nil {
		return err
	}
	if time.Since(*lastPull) < pullEvery {
		return nil
	}
	if err := s.pull(c); err != nil {
		return err
	}
	*lastPull = time.Now()
	return nil
}

// Run loops forever: woken by Save and by Settings, otherwise ticking. Failures
// back off up to five minutes, but a wake always cuts the wait short.
func (s *Syncer) Run() {
	backoff := 5 * time.Second
	var lastPull time.Time
	for {
		select {
		case <-s.poke:
		case <-time.After(5 * time.Second):
		}
		if err := s.once(&lastPull); err != nil {
			log.Print("sync: ", err)
			select {
			case <-s.poke:
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 5*time.Minute)
		} else {
			backoff = 5 * time.Second
		}
	}
}

// Wake nudges the loop without ever blocking the caller.
func (s *Syncer) Wake() {
	select {
	case s.poke <- struct{}{}:
	default:
	}
}

type deviceStat struct {
	DeviceID string `json:"device_id"`
	Device   string `json:"device"`
	Rows     int    `json:"rows"`
	LastTime int64  `json:"last_time"`
}

// view is the GET /api/settings body: the settings with the key redacted to a
// boolean, plus everything the Sync section shows about progress.
func (s *Syncer) view() map[string]any {
	byDevice := map[string]*deviceStat{}
	remote := s.store.Remote()
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
	devices := []deviceStat{}
	for _, d := range byDevice {
		devices = append(devices, *d)
	}
	local := s.store.Len()
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]any{
		"settings": map[string]any{"sync_enabled": s.cfg.SyncEnabled, "device_name": s.cfg.DeviceName, "url": s.cfg.URL, "key_set": s.cfg.Key != ""},
		"status": map[string]any{
			"device_id": s.st.DeviceID, "pushed": s.st.Pushed, "pending": local - s.st.Pushed,
			"last_push_ok": s.lastPushOK, "last_push_error": s.lastPushErr, "last_push_error_at": s.lastPushErrAt,
			"last_pull_ok": s.lastPullOK, "last_pull_error": s.lastPullErr, "last_pull_error_at": s.lastPullErrAt,
			"remote_rows": len(remote), "devices": devices,
		},
	}
}

// ServeHTTP is GET/PUT /api/settings.
func (s *Syncer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// The server-wide gate (internal/local) already rejects DNS-rebound names;
	// the endpoint that stores the key stays safe on its own too.
	if !local.Host(r.Host) {
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
		s.mu.Lock()
		prev := s.cfg
		s.mu.Unlock()
		next := prev
		next.SyncEnabled, next.DeviceName, next.URL = in.SyncEnabled, strings.TrimSpace(in.DeviceName), strings.TrimRight(strings.TrimSpace(in.URL), "/")
		if in.Key != nil {
			next.Key = strings.TrimSpace(*in.Key)
		}
		// The stored key is only ever sent to the URL it was saved with. A different
		// project has a different key, so a URL change must bring one; otherwise
		// anyone who can reach this port could point the probe at a host they own.
		if next.URL != prev.URL && next.URL != "" && in.Key == nil && prev.Key != "" {
			http.Error(w, "the project URL changed: enter the secret key again (each project has its own key, and the stored one is only sent to the URL it was saved with)", http.StatusBadRequest)
			return
		}
		if err := s.validate(next); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if next.SyncEnabled {
			if err := s.probe(next.URL, next.Key); err != nil {
				http.Error(w, "could not reach the table: "+err.Error(), http.StatusBadRequest)
				return
			}
		}
		s.mu.Lock()
		urlChanged, nameChanged := next.URL != s.cfg.URL, next.DeviceName != s.cfg.DeviceName
		if urlChanged { // a different project: start over
			s.st.Pushed, s.st.PullCursor = 0, ""
			writeJSON(s.statePath, s.st)
		}
		s.cfg = next
		err := writeJSON(s.configPath, s.cfg)
		s.mu.Unlock()
		if urlChanged {
			s.store.ClearRemote()
		}
		if nameChanged {
			s.store.SetDevice(next.DeviceName)
		}
		if err != nil {
			http.Error(w, "could not write "+s.configPath+": "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.Wake()
	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.view())
}

func (s *Syncer) validate(c settings) error {
	if c.DeviceName == "" {
		return errors.New("device name is required")
	}
	if c.URL != "" {
		u, err := url.Parse(c.URL)
		if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && local.Host(u.Host))) {
			return errors.New("project URL must be https://<host> with nothing after the host, like https://<ref>.supabase.co")
		}
	}
	if c.Key != "" {
		if err := validKey(c.Key); err != nil {
			return err
		}
	}
	if c.SyncEnabled && (c.URL == "" || c.Key == "") {
		return errors.New("a project URL and secret key are needed before sync can be enabled")
	}
	return nil
}
