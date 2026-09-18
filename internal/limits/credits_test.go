package limits

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const resetFixture = `{"available_count":2,"credits":[{"id":"not-needed","status":"available","expires_at":"2026-10-05T04:18:43.187435Z"},{"status":"redeemed","expires_at":"2026-10-02T00:00:00Z"},{"status":"available","expires_at":null}]}`

func TestResetCreditsParseAvailabilityAndExpiration(t *testing.T) {
	c, err := parseResetCredits([]byte(resetFixture))
	if err != nil || *c.AvailableCount != 2 || len(c.Credits) != 2 || c.Credits[0].ExpiresAt != 0 || c.Credits[1].ExpiresAt != 1791173923187 {
		t.Fatalf("credits=%+v err=%v", c, err)
	}
	for _, raw := range []string{`{}`, `null`, `{"available_count":-1}`, `{"available_count":1,"credits":[{"status":"available","expires_at":"bad"}]}`} {
		if _, err := parseResetCredits([]byte(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	zero, err := parseResetCredits([]byte(`{"available_count":0,"credits":[]}`))
	if err != nil || zero.AvailableCount == nil || *zero.AvailableCount != 0 {
		t.Fatal("zero credits became unavailable")
	}
	b, _ := json.Marshal(c)
	if strings.Contains(string(b), "not-needed") {
		t.Fatal("unnecessary credit identifier persisted")
	}
}

func TestResetCreditCollectionDeduplicatesAndFailsIndependently(t *testing.T) {
	a := codexSource(jwt("a", "workspace", "a@example.com"), "")
	creditCalls, status := 0, 200
	s := testService(t, []source{a, a}, transport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			t.Fatal("credit collection attempted a mutation")
		}
		switch r.URL.String() {
		case codexURL:
			return reply(200, codexFixture), nil
		case creditsURL:
			creditCalls++
			if r.Header.Get("ChatGPT-Account-Id") != "workspace" {
				t.Fatal("wrong workspace")
			}
			res := reply(status, resetFixture)
			if status == 429 {
				res.Header.Set("Retry-After", "3600")
			}
			return res, nil
		default:
			t.Fatalf("unexpected endpoint: %s", r.URL)
			return nil, nil
		}
	}))
	s.Refresh(context.Background())
	first := s.Snapshot().Accounts[0]
	if creditCalls != 1 || *first.ResetCredits.AvailableCount != 2 {
		t.Fatal("duplicate credit collection")
	}
	status = 429
	next(s)
	failed := s.Snapshot().Accounts[0]
	if failed.Status != "fresh" || failed.ResetCredits.Status != "stale" || failed.ResetCredits.FetchedAt != first.ResetCredits.FetchedAt || *failed.ResetCredits.AvailableCount != 2 {
		t.Fatalf("failure erased quota or credits: %+v", failed)
	}
	next(s)
	if creditCalls != 2 {
		t.Fatal("credit rate limit backoff ignored")
	}
	// API callers must not mutate the cached count or expirations.
	*failed.ResetCredits.AvailableCount = 999
	failed.ResetCredits.Credits[0].ExpiresAt = 1
	if got := s.Snapshot().Accounts[0].ResetCredits; *got.AvailableCount != 2 || got.Credits[0].ExpiresAt == 1 {
		t.Fatal("snapshot aliases credit cache")
	}
}
