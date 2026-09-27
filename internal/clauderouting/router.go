// Package clauderouting assigns Claude gateway sessions to local subscription
// accounts. It shares Codex's quota-per-hour policy, but keeps its own pins.
package clauderouting

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"garcon/internal/claude"
	"garcon/internal/codexrouting"
	"garcon/internal/database"
	"garcon/internal/limits"
	"garcon/internal/proxy"
)

type candidate struct {
	ID, Email, Profile, Plan string
	TokenHash                [32]byte
	Expires                  int64
	Models                   map[string]bool
	Checked                  time.Time
	Problem                  string
}
type pool struct {
	Accounts []string `json:"accounts"`
}
type Router struct {
	mu          sync.Mutex
	refreshMu   sync.Mutex
	db          *database.DB
	home, extra string
	config      pool
	problem     string
	candidates  []candidate
	active      map[string]int
	cooldown    map[string]time.Time
	snapshot    func() limits.Snapshot
	now         func() time.Time
	load        func(context.Context, string) (claude.Credentials, error)
	renew       func(context.Context, string) (bool, error)
	client      *http.Client
}

func New(db *database.DB, snapshot func() limits.Snapshot, extra string) (*Router, error) {
	home, _ := os.UserHomeDir()
	r := &Router{db: db, home: home, extra: extra, snapshot: snapshot, now: time.Now, load: claude.LoadContext, renew: claude.NewRenewer().Refresh, active: map[string]int{}, cooldown: map[string]time.Time{}, client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	// Separate table prevents Claude session IDs from colliding with Codex pins.
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS claude_routing_sessions (
 key TEXT PRIMARY KEY,session_id TEXT NOT NULL,account_id TEXT NOT NULL,
 account TEXT NOT NULL,model TEXT NOT NULL,created_at INTEGER NOT NULL,last_seen INTEGER NOT NULL)`)
	if err != nil {
		return nil, err
	}
	if err = db.Get("claude-routing", &r.config); err != nil && err != sql.ErrNoRows {
		return nil, errors.New("cannot read Claude account pool")
	}
	return r, nil
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
func (r *Router) profiles() []string {
	dirs, _ := filepath.Glob(filepath.Join(r.home, ".claude-*"))
	dirs = append([]string{filepath.Join(r.home, ".claude"), r.extra}, dirs...)
	dirs = append(dirs, os.Getenv("CLAUDE_CONFIG_DIR"))
	seen := map[string]bool{}
	out := []string{}
	for _, p := range dirs {
		if p == "" {
			continue
		}
		p, _ = filepath.Abs(p)
		if seen[p] {
			continue
		}
		seen[p] = true
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			out = append(out, p)
		}
		if len(out) >= 128 {
			break
		}
	}
	return out
}
func (r *Router) get(ctx context.Context, path, token string, out any) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.anthropic.com"+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Anthropic-Beta", "oauth-2025-04-20")
	req.Header.Set("Anthropic-Version", "2023-06-01")
	res, err := r.client.Do(req)
	if err != nil {
		return errors.New("Claude availability check failed")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("Claude availability check returned HTTP %d", res.StatusCode)
	}
	if json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(out) != nil {
		return errors.New("invalid Claude availability response")
	}
	return nil
}
func (r *Router) Refresh(ctx context.Context) {
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()
	r.mu.Lock()
	old := append([]candidate(nil), r.candidates...)
	r.mu.Unlock()
	candidates := []candidate{}
	seen := map[string]bool{}
	for _, dir := range r.profiles() {
		if ctx.Err() != nil {
			return
		}
		var c candidate
		for _, previous := range old {
			if previous.Profile == dir {
				c = previous
				break
			}
		}
		c.Profile = dir
		c.Problem = ""
		_, err := r.renew(ctx, dir)
		creds, loadErr := r.load(ctx, dir)
		if err != nil || loadErr != nil || creds.AccessToken == "" || creds.ExpiresAt <= r.now().Add(time.Minute).UnixMilli() {
			if c.ID != "" {
				c.Problem = "Login needs refresh"
				candidates = append(candidates, c)
				seen[c.ID] = true
			}
			continue
		}
		var identity struct {
			Account      struct{ UUID, Email string }
			Organization struct{ UUID string }
		}
		if err = r.get(ctx, "/api/oauth/profile", creds.AccessToken, &identity); err != nil || identity.Account.UUID == "" {
			if c.ID != "" {
				c.Problem = "Account identity unavailable"
				candidates = append(candidates, c)
				seen[c.ID] = true
			}
			continue
		}
		c.ID = limits.AccountID("claude", identity.Account.UUID, identity.Organization.UUID)
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		c.Email = identity.Account.Email
		c.Plan = creds.SubscriptionType
		c.TokenHash = sha256.Sum256([]byte(creds.AccessToken))
		c.Expires = creds.ExpiresAt
		c.Checked = r.now()
		var catalog struct{ Data []struct{ ID string } }
		if err = r.get(ctx, "/v1/models", creds.AccessToken, &catalog); err != nil || len(catalog.Data) == 0 {
			c.Problem = "Model availability check failed"
		} else {
			c.Models = map[string]bool{}
			for _, m := range catalog.Data {
				c.Models[m.ID] = true
			}
		}
		candidates = append(candidates, c)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	r.mu.Lock()
	defer r.mu.Unlock()
	r.candidates = candidates
	// Like Codex, enroll the initial discovered set once; later logins do not
	// silently join an existing pool. An empty saved pool remains empty.
	if r.config.Accounts == nil && len(candidates) > 0 {
		config := pool{Accounts: []string{}}
		for _, c := range candidates {
			config.Accounts = append(config.Accounts, c.ID)
		}
		if err := r.db.Put("claude-routing", config); err != nil {
			r.problem = "Cannot save Claude account pool"
		} else {
			r.config = config
			r.problem = ""
		}
	}
}
func (r *Router) enrolled(id string) bool {
	for _, v := range r.config.Accounts {
		if v == id {
			return true
		}
	}
	return false
}

// General limits and OAuth-app limits apply to every gateway request. Model
// scopes constrain only that model family; Cowork-only limits do not apply to Code.
func relevant(w limits.Window, model string) bool {
	label := strings.ToLower(w.ID + " " + w.Label)
	model = strings.ToLower(model)
	if strings.Contains(label, "cowork") {
		return false
	}
	for _, family := range []string{"opus", "sonnet", "fable", "haiku"} {
		if strings.Contains(label, family) {
			return strings.Contains(model, family)
		}
	}
	return true
}

// Claude reports an unused rolling five-hour window as explicitly 0% used
// with no reset. Its maximum duration is known, so conservatively score all five
// hours. Other missing reset times remain unavailable, exactly as for Codex.
func quotaWindows(a limits.Account, now time.Time) limits.Account {
	a.Windows = append([]limits.Window(nil), a.Windows...)
	for i, w := range a.Windows {
		if (w.ID == "five_hour" || w.ID == "session:5-hour") && w.WindowSeconds == 18000 && w.UsedPercent != nil && *w.UsedPercent == 0 && w.ResetsAt == 0 && !w.Expired {
			a.Windows[i].ResetsAt = now.Add(5 * time.Hour).UnixMilli()
		}
	}
	return a
}

func (r *Router) account(c candidate, model string, snapshot limits.Snapshot) codexrouting.Account {
	a := codexrouting.Account{ID: c.ID, Email: c.Email, Plan: c.Plan, Profile: filepath.Base(c.Profile), Priority: 1, Enrolled: r.enrolled(c.ID), Active: r.active[c.ID], Windows: []limits.Window{}, Status: "Waiting for fresh usage limits"}
	include := func(w limits.Window) bool { return relevant(w, model) }
	for _, q := range snapshot.Accounts {
		if q.Provider == "claude" && q.ID == c.ID {
			a.Windows = q.Windows
			scored := quotaWindows(q, r.now())
			remaining, status := limits.Headroom(scored, r.now(), include)
			a.Status = status
			if status == "Ready" || status == "Usage exhausted" {
				a.Remaining = &remaining
				score, hours := limits.QuotaScore(scored, r.now(), include)
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
	if c.Expires <= r.now().Add(time.Minute).UnixMilli() {
		a.Status = "Login needs refresh"
	} else if c.Problem != "" {
		a.Status = c.Problem
	} else if c.Models == nil || r.now().Sub(c.Checked) > 10*time.Minute {
		a.Status = "Checking model availability"
	} else if model != "" && !c.Models[model] {
		a.Status = "Model unavailable on this account"
	}
	if r.now().Before(r.cooldown[c.ID]) {
		a.Status = "Temporarily unavailable after an upstream error"
	}
	if !a.Enrolled {
		a.Status = "Not enrolled"
	}
	return a
}
func (r *Router) Status() codexrouting.Status {
	snapshot := r.snapshot()
	r.mu.Lock()
	defer r.mu.Unlock()
	s := codexrouting.Status{Enabled: true, Error: r.problem, Accounts: []codexrouting.Account{}}
	counts := map[string]int{}
	rows, err := r.db.Query("SELECT account_id,count(*) FROM claude_routing_sessions GROUP BY account_id")
	if err == nil {
		for rows.Next() {
			var id string
			var n int
			if rows.Scan(&id, &n) == nil {
				counts[id] = n
			}
		}
		rows.Close()
	}
	seen := map[string]bool{}
	for _, c := range r.candidates {
		a := r.account(c, "", snapshot)
		a.Conversations = counts[c.ID]
		s.Accounts = append(s.Accounts, a)
		seen[c.ID] = true
	}
	for _, id := range r.config.Accounts {
		if !seen[id] {
			s.Accounts = append(s.Accounts, codexrouting.Account{ID: id, Enrolled: true, Status: "Saved login missing", Windows: []limits.Window{}})
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
		return a.ID < b.ID
	})
	return s
}

type routingError struct {
	status  int
	message string
}

func (e *routingError) Error() string          { return e.message }
func (e *routingError) HTTPStatus() int        { return e.status }
func failure(status int, message string) error { return &routingError{status, message} }

func (r *Router) Prepare(req *http.Request) (func(), error) {
	// Gateway has already authenticated the caller and enforced loopback.
	var payload struct {
		Model    string `json:"model"`
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
	}
	if req.Body != nil && req.Body != http.NoBody {
		b, err := io.ReadAll(io.LimitReader(req.Body, (32<<20)+1))
		req.Body.Close()
		if err != nil || len(b) > 32<<20 {
			return nil, failure(413, "Claude routing request exceeds 32 MiB or could not be read")
		}
		if json.Unmarshal(b, &payload) != nil {
			return nil, failure(400, "Invalid Claude request JSON")
		}
		req.Body = io.NopCloser(bytes.NewReader(b))
	}
	session, model := proxy.RequestMetadata(req, "claude")
	if req.Method != "GET" && req.URL.Path != "/claude/v1/messages/count_tokens" && session == "" {
		return nil, failure(400, "A stable Claude session identifier is required for account routing")
	}
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(session)))
	snapshot := r.snapshot()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.problem != "" {
		return nil, failure(503, r.problem)
	}
	pinned := ""
	if session != "" {
		err := r.db.QueryRow("SELECT account_id FROM claude_routing_sessions WHERE key=?", key).Scan(&pinned)
		if err != nil && err != sql.ErrNoRows {
			return nil, failure(503, "Cannot read Claude session assignment")
		}
		if pinned == "" {
			// Preserve conversations observed before routing was enabled. Do not move
			// an established session from the old single-profile gateway into the pool.
			var oldID, email string
			err = r.db.QueryRow(`SELECT account_id,coalesce(json_extract(record,'$.account'),'') FROM requests WHERE session_id=? AND json_extract(record,'$.harness')='claude' AND status BETWEEN 200 AND 299 ORDER BY sequence DESC LIMIT 1`, session).Scan(&oldID, &email)
			if err != nil && err != sql.ErrNoRows {
				return nil, failure(503, "Cannot read Claude session history")
			}
			if err == nil {
				for _, c := range r.candidates {
					if c.ID == oldID {
						pinned = c.ID
						break
					}
				}
				if pinned == "" {
					for _, c := range r.candidates {
						if c.Email == email {
							if pinned != "" {
								return nil, failure(409, "Claude session account is ambiguous")
							}
							pinned = c.ID
						}
					}
				}
				if pinned == "" {
					return nil, failure(503, "The session's original Claude account is unavailable")
				}
			} else {
				for _, m := range payload.Messages {
					if m.Role == "assistant" {
						return nil, failure(409, "Cannot determine the original account for this Claude conversation; start a new session")
					}
				}
			}
		}
	}
	best := -1.0
	chosen := candidate{}
	for _, c := range r.candidates {
		if pinned != "" && c.ID != pinned {
			continue
		}
		a := r.account(c, model, snapshot)
		if a.Status == "Ready" && a.Score != nil && *a.Score > best {
			best = *a.Score
			chosen = c
		}
	}
	if chosen.ID == "" {
		if pinned != "" {
			return nil, failure(503, "The session's Claude account is unavailable; refresh its login or limits, or start a new session")
		}
		return nil, failure(503, "No enrolled Claude account has fresh available quota and access to this model")
	}
	creds, err := r.load(req.Context(), chosen.Profile)
	if err != nil || creds.ExpiresAt <= r.now().Add(time.Minute).UnixMilli() || sha256.Sum256([]byte(creds.AccessToken)) != chosen.TokenHash {
		return nil, failure(503, "Claude login changed or expired; waiting for account verification")
	}
	if session != "" {
		now := r.now().UnixMilli()
		_, err = r.db.Exec(`INSERT INTO claude_routing_sessions(key,session_id,account_id,account,model,created_at,last_seen) VALUES(?,?,?,?,?,?,?) ON CONFLICT(key) DO UPDATE SET last_seen=excluded.last_seen,model=CASE WHEN excluded.model!='' THEN excluded.model ELSE claude_routing_sessions.model END`, key, session, chosen.ID, chosen.Email, model, now, now)
		if err != nil {
			return nil, failure(503, "Could not persist Claude session assignment")
		}
	}
	req.Header.Del("X-Api-Key")
	req.Header.Del("Cookie")
	req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	claude.WithGatewayDetails(req, claude.GatewayDetails{Session: session, Model: model, Account: chosen.Email, AccountID: chosen.ID, Observe: func(res *http.Response) { r.observe(chosen.ID, res) }})
	r.active[chosen.ID]++
	var once sync.Once
	return func() { once.Do(func() { r.mu.Lock(); r.active[chosen.ID]--; r.mu.Unlock() }) }, nil
}
func (r *Router) observe(id string, res *http.Response) {
	if res.StatusCode != 401 && res.StatusCode != 403 && res.StatusCode != 429 {
		return
	}
	delay := time.Minute
	if n, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && n > 0 {
		delay = time.Duration(min(n, 3600)) * time.Second
	} else if t, err := http.ParseTime(res.Header.Get("Retry-After")); err == nil {
		delay = min(time.Hour, max(time.Minute, t.Sub(r.now())))
	}
	r.mu.Lock()
	r.cooldown[id] = r.now().Add(delay)
	r.mu.Unlock()
}
