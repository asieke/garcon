package database

import (
	"os"
	"path/filepath"
	"testing"
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
