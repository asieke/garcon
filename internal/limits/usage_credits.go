package limits

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

func parseUsageCredits(raw []byte) *UsageCredits {
	var data struct {
		HasCredits bool            `json:"has_credits"`
		Unlimited  bool            `json:"unlimited"`
		Balance    json.RawMessage `json:"balance"`
	}
	if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &data) != nil {
		return nil
	}
	c := &UsageCredits{HasCredits: data.HasCredits, Unlimited: data.Unlimited}
	if len(data.Balance) > 0 && string(data.Balance) != "null" {
		// Codex reports a decimal string; accept a JSON number as well.
		balance := string(data.Balance)
		if data.Balance[0] == '"' && json.Unmarshal(data.Balance, &balance) != nil {
			return nil
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(balance), 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return nil
		}
		c.Balance = &value
	}
	return c
}

func fresh(a Account, now time.Time) bool {
	return a.Status == "fresh" && a.FetchedAt > 0 && now.UnixMilli()-a.FetchedAt <= 600000
}

// HasUsageCredits requires a fresh Codex usage response. An undisclosed balance
// may still be available according to the provider's explicit has_credits flag.
func HasUsageCredits(a Account, now time.Time) bool {
	c := a.UsageCredits
	if a.Provider != "codex" || !fresh(a, now) || c == nil {
		return false
	}
	if c.Unlimited {
		return true
	}
	return c.HasCredits && (c.Balance == nil || (*c.Balance > 0 && !math.IsNaN(*c.Balance) && !math.IsInf(*c.Balance, 0)))
}

func copyUsageCredits(c *UsageCredits) *UsageCredits {
	if c == nil {
		return nil
	}
	copy := *c
	if c.Balance != nil {
		balance := *c.Balance
		copy.Balance = &balance
	}
	return &copy
}
