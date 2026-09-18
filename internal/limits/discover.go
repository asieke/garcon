package limits

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"garcon/internal/claude"
)

// source exists only during a refresh. Access tokens never enter snapshots.
type source struct {
	account  Account
	token    string
	selected string
	expires  int64
	problem  string
}

func readFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 1<<20+1))
	if len(b) > 1<<20 {
		return nil, fmt.Errorf("login file too large")
	}
	return b, err
}

func roots(home, prefix, env string) []string {
	paths := []string{filepath.Join(home, prefix)}
	matches, _ := filepath.Glob(filepath.Join(home, prefix+"-*"))
	paths = append(paths, matches...)
	if env != "" {
		paths = append(paths, env)
	}
	out := []string{}
	seen := map[string]bool{}
	for _, p := range paths {
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

func codexSource(token, selected string) source {
	s := source{token: token, selected: selected, account: Account{Provider: "codex"}}
	var c struct {
		Email   string `json:"email"`
		Exp     int64  `json:"exp"`
		Profile struct {
			Email string `json:"email"`
		} `json:"https://api.openai.com/profile"`
		Auth struct {
			Account     string `json:"chatgpt_account_id"`
			User        string `json:"chatgpt_user_id"`
			AccountUser string `json:"chatgpt_account_user_id"`
			Plan        string `json:"chatgpt_plan_type"`
		} `json:"https://api.openai.com/auth"`
	}
	p := strings.Split(token, ".")
	if len(p) == 3 && len(token) < 64*1024 {
		b, err := base64.RawURLEncoding.DecodeString(p[1])
		if err == nil {
			if json.Unmarshal(b, &c) != nil {
				c.Auth.Account = ""
				c.Auth.User = ""
				c.Auth.AccountUser = ""
				c.Email = ""
				c.Profile.Email = ""
			}
		}
	}
	if s.selected == "" {
		s.selected = c.Auth.Account
	}
	s.account.Email = c.Profile.Email
	if s.account.Email == "" {
		s.account.Email = c.Email
	}
	s.account.Plan = c.Auth.Plan
	s.account.Workspace = s.selected
	s.expires = c.Exp * 1000
	person := c.Auth.User
	if person == "" {
		person = c.Auth.AccountUser
	}
	if person == "" {
		person = s.account.Email
	}
	if person == "" {
		d := sha256.Sum256([]byte(token))
		person = fmt.Sprintf("unknown:%x", d[:16])
	}
	s.account.ID = accountID("codex", person, s.selected)
	return s
}

func discover(ctx context.Context, home string) []source {
	out := []source{}
	for _, dir := range roots(home, ".codex", os.Getenv("CODEX_HOME")) {
		b, err := readFile(filepath.Join(dir, "auth.json"))
		if err != nil {
			continue
		}
		var d struct {
			Tokens struct {
				Access  string `json:"access_token"`
				Account string `json:"account_id"`
			} `json:"tokens"`
		}
		if json.Unmarshal(b, &d) == nil && d.Tokens.Access != "" {
			out = append(out, codexSource(d.Tokens.Access, d.Tokens.Account))
		}
	}
	for _, dir := range roots(home, ".hermes", os.Getenv("HERMES_HOME")) {
		b, err := readFile(filepath.Join(dir, "auth.json"))
		if err != nil {
			continue
		}
		out = append(out, hermesSources(b)...)
	}
	for _, dir := range roots(home, ".claude", os.Getenv("CLAUDE_CONFIG_DIR")) {
		// Metadata is only a fallback for a login that cannot currently be read.
		// With a usable token, /profile supplies the authoritative identity.
		metaPath := filepath.Join(dir, ".claude.json")
		if dir == filepath.Join(home, ".claude") {
			metaPath = filepath.Join(home, ".claude.json")
		}
		var meta struct {
			OAuth struct {
				Email        string `json:"emailAddress"`
				Account      string `json:"accountUuid"`
				Organization string `json:"organizationUuid"`
			} `json:"oauthAccount"`
		}
		b, _ := readFile(metaPath)
		_ = json.Unmarshal(b, &meta)
		c, err := claude.LoadContext(ctx, dir)
		if err != nil && meta.OAuth.Account == "" {
			continue
		}
		s := source{token: c.AccessToken, expires: c.ExpiresAt, account: Account{Provider: "claude", Email: meta.OAuth.Email, Workspace: meta.OAuth.Organization, Plan: c.SubscriptionType}}
		if meta.OAuth.Account != "" {
			s.account.ID = accountID("claude", meta.OAuth.Account, meta.OAuth.Organization)
		}
		if err != nil {
			s.problem = "Login needs refresh"
		}
		out = append(out, s)
	}
	return out
}

func hermesSources(raw []byte) []source {
	type tokens struct {
		Access  string `json:"access_token"`
		Account string `json:"account_id"`
	}
	type entry struct {
		Tokens  tokens `json:"tokens"`
		Access  string `json:"access_token"`
		Account string `json:"account_id"`
	}
	var data struct {
		Providers map[string]entry   `json:"providers"`
		Pool      map[string][]entry `json:"credential_pool"`
	}
	if json.Unmarshal(raw, &data) != nil {
		return nil
	}
	entries := append([]entry{data.Providers["openai-codex"]}, data.Pool["openai-codex"]...)
	out := []source{}
	for _, e := range entries {
		if e.Tokens.Access != "" {
			out = append(out, codexSource(e.Tokens.Access, e.Tokens.Account))
		} else if e.Access != "" {
			out = append(out, codexSource(e.Access, e.Account))
		}
	}
	return out
}
