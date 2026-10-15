package limits

import (
	"bufio"
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
	"runtime"
	"strings"
	"sync"
	"time"

	"garcon/internal/claude"

	"github.com/google/uuid"
)

// source exists only during a refresh. Access tokens never enter snapshots.
type source struct {
	account  Account
	token    string
	selected string
	expires  int64
	problem  string
	// localFetch returns the email, plan, windows and, when the provider stamps
	// its reading, when it observed them (Unix milliseconds).
	localFetch func(context.Context) (string, string, []Window, int64, error)
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
	s.localFetch = func(ctx context.Context) (string, string, []Window, int64, error) {
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
			return email, "Antigravity", nil, 0, errors.New("Could not query AGY CLI usage")
		}
		ws, err := parseAGY(out)
		if err != nil {
			return email, "Antigravity", nil, 0, err
		}
		return email, "Antigravity", ws, 0, nil
	}
	return []source{s}
}

// Muse keeps subscription usage only in a host's memory, learns it only from
// the credential mint behind a model call, and writes none of it to disk. So
// Garcon sends one minimal prompt from a private, memory-only session to read
// it: hourly, or on a manual refresh at most every five minutes. Between
// probes the account shows the last reading.
const (
	museProbeInterval       = time.Hour
	museManualProbeInterval = 5 * time.Minute
	museProbePrompt         = "Reply with OK."
)

var errMuseIdle = errors.New("Muse is checked hourly with a minimal prompt; showing its last reading")

var museProbes = struct {
	sync.Mutex
	last time.Time
	now  func() time.Time
}{now: time.Now}

type manualRefreshKey struct{}

// withManualRefresh marks a refresh the user asked for, which may probe Muse sooner.
func withManualRefresh(ctx context.Context) context.Context {
	return context.WithValue(ctx, manualRefreshKey{}, true)
}

// museProbeDue claims the next probe slot. A failed probe also waits out the
// interval, so an outage never turns into a prompt every refresh.
func museProbeDue(ctx context.Context) bool {
	museProbes.Lock()
	defer museProbes.Unlock()
	interval := museProbeInterval
	if manual, _ := ctx.Value(manualRefreshKey{}).(bool); manual {
		interval = museManualProbeInterval
	}
	now := museProbes.now()
	if !museProbes.last.IsZero() && now.Sub(museProbes.last) < interval {
		return false
	}
	museProbes.last = now
	return true
}

func museBinary(home string) string {
	if bin := os.Getenv("MUSE_BIN"); bin != "" {
		return bin
	}
	// A login service often has a minimal PATH, so check the usual install
	// locations before PATH.
	for _, candidate := range []string{
		filepath.Join(home, ".local", "bin", "muse"),
		filepath.Join(home, ".muse", "bin", "muse"),
		"/opt/homebrew/bin/muse",
		"/usr/local/bin/muse",
	} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate
		}
	}
	if p, err := exec.LookPath("muse"); err == nil {
		return p
	}
	return ""
}

// museProbeDir holds the probe's Muse data and workspace, apart from the
// user's sessions, so probes never appear in Muse history or Garcon's import.
func museProbeDir(home string) string {
	dir := os.Getenv("XDG_CACHE_HOME")
	switch {
	case dir != "":
	case runtime.GOOS == "darwin":
		dir = filepath.Join(home, "Library", "Caches")
	default:
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, "garcon", "muse-probe")
}

// museProbeUsage runs one minimal turn on a memory-only `muse serve` host and
// returns the subscription usage the host observes, stopping as soon as it
// arrives (about ten seconds) rather than waiting for the turn to wind down.
func museProbeUsage(ctx context.Context, bin, dir string) (*museUsage, error) {
	workspace := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, bin, "serve", "--no-session-log")
	cmd.Dir = workspace
	cmd.Env = append(os.Environ(), "XDG_DATA_HOME="+filepath.Join(dir, "data"))
	cmd.WaitDelay = 2 * time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	defer func() {
		stdin.Close()
		cancel()
		_ = cmd.Wait()
	}()
	usage, err := museProbeSession(&msp{w: stdin, lines: bufio.NewScanner(stdout)}, workspace)
	if err != nil && runCtx.Err() != nil {
		return nil, errors.New("Muse did not report usage in time")
	}
	return usage, err
}

func museProbeSession(c *msp, workspace string) (*museUsage, error) {
	c.lines.Buffer(make([]byte, 64<<10), 4<<20)
	if _, err := c.call("initialize", map[string]any{"clientInfo": map[string]string{"name": "garcon", "version": "1"}}); err != nil {
		return nil, err
	}
	if err := c.send(map[string]any{"jsonrpc": "2.0", "method": "initialized"}); err != nil {
		return nil, err
	}
	raw, err := c.call("session/start", map[string]any{"commandId": uuidV7(), "workspaceRoot": workspace})
	if err != nil {
		return nil, err
	}
	var started struct {
		Session struct {
			SessionID string `json:"sessionId"`
		} `json:"session"`
	}
	if json.Unmarshal(raw, &started) != nil || started.Session.SessionID == "" {
		return nil, errors.New("Unrecognized Muse session response")
	}
	if _, err = c.call("turn/start", map[string]any{
		"commandId": uuidV7(), "sessionId": started.Session.SessionID, "reasoningEffort": "none",
		"input": []map[string]string{{"type": "text", "text": museProbePrompt}},
	}); err != nil {
		return nil, err
	}
	// The reading lands shortly after the model answers; the turn itself winds
	// down a minute later, so stop at the answer and poll briefly.
	for c.usage == nil {
		method, err := c.next()
		if err != nil {
			return nil, err
		}
		if method == "turn/foregroundCompleted" || method == "turn/completed" || method == "turn/failed" {
			break
		}
	}
	for i := 0; c.usage == nil && i < 10; i++ {
		raw, err := c.call("usage/read", nil)
		if err != nil {
			return nil, err
		}
		var result struct {
			Usage *museUsage `json:"usage"`
		}
		if json.Unmarshal(raw, &result) == nil && len(parseMuseWindows(result.Usage)) > 0 {
			c.usage = result.Usage
			break
		}
		time.Sleep(time.Second)
	}
	if c.usage == nil {
		return nil, errors.New("Muse did not report usage")
	}
	return c.usage, nil
}

// msp is a minimal MSP client: newline-delimited JSON-RPC that remembers the
// last usage/changed notification it reads.
type msp struct {
	w     io.Writer
	lines *bufio.Scanner
	id    int
	usage *museUsage
}

func (c *msp) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = c.w.Write(append(b, '\n'))
	return err
}

// next reads one frame and returns its notification method, if any.
func (c *msp) next() (string, error) {
	if !c.lines.Scan() {
		return "", errors.New("Muse exited before reporting usage")
	}
	var frame struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if json.Unmarshal(c.lines.Bytes(), &frame) != nil {
		return "", nil
	}
	c.observe(frame.Method, frame.Params)
	return frame.Method, nil
}

func (c *msp) observe(method string, params json.RawMessage) {
	var u museUsage
	if method == "usage/changed" && json.Unmarshal(params, &u) == nil && len(parseMuseWindows(&u)) > 0 {
		c.usage = &u
	}
}

func (c *msp) call(method string, params any) (json.RawMessage, error) {
	c.id++
	req := map[string]any{"jsonrpc": "2.0", "id": c.id, "method": method}
	if params != nil {
		req["params"] = params
	}
	if err := c.send(req); err != nil {
		return nil, err
	}
	for c.lines.Scan() {
		var msg struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(c.lines.Bytes(), &msg) != nil {
			continue
		}
		c.observe(msg.Method, msg.Params)
		if msg.ID == nil || *msg.ID != c.id {
			continue // notifications and unrelated frames
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("Muse rejected %s", method)
		}
		return msg.Result, nil
	}
	return nil, errors.New("Muse exited before reporting usage")
}

func uuidV7() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return id.String()
}

func discoverMuse(_ context.Context, home string) []source {
	paths := []string{filepath.Join(home, ".config", "muse", "auth.json")}
	if env := os.Getenv("MUSE_AUTH_PATH"); env != "" {
		paths = append(paths, env)
	}
	bin := museBinary(home)
	probeDir := museProbeDir(home)
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
			out = append(out, s)
			continue
		}
		snapshot := meta.SubsUsage
		s.localFetch = func(ctx context.Context) (string, string, []Window, int64, error) {
			// Older Muse releases stored a snapshot in auth.json.
			if ws := parseMuseWindows(snapshot); len(ws) > 0 {
				return meta.UserEmail, plan, ws, snapshot.ObservedAtMs, nil
			}
			if bin == "" {
				return meta.UserEmail, plan, nil, 0, errors.New("Muse CLI not found; set MUSE_BIN or install muse on PATH")
			}
			if !museProbeDue(ctx) {
				return meta.UserEmail, plan, nil, 0, errMuseIdle
			}
			u, err := museProbeUsage(ctx, bin, probeDir)
			if err != nil {
				return meta.UserEmail, plan, nil, 0, err
			}
			return meta.UserEmail, plan, parseMuseWindows(u), u.ObservedAtMs, nil
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
