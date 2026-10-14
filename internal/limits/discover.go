package limits

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"garcon/internal/claude"
)

// source exists only during a refresh. Access tokens never enter snapshots.
type source struct {
	account    Account
	token      string
	selected   string
	expires    int64
	problem    string
	localFetch func(context.Context) (string, string, []Window, error)
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

func discoverCodex(home string) []source {
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
	return out
}

func discover(ctx context.Context, home string) []source {
	out := discoverCodex(home)
	for _, dir := range roots(home, ".hermes", os.Getenv("HERMES_HOME")) {
		b, err := readFile(filepath.Join(dir, "auth.json"))
		if err != nil {
			continue
		}
		out = append(out, hermesSources(b)...)
	}
	out = append(out, discoverClaude(ctx, home)...)
	out = append(out, discoverAGY(ctx, home)...)
	out = append(out, discoverMuse(ctx, home)...)
	return out
}

func discoverClaude(ctx context.Context, home string) []source {
	out := []source{}
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

func extractAGYEmail(b []byte) string {
	const marker = "applyAuthResult: email="
	idx := strings.LastIndex(string(b), marker)
	if idx < 0 {
		return ""
	}
	rest := string(b)[idx+len(marker):]
	if end := strings.IndexAny(rest, ", \t\r\n"); end >= 0 {
		rest = rest[:end]
	}
	if strings.Contains(rest, "@") && len(rest) <= 320 {
		return rest
	}
	return ""
}

func discoverAGY(_ context.Context, home string) []source {
	agyDir := filepath.Join(home, ".gemini", "antigravity-cli")
	if env := os.Getenv("AGY_HOME"); env != "" {
		agyDir = env
	}
	st, err := os.Stat(agyDir)
	if err != nil || !st.IsDir() {
		return nil
	}
	cachedEmail := ""
	if b, err := readFile(filepath.Join(agyDir, "cli.log")); err == nil {
		cachedEmail = extractAGYEmail(b)
	}
	bin := os.Getenv("AGY_BIN")
	if bin == "" {
		candidate := filepath.Join(home, ".local", "bin", "agy")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			bin = candidate
		} else if p, err := exec.LookPath("agy"); err == nil {
			bin = p
		}
	}
	id := accountID("agy", agyDir, "")
	s := source{
		token:   "local:agy",
		account: Account{ID: id, Provider: "agy", Email: cachedEmail, Plan: "Antigravity"},
	}
	if bin == "" {
		s.problem = "AGY CLI binary not found"
		return []source{s}
	}
	s.localFetch = func(ctx context.Context) (string, string, []Window, error) {
		runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		tmpLog, err := os.CreateTemp("", "garcon-agy-*.log")
		logPath := ""
		if err == nil {
			logPath = tmpLog.Name()
			tmpLog.Close()
			defer os.Remove(logPath)
		}
		args := []string{"-p", "/usage", "--output-format", "json"}
		if logPath != "" {
			args = append(args, "--log-file", logPath)
		}
		cmd := exec.CommandContext(runCtx, bin, args...)
		out, err := cmd.Output()
		email := cachedEmail
		if logPath != "" {
			if b, readErr := readFile(logPath); readErr == nil {
				if e := extractAGYEmail(b); e != "" {
					email = e
				}
			}
		}
		if err != nil {
			return email, "Antigravity", nil, errors.New("Could not query AGY CLI usage")
		}
		ws, err := parseAGY(out)
		if err != nil {
			return email, "Antigravity", nil, err
		}
		return email, "Antigravity", ws, nil
	}
	return []source{s}
}

func discoverMuse(_ context.Context, home string) []source {
	paths := []string{filepath.Join(home, ".config", "muse", "auth.json")}
	if env := os.Getenv("MUSE_AUTH_PATH"); env != "" {
		paths = append(paths, env)
	}
	seen := map[string]bool{}
	out := []source{}
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil || seen[abs] {
			continue
		}
		seen[abs] = true
		b, err := readFile(abs)
		if err != nil {
			continue
		}
		var data struct {
			Providers map[string]struct {
				Mechanism    string     `json:"mechanism"`
				Storage      string     `json:"storage"`
				SubsActive   bool       `json:"subs_active"`
				SubsTierName string     `json:"subs_tier_name"`
				UserFullName string     `json:"user_full_name"`
				UserEmail    string     `json:"user_email"`
				SubsUsage    *museUsage `json:"subs_usage"`
			} `json:"providers"`
		}
		if json.Unmarshal(b, &data) != nil {
			continue
		}
		meta, ok := data.Providers["meta"]
		if !ok || (meta.UserEmail == "" && !meta.SubsActive && meta.Mechanism == "") {
			continue
		}
		person := meta.UserEmail
		if person == "" {
			person = meta.UserFullName
		}
		if person == "" {
			person = abs
		}
		plan := meta.SubsTierName
		if plan == "" {
			if meta.SubsActive {
				plan = "Muse Code"
			} else {
				plan = "Meta"
			}
		}
		s := source{
			token:   "local:muse",
			account: Account{ID: accountID("muse", person, ""), Provider: "muse", Email: meta.UserEmail, Workspace: meta.UserFullName, Plan: plan},
		}
		if !meta.SubsActive && meta.SubsUsage == nil && meta.UserEmail == "" {
			s.problem = "Login needs refresh"
		} else {
			usageSnap := meta.SubsUsage
			s.localFetch = func(context.Context) (string, string, []Window, error) {
				return meta.UserEmail, plan, parseMuseWindows(usageSnap), nil
			}
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
