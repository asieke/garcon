// Package claudeimport reads local Claude transcripts without changing them.
// Only usage and session metadata are persisted; conversation content is ignored.
package claudeimport

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
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
	if ctx.Err() == nil {
		status.LastScan = time.Now().UnixMilli()
	}
	if err := i.Store.DB.QueryRow(`SELECT count(*) FROM requests WHERE json_extract(record,'$.source')='claude-local'`).Scan(&status.Records); err != nil {
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

func (i *Importer) file(ctx context.Context, path, account string) (int, error) {
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
			var e event
			if json.Unmarshal(line, &e) != nil {
				next.Skipped++
			} else if rec, ok := e.record(account); ok {
				if err = i.Store.SaveRecord(rec); err != nil {
					return next.Skipped, err
				}
				if e.CWD != "" && len(e.CWD) <= 4096 && len(e.Entrypoint) <= 128 {
					_, err = i.Store.DB.Exec(`INSERT INTO claude_import_sessions(session_id,cwd,entrypoint) VALUES(?,?,?) ON CONFLICT(session_id) DO UPDATE SET cwd=excluded.cwd,entrypoint=excluded.entrypoint`, rec.SessionID, e.CWD, e.Entrypoint)
					if err != nil {
						return next.Skipped, err
					}
				}
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
