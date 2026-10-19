package limits

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The allowed fields of a live read on 2026-10-18; identifiers and event_props
// are intentionally excluded. No credentials or profile files are fixtures.
const claudeResetFixture = `{"cedar_ember":{"eligible":true,"ineligible_reason":null,"at_limit":false,"grants":[{"resets_total":1,"resets_left":1,"starts_at":"2026-09-22T16:00:00+00:00","ends_at":"2026-10-22T16:00:00+00:00","clears":["five_hour","seven_day","seven_day_overage_included"],"paused":false,"usable_now":true,"use_requires_limit":false}],"weekly_resets_at":"2026-10-19T21:00:00+00:00","cooldown_until":null}}`
const claudeIneligibleFixture = `{"cedar_ember":{"eligible":false,"ineligible_reason":"surface","at_limit":false,"exhausted":[],"grants":[],"next_grant_id":null,"weekly_resets_at":null,"cooldown_until":null,"event_props":null}}`

var resetNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

func TestClaudeResetObservedShape(t *testing.T) {
	c, err := parseClaudeResets([]byte(claudeResetFixture), resetNow)
	if err != nil || c == nil || *c.AvailableCount != 1 || c.Status != "fresh" || len(c.Credits) != 1 {
		t.Fatalf("credits=%+v err=%v", c, err)
	}
	grant := c.Credits[0]
	if grant.ExpiresAt != time.Date(2026, 10, 22, 16, 0, 0, 0, time.UTC).UnixMilli() || grant.Count != 1 || !reflect.DeepEqual(grant.Limits, []string{"five_hour", "seven_day", "seven_day_overage_included"}) {
		t.Fatalf("wrong count, expiry or coverage: %+v", grant)
	}
	for _, raw := range []string{`{}`, `{"cedar_ember":null}`, claudeIneligibleFixture} {
		c, err := parseClaudeResets([]byte(raw), resetNow)
		if c != nil || err != nil {
			t.Fatalf("absent/ineligible resets became an error: %+v %v", c, err)
		}
	}
	for _, raw := range []string{`{`, `{"cedar_ember":[]}`, `{"cedar_ember":{}}`, `{"cedar_ember":{"eligible":true,"grants":"bad"}}`, `{"cedar_ember":{"eligible":true,"grants":[{"resets_left":-1}]}}`, `{"cedar_ember":{"eligible":true,"grants":[{"resets_left":1,"ends_at":"bad"}]}}`} {
		if _, err := parseClaudeResets([]byte(raw), resetNow); err == nil {
			t.Fatalf("accepted malformed response %s", raw)
		}
	}
}

func TestClaudeResetsExcludeUnavailableGrantsAndPreserveUnknowns(t *testing.T) {
	raw := `{"cedar_ember":{"eligible":true,"grants":[
		{"resets_left":1,"ends_at":"2026-10-18T12:00:00Z"},
		{"resets_left":1,"starts_at":"2027-01-01T00:00:00Z"},
		{"resets_left":1,"paused":true},
		{"resets_left":0},
		{"resets_left":2,"ends_at":"2026-10-25T00:00:00Z","clears":["five_hour","five_hour","new_unknown_limit"],"usable_now":false,"use_requires_limit":true},
		{"resets_left":1,"ends_at":null,"clears":[],"id":"not-stored","event_props":{"secret":"not-stored"}},
		{"resets_left":1,"ends_at":"2026-10-22T00:00:00Z","clears":["seven_day"]}
	]}}`
	c, err := parseClaudeResets([]byte(raw), resetNow)
	if err != nil || c == nil || *c.AvailableCount != 4 || len(c.Credits) != 3 {
		t.Fatalf("credits=%+v err=%v", c, err)
	}
	if c.Credits[0].ExpiresAt != 0 || len(c.Credits[0].Limits) != 0 || c.Credits[2].Count != 2 || !reflect.DeepEqual(c.Credits[2].Limits, []string{"five_hour"}) {
		t.Fatalf("unknown/ordered/grouped grants lost: %+v", c.Credits)
	}
	b, _ := json.Marshal(c)
	if strings.Contains(string(b), "not-stored") || strings.Contains(string(b), "event_props") {
		t.Fatal("unneeded provider metadata persisted")
	}
	copy := copyCredits(c)
	copy.Credits[2].Limits[0] = "mutated"
	if c.Credits[2].Limits[0] != "five_hour" {
		t.Fatal("credit coverage aliases snapshot")
	}
}

func TestClaudeResetCollectionIsOneReadAndDoesNotClaim(t *testing.T) {
	src := source{token: "disposable-test-token", account: Account{Provider: "claude"}}
	status, polls, profiles := 200, 0, 0
	body := strings.TrimSuffix(claudeFixture, "}") + "," + strings.TrimPrefix(claudeResetFixture, "{")
	s := testService(t, []source{src, src}, transport(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Host != "api.anthropic.com" || strings.Contains(r.URL.Path, "reset_rate_limits") {
			t.Fatalf("reset collector attempted an unexpected operation: %s %s", r.Method, r.URL)
		}
		switch r.URL.String() {
		case profileURL:
			profiles++
			if r.Header.Get("x-app") != "" {
				t.Fatal("reset compatibility headers escaped the usage request")
			}
			return reply(200, `{"account":{"uuid":"test-person"},"organization":{"uuid":"test-space"}}`), nil
		case claudeURL:
			polls++
			if r.URL.Query().Get("cedar_ember") != "1" || r.URL.Query().Get("skip_spend") != "1" || r.Header.Get("x-app") != "cli" || r.Header.Get("X-Claude-Code-Session-Id") == "" || r.Header.Get("User-Agent") != "claude-cli/2.1.303 (external, cli)" {
				t.Fatal("saved reset read lost verified query/client identification")
			}
			res := reply(status, body)
			if status == 429 {
				res.Header.Set("Retry-After", "3600")
			}
			return res, nil
		default:
			t.Fatalf("unexpected endpoint: %s", r.URL)
			return nil, nil
		}
	}))
	s.now = func() time.Time { return resetNow }
	s.Refresh(context.Background())
	a := s.Snapshot().Accounts[0]
	windows, _ := parseClaude([]byte(claudeFixture))
	if polls != 1 || profiles != 1 || !reflect.DeepEqual(a.Windows, windows) || a.ResetCredits == nil || *a.ResetCredits.AvailableCount != 1 {
		t.Fatalf("extra reads or lost windows/resets: polls=%d profiles=%d account=%+v", polls, profiles, a)
	}
	// Optional malformed data must never spoil valid quota windows.
	body = strings.TrimSuffix(claudeFixture, "}") + `,"cedar_ember":"bad"}`
	next(s)
	a = s.Snapshot().Accounts[0]
	if a.Status != "fresh" || a.ResetCredits.Status != "stale" || *a.ResetCredits.AvailableCount != 1 {
		t.Fatalf("malformed optional block spoiled quota: %+v", a)
	}
	status = 429
	next(s)
	next(s)
	a = s.Snapshot().Accounts[0]
	if polls != 3 || a.Status != "stale" || a.ResetCredits.Status != "stale" || !reflect.DeepEqual(a.Windows, windows) {
		t.Fatalf("rate limit lost snapshot or ignored backoff: polls=%d account=%+v", polls, a)
	}
	// A new successful response with no block must remove an old offer.
	s.backoff = map[string]time.Time{}
	status, body = 200, claudeFixture
	next(s)
	if a = s.Snapshot().Accounts[0]; a.Status != "fresh" || a.ResetCredits != nil {
		t.Fatalf("removed reset offer retained: %+v", a)
	}
}
