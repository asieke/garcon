package limits

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

// This is the read-only endpoint used by Codex. No redemption endpoint is used.
const creditsURL = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits"

func parseResetCredits(raw []byte) (*ResetCredits, error) {
	var data struct {
		Available *int `json:"available_count"`
		Credits   []struct {
			Status    string `json:"status"`
			ExpiresAt string `json:"expires_at"`
		} `json:"credits"`
	}
	if json.Unmarshal(raw, &data) != nil || data.Available == nil || *data.Available < 0 {
		return nil, errors.New("Reset credits unavailable")
	}
	c := &ResetCredits{AvailableCount: data.Available, Credits: []ResetCredit{}, Status: "fresh"}
	for _, credit := range data.Credits {
		if credit.Status != "available" {
			continue
		}
		var expiry int64
		if credit.ExpiresAt != "" {
			t, err := time.Parse(time.RFC3339Nano, credit.ExpiresAt)
			if err != nil {
				return nil, errors.New("Unrecognized reset credit expiration")
			}
			expiry = t.UnixMilli()
		}
		c.Credits = append(c.Credits, ResetCredit{ExpiresAt: expiry})
	}
	sort.SliceStable(c.Credits, func(i, j int) bool { return c.Credits[i].ExpiresAt < c.Credits[j].ExpiresAt })
	return c, nil
}

func copyCredits(c *ResetCredits) *ResetCredits {
	if c == nil {
		return nil
	}
	copy := *c
	copy.Credits = append([]ResetCredit{}, c.Credits...)
	if c.AvailableCount != nil {
		n := *c.AvailableCount
		copy.AvailableCount = &n
	}
	return &copy
}

func staleCredits(c *ResetCredits, message string) *ResetCredits {
	c = copyCredits(c)
	if c == nil {
		c = &ResetCredits{Credits: []ResetCredit{}}
	}
	c.Status = "unavailable"
	if c.FetchedAt > 0 {
		c.Status = "stale"
	}
	c.Error = message
	return c
}

func (s *Service) collectCredits(ctx context.Context, src source, previous *ResetCredits) *ResetCredits {
	key := src.account.ID + ":credits"
	if s.now().Before(s.backoff[key]) {
		return staleCredits(previous, "Reset credit refresh is rate limited")
	}
	raw, err := s.fetch(ctx, creditsURL, src)
	if err == nil {
		var credits *ResetCredits
		credits, err = parseResetCredits(raw)
		if err == nil {
			credits.FetchedAt = s.now().UnixMilli()
			return credits
		}
	}
	var fe *fetchError
	if errors.As(err, &fe) && !fe.retry.IsZero() {
		s.backoff[key] = fe.retry
	}
	return staleCredits(previous, err.Error())
}
