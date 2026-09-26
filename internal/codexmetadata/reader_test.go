package codexmetadata

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestLookupReadOnlyProfilesAndSchemaFallback(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "state_5.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`PRAGMA journal_mode=WAL;
 CREATE TABLE threads(id TEXT PRIMARY KEY,title TEXT,cwd TEXT,archived INTEGER);
 INSERT INTO threads VALUES('one','Routing visibility','/projects/garcon',0),('unrelated','Private unrelated task','/private',0);`)
	if err != nil {
		t.Fatal(err)
	}
	reader := &Reader{Homes: []string{filepath.Join(home, "missing"), home, home}}
	tasks := reader.Lookup(context.Background(), []string{"one", "missing"})
	if len(tasks) != 1 || tasks["one"].Title != "Routing visibility" || tasks["one"].CWD != "/projects/garcon" {
		t.Fatalf("unexpected metadata: %+v", tasks)
	}
	// User-assigned names override generated titles and changes appear on refresh.
	if _, err = db.Exec(`ALTER TABLE threads ADD COLUMN name TEXT; UPDATE threads SET name='My renamed task',archived=1 WHERE id='one'`); err != nil {
		t.Fatal(err)
	}
	tasks = reader.Lookup(context.Background(), []string{"one"})
	if tasks["one"].Title != "My renamed task" || !tasks["one"].Archived {
		t.Fatalf("rename/archive not reflected: %+v", tasks)
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM threads").Scan(&count); err != nil || count != 2 {
		t.Fatal("source rows changed", err)
	}
	if _, err = os.Stat(filepath.Join(home, "missing")); !os.IsNotExist(err) {
		t.Fatal("lookup created missing profile")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := reader.Lookup(ctx, []string{"one"}); len(got) != 0 {
		t.Fatal("ignored cancellation")
	}
}

func TestLookupPrefersNewestIndexAndToleratesCorruptFiles(t *testing.T) {
	home := t.TempDir()
	for _, version := range []string{"5", "10"} {
		db, err := sql.Open("sqlite", filepath.Join(home, "state_"+version+".sqlite"))
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Exec(`CREATE TABLE threads(id TEXT,title TEXT,cwd TEXT,archived INTEGER); INSERT INTO threads VALUES('one',?, '/project',0)`, version)
		db.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, "state_11.sqlite"), []byte("not sqlite"), 0600); err != nil {
		t.Fatal(err)
	}
	reader := &Reader{Homes: []string{home}}
	if got := reader.Lookup(context.Background(), []string{"one"}); got["one"].Title != "10" {
		t.Fatalf("wrong version: %+v", got)
	}
}
