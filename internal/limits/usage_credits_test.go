package limits

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodexUsageCredits(t *testing.T) {
	for _, tt := range []struct {
		name, credits string
		available     bool
	}{
		{"decimal", `{"has_credits":true,"unlimited":false,"balance":"1234.50"}`, true},
		{"numeric", `{"has_credits":true,"balance":12.5}`, true},
		{"hidden", `{"has_credits":true,"balance":null}`, true},
		{"omitted balance", `{"has_credits":true}`, true},
		{"unlimited", `{"has_credits":false,"unlimited":true,"balance":"0"}`, true},
		{"zero", `{"has_credits":true,"balance":"0"}`, false},
		{"disabled", `{"has_credits":false,"balance":"1234"}`, false},
		{"missing", `null`, false},
		{"empty", `{}`, false},
		{"negative", `{"has_credits":true,"balance":"-1"}`, false},
		{"nan", `{"has_credits":true,"balance":"NaN"}`, false},
		{"infinite", `{"has_credits":true,"balance":"Inf"}`, false},
		{"bad balance", `{"has_credits":true,"balance":{}}`, false},
		{"bad flag", `{"has_credits":"true","balance":"10"}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw := strings.TrimSuffix(codexFixture, "}") + `,"credits":` + tt.credits + `}`
			_, windows, credits, err := parseCodex([]byte(raw))
			if err != nil || len(windows) != 1 {
				t.Fatalf("optional credits disrupted quota: %v %+v", err, windows)
			}
			now := time.Now()
			a := Account{Provider: "codex", Status: "fresh", FetchedAt: now.UnixMilli(), UsageCredits: credits}
			if HasUsageCredits(a, now) != tt.available {
				t.Fatalf("availability = %v, want %v", HasUsageCredits(a, now), tt.available)
			}
		})
	}
}

func TestUsageCreditsRefreshPersistenceAndIsolation(t *testing.T) {
	src := codexSource(jwt("user", "workspace", "user@example.com"), "")
	body := `{"plan_type":"pro","credits":{"has_credits":true,"balance":"1234.5"}}`
	fail := false
	s := testService(t, []source{src}, transport(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected write to provider: %s", r.Method)
		}
		switch r.URL.String() {
		case codexURL:
			if fail {
				return nil, errors.New("unavailable")
			}
			return reply(200, body), nil
		case creditsURL:
			return reply(200, resetFixture), nil
		default:
			t.Fatalf("unexpected endpoint: %s", r.URL)
			return nil, nil
		}
	}))
	s.Refresh(context.Background())
	first := s.Snapshot().Accounts[0]
	if first.Status != "fresh" || !HasUsageCredits(first, s.now()) || *first.UsageCredits.Balance != 1234.5 || *first.ResetCredits.AvailableCount != 2 {
		t.Fatalf("credit-only snapshot not usable: %+v", first)
	}
	*first.UsageCredits.Balance = 0
	first.UsageCredits.HasCredits = false
	if !HasUsageCredits(s.Snapshot().Accounts[0], s.now()) {
		t.Fatal("snapshot mutated the collector")
	}
	raw, err := os.ReadFile(s.path)
	var saved Snapshot
	if err != nil || json.Unmarshal(raw, &saved) != nil || *saved.Accounts[0].UsageCredits.Balance != 1234.5 {
		t.Fatal("usage credits did not persist")
	}
	restarted := New(filepath.Dir(s.path))
	if HasUsageCredits(restarted.Snapshot().Accounts[0], s.now()) {
		t.Fatal("restart trusted persisted credits without a refresh")
	}
	if HasUsageCredits(s.Snapshot().Accounts[0], s.now().Add(11*time.Minute)) {
		t.Fatal("expired credit snapshot remained usable")
	}
	fail = true
	next(s)
	stale := s.Snapshot().Accounts[0]
	if stale.Status != "stale" || stale.UsageCredits == nil || HasUsageCredits(stale, s.now()) {
		t.Fatal("failed refresh trusted stale credits or erased the last reading")
	}
	fail = false
	body = `{"rate_limit":"malformed"}`
	next(s)
	stale = s.Snapshot().Accounts[0]
	if stale.Status != "stale" || stale.UsageCredits == nil || HasUsageCredits(stale, s.now()) {
		t.Fatal("malformed refresh trusted stale credits or erased the last reading")
	}
	body = strings.TrimSuffix(codexFixture, "}") + `,"credits":{"has_credits":false,"balance":"0"}}`
	next(s)
	if HasUsageCredits(s.Snapshot().Accounts[0], s.now()) {
		t.Fatal("depleted credits remained usable")
	}
	body = codexFixture
	next(s)
	if s.Snapshot().Accounts[0].UsageCredits != nil {
		t.Fatal("omitted credits reused an older balance")
	}
}
