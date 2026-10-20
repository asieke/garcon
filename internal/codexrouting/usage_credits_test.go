package codexrouting

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"garcon/internal/limits"
)

func TestUsageCreditsKeepConversationAndPreferIncludedQuota(t *testing.T) {
	r, s, _ := setup(t)
	prepare := func(session, want string) {
		t.Helper()
		req := request(session)
		done, err := r.Prepare(req)
		if err != nil {
			t.Fatal(err)
		}
		done()
		if got := req.Header.Get("ChatGPT-Account-Id"); got != want {
			t.Fatalf("account = %s, want %s", got, want)
		}
	}
	prepare("existing", "b")
	exhausted, balance := 100.0, 1234.5
	s.Accounts[1].Windows[0].UsedPercent = &exhausted
	s.Accounts[1].UsageCredits = &limits.UsageCredits{HasCredits: true, Balance: &balance}
	prepare("existing", "b")
	if got := r.Status().NextAccount; got != "a" {
		t.Fatalf("credits displaced included quota: %s", got)
	}
	prepare("new", "a")
	if err := r.Pin("b"); err != nil {
		t.Fatal(err)
	}
	if got := r.Status().NextAccount; got != "b" {
		t.Fatalf("credit-backed manual pin missing from status: %s", got)
	}
	prepare("manual", "b")
	// Restart retains the saved conversation and manual pin.
	restarted := New(r.dir, r.snapshot)
	restarted.now, restarted.health = r.now, r.health
	r = restarted
	prepare("existing", "b")
	if err := r.Pin(""); err != nil {
		t.Fatal(err)
	}
	for i := range s.Accounts {
		s.Accounts[i].Windows[0].UsedPercent = &exhausted
	}
	if got := r.Status().NextAccount; got != "b" {
		t.Fatalf("credit fallback unavailable: %s", got)
	}
	prepare("fallback", "b")
	// Credit-only plans need no invented subscription percentage/reset time.
	s.Accounts[1].Windows = nil
	prepare("credit-only", "b")
	balance = 0
	if _, err := r.Prepare(request("existing")); err == nil {
		t.Fatal("depleted credits allowed an exhausted conversation")
	}
}

func TestUsageCreditsCannotBypassAccountGuards(t *testing.T) {
	for _, condition := range []string{"stale", "old", "expired-window", "unknown-window", "login", "model", "cooldown", "not-enrolled", "other-account", "reset-only"} {
		t.Run(condition, func(t *testing.T) {
			r, s, home := setup(t)
			done, err := r.Prepare(request("existing"))
			if err != nil {
				t.Fatal(err)
			}
			done()
			exhausted, balance := 100.0, 1234.5
			a := &s.Accounts[1]
			a.Windows[0].UsedPercent = &exhausted
			a.UsageCredits = &limits.UsageCredits{HasCredits: true, Balance: &balance}
			switch condition {
			case "stale":
				a.Status = "stale"
			case "old":
				a.FetchedAt = r.now().Add(-11 * time.Minute).UnixMilli()
			case "expired-window":
				a.Windows[0].ResetsAt = r.now().Add(-time.Minute).UnixMilli()
			case "unknown-window":
				a.Windows[0].UsedPercent = nil
			case "login":
				saveLogin(t, filepath.Join(home, ".codex-b"), "b", r.now().Add(-time.Minute))
			case "model":
				r.health["b"] = health{models: map[string]bool{"other": true}, checked: r.now()}
			case "cooldown":
				r.Observe("b", &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"120"}}})
			case "not-enrolled":
				r.config.Accounts = []string{"a", "c"}
			case "other-account":
				s.Accounts[0].UsageCredits, a.UsageCredits = a.UsageCredits, nil
			case "reset-only":
				a.UsageCredits = nil
				count := 2
				a.ResetCredits = &limits.ResetCredits{AvailableCount: &count, Status: "fresh", FetchedAt: r.now().UnixMilli()}
			}
			if _, err := r.Prepare(request("existing")); err == nil || !strings.Contains(err.Error(), "conversation's Codex account is unavailable") {
				t.Fatalf("account guard bypassed: %v", err)
			}
		})
	}
}
