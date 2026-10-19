package limits

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"
)

// This is the CLI protocol version verified for reset reads, not the version of
// an installed CLI. The provider gates this unpublished field on client headers.
const claudeResetCompatibilityVersion = "2.1.303"

func claudeResetHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "claude-cli/"+claudeResetCompatibilityVersion+" (external, cli)")
	req.Header.Set("x-app", "cli")
	req.Header.Set("X-Claude-Code-Session-Id", uuid.NewString())
	req.Header.Set("Content-Type", "application/json")
}

// Saved resets are an optional block on the ordinary, read-only usage response.
// The unpublished schema was checked against Claude Code 2.1.303 and a live
// response. ends_at is the grant expiry; clears lists the windows it refills.
func parseClaudeResets(raw []byte, now time.Time) (*ResetCredits, error) {
	var response struct {
		Block json.RawMessage `json:"cedar_ember"`
	}
	if json.Unmarshal(raw, &response) != nil {
		return nil, errors.New("Claude resets unavailable")
	}
	if len(response.Block) == 0 || string(response.Block) == "null" {
		return nil, nil
	}
	var block struct {
		Eligible *bool             `json:"eligible"`
		Grants   []json.RawMessage `json:"grants"`
	}
	if json.Unmarshal(response.Block, &block) != nil || block.Eligible == nil {
		return nil, errors.New("Claude resets unavailable")
	}
	if !*block.Eligible {
		return nil, nil
	}
	count := 0
	credits := &ResetCredits{AvailableCount: &count, Credits: []ResetCredit{}, Status: "fresh", FetchedAt: now.UnixMilli()}
	for _, rawGrant := range block.Grants {
		var grant struct {
			Left   *int     `json:"resets_left"`
			Starts string   `json:"starts_at"`
			Ends   string   `json:"ends_at"`
			Clears []string `json:"clears"`
			Paused bool     `json:"paused"`
		}
		if json.Unmarshal(rawGrant, &grant) != nil || grant.Left == nil || *grant.Left < 0 {
			return nil, errors.New("Unrecognized Claude reset grant")
		}
		starts, ok := resetDate(grant.Starts)
		ends, endOK := resetDate(grant.Ends)
		if !ok || !endOK {
			return nil, errors.New("Unrecognized Claude reset expiration")
		}
		if *grant.Left == 0 || grant.Paused || starts > now.UnixMilli() || (ends > 0 && ends <= now.UnixMilli()) {
			continue
		}
		// usable_now may be false only because use_requires_limit is true.
		// It must not hide a saved reset that can be used at a future limit.
		if *grant.Left > math.MaxInt-count {
			return nil, errors.New("Unrecognized Claude reset count")
		}
		limits := []string{}
		seen := map[string]bool{}
		for _, limit := range grant.Clears {
			switch limit {
			case "five_hour", "seven_day", "seven_day_overage_included", "seven_day_opus", "seven_day_sonnet", "seven_day_cowork", "seven_day_omelette", "seven_day_oauth_apps":
				if !seen[limit] {
					limits = append(limits, limit)
					seen[limit] = true
				}
			}
		}
		count += *grant.Left
		credits.Credits = append(credits.Credits, ResetCredit{ExpiresAt: ends, Count: *grant.Left, Limits: limits})
	}
	sort.SliceStable(credits.Credits, func(i, j int) bool { return credits.Credits[i].ExpiresAt < credits.Credits[j].ExpiresAt })
	return credits, nil
}

func resetDate(value string) (int64, bool) {
	if value == "" {
		return 0, true
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	return t.UnixMilli(), err == nil
}
