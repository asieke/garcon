package claude

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Claude Code renews its own saved login during startup, before rejecting empty input.
// Both socket sessions and the limits service use this no-prompt renewal path.
const (
	renewMargin  = 5 * time.Minute
	renewPeriod  = time.Minute
	renewTimeout = 45 * time.Second
	lockName     = ".garcon-renew.lock"
)

func needsRenewal(expiresAt int64, now time.Time) bool {
	return expiresAt > 0 && time.UnixMilli(expiresAt).Sub(now) < renewMargin
}

// lock serialises renewals for one configuration directory across concurrent sessions. A lock older
// than the renewal timeout was left by a crash and is taken over.
func lock(dir string) (release func(), ok bool) {
	path := filepath.Join(dir, lockName)
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			fmt.Fprint(f, os.Getpid())
			f.Close()
			return func() { os.Remove(path) }, true
		}
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > renewMargin {
			os.Remove(path)
			continue
		}
		return nil, false
	}
	return nil, false
}

type session struct {
	claude string // path to the claude executable
	dir    string // Claude Code configuration directory
	base   string // Garcon's URL
}

func (s session) route() string { return s.base + "/claude" }

// renewLoop checks at once and then every minute, until the session ends.
func (s session) renewLoop(ctx context.Context) {
	renewer := NewRenewer()
	renewer.executable = s.claude
	ticker := time.NewTicker(renewPeriod)
	defer ticker.Stop()
	for {
		if _, err := renewer.Refresh(ctx, s.dir); err != nil {
			fmt.Fprintln(os.Stderr, "garcon claude: login renewal failed:", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
