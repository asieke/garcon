package limits

import (
	"math"
	"time"
)

// Headroom validates freshness and returns the tightest relevant allowance.
func Headroom(a Account, now time.Time, include func(Window) bool) (float64, string) {
	if a.Status != "fresh" || a.FetchedAt <= 0 || now.UnixMilli()-a.FetchedAt > 600000 {
		return 0, "Waiting for fresh usage limits"
	}
	remaining, found := 100.0, false
	for _, w := range a.Windows {
		if !include(w) {
			continue
		}
		if w.UsedPercent == nil || math.IsNaN(*w.UsedPercent) || math.IsInf(*w.UsedPercent, 0) || *w.UsedPercent < 0 || w.Expired || (w.ResetsAt > 0 && w.ResetsAt <= now.UnixMilli()) {
			return 0, "Waiting for fresh usage limits"
		}
		found = true
		remaining = math.Min(remaining, math.Max(0, 100-*w.UsedPercent))
	}
	if !found {
		return 0, "Usage limits unavailable"
	}
	if remaining <= 0 {
		return 0, "Usage exhausted"
	}
	return remaining, "Ready"
}

// QuotaScore is the shared Codex and Claude quota-per-hour selection formula.
func QuotaScore(a Account, now time.Time, include func(Window) bool) (float64, float64) {
	score, hours := math.Inf(1), 0.0
	found := false
	for _, w := range a.Windows {
		if !include(w) {
			continue
		}
		if w.UsedPercent == nil || w.ResetsAt <= now.UnixMilli() {
			return -1, 0
		}
		h := float64(w.ResetsAt-now.UnixMilli()) / 3600000
		v := math.Max(0, 100-*w.UsedPercent) / h
		if v < score {
			score, hours = v, h
		}
		found = true
	}
	if !found {
		return -1, 0
	}
	return score, hours
}
