package codexrouting

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"garcon/internal/limits"
	"garcon/internal/local"
)

type Config struct {
	Enabled  bool     `json:"enabled"`
	Accounts []string `json:"accounts"`
}
type health struct {
	models  map[string]bool
	checked time.Time
	problem string
}
type Account struct {
	ID            string   `json:"id"`
	Email         string   `json:"email"`
	Plan          string   `json:"plan"`
	Enrolled      bool     `json:"enrolled"`
	Status        string   `json:"status"`
	Remaining     *float64 `json:"remaining_percent"`
	Active        int      `json:"active_requests"`
	Conversations int      `json:"conversations"`
}
type Status struct {
	Enabled  bool      `json:"enabled"`
	Accounts []Account `json:"accounts"`
	Error    string    `json:"error,omitempty"`
}
type Router struct {
	mu           sync.Mutex
	refreshMu    sync.Mutex
	dir          string
	config       Config
	problem      string
	pins         map[string]string
	health       map[string]health
	active       map[string]int
	cooldown     map[string]time.Time
	forceRefresh map[string]bool
	discover     func() []credential
	renew        func(context.Context, credential) error
	client       *http.Client
	snapshot     func() limits.Snapshot
	now          func() time.Time
}

func New(dir string, snapshot func() limits.Snapshot) *Router {
	home, _ := os.UserHomeDir()
	r := &Router{dir: dir, pins: map[string]string{}, health: map[string]health{}, active: map[string]int{}, cooldown: map[string]time.Time{}, forceRefresh: map[string]bool{}, discover: func() []credential { return discover(home) }, renew: renew, snapshot: snapshot, now: time.Now,
		client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	if err := readJSON(filepath.Join(dir, "codex-routing.json"), &r.config); err != nil && !os.IsNotExist(err) {
		r.problem = "Cannot read Codex routing configuration"
	}
	if err := readJSON(filepath.Join(dir, "codex-routing-pins.json"), &r.pins); err != nil && !os.IsNotExist(err) {
		r.problem = "Cannot read Codex conversation assignments"
	}
	if r.pins == nil {
		r.pins = map[string]string{}
	}
	return r
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".routing-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = json.NewEncoder(f).Encode(value); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func (r *Router) enrolled(id string) bool {
	for _, allowed := range r.config.Accounts {
		if allowed == id {
			return true
		}
	}
	return false
}

// Configure enrolls only the currently discovered accounts. Later logins need
// explicit enrollment, preventing an unrelated workspace silently joining a pool.
func (r *Router) Configure(enabled bool) error {
	return r.ConfigureAccounts(enabled, nil)
}

func (r *Router) ConfigureAccounts(enabled bool, selected []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	config := r.config
	config.Enabled = enabled
	if enabled {
		config.Accounts = nil
		available := map[string]bool{}
		for _, c := range r.discover() {
			available[c.id] = true
			config.Accounts = append(config.Accounts, c.id)
		}
		if selected != nil {
			config.Accounts = nil
			for _, id := range selected {
				if !available[id] {
					return errors.New("account is missing or duplicated")
				}
				delete(available, id)
				config.Accounts = append(config.Accounts, id)
			}
		}
		if len(config.Accounts) == 0 {
			return errors.New("no file-based Codex OAuth logins found")
		}
	}
	if err := writeJSON(filepath.Join(r.dir, "codex-routing.json"), config); err != nil {
		return err
	}
	r.config = config
	return nil
}

func (r *Router) Enabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.config.Enabled || r.problem != ""
}

func (r *Router) Run(ctx context.Context) {
	r.Refresh(ctx)
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.Refresh(ctx)
		}
	}
}

// Refresh verifies credentials and model access without generating any tokens.
func (r *Router) Refresh(ctx context.Context) {
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()
	for _, c := range r.discover() {
		r.mu.Lock()
		enabled := r.config.Enabled && r.enrolled(c.id)
		force := r.forceRefresh[c.id]
		r.mu.Unlock()
		if !enabled || ctx.Err() != nil {
			continue
		}
		h := health{checked: r.now()}
		if force || c.expires.Before(r.now().Add(5*time.Minute)) {
			if !c.refresh || r.renew(ctx, c) != nil {
				h.problem = "Login needs refresh"
			} else if fresh, err := loadCredential(c.dir); err != nil || fresh.id != c.id {
				h.problem = "Login changed during renewal"
			} else {
				c = fresh
				r.mu.Lock()
				delete(r.forceRefresh, c.id)
				r.mu.Unlock()
			}
		}
		if h.problem == "" {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://chatgpt.com/backend-api/codex/models?client_version=0.155.0", nil)
			req.Header.Set("Authorization", "Bearer "+c.token)
			req.Header.Set("ChatGPT-Account-Id", c.id)
			req.Header.Set("User-Agent", "codex_cli_rs/0.155.0")
			res, err := r.client.Do(req)
			if err != nil {
				h.problem = "Model availability check failed"
			} else {
				var catalog struct {
					Models []struct {
						Slug string `json:"slug"`
					} `json:"models"`
				}
				err = json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&catalog)
				res.Body.Close()
				if res.StatusCode != 200 || err != nil || len(catalog.Models) == 0 {
					h.problem = "Model availability check failed"
				} else {
					h.models = map[string]bool{}
					for _, m := range catalog.Models {
						h.models[m.Slug] = true
					}
				}
				if res.StatusCode == 401 {
					r.mu.Lock()
					r.forceRefresh[c.id] = true
					r.mu.Unlock()
					h.problem = "Login needs refresh"
				}
			}
		}
		r.mu.Lock()
		r.health[c.id] = h
		r.mu.Unlock()
	}
}

// Score uses the tightest reported general allowance, never treating missing or
// expired windows as unused quota. Percentages across different plans are a
// headroom heuristic, not a claim about absolute tokens remaining.
func headroom(a limits.Account, now time.Time) (float64, string) {
	if a.Status != "fresh" || a.FetchedAt <= 0 || now.UnixMilli()-a.FetchedAt > 600000 {
		return 0, "Waiting for fresh usage limits"
	}
	remaining, found := 100.0, false
	for _, w := range a.Windows {
		if !strings.HasPrefix(w.ID, "codex:") {
			continue
		}
		if w.UsedPercent == nil || math.IsNaN(*w.UsedPercent) || math.IsInf(*w.UsedPercent, 0) || *w.UsedPercent < 0 || w.Expired || (w.ResetsAt > 0 && w.ResetsAt <= now.UnixMilli()) {
			return 0, "Waiting for fresh usage limits"
		}
		found = true
		remaining = math.Min(remaining, math.Max(0, 100-*w.UsedPercent))
	}
	if !found {
		return 0, "Usage limits unavailable"
	}
	if remaining <= 0 {
		return 0, "Usage exhausted"
	}
	return remaining, "Ready"
}

func (r *Router) account(c credential, model string, snapshot limits.Snapshot) Account {
	a := Account{ID: c.id, Email: c.email, Plan: c.plan, Enrolled: r.enrolled(c.id), Status: "Not enrolled", Active: r.active[c.id]}
	for _, id := range r.pins {
		if id == c.id {
			a.Conversations++
		}
	}
	if !a.Enrolled {
		return a
	}
	a.Status = "Waiting for fresh usage limits"
	for _, q := range snapshot.Accounts {
		if q.Provider == "codex" && q.Workspace == c.id {
			value, status := headroom(q, r.now())
			a.Status = status
			if status == "Ready" || status == "Usage exhausted" {
				a.Remaining = &value
			}
			break
		}
	}
	h := r.health[c.id]
	if c.expires.Before(r.now().Add(time.Minute)) || r.forceRefresh[c.id] {
		a.Status = "Login needs refresh"
	} else if h.problem != "" {
		a.Status = h.problem
	} else if h.models == nil || r.now().Sub(h.checked) > 10*time.Minute {
		a.Status = "Checking model availability"
	} else if model != "" && !h.models[model] {
		a.Status = "Model unavailable on this account"
	}
	if r.now().Before(r.cooldown[c.id]) {
		a.Status = "Temporarily unavailable after an upstream error"
	}
	return a
}

func (r *Router) Status() Status {
	creds, snapshot := r.discover(), r.snapshot()
	r.mu.Lock()
	defer r.mu.Unlock()
	s := Status{Enabled: r.config.Enabled, Error: r.problem, Accounts: []Account{}}
	seen := map[string]bool{}
	for _, c := range creds {
		s.Accounts = append(s.Accounts, r.account(c, "", snapshot))
		seen[c.id] = true
	}
	for _, id := range r.config.Accounts {
		if !seen[id] {
			s.Accounts = append(s.Accounts, Account{ID: id, Enrolled: true, Status: "Saved login missing"})
		}
	}
	sort.Slice(s.Accounts, func(i, j int) bool { return s.Accounts[i].Email < s.Accounts[j].Email })
	return s
}

type routingError struct {
	status  int
	message string
}

func (e *routingError) Error() string { return e.message }

func localRequest(req *http.Request) bool {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil || !local.Host(host) || !local.Host(req.Host) {
		return false
	}
	if origin := req.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != req.Host {
			return false
		}
	}
	return true
}

// Prepare changes only outbound identity headers. Assignments survive restarts.
// Unknown continuations stay on their original enrolled account. No automatic
// replay or cross-account switch is performed after a request has started.
func (r *Router) Prepare(req *http.Request) (func(), error) {
	if !localRequest(req) {
		return nil, &routingError{403, "Codex account routing requires a same-origin local client"}
	}
	if strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
		return nil, &routingError{426, "Codex routing currently requires HTTP streaming; disable provider WebSockets"}
	}
	var body struct {
		Model    string            `json:"model"`
		Previous string            `json:"previous_response_id"`
		CacheKey string            `json:"prompt_cache_key"`
		Metadata map[string]string `json:"client_metadata"`
	}
	hasState := req.Header.Get("X-Codex-Turn-State") != ""
	if req.Body != nil && req.Body != http.NoBody {
		b, err := io.ReadAll(io.LimitReader(req.Body, 32<<20+1))
		req.Body.Close()
		if err != nil || len(b) > 32<<20 {
			return nil, &routingError{413, "Codex routing request exceeds 32 MiB or could not be read"}
		}
		req.Body = io.NopCloser(bytes.NewReader(b))
		if len(b) > 0 && json.Unmarshal(b, &body) != nil {
			return nil, &routingError{400, "Invalid Codex request JSON"}
		}
		hasState = hasState || body.Previous != "" || bytes.Contains(b, []byte(`"encrypted_content"`))
	}
	session := ""
	for _, value := range []string{req.Header.Get("Session_id"), req.Header.Get("Session-Id"), body.Metadata["session_id"], req.Header.Get("Thread-Id"), body.Metadata["thread_id"], body.CacheKey} {
		if value != "" {
			session = value
			break
		}
	}
	if len(session) > 512 {
		return nil, &routingError{400, "Codex session identifier is too long"}
	}
	if session == "" && req.Method != http.MethodGet {
		return nil, &routingError{400, "A stable Codex session identifier is required for account routing"}
	}
	key := ""
	if session != "" {
		key = fmt.Sprintf("%x", sha256.Sum256([]byte(session)))
	}
	creds, snapshot := r.discover(), r.snapshot()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.problem != "" {
		return nil, &routingError{503, r.problem}
	}
	pinned := r.pins[key]
	if pinned == "" && hasState {
		pinned = req.Header.Get("ChatGPT-Account-Id")
		if pinned == "" {
			return nil, &routingError{409, "Cannot determine the original account for this existing Codex conversation; start a new conversation"}
		}
	}
	var chosen credential
	best := -1.0
	for _, c := range creds {
		if pinned != "" && c.id != pinned {
			continue
		}
		a := r.account(c, body.Model, snapshot)
		if a.Status != "Ready" || !a.Enrolled || a.Remaining == nil {
			continue
		}
		score := *a.Remaining / float64(1+a.Active)
		if score > best {
			chosen = c
			best = score
		}
	}
	if chosen.id == "" {
		message := "No enrolled Codex account has fresh available quota and access to this model"
		if pinned != "" {
			message = "The conversation's Codex account is unavailable; refresh its login or limits, or start a new conversation"
		}
		return nil, &routingError{503, message}
	}
	if key != "" && r.pins[key] == "" {
		if len(r.pins) >= 8000 {
			return nil, &routingError{503, "Codex conversation assignment store is full"}
		}
		r.pins[key] = chosen.id
		if err := writeJSON(filepath.Join(r.dir, "codex-routing-pins.json"), r.pins); err != nil {
			delete(r.pins, key)
			return nil, &routingError{503, "Could not persist Codex conversation assignment"}
		}
	}
	for _, h := range []string{"Authorization", "ChatGPT-Account-Id", "OpenAI-Organization", "OpenAI-Project", "X-Api-Key", "Cookie"} {
		req.Header.Del(h)
	}
	req.Header.Set("Authorization", "Bearer "+chosen.token)
	req.Header.Set("ChatGPT-Account-Id", chosen.id)
	r.active[chosen.id]++
	var once sync.Once
	return func() { once.Do(func() { r.mu.Lock(); r.active[chosen.id]--; r.mu.Unlock() }) }, nil
}

func WriteError(w http.ResponseWriter, err error) {
	status := 503
	var e *routingError
	if errors.As(err, &e) {
		status = e.status
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": err.Error(), "type": "garcon_routing_error", "code": "codex_account_unavailable"}})
}

func (r *Router) Observe(id string, res *http.Response) {
	if res.StatusCode != 401 && res.StatusCode != 403 && res.StatusCode != 429 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delay := time.Minute
	if seconds, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && seconds > 0 {
		delay = time.Duration(min(seconds, 3600)) * time.Second
	} else if t, err := http.ParseTime(res.Header.Get("Retry-After")); err == nil {
		delay = min(time.Hour, max(time.Minute, t.Sub(r.now())))
	}
	r.cooldown[id] = r.now().Add(delay)
	if res.StatusCode == 401 {
		r.forceRefresh[id] = true
	}
}
