package claude

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNeedsRenewal(t *testing.T) {
	now := time.Now()
	for offset, want := range map[time.Duration]bool{10 * time.Minute: false, 4 * time.Minute: true, -time.Hour: true} {
		if got := needsRenewal(now.Add(offset).UnixMilli(), now); got != want {
			t.Errorf("expiring in %v: %v", offset, got)
		}
	}
	if needsRenewal(0, now) {
		t.Error("no expiry should never renew")
	}
}

func TestLock(t *testing.T) {
	dir := t.TempDir()
	release, ok := lock(dir)
	if !ok {
		t.Fatal("first lock refused")
	}
	if _, again := lock(dir); again {
		t.Error("second lock granted while held")
	}
	release()
	if _, ok := lock(dir); !ok {
		t.Error("lock refused after release")
	}
	stale := filepath.Join(dir, lockName)
	old := time.Now().Add(-6 * time.Minute)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	if _, ok := lock(dir); !ok {
		t.Error("stale lock not taken over")
	}
}
