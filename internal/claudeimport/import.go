// Package claudeimport reads local Claude transcripts without changing them.
// Only usage and session metadata are persisted; conversation content is ignored.
package claudeimport

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"garcon/internal/store"
	"garcon/internal/usage"
)

const maxLine = 16 << 20

type Status struct {
	Enabled      bool  `json:"enabled"`
	Scanning     bool  `json:"scanning"`
	LastScan     int64 `json:"last_scan"`
	Files        int   `json:"files"`
	Records      int   `json:"records"`
	FailedFiles  int   `json:"failed_files"`
	SkippedLines int   `json:"skipped_lines"`
}

type Importer struct {
	Store      *store.Store
	Home       string
	ExtraHomes []string
	mu         sync.Mutex
	scanMu     sync.Mutex
	status     Status
}

func New(s *store.Store) *Importer {
	home, _ := os.UserHomeDir()
	i := &Importer{Store: s, Home: home}
	if p := os.Getenv("CLAUDE_CONFIG_DIR"); p != "" {
		i.ExtraHomes = []string{p}
	}
	return i
}

func (i *Importer) Snapshot() Status { i.mu.Lock(); defer i.mu.Unlock(); return i.status }
func (i *Importer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(i.Snapshot())
}
func (i *Importer) Run(ctx context.Context) {
	i.Scan(ctx)
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			i.Scan(ctx)
		}
	}
}

func readSmall(path string, v any) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 1<<20+1))
	return err == nil && len(b) <= 1<<20 && json.Unmarshal(b, v) == nil
}

func (i *Importer) homes() []string {
	paths := []string{filepath.Join(i.Home, ".claude")}
	profiles, _ := filepath.Glob(filepath.Join(i.Home, ".claude-*"))
	paths = append(paths, profiles...)
	paths = append(paths, i.ExtraHomes...)
	var launchers []struct {
		Profile string `json:"profile"`
	}
	if readSmall(filepath.Join(i.Home, ".config/garcon/claude-launchers.json"), &launchers) {
		for _, l := range launchers {
			if l.Profile != "" {
				paths = append(paths, l.Profile)
			}
		}
	}
	seen := map[string]bool{}
	out := []string{}
	for _, p := range paths {
		p, err := filepath.EvalSymlinks(p)
		if err != nil {
			continue
		}
		p, err = filepath.Abs(p)
		if err != nil || seen[p] {
			continue
		}
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			seen[p] = true
			out = append(out, p)
		}
		if len(out) >= 128 {
			break
		}
	}
	return out
}

func (i *Importer) account(home string) string {
	var meta struct {
		OAuth struct {
			Email string `json:"emailAddress"`
		} `json:"oauthAccount"`
	}
	p := filepath.Join(home, ".claude.json")
	if home == filepath.Join(i.Home, ".claude") {
		p = filepath.Join(i.Home, ".claude.json")
	}
	if readSmall(p, &meta) && len(meta.OAuth.Email) <= 320 && strings.Contains(meta.OAuth.Email, "@") {
		return meta.OAuth.Email
	}
	return "Local profile: " + filepath.Base(home)
}

// Scan is restart-safe: records are idempotent and committed before advancing
// each file's checkpoint. Partial trailing lines are retried on the next scan.
func (i *Importer) Scan(ctx context.Context) {
	i.scanMu.Lock()
	defer i.scanMu.Unlock()
	i.mu.Lock()
	i.status.Enabled = true
	i.status.Scanning = true
	i.mu.Unlock()
	status := Status{Enabled: true}
	seen := map[string]bool{}
	for _, home := range i.homes() {
		account := i.account(home)
		root := filepath.Join(home, "projects")
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				status.FailedFiles++
				return nil
			}
			if d.IsDir() || !d.Type().IsRegular() || !strings.HasSuffix(path, ".jsonl") || seen[path] {
				return nil
			}
			seen[path] = true
			status.Files++
			skipped, err := i.file(ctx, path, account)
			status.SkippedLines += skipped
			if err != nil {
				status.FailedFiles++
			}
			return nil
		})
		if err != nil && ctx.Err() == nil {
			status.FailedFiles++
		}
	}
	i.scanMuse(ctx, seen, &status)
	i.scanAGY(ctx, seen, &status)
	if ctx.Err() == nil {
		status.LastScan = time.Now().UnixMilli()
	}
	if err := i.Store.DB.QueryRow(`SELECT count(*) FROM requests WHERE json_extract(record,'$.source') IN ('claude-local','muse-local','agy-local')`).Scan(&status.Records); err != nil {
		status.FailedFiles++
	}
	i.mu.Lock()
	i.status = status
	i.mu.Unlock()
}

type checkpoint struct {
	Offset, Size, Modified int64
	Fingerprint            string
	Skipped                int
}

func (i *Importer) scanFile(ctx context.Context, path string, handle func([]byte) (bool, error)) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	var old checkpoint
	var raw string
	if err = i.Store.DB.QueryRow("SELECT checkpoint FROM claude_import_files WHERE path=?", path).Scan(&raw); err == nil {
		_ = json.Unmarshal([]byte(raw), &old)
	}
	// Hash a bounded prefix to detect replacement without storing transcript text.
	prefix := make([]byte, min(info.Size(), 512))
	if _, err = io.ReadFull(f, prefix); err != nil {
		return old.Skipped, err
	}
	next := checkpoint{Size: info.Size(), Modified: info.ModTime().UnixNano(), Fingerprint: fmt.Sprintf("%x", sha256.Sum256(prefix))}
	if old.Fingerprint == next.Fingerprint && old.Size == next.Size && old.Modified == next.Modified && old.Offset == next.Size {
		return old.Skipped, nil
	}
	if old.Fingerprint == next.Fingerprint && old.Offset <= next.Size && !(old.Size == next.Size && old.Modified != next.Modified) {
		next.Offset = old.Offset
		next.Skipped = old.Skipped
	}
	if _, err = f.Seek(next.Offset, io.SeekStart); err != nil {
		return next.Skipped, err
	}
	// Read only the size observed at scan start; appends are picked up next time.
	reader := bufio.NewReader(io.LimitReader(f, next.Size-next.Offset))
	for ctx.Err() == nil {
		line, bytes, oversize, err := readLine(reader)
		if err == io.EOF {
			break
		} // Never advance past an unfinished JSONL record.
		if err != nil {
			return next.Skipped, err
		}
		if oversize {
			next.Skipped++
		} else if len(strings.TrimSpace(string(line))) > 0 {
			badJSON, err := handle(line)
			if badJSON {
				next.Skipped++
			} else if err != nil {
				return next.Skipped, err
			}
		}
		next.Offset += int64(bytes)
	}
	if ctx.Err() != nil {
		return next.Skipped, ctx.Err()
	}
	b, _ := json.Marshal(next)
	_, err = i.Store.DB.Exec("INSERT INTO claude_import_files(path,checkpoint) VALUES(?,?) ON CONFLICT(path) DO UPDATE SET checkpoint=excluded.checkpoint", path, string(b))
	return next.Skipped, err
}

func (i *Importer) saveSessionMeta(sessionID, cwd, entrypoint string) error {
	if sessionID == "" || len(sessionID) > 256 || len(cwd) > 4096 || len(entrypoint) > 512 {
		return nil
	}
	if cwd == "" && entrypoint == "" {
		return nil
	}
	_, err := i.Store.DB.Exec(`INSERT INTO claude_import_sessions(session_id,cwd,entrypoint) VALUES(?,?,?) ON CONFLICT(session_id) DO UPDATE SET cwd=CASE WHEN excluded.cwd!='' THEN excluded.cwd ELSE claude_import_sessions.cwd END,entrypoint=CASE WHEN excluded.entrypoint!='' THEN excluded.entrypoint ELSE claude_import_sessions.entrypoint END`, sessionID, cwd, entrypoint)
	return err
}

func (i *Importer) file(ctx context.Context, path, account string) (int, error) {
	return i.scanFile(ctx, path, func(line []byte) (bool, error) {
		var e event
		if json.Unmarshal(line, &e) != nil {
			return true, nil
		}
		if rec, ok := e.record(account); ok {
			if err := i.Store.SaveRecord(rec); err != nil {
				return false, err
			}
			if e.CWD != "" && len(e.CWD) <= 4096 && len(e.Entrypoint) <= 128 {
				if err := i.saveSessionMeta(rec.SessionID, e.CWD, e.Entrypoint); err != nil {
					return false, err
				}
			}
		}
		return false, nil
	})
}

func (i *Importer) museAccount() string {
	var data struct {
		Providers map[string]struct {
			UserEmail string `json:"user_email"`
		} `json:"providers"`
	}
	if readSmall(filepath.Join(i.Home, ".config/muse/auth.json"), &data) {
		if email := data.Providers["meta"].UserEmail; len(email) <= 320 && strings.Contains(email, "@") {
			return email
		}
	}
	return "Local profile: muse"
}

func (i *Importer) scanMuse(ctx context.Context, seen map[string]bool, status *Status) {
	museDir := filepath.Join(i.Home, ".local/share/muse")
	sessionsRoot := filepath.Join(museDir, "sessions")
	if _, err := os.Stat(sessionsRoot); os.IsNotExist(err) {
		return
	}
	account := i.museAccount()
	if dbPath := filepath.Join(museDir, "session-index.db"); fileExists(dbPath) {
		u := url.URL{Scheme: "file", Path: dbPath}
		if db, err := sql.Open("sqlite", u.String()+"?mode=ro&_pragma=busy_timeout(2000)"); err == nil {
			rows, qErr := db.QueryContext(ctx, "SELECT session_id, workspace_root, title FROM sessions")
			if qErr == nil {
				for rows.Next() {
					var id, cwd, title string
					if rows.Scan(&id, &cwd, &title) == nil {
						_ = i.saveSessionMeta(id, cwd, title)
					}
				}
				rows.Close()
			}
			db.Close()
		}
	}
	err := filepath.WalkDir(sessionsRoot, func(path string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			status.FailedFiles++
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() || !strings.HasSuffix(path, ".jsonl") || seen[path] {
			return nil
		}
		seen[path] = true
		status.Files++
		parentSession := museParentSession(path)
		skipped, err := i.scanFile(ctx, path, func(line []byte) (bool, error) {
			var e museEvent
			if json.Unmarshal(line, &e) != nil {
				return true, nil
			}
			if rec, ok := e.record(account, parentSession); ok {
				if err := i.Store.SaveRecord(rec); err != nil {
					return false, err
				}
			}
			return false, nil
		})
		status.SkippedLines += skipped
		if err != nil {
			status.FailedFiles++
		}
		return nil
	})
	if err != nil && ctx.Err() == nil {
		status.FailedFiles++
	}
}

func museParentSession(path string) string {
	slash := filepath.ToSlash(path)
	if idx := strings.Index(slash, "/subagent/"); idx > 0 {
		return filepath.Base(slash[:idx])
	}
	return filepath.Base(filepath.Dir(slash))
}

type museEvent struct {
	ID          string `json:"id"`
	RecordedAt  int64  `json:"recorded_at"`
	PayloadType string `json:"payload_type"`
	Stream      struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
	} `json:"stream"`
	Payload struct {
		Kind  string `json:"kind"`
		Event struct {
			Kind       string `json:"kind"`
			Model      string `json:"model"`
			DurationMs int64  `json:"duration_ms"`
			Usage      *struct {
				Input      int64 `json:"input_tokens"`
				Output     int64 `json:"output_tokens"`
				Cached     int64 `json:"cached_tokens"`
				CacheWrite int64 `json:"cache_write_tokens"`
				CacheRead  int64 `json:"cache_read_tokens"`
			} `json:"usage"`
		} `json:"event"`
	} `json:"payload"`
}

func (e museEvent) record(account, parentSession string) (usage.Record, bool) {
	ev := e.Payload.Event
	u := ev.Usage
	if e.PayloadType != "runtime.session" || e.Payload.Kind != "run" || ev.Kind != "model_completed" || u == nil || e.ID == "" || ev.Model == "" {
		return usage.Record{}, false
	}
	sessionID := parentSession
	if sessionID == "" {
		sessionID = e.Stream.ID
	}
	tMs := e.RecordedAt / 1000
	if tMs <= 0 || sessionID == "" || len(e.ID) > 256 || len(sessionID) > 256 || len(ev.Model) > 200 || min(u.Input, u.Output, u.Cached, u.CacheRead, u.CacheWrite) < 0 {
		return usage.Record{}, false
	}
	cacheRead := max(u.CacheRead, u.Cached)
	input := max(0, u.Input-cacheRead)
	return usage.Record{
		RequestID:          "muse-local:" + e.ID,
		MessageID:          e.ID,
		SessionID:          sessionID,
		Time:               tMs,
		Ms:                 max(0, ev.DurationMs),
		Harness:            "muse",
		Provider:           "meta",
		Model:              ev.Model,
		Account:            account,
		AccountAttribution: "profile",
		Source:             "muse-local",
		Kind:               "completion",
		State:              "recorded",
		Input:              input,
		Output:             u.Output,
		CacheRead:          cacheRead,
		CacheWrite:         u.CacheWrite,
	}, true
}

func (i *Importer) agyAccountAndModel(agyDir string) (string, string) {
	model := "agy"
	var settings struct {
		Model string `json:"model"`
	}
	if readSmall(filepath.Join(agyDir, "settings.json"), &settings) && settings.Model != "" && len(settings.Model) <= 200 {
		model = settings.Model
	}
	if f, err := os.Open(filepath.Join(agyDir, "cli.log")); err == nil {
		b, _ := io.ReadAll(io.LimitReader(f, 1<<20))
		f.Close()
		const marker = "applyAuthResult: email="
		if idx := strings.LastIndex(string(b), marker); idx >= 0 {
			rest := string(b)[idx+len(marker):]
			if end := strings.IndexAny(rest, ", \t\r\n"); end >= 0 {
				rest = rest[:end]
			}
			if len(rest) <= 320 && strings.Contains(rest, "@") {
				return rest, model
			}
		}
	}
	return "Local profile: antigravity-cli", model
}

func (i *Importer) scanAGY(ctx context.Context, seen map[string]bool, status *Status) {
	agyDir := filepath.Join(i.Home, ".gemini/antigravity-cli")
	brainRoot := filepath.Join(agyDir, "brain")
	if _, err := os.Stat(brainRoot); os.IsNotExist(err) {
		return
	}
	account, defaultModel := i.agyAccountAndModel(agyDir)
	if dbPath := filepath.Join(agyDir, "conversation_summaries.db"); fileExists(dbPath) {
		u := url.URL{Scheme: "file", Path: dbPath}
		if db, err := sql.Open("sqlite", u.String()+"?mode=ro&_pragma=busy_timeout(2000)"); err == nil {
			rows, qErr := db.QueryContext(ctx, "SELECT conversation_id, title, workspace_uris FROM conversation_summaries")
			if qErr == nil {
				for rows.Next() {
					var id, title, urisRaw string
					if rows.Scan(&id, &title, &urisRaw) == nil {
						var uris []string
						_ = json.Unmarshal([]byte(urisRaw), &uris)
						cwd := ""
						if len(uris) > 0 {
							cwd = strings.TrimPrefix(uris[0], "file://")
						}
						_ = i.saveSessionMeta(id, cwd, title)
					}
				}
				rows.Close()
			}
			db.Close()
		}
	}
	entries, err := os.ReadDir(brainRoot)
	if err != nil {
		status.FailedFiles++
		return
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return
		}
		if !entry.IsDir() {
			continue
		}
		convID := entry.Name()
		path := filepath.Join(brainRoot, convID, ".system_generated/logs/transcript.jsonl")
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || seen[path] {
			continue
		}
		seen[path] = true
		status.Files++
		skipped, err := i.scanFile(ctx, path, func(line []byte) (bool, error) {
			var e agyEvent
			if json.Unmarshal(line, &e) != nil {
				return true, nil
			}
			if rec, ok := e.record(account, convID, defaultModel); ok {
				if err := i.Store.SaveRecord(rec); err != nil {
					return false, err
				}
			}
			return false, nil
		})
		status.SkippedLines += skipped
		if err != nil {
			status.FailedFiles++
		}
	}
}

type agyEvent struct {
	Type            string `json:"type"`
	StepIndex       int    `json:"step_index"`
	CreatedAt       string `json:"created_at"`
	Status          string `json:"status"`
	Model           string `json:"model"`
	InputTokens     int64  `json:"input_tokens"`
	CacheReadTokens int64  `json:"cache_read_tokens"`
	OutputTokens    int64  `json:"output_tokens"`
}

func (e agyEvent) record(account, convID, defaultModel string) (usage.Record, bool) {
	t, err := time.Parse(time.RFC3339Nano, e.CreatedAt)
	if e.Type != "PLANNER_RESPONSE" || err != nil || t.UnixMilli() <= 0 || convID == "" || e.StepIndex < 0 {
		return usage.Record{}, false
	}
	if min(e.InputTokens, e.CacheReadTokens, e.OutputTokens) < 0 || e.InputTokens+e.CacheReadTokens+e.OutputTokens == 0 {
		return usage.Record{}, false
	}
	model := e.Model
	if model == "" {
		model = defaultModel
	}
	if len(convID) > 256 || len(model) > 200 {
		return usage.Record{}, false
	}
	id := fmt.Sprintf("agy-local:%s:%d", convID, e.StepIndex)
	return usage.Record{
		RequestID:          id,
		MessageID:          id,
		SessionID:          convID,
		Time:               t.UnixMilli(),
		Harness:            "agy",
		Provider:           "google",
		Model:              model,
		Account:            account,
		AccountAttribution: "profile",
		Source:             "agy-local",
		Kind:               "completion",
		State:              "recorded",
		Input:              e.InputTokens,
		Output:             e.OutputTokens,
		CacheRead:          e.CacheReadTokens,
	}, true
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func readLine(r *bufio.Reader) ([]byte, int, bool, error) {
	var line []byte
	n := 0
	oversize := false
	for {
		part, err := r.ReadSlice('\n')
		n += len(part)
		if n > maxLine {
			oversize = true
			line = nil
		} else if !oversize {
			line = append(line, part...)
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		return line, n, oversize, err
	}
}

// Intentionally omit content, tool inputs/results, prompts and credentials.
type event struct {
	Type       string `json:"type"`
	Session    string `json:"sessionId"`
	Request    string `json:"requestId"`
	Timestamp  string `json:"timestamp"`
	CWD        string `json:"cwd"`
	Entrypoint string `json:"entrypoint"`
	Error      bool   `json:"isApiErrorMessage"`
	Message    struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			Input  int64 `json:"input_tokens"`
			Output int64 `json:"output_tokens"`
			Read   int64 `json:"cache_read_input_tokens"`
			Write  int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

func (e event) record(account string) (usage.Record, bool) {
	m := e.Message
	u := m.Usage
	t, err := time.Parse(time.RFC3339Nano, e.Timestamp)
	if e.Type != "assistant" || e.Error || u == nil || err != nil || t.UnixMilli() <= 0 || !strings.HasPrefix(m.ID, "msg_") || m.Model == "" || m.Model == "<synthetic>" || e.Session == "" {
		return usage.Record{}, false
	}
	if len(m.ID) > 256 || len(e.Request) > 256 || len(e.Session) > 256 || len(m.Model) > 200 || min(u.Input, u.Output, u.Read, u.Write) < 0 {
		return usage.Record{}, false
	}
	return usage.Record{RequestID: "claude-local:" + m.ID, MessageID: m.ID, UpstreamRequestID: e.Request, SessionID: e.Session, Time: t.UnixMilli(), Harness: "claude", Provider: "anthropic", Model: m.Model, Account: account, AccountAttribution: "profile", Source: "claude-local", Kind: "completion", State: "recorded", Input: u.Input, Output: u.Output, CacheRead: u.Read, CacheWrite: u.Write}, true
}
