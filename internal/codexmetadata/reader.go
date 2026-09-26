// Package codexmetadata joins Garcon session IDs to local Codex task metadata.
// This is a best-effort adapter for Codex's private local schema. It never reads
// conversation contents or credentials, and never opens a database for writing.
package codexmetadata

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Task struct {
	Title    string `json:"title"`
	CWD      string `json:"cwd"`
	Archived bool   `json:"archived"`
}

type Reader struct{ Homes []string }

func New() *Reader {
	home, _ := os.UserHomeDir()
	homes := []string{filepath.Join(home, ".codex")}
	if explicit := os.Getenv("CODEX_HOME"); explicit != "" {
		homes = append([]string{explicit}, homes...)
	}
	profiles, _ := filepath.Glob(filepath.Join(home, ".codex-*"))
	return &Reader{Homes: append(homes, profiles...)}
}

func (r *Reader) Lookup(ctx context.Context, ids []string) map[string]Task {
	out := map[string]Task{}
	if len(ids) == 0 {
		return out
	}
	// A missing, locked, or upgraded index must not hold up the dashboard.
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	where := " WHERE id IN (" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")"
	seen := map[string]bool{}
	for _, home := range r.Homes {
		paths, _ := filepath.Glob(filepath.Join(home, "state_*.sqlite"))
		version := func(p string) int {
			n, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(p), "state_"), ".sqlite"))
			return n
		}
		sort.Slice(paths, func(i, j int) bool { return version(paths[i]) > version(paths[j]) })
		for _, path := range paths {
			if seen[path] || ctx.Err() != nil {
				continue
			}
			seen[path] = true
			u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}
			db, err := sql.Open("sqlite", u.String())
			if err != nil {
				continue
			}
			db.SetMaxOpenConns(1)
			rows, err := db.QueryContext(ctx, "SELECT id,coalesce(nullif(name,''),title),cwd,archived FROM threads"+where, args...)
			if err != nil {
				// Older Codex versions have title but no user-assigned name.
				rows, err = db.QueryContext(ctx, "SELECT id,title,cwd,archived FROM threads"+where, args...)
			}
			if err != nil {
				db.Close()
				continue
			}
			for rows.Next() {
				var id string
				var task Task
				if rows.Scan(&id, &task.Title, &task.CWD, &task.Archived) == nil {
					if _, exists := out[id]; !exists {
						out[id] = task
					}
				}
			}
			rows.Close()
			db.Close()
		}
	}
	return out
}
