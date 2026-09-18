// Package accounts attributes usage to the credentials on an individual request.
// It never reads a machine-wide login, stores credentials, or inspects prompts.
package accounts

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"time"
	"unicode"
)

const profileURL = "https://api.anthropic.com/api/oauth/profile"

var profileClient = &http.Client{
	Timeout: 2 * time.Second,
	// Never redirect a credential-bearing lookup to another endpoint.
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

type entry struct {
	label   string
	expires time.Time
	ready   chan struct{}
}

// Resolver's zero value is ready to use. The bounded cache holds only credential
// digests and resolved labels, and coalesces concurrent profile lookups.
type Resolver struct {
	mu    sync.Mutex
	cache map[[32]byte]*entry
	// Tests inject a transport; production always uses the fixed profile URL.
	client *http.Client
}

// Resolve returns a display label, never a credential. JWT claims and headers
// are attribution hints, not authentication; the upstream still validates auth.
func (r *Resolver) Resolve(provider string, h http.Header) string {
	token := bearer(h.Get("Authorization"))
	if provider == "chatgpt" {
		if label := chatGPT(h.Get("ChatGPT-Account-Id"), token); label != "" {
			return label
		}
	}
	// Anthropic API keys take precedence over OAuth attribution when both are
	// present. Do not associate a key-backed call with an unrelated login.
	if provider == "anthropic" && h.Get("X-Api-Key") != "" {
		return fingerprint(provider, "key", h.Get("X-Api-Key"))
	}
	if provider == "anthropic" && strings.HasPrefix(token, "sk-ant-oat") {
		if label := r.anthropic(token); label != "" {
			return label
		}
		return fingerprint(provider, "credential", token)
	}
	if token != "" {
		return fingerprint(provider, "key", token)
	}
	return provider + ":unknown"
}

func bearer(header string) string {
	parts := strings.Fields(header)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return parts[1]
	}
	return ""
}

func fingerprint(provider, kind, credential string) string {
	digest := sha256.Sum256([]byte(provider + "\x00" + credential))
	return fmt.Sprintf("%s:%s:%x", provider, kind, digest[:12])
}

func label(value string) string {
	if len(value) > 320 || strings.ContainsFunc(value, unicode.IsControl) {
		return ""
	}
	return strings.TrimSpace(value)
}

func chatGPT(selected, token string) string {
	selected = label(selected)
	var claims struct {
		Email   string `json:"email"`
		Profile struct {
			Email string `json:"email"`
		} `json:"https://api.openai.com/profile"`
		Auth struct {
			Account string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	parts := strings.Split(token, ".")
	if len(token) <= 32*1024 && len(parts) == 3 {
		if b, err := base64.RawURLEncoding.DecodeString(parts[1]); err == nil {
			// Discard partially decoded claims if any field has the wrong type.
			if json.Unmarshal(b, &claims) != nil {
				return accountID(selected)
			}
		}
	}
	id := label(claims.Auth.Account)
	// The selected workspace wins over the token's default workspace. Do not
	// attach an email from claims describing a different account.
	if selected != "" && selected != id {
		return accountID(selected)
	}
	if selected != "" {
		id = selected
	}
	email := label(claims.Profile.Email)
	if email == "" {
		email = label(claims.Email)
	}
	if email != "" {
		return email
	}
	return accountID(id)
}

// DisplayLabel folds labels emitted by the first autodetection build into their
// email account. ID-only accounts remain distinct when no email is available.
func DisplayLabel(value string) string {
	email, suffix, found := strings.Cut(value, " [chatgpt:")
	if !found || !strings.HasSuffix(suffix, "]") {
		return value
	}
	id := strings.TrimSuffix(suffix, "]")
	if id == "" || strings.ContainsAny(id, " []\t\r\n") {
		return value
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return value
	}
	return email
}

func accountID(id string) string {
	if id == "" {
		return ""
	}
	return "chatgpt:" + id
}

func (r *Resolver) anthropic(token string) string {
	key := sha256.Sum256([]byte(token))
	now := time.Now()
	r.mu.Lock()
	if e := r.cache[key]; e != nil && (e.expires.IsZero() || now.Before(e.expires)) {
		r.mu.Unlock()
		<-e.ready
		return e.label
	}
	if r.cache == nil {
		r.cache = make(map[[32]byte]*entry)
	}
	// Evict completed entries only; an in-flight lookup must keep its waiters.
	if len(r.cache) >= 1024 {
		for k, e := range r.cache {
			if !e.expires.IsZero() {
				delete(r.cache, k)
				break
			}
		}
		if len(r.cache) >= 1024 {
			r.mu.Unlock()
			return ""
		}
	}
	e := &entry{ready: make(chan struct{})}
	r.cache[key] = e
	r.mu.Unlock()

	value := r.fetchProfile(token)
	ttl := time.Hour
	if value == "" {
		ttl = time.Minute
	}
	r.mu.Lock()
	e.label, e.expires = value, time.Now().Add(ttl)
	close(e.ready)
	r.mu.Unlock()
	return value
}

func (r *Resolver) fetchProfile(token string) string {
	req, _ := http.NewRequest(http.MethodGet, profileURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Anthropic-Beta", "oauth-2025-04-20")
	req.Header.Set("Accept", "application/json")
	client := r.client
	if client == nil {
		client = profileClient
	}
	res, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ""
	}
	var profile struct {
		Account struct {
			Email string `json:"email"`
			UUID  string `json:"uuid"`
		} `json:"account"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 64*1024)).Decode(&profile) != nil {
		return ""
	}
	if email := label(profile.Account.Email); email != "" {
		return email
	}
	if id := label(profile.Account.UUID); id != "" {
		return "anthropic:" + id
	}
	return ""
}
