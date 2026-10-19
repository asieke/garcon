// Package database owns Garcon's local state. OAuth secrets stay in Codex's login stores.
package database

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	// Reads never borrow the single writer connection. In WAL mode a long
	// dashboard snapshot can coexist with request and routing commits.
	Readers *sql.DB
	Path    string
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
	readers, err := sql.Open("sqlite", u.String()+"?mode=ro&_pragma=busy_timeout(5000)&_pragma=query_only(1)&_pragma=foreign_keys(1)")
	if err != nil {
		db.Close()
		return nil, err
	}
	readers.SetMaxOpenConns(4)
	readers.SetMaxIdleConns(4)
	if err = readers.Ping(); err != nil {
		readers.Close()
		db.Close()
		return nil, err
	}
	d.Readers = readers
	return d, nil
}

func (d *DB) Query(query string, args ...any) (*sql.Rows, error) {
	return d.Readers.Query(query, args...)
}
func (d *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.Readers.QueryContext(ctx, query, args...)
}
func (d *DB) QueryRow(query string, args ...any) *sql.Row {
	return d.Readers.QueryRow(query, args...)
}
func (d *DB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return d.Readers.QueryRowContext(ctx, query, args...)
}
func (d *DB) Close() error { return errors.Join(d.Readers.Close(), d.DB.Close()) }

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
 CREATE INDEX IF NOT EXISTS requests_message ON requests(json_extract(record,'$.message_id'));
 CREATE INDEX IF NOT EXISTS requests_upstream ON requests(json_extract(record,'$.upstream_request_id'));
 CREATE INDEX IF NOT EXISTS requests_parent ON requests(json_extract(record,'$.parent_session_id'));
 CREATE TABLE IF NOT EXISTS claude_import_files (path TEXT PRIMARY KEY, checkpoint TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS claude_import_sessions (session_id TEXT PRIMARY KEY, cwd TEXT NOT NULL, entrypoint TEXT NOT NULL DEFAULT '');
 CREATE TABLE IF NOT EXISTS sessions (
 key TEXT PRIMARY KEY, session_id TEXT NOT NULL DEFAULT '', account_id TEXT NOT NULL,
 account TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, last_seen INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS events (id INTEGER PRIMARY KEY AUTOINCREMENT, time INTEGER NOT NULL, message TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS events_time ON events(time);
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
	// Bootstrap once per open, after legacy import, then maintain the total in
	// the same transaction as inserts/deletes (upserts and rollbacks included).
	// The recent feed and recording never need a full-ledger COUNT.
	_, err = tx.Exec(`
 CREATE TABLE IF NOT EXISTS request_totals (id INTEGER PRIMARY KEY CHECK(id=1), total INTEGER NOT NULL);
 INSERT INTO request_totals(id,total) VALUES(1,(SELECT count(*) FROM requests))
 ON CONFLICT(id) DO UPDATE SET total=excluded.total;
 CREATE TRIGGER IF NOT EXISTS requests_count_insert AFTER INSERT ON requests BEGIN
   UPDATE request_totals SET total=total+1 WHERE id=1;
 END;
 CREATE TRIGGER IF NOT EXISTS requests_count_delete AFTER DELETE ON requests BEGIN
   UPDATE request_totals SET total=total-1 WHERE id=1;
 END;
 CREATE TRIGGER IF NOT EXISTS events_retention AFTER INSERT ON events BEGIN
   DELETE FROM events WHERE id<=NEW.id-10000;
   DELETE FROM events WHERE time<NEW.time-2592000000;
 END;
 DELETE FROM events WHERE id<=(SELECT coalesce(max(id),0)-10000 FROM events);
 DELETE FROM events WHERE time<?;
 `, time.Now().Add(-30*24*time.Hour).UnixMilli())
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
