package codexrouting

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"garcon/internal/database"
	"io"
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
	PinnedAccount string         `json:"pinned_account,omitempty"`
	Enabled       bool           `json:"enabled"`
	Accounts      []string       `json:"accounts"`
	Priorities    map[string]int `json:"priorities,omitempty"`
}
type health struct {
	models  map[string]bool
	checked time.Time
	problem string
}
type Account struct {
	ID            string          `json:"id"`
	Priority      int             `json:"priority"`
	Score         *float64        `json:"score"`
	HoursLeft     *float64        `json:"hours_left"`
	Profile       string          `json:"profile"`
	Windows       []limits.Window `json:"windows"`
	Email         string          `json:"email"`
	Plan          string          `json:"plan"`
	Enrolled      bool            `json:"enrolled"`
	Status        string          `json:"status"`
	Remaining     *float64        `json:"remaining_percent"`
	Active        int             `json:"active_requests"`
	Conversations int             `json:"conversations"`
}
type Status struct {
	PinnedAccount string    `json:"pinned_account"`
	NextAccount   string    `json:"next_account"`
	Enabled       bool      `json:"enabled"`
	Accounts      []Account `json:"accounts"`
	Error         string    `json:"error,omitempty"`
}
type Router struct {
	db           *database.DB
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

func New(dir string, snapshot func() limits.Snapshot, databases ...*database.DB) *Router {
	home, _ := os.UserHomeDir()
	r := &Router{dir: dir, pins: map[string]string{}, health: map[string]health{}, active: map[string]int{}, cooldown: map[string]time.Time{}, forceRefresh: map[string]bool{}, discover: func() []credential { return discover(home) }, renew: renew, snapshot: snapshot, now: time.Now,
		client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	if len(databases) > 0 {
		r.db = databases[0]
		if err := r.db.Get("codex-routing", &r.config); err != nil && err != sql.ErrNoRows {
			r.problem = "Cannot read Codex routing configuration"
		}
		rows, err := r.db.Query("SELECT key,account_id FROM sessions")
		if err != nil {
			r.problem = "Cannot read Codex conversation assignments"
		} else {
			for rows.Next() {
				var k, id string
				if rows.Scan(&k, &id) == nil {
					r.pins[k] = id
				}
			}
			if rows.Err() != nil {
				r.problem = "Cannot read Codex conversation assignments"
			}
			rows.Close()
		}
	} else {
		if err := readJSON(filepath.Join(dir, "codex-routing.json"), &r.config); err != nil && !os.IsNotExist(err) {
			r.problem = "Cannot read Codex routing configuration"
		}
		if err := readJSON(filepath.Join(dir, "codex-routing-pins.json"), &r.pins); err != nil && !os.IsNotExist(err) {
			r.problem = "Cannot read Codex conversation assignments"
		}
	}
	if r.pins == nil {
		r.pins = map[string]string{}
	}
	// Routing is intrinsic to the harness. Preserve saved pool choices, and
	// initialize a new pool once from the logins already on this machine.
	r.config.Enabled = true
	if r.config.Accounts == nil && r.problem == "" {
		r.config.Accounts = []string{}
		for _, c := range r.discover() {
			r.config.Accounts = append(r.config.Accounts, c.id)
		}
		if err := r.persistConfig(r.config); err != nil {
			r.problem = "Cannot save Codex account pool"
		}
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
	return r.ConfigurePriorities(enabled, selected, nil)
}
func (r *Router) ConfigurePriorities(enabled bool, selected []string, priorities map[string]int) error {
	return r.configure(enabled, selected, priorities, nil)
}

// Pin overrides automatic and conversation routing until explicitly cleared.
func (r *Router) Pin(id string) error {
	return r.configure(false, nil, nil, &id)
}

func (r *Router) configure(enabled bool, selected []string, priorities map[string]int, pinned *string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	config := r.config
	// The legacy enabled field is accepted, but routing is always automatic.
	config.Enabled = true
	if enabled || selected != nil {
		config.Accounts = []string{}
		available := map[string]bool{}
		for _, id := range r.config.Accounts {
			available[id] = true
		}
		for _, c := range r.discover() {
			available[c.id] = true
			config.Accounts = append(config.Accounts, c.id)
		}
		if selected != nil {
			config.Accounts = []string{}
			for _, id := range selected {
				if !available[id] {
					return errors.New("account is missing or duplicated")
				}
				delete(available, id)
				config.Accounts = append(config.Accounts, id)
			}
		}
	}
	if priorities != nil {
		available := map[string]bool{}
		for _, id := range r.config.Accounts {
			available[id] = true
		}
		for _, c := range r.discover() {
			available[c.id] = true
		}
		for id, p := range priorities {
			if !available[id] || p < 1 || p > 99 {
				return errors.New("priority must be between 1 and 99 for a discovered account")
			}
		}
		config.Priorities = priorities
	}
	if pinned != nil {
		config.PinnedAccount = *pinned
		if *pinned != "" {
			found := false
			for _, c := range r.discover() {
				found = found || c.id == *pinned
			}
			if !found {
				return errors.New("pinned account must have a local login")
			}
		}
	}
	if config.PinnedAccount != "" {
		found := false
		for _, id := range config.Accounts {
			found = found || id == config.PinnedAccount
		}
		if !found {
			return errors.New("unpin the account before removing it from the pool")
		}
	}
	if err := r.persistConfig(config); err != nil {
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
	return limits.Headroom(a, now, func(w limits.Window) bool { return strings.HasPrefix(w.ID, "codex:") })
}

func (r *Router) account(c credential, model string, snapshot limits.Snapshot) Account {
	a := Account{ID: c.id, Email: c.email, Plan: c.plan, Enrolled: r.enrolled(c.id), Status: "Not enrolled", Active: r.active[c.id], Priority: max(1, r.config.Priorities[c.id]), Profile: filepath.Base(c.dir), Windows: []limits.Window{}}
	for _, id := range r.pins {
		if id == c.id {
			a.Conversations++
		}
	}
	a.Status = "Waiting for fresh usage limits"
	for _, q := range snapshot.Accounts {
		if q.Provider == "codex" && q.Workspace == c.id {
			value, status := headroom(q, r.now())
			a.Windows = q.Windows
			a.Status = status
			if status == "Ready" || status == "Usage exhausted" {
				a.Remaining = &value
				score, hours := quotaScore(q, r.now())
				if score >= 0 {
					a.Score = &score
					a.HoursLeft = &hours
				} else if status == "Ready" {
					a.Status = "Waiting for a reset time"
				}
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
	if !a.Enrolled {
		a.Status = "Not enrolled"
	}
	return a
}

func (r *Router) Status() Status {
	creds, snapshot := r.discover(), r.snapshot()
	r.mu.Lock()
	defer r.mu.Unlock()
	s := Status{Enabled: r.config.Enabled, Error: r.problem, Accounts: []Account{}, PinnedAccount: r.config.PinnedAccount}
	var next Account
	seen := map[string]bool{}
	for _, c := range creds {
		a := r.account(c, "", snapshot)
		s.Accounts = append(s.Accounts, a)
		if r.problem == "" && (s.PinnedAccount == "" || s.PinnedAccount == a.ID) && eligible(a) && betterAccount(a, next) {
			next = a
			s.NextAccount = a.ID
		}
		seen[c.id] = true
	}
	for _, id := range r.config.Accounts {
		if !seen[id] {
			s.Accounts = append(s.Accounts, Account{ID: id, Priority: max(1, r.config.Priorities[id]), Enrolled: true, Status: "Saved login missing", Windows: []limits.Window{}})
		}
	}
	sort.Slice(s.Accounts, func(i, j int) bool {
		a, b := s.Accounts[i], s.Accounts[j]
		if (a.Score == nil) != (b.Score == nil) {
			return a.Score != nil
		}
		if a.Score != nil && b.Score != nil && *a.Score != *b.Score {
			return *a.Score > *b.Score
		}
		return a.Email < b.Email
	})
	return s
}

func eligible(a Account) bool { return a.Enrolled && a.Status == "Ready" && a.Score != nil }

// Use the same deterministic tie-break for the status indicator and requests.
func betterAccount(a, b Account) bool {
	return b.ID == "" || *a.Score > *b.Score || (*a.Score == *b.Score && a.ID < b.ID)
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
// Without a manual override, continuations stay on their original account. No automatic
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
	*req = *req.WithContext(context.WithValue(req.Context(), detailKey{}, RequestDetails{Session: session, Model: body.Model}))
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
	pinned := r.config.PinnedAccount
	if pinned == "" {
		pinned = r.pins[key]
	}
	if pinned == "" && hasState {
		pinned = req.Header.Get("ChatGPT-Account-Id")
		if pinned == "" {
			return nil, &routingError{409, "Cannot determine the original account for this existing Codex conversation; start a new conversation"}
		}
	}
	var chosen credential
	var best Account
	for _, c := range creds {
		if pinned != "" && c.id != pinned {
			continue
		}
		a := r.account(c, body.Model, snapshot)
		if !eligible(a) {
			continue
		}
		// Legacy priorities remain readable for API compatibility. All eligible
		// accounts now compete on quota per hour within one smart-routing pool.
		if betterAccount(a, best) {
			chosen = c
			best = a
		}
	}
	if chosen.id == "" {
		message := "No enrolled Codex account has fresh available quota and access to this model"
		if pinned != "" {
			message = "The conversation's Codex account is unavailable; refresh its login or limits, or start a new conversation"
		}
		if r.config.PinnedAccount != "" {
			message = "The pinned Codex account is unavailable for this request; refresh its login or limits, or unpin it in Usage"
		}
		return nil, &routingError{503, message}
	}
	if key != "" {
		previous := r.pins[key]
		if previous == "" && r.db == nil && len(r.pins) >= 8000 {
			return nil, &routingError{503, "Codex conversation assignment store is full"}
		}
		r.pins[key] = chosen.id
		if previous != chosen.id || r.db != nil {
			if err := r.persistPin(key, session, chosen, body.Model); err != nil {
				if previous == "" {
					delete(r.pins, key)
				} else {
					r.pins[key] = previous
				}
				return nil, &routingError{503, "Could not persist Codex conversation assignment"}
			}
		}
	}
	*req = *req.WithContext(context.WithValue(req.Context(), detailKey{}, RequestDetails{Session: session, Model: body.Model, Account: chosen.email, AccountID: chosen.id}))
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

// The most constrained general quota window determines the account's score.
func quotaScore(a limits.Account, now time.Time) (float64, float64) {
	return limits.QuotaScore(a, now, func(w limits.Window) bool { return strings.HasPrefix(w.ID, "codex:") })
}

func (r *Router) persistConfig(c Config) error {
	if r.db != nil {
		return r.db.Put("codex-routing", c)
	}
	return writeJSON(filepath.Join(r.dir, "codex-routing.json"), c)
}
func (r *Router) persistPin(key, session string, c credential, model string) error {
	if r.db == nil {
		return writeJSON(filepath.Join(r.dir, "codex-routing-pins.json"), r.pins)
	}
	now := r.now().UnixMilli()
	_, err := r.db.Exec(`INSERT INTO sessions(key,session_id,account_id,account,model,created_at,last_seen) VALUES(?,?,?,?,?,?,?)
 ON CONFLICT(key) DO UPDATE SET session_id=excluded.session_id,account_id=excluded.account_id,account=excluded.account,model=CASE WHEN excluded.model!='' THEN excluded.model ELSE sessions.model END,last_seen=excluded.last_seen,
 created_at=CASE WHEN sessions.created_at=0 THEN excluded.created_at ELSE sessions.created_at END`, key, session, c.id, c.email, model, now, now)
	return err
}

type detailKey struct{}
type RequestDetails struct{ Session, Model, Account, AccountID string }

func Details(req *http.Request) RequestDetails {
	v, _ := req.Context().Value(detailKey{}).(RequestDetails)
	return v
}
func ErrorStatus(err error) int {
	var e *routingError
	if errors.As(err, &e) {
		return e.status
	}
	return 503
}
