// Package codexrouting selects locally enrolled Codex OAuth accounts. Credentials
// stay in Codex's stores; configuration, status and conversation pins contain none.
package codexrouting

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type credential struct {
	id, email, plan, token, dir string
	expires                     time.Time
	refresh                     bool
}

func readJSON(path string, dst any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 1<<20+1))
	if err != nil || len(b) > 1<<20 {
		return errors.New("file too large or unreadable")
	}
	return json.Unmarshal(b, dst)
}

func loadCredential(dir string) (credential, error) {
	var data struct {
		Mode   string `json:"auth_mode"`
		Tokens struct {
			Access  string `json:"access_token"`
			Refresh string `json:"refresh_token"`
			Account string `json:"account_id"`
		} `json:"tokens"`
	}
	if err := readJSON(filepath.Join(dir, "auth.json"), &data); err != nil {
		return credential{}, err
	}
	if data.Mode != "" && data.Mode != "chatgpt" {
		return credential{}, errors.New("not a managed ChatGPT login")
	}
	parts := strings.Split(data.Tokens.Access, ".")
	if len(parts) != 3 {
		return credential{}, errors.New("not a Codex OAuth login")
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return credential{}, errors.New("invalid login claims")
	}
	var claims struct {
		Email   string `json:"email"`
		Exp     int64  `json:"exp"`
		Profile struct {
			Email string `json:"email"`
		} `json:"https://api.openai.com/profile"`
		Auth struct {
			Account string `json:"chatgpt_account_id"`
			Plan    string `json:"chatgpt_plan_type"`
		} `json:"https://api.openai.com/auth"`
	}
	if json.Unmarshal(b, &claims) != nil {
		return credential{}, errors.New("invalid login claims")
	}
	id := data.Tokens.Account
	if id == "" {
		id = claims.Auth.Account
	}
	if id == "" || claims.Exp == 0 {
		return credential{}, errors.New("login identity or expiration missing")
	}
	email := claims.Profile.Email
	if email == "" {
		email = claims.Email
	}
	return credential{id: id, email: email, plan: claims.Auth.Plan, token: data.Tokens.Access, dir: dir, expires: time.Unix(claims.Exp, 0), refresh: data.Tokens.Refresh != ""}, nil
}

func discover(home string) []credential {
	dirs, _ := filepath.Glob(filepath.Join(home, ".codex-*"))
	dirs = append([]string{filepath.Join(home, ".codex")}, dirs...)
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		dirs = append(dirs, dir)
	}
	byID := map[string]credential{}
	for i, dir := range dirs {
		if i >= 128 {
			break
		}
		c, err := loadCredential(dir)
		if err != nil {
			continue
		}
		// Prefer the freshest saved login; do not rotate stale copies of a grant.
		if old, ok := byID[c.id]; !ok || c.expires.After(old.expires) {
			byID[c.id] = c
		}
	}
	out := make([]credential, 0, len(byID))
	for _, c := range byID {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

// renew asks Codex to perform its own managed OAuth refresh. No prompt, thread,
// tool call or model request is created, and no token is copied by Garcon.
func renew(ctx context.Context, c credential) error {
	exe, err := exec.LookPath("codex")
	if err != nil {
		home, _ := os.UserHomeDir()
		exe = filepath.Join(home, ".local/bin/codex")
	}
	dir, err := os.MkdirTemp("", "garcon-codex-auth-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-c", `model_provider="openai"`, "-c", `cli_auth_credentials_store="file"`, "app-server", "--listen", "stdio://")
	cmd.Dir = dir
	for _, e := range os.Environ() {
		key, _, _ := strings.Cut(e, "=")
		if key == "CODEX_HOME" || strings.HasPrefix(key, "OPENAI_") || strings.HasPrefix(key, "CHATGPT_") {
			continue
		}
		cmd.Env = append(cmd.Env, e)
	}
	cmd.Env = append(cmd.Env, "CODEX_HOME="+c.dir)
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = io.Discard // CLI diagnostics must never become public routing errors.
	if err := cmd.Start(); err != nil {
		return errors.New("Codex CLI is unavailable for login renewal")
	}
	defer func() { in.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	scan := bufio.NewScanner(out)
	scan.Buffer(make([]byte, 4096), 1<<20)
	rpc := func(id int, method string, params any) error {
		if err := json.NewEncoder(in).Encode(map[string]any{"id": id, "method": method, "params": params}); err != nil {
			return err
		}
		for scan.Scan() {
			var message struct {
				ID    int             `json:"id"`
				Error json.RawMessage `json:"error"`
			}
			if json.Unmarshal(scan.Bytes(), &message) == nil && message.ID == id {
				if len(message.Error) > 0 && string(message.Error) != "null" {
					return errors.New("Codex could not refresh this login")
				}
				return nil
			}
		}
		return errors.New("Codex login renewal ended without a response")
	}
	if err := rpc(1, "initialize", map[string]any{"clientInfo": map[string]string{"name": "garcon", "version": "1"}}); err != nil {
		return err
	}
	if err := json.NewEncoder(in).Encode(map[string]string{"method": "initialized"}); err != nil {
		return err
	}
	if err := rpc(2, "account/read", map[string]bool{"refreshToken": true}); err != nil {
		return err
	}
	after, err := loadCredential(c.dir)
	if err != nil || after.id != c.id || after.token == c.token || !after.expires.After(time.Now().Add(5*time.Minute)) {
		return errors.New("Codex did not save a renewed login for the same account")
	}
	return nil
}
