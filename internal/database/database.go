// Package database owns Garcon's local state. OAuth secrets stay in Codex's login stores.
package database

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type DB struct {
	*sql.DB
	Path string
}

func Open(path string) (*DB, error) {
	legacy := path
	if strings.HasSuffix(path, ".jsonl") {
		path = strings.TrimSuffix(path, ".jsonl") + ".db"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	f.Close()
	if err = os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: path}
	db, err := sql.Open("sqlite", u.String()+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	d := &DB{DB: db, Path: path}
	if err = d.migrate(legacy); err != nil {
		db.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) migrate(legacy string) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`
 CREATE TABLE IF NOT EXISTS state (key TEXT PRIMARY KEY, value TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS requests (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT, request_id TEXT UNIQUE, time INTEGER NOT NULL,
 account_id TEXT NOT NULL DEFAULT '', session_id TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '',
 status INTEGER NOT NULL DEFAULT 0, kind TEXT NOT NULL DEFAULT 'completion', state TEXT NOT NULL DEFAULT 'complete',
 record TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS requests_time ON requests(time);
 CREATE INDEX IF NOT EXISTS requests_session ON requests(session_id);
 CREATE TABLE IF NOT EXISTS sessions (
 key TEXT PRIMARY KEY, session_id TEXT NOT NULL DEFAULT '', account_id TEXT NOT NULL,
 account TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, last_seen INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS events (id INTEGER PRIMARY KEY AUTOINCREMENT, time INTEGER NOT NULL, message TEXT NOT NULL);
 `)
	if err != nil {
		return err
	}
	var done string
	err = tx.QueryRow("SELECT value FROM state WHERE key='legacy_import_v1'").Scan(&done)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == sql.ErrNoRows {
		dir := filepath.Dir(d.Path)
		for _, name := range []string{"limits", "prices", "codex-routing"} {
			b, e := os.ReadFile(filepath.Join(dir, name+".json"))
			if os.IsNotExist(e) {
				continue
			}
			if e != nil {
				return e
			}
			if !json.Valid(b) {
				return fmt.Errorf("invalid legacy %s.json; original preserved", name)
			}
			if _, e = tx.Exec("INSERT OR IGNORE INTO state(key,value) VALUES(?,?)", name, string(b)); e != nil {
				return e
			}
		}
		var pins map[string]string
		if b, e := os.ReadFile(filepath.Join(dir, "codex-routing-pins.json")); e == nil {
			if e = json.Unmarshal(b, &pins); e != nil {
				return e
			}
			for k, id := range pins {
				if _, e = tx.Exec("INSERT OR IGNORE INTO sessions(key,account_id,created_at,last_seen) VALUES(?,?,0,0)", k, id); e != nil {
					return e
				}
			}
		} else if !os.IsNotExist(e) {
			return e
		}
		if !strings.HasSuffix(legacy, ".jsonl") {
			legacy = filepath.Join(dir, "usage.jsonl")
		}
		if b, e := os.ReadFile(legacy); e == nil {
			for n, line := range bytes.Split(b, []byte("\n")) {
				if len(bytes.TrimSpace(line)) == 0 {
					continue
				}
				var rec struct {
					Time   int64  `json:"time"`
					Model  string `json:"model"`
					Status int    `json:"status"`
				}
				if json.Unmarshal(line, &rec) != nil {
					return fmt.Errorf("invalid legacy usage at line %d; original preserved", n+1)
				}
				if _, e = tx.Exec("INSERT INTO requests(time,model,status,record) VALUES(?,?,?,?)", rec.Time, rec.Model, rec.Status, string(line)); e != nil {
					return e
				}
			}
		} else if !os.IsNotExist(e) {
			return e
		}
		if _, err = tx.Exec("INSERT INTO state(key,value) VALUES('legacy_import_v1','true')"); err != nil {
			return err
		}
	}
	// A stream that ended with the process has an explicit terminal state.
	_, err = tx.Exec(`UPDATE requests SET state='interrupted', record=json_set(record,'$.state','interrupted','$.error','Garcon restarted before this request completed') WHERE state='streaming'`)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (d *DB) Get(key string, v any) error {
	var b string
	if err := d.QueryRow("SELECT value FROM state WHERE key=?", key).Scan(&b); err != nil {
		return err
	}
	return json.Unmarshal([]byte(b), v)
}
func (d *DB) Put(key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = d.Exec("INSERT INTO state(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, string(b))
	return err
}
func (d *DB) Write(p []byte) (int, error) {
	_, err := d.Exec("INSERT INTO events(time,message) VALUES(?,?)", time.Now().UnixMilli(), strings.TrimSpace(string(p)))
	return len(p), err
}
