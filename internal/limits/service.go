package limits

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"garcon/internal/local"
)

const (
	codexURL   = "https://chatgpt.com/backend-api/wham/usage"
	claudeURL  = "https://api.anthropic.com/api/oauth/usage"
	profileURL = "https://api.anthropic.com/api/oauth/profile"
)

type identity struct {
	account Account
	expires time.Time
}
type fetchError struct {
	status  int
	retry   time.Time
	message string
}

func (e *fetchError) Error() string { return e.message }

type Service struct {
	mu         sync.Mutex
	cache      Snapshot
	path       string
	client     *http.Client
	now        func() time.Time
	discover   func(context.Context) []source
	identities map[[32]byte]identity
	backoff    map[string]time.Time
	lastStart  time.Time
}

func New(dir string) *Service {
	home, _ := os.UserHomeDir()
	s := &Service{path: filepath.Join(dir, "limits.json"), now: time.Now,
		client:     &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		discover:   func(ctx context.Context) []source { return discover(ctx, home) },
		identities: map[[32]byte]identity{}, backoff: map[string]time.Time{}, cache: Snapshot{Accounts: []Account{}}}
	if b, err := readFile(s.path); err == nil {
		var c Snapshot
		if json.Unmarshal(b, &c) == nil && c.Accounts != nil {
			s.cache = c
			s.cache.Refreshing = false
			for i := range s.cache.Accounts {
				s.cache.Accounts[i].Status = "stale"
				if credits := s.cache.Accounts[i].ResetCredits; credits != nil {
					credits.Status = "stale"
				}
			}
		}
	}
	return s
}

func (s *Service) Run(ctx context.Context) {
	s.Refresh(ctx)
	t := time.NewTicker(refreshSeconds * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Refresh(ctx)
		}
	}
}

func (s *Service) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.cache
	c.Accounts = append([]Account{}, c.Accounts...)
	for i := range c.Accounts {
		a := &c.Accounts[i]
		a.Windows = append([]Window{}, a.Windows...)
		a.ResetCredits = copyCredits(a.ResetCredits)
		if credits := a.ResetCredits; credits != nil && credits.Status == "fresh" && s.now().UnixMilli()-credits.FetchedAt > 2*refreshSeconds*1000 {
			credits.Status = "stale"
		}
		if a.Status == "fresh" && s.now().UnixMilli()-a.FetchedAt > 2*refreshSeconds*1000 {
			a.Status = "stale"
		}
		for j := range a.Windows {
			w := &a.Windows[j]
			w.Expired = w.ResetsAt > 0 && w.ResetsAt <= s.now().UnixMilli()
		}
	}
	return c
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/api/limits/refresh" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", 405)
			return
		}
		if !local.Host(r.Host) {
			http.Error(w, "refresh requires a loopback Host", 403)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host {
				http.Error(w, "cross-origin refresh refused", 403)
				return
			}
		}
		// One bounded refresh continues if the browser disconnects.
		if old, ok := s.beginRefresh(); ok {
			go s.collect(context.Background(), old)
		}
		w.WriteHeader(http.StatusAccepted)
	} else if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", 405)
		return
	}
	json.NewEncoder(w).Encode(s.Snapshot())
}

// Refresh coalesces all callers; only this goroutine uses identities/backoff.
func (s *Service) Refresh(ctx context.Context) {
	if old, ok := s.beginRefresh(); ok {
		s.collect(ctx, old)
	}
}

func (s *Service) beginRefresh() (map[string]Account, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache.Refreshing || (!s.lastStart.IsZero() && s.now().Sub(s.lastStart) < 30*time.Second) {
		return nil, false
	}
	s.cache.Refreshing = true
	s.lastStart = s.now()
	old := map[string]Account{}
	for _, a := range s.cache.Accounts {
		old[a.ID] = a
	}
	return old, true
}

func (s *Service) collect(ctx context.Context, old map[string]Account) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	sources := s.discover(ctx)
	groups := map[string][]source{}
	for _, src := range sources {
		if ctx.Err() != nil {
			break
		}
		if src.account.Provider == "claude" && src.token != "" {
			key := sha256.Sum256([]byte(src.token))
			profileKey := src.account.ID + ":profile"
			if src.account.ID == "" {
				profileKey = fmt.Sprintf("profile:%x", key)
			}
			if v, ok := s.identities[key]; ok && s.now().Before(v.expires) {
				src.account = v.account
			} else if src.expires == 0 || src.expires > s.now().UnixMilli() {
				if s.now().Before(s.backoff[profileKey]) {
					src.problem = "Provider refresh is rate limited"
				} else {
					a, err := s.profile(ctx, src)
					if err == nil {
						src.account = a
						if len(s.identities) >= 1024 {
							s.identities = map[[32]byte]identity{}
						}
						s.identities[key] = identity{a, s.now().Add(time.Hour)}
					} else {
						src.problem = err.Error()
						var fe *fetchError
						if errors.As(err, &fe) && !fe.retry.IsZero() {
							s.backoff[profileKey] = fe.retry
						}
					}
				}
			}
		}
		if src.account.ID == "" {
			src.account.ID = fmt.Sprintf("%s:unresolved:%x", src.account.Provider, sha256.Sum256([]byte(src.token)))
		}
		groups[src.account.ID] = append(groups[src.account.ID], src)
	}
	accounts := []Account{}
	for id, ss := range groups {
		// Prefer a currently usable login, then the furthest expiry. A failing
		// duplicate must not hide another valid login for the same provider seat.
		sort.SliceStable(ss, func(i, j int) bool {
			usable := func(x source) bool {
				return x.token != "" && x.problem == "" && (x.expires == 0 || x.expires > s.now().UnixMilli())
			}
			if usable(ss[i]) != usable(ss[j]) {
				return usable(ss[i])
			}
			return ss[i].expires > ss[j].expires
		})
		src := ss[0]
		a := src.account
		a.Windows = []Window{}
		if prev, ok := old[id]; ok {
			a.Windows = prev.Windows
			a.FetchedAt = prev.FetchedAt
			a.ResetCredits = prev.ResetCredits
			if a.Email == "" {
				a.Email = prev.Email
			}
			if a.Plan == "" {
				a.Plan = prev.Plan
			}
		}
		if a.Provider == "codex" {
			a.ResetCredits = staleCredits(a.ResetCredits, "Awaiting reset credit refresh")
		} else {
			a.ResetCredits = nil
		}
		a.Status = "unavailable"
		a.Error = ""
		switch {
		case src.token == "" || (src.expires > 0 && src.expires <= s.now().UnixMilli()):
			a.Status = "needs_login"
			a.Error = "Login needs refresh"
		case src.problem != "":
			a.Error = src.problem
			if a.Error == "Login needs refresh" {
				a.Status = "needs_login"
			} else if a.FetchedAt > 0 {
				a.Status = "stale"
			}
		case s.now().Before(s.backoff[id]):
			a.Error = "Provider refresh is rate limited"
			if a.FetchedAt > 0 {
				a.Status = "stale"
			}
		default:
			var raw []byte
			var err error
			usedSource := src
			endpoint := codexURL
			if a.Provider == "claude" {
				endpoint = claudeURL
			}
			seenTokens := map[string]bool{}
			for _, candidate := range ss {
				if candidate.token == "" || candidate.problem != "" || seenTokens[candidate.token] || (candidate.expires > 0 && candidate.expires <= s.now().UnixMilli()) {
					continue
				}
				seenTokens[candidate.token] = true
				raw, err = s.fetch(ctx, endpoint, candidate)
				usedSource = candidate
				var fe *fetchError
				if !errors.As(err, &fe) || (fe.status != 401 && fe.status != 403) {
					break
				}
			}
			if err == nil {
				var ws []Window
				var plan string
				if a.Provider == "codex" {
					plan, ws, err = parseCodex(raw)
				} else {
					ws, err = parseClaude(raw)
				}
				if err == nil {
					a.Windows = ws
					a.FetchedAt = s.now().UnixMilli()
					a.Status = "fresh"
					if a.Provider == "codex" {
						a.ResetCredits = s.collectCredits(ctx, usedSource, a.ResetCredits)
					}
					if plan != "" {
						a.Plan = plan
					}
					if len(ws) == 0 {
						a.Status = "unavailable"
						a.Error = "No subscription limits reported"
					}
				}
			}
			if err != nil {
				a.Error = err.Error()
				if a.FetchedAt > 0 {
					a.Status = "stale"
				}
				var fe *fetchError
				if errors.As(err, &fe) {
					if fe.status == 401 || fe.status == 403 {
						a.Status = "needs_login"
					}
					if !fe.retry.IsZero() {
						s.backoff[id] = fe.retry
					}
				}
			}
		}
		accounts = append(accounts, a)
	}
	// A missing or temporarily unreadable login must not erase its last snapshot.
	for id, a := range old {
		if strings.Contains(id, ":unresolved:") {
			continue
		}
		if _, ok := groups[id]; !ok {
			a.Status = "needs_login"
			a.Error = "Local login unavailable; open the CLI to refresh it"
			if a.Provider == "codex" {
				a.ResetCredits = staleCredits(a.ResetCredits, "Login needs refresh")
			}
			accounts = append(accounts, a)
		}
	}
	sort.Slice(accounts, func(i, j int) bool {
		a, b := accounts[i], accounts[j]
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		if a.Email != b.Email {
			return a.Email < b.Email
		}
		return a.ID < b.ID
	})
	c := Snapshot{Accounts: accounts, UpdatedAt: s.now().UnixMilli()}
	if err := s.persist(c); err != nil {
		c.Error = "Could not save the latest limits snapshot"
	}
	s.mu.Lock()
	s.cache = c
	s.mu.Unlock()
}

func (s *Service) profile(ctx context.Context, src source) (Account, error) {
	b, err := s.fetch(ctx, profileURL, src)
	if err != nil {
		return Account{}, err
	}
	var p struct {
		Account struct {
			UUID  string `json:"uuid"`
			Email string `json:"email"`
		} `json:"account"`
		Organization struct {
			UUID string `json:"uuid"`
			Name string `json:"name"`
		} `json:"organization"`
	}
	if json.Unmarshal(b, &p) != nil || p.Account.UUID == "" {
		return Account{}, errors.New("Account identity unavailable")
	}
	return Account{ID: accountID("claude", p.Account.UUID, p.Organization.UUID), Provider: "claude", Email: p.Account.Email, Workspace: p.Organization.Name, Plan: src.account.Plan}, nil
}

func (s *Service) fetch(ctx context.Context, endpoint string, src source) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+src.token)
	req.Header.Set("Accept", "application/json")
	if src.account.Provider == "claude" {
		req.Header.Set("Anthropic-Beta", "oauth-2025-04-20")
	} else if src.selected != "" {
		req.Header.Set("ChatGPT-Account-Id", src.selected)
	}
	res, err := s.client.Do(req)
	if err != nil {
		return nil, errors.New("Provider unavailable; retrying automatically")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		e := &fetchError{status: res.StatusCode, message: "Provider unavailable; retrying automatically"}
		if res.StatusCode == 401 || res.StatusCode == 403 {
			e.message = "Login needs refresh"
		}
		if res.StatusCode == 429 {
			e.message = "Provider refresh is rate limited"
			e.retry = s.now().Add(refreshSeconds * time.Second)
			if n, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && n > 0 {
				e.retry = s.now().Add(time.Duration(min(n, 604800)) * time.Second)
			} else if t, err := http.ParseTime(res.Header.Get("Retry-After")); err == nil && t.After(s.now()) {
				e.retry = t
			}
		}
		return nil, e
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 256*1024+1))
	if err != nil || len(b) > 256*1024 {
		return nil, errors.New("Unrecognized provider response")
	}
	return b, nil
}

func (s *Service) persist(c Snapshot) error {
	if s.path == "" {
		return nil
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".limits-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.path)
}
