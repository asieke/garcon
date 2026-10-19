package database

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLegacyMigrationIsAtomicAndIdempotent(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "usage.jsonl")
	original := `{"time":12,"model":"model","input":5}` + "\n"
	os.WriteFile(legacy, []byte(original), 0600)
	os.WriteFile(filepath.Join(dir, "codex-routing.json"), []byte(`{"enabled":true,"accounts":["a"]}`), 0600)
	os.WriteFile(filepath.Join(dir, "codex-routing-pins.json"), []byte(`{"hash":"a"}`), 0600)
	path := filepath.Join(dir, "usage.db")
	for i := 0; i < 2; i++ {
		d, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var n int
		d.QueryRow("SELECT count(*) FROM requests").Scan(&n)
		if n != 1 {
			t.Fatalf("duplicated legacy requests: %d", n)
		}
		d.QueryRow("SELECT count(*) FROM sessions WHERE account_id='a'").Scan(&n)
		if n != 1 {
			t.Fatal("lost legacy pin")
		}
		var c struct{ Enabled bool }
		if d.Get("codex-routing", &c) != nil || !c.Enabled {
			t.Fatal("lost routing state")
		}
		d.Close()
	}
	b, _ := os.ReadFile(legacy)
	if string(b) != original {
		t.Fatal("legacy data modified")
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0600 {
		t.Fatal("database permissions")
	}
}

func TestReadPoolConnectionsCannotWriteAndCloseTogether(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for range d.Readers.Stats().MaxOpenConnections {
		conn, err := d.Readers.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		var mode string
		var timeout, readOnly int
		if err := conn.QueryRowContext(context.Background(), "PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
			t.Fatalf("reader journal mode=%q err=%v", mode, err)
		}
		if err := conn.QueryRowContext(context.Background(), "PRAGMA busy_timeout").Scan(&timeout); err != nil || timeout != 5000 {
			t.Fatalf("reader busy_timeout=%d err=%v", timeout, err)
		}
		if err := conn.QueryRowContext(context.Background(), "PRAGMA query_only").Scan(&readOnly); err != nil || readOnly != 1 {
			t.Fatalf("reader query_only=%d err=%v", readOnly, err)
		}
		if _, err := conn.ExecContext(context.Background(), "INSERT INTO state(key,value) VALUES('test','true')"); err == nil {
			t.Fatal("read connection accepted a write")
		}
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if err := d.Ping(); err == nil {
		t.Fatal("writer left open")
	}
	if err := d.Readers.Ping(); err == nil {
		t.Fatal("read pool left open")
	}
}

func TestEventsRetainNewestTenThousandAndThirtyDays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.db")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { d.Close() }()
	tx, err := d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	for i := range 10005 {
		if _, err := tx.Exec("INSERT INTO events(time,message) VALUES(?,?)", now, i); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var count, oldest, newest int
	if err := d.QueryRow("SELECT count(*),min(id),max(id) FROM events").Scan(&count, &oldest, &newest); err != nil || count != 10000 || oldest != 6 || newest != 10005 {
		t.Fatalf("event cap: count=%d range=%d..%d err=%v", count, oldest, newest, err)
	}
	if _, err := d.Exec("UPDATE events SET time=? WHERE id=6", time.Now().Add(-31*24*time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Write([]byte("new log line")); err != nil {
		t.Fatal(err)
	}
	if err := d.QueryRow("SELECT count(*) FROM events WHERE time<?", now).Scan(&count); err != nil || count != 0 {
		t.Fatalf("expired event retained: count=%d err=%v", count, err)
	}
	// Startup also prunes quiet databases before any new log line is written.
	if _, err := d.Exec("UPDATE events SET time=?", time.Now().Add(-31*24*time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	d = reopened
	if err := d.QueryRow("SELECT count(*) FROM events").Scan(&count); err != nil || count != 0 {
		t.Fatalf("expired events retained on restart: count=%d err=%v", count, err)
	}
}
func TestFailedMigrationRollsBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.db")
	legacy := filepath.Join(dir, "usage.jsonl")
	os.WriteFile(legacy, []byte("{\"time\":1}\n{bad}\n"), 0600)
	if d, err := Open(path); err == nil {
		d.Close()
		t.Fatal("accepted invalid legacy data")
	}
	os.WriteFile(legacy, []byte("{\"time\":1}\n"), 0600)
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var n int
	d.QueryRow("SELECT count(*) FROM requests").Scan(&n)
	if n != 1 {
		t.Fatal("partial migration duplicated requests")
	}
}
