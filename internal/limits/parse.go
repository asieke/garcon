package limits

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

func validPercent(p *float64) *float64 {
	if p == nil || math.IsNaN(*p) || math.IsInf(*p, 0) || *p < 0 {
		return nil
	}
	return p
}

func durationLabel(seconds int64) string {
	switch seconds {
	case 604800:
		return "Weekly"
	case 18000:
		return "5-hour"
	case 0:
		return "Usage"
	default:
		if seconds%3600 == 0 {
			return fmt.Sprintf("%d-hour", seconds/3600)
		}
		return fmt.Sprintf("%d-minute", seconds/60)
	}
}

func sortWindows(ws []Window) {
	rank := func(w Window) int {
		if w.Label == "Weekly" {
			return 0
		}
		if w.Label == "5-hour" {
			return 1
		}
		return 2
	}
	sort.SliceStable(ws, func(i, j int) bool {
		if rank(ws[i]) != rank(ws[j]) {
			return rank(ws[i]) < rank(ws[j])
		}
		return ws[i].Label < ws[j].Label
	})
}

type codexWindow struct {
	Used    *float64 `json:"used_percent"`
	Seconds int64    `json:"limit_window_seconds"`
	Reset   int64    `json:"reset_at"`
}
type codexBucket struct {
	Primary   *codexWindow `json:"primary_window"`
	Secondary *codexWindow `json:"secondary_window"`
}

func parseCodex(raw []byte) (string, []Window, error) {
	var data struct {
		Plan       string       `json:"plan_type"`
		Rate       *codexBucket `json:"rate_limit"`
		Review     *codexBucket `json:"code_review_rate_limit"`
		Additional []struct {
			Name    string       `json:"limit_name"`
			ID      string       `json:"limit_id"`
			Feature string       `json:"metered_feature"`
			Rate    *codexBucket `json:"rate_limit"`
		} `json:"additional_rate_limits"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return "", nil, errors.New("Unrecognized provider response")
	}
	ws := []Window{}
	add := func(id, label string, b *codexBucket) {
		if b == nil {
			return
		}
		for i, w := range []*codexWindow{b.Primary, b.Secondary} {
			if w == nil {
				continue
			}
			name := durationLabel(w.Seconds)
			if label != "" {
				name = label + " · " + name
			}
			ws = append(ws, Window{ID: fmt.Sprintf("%s:%d", id, i), Label: name, UsedPercent: validPercent(w.Used), WindowSeconds: max(0, w.Seconds), ResetsAt: max(0, w.Reset) * 1000})
		}
	}
	add("codex", "", data.Rate)
	add("review", "Code review", data.Review)
	for i, b := range data.Additional {
		name := b.Name
		if name == "" {
			name = b.Feature
		}
		if name == "" {
			name = "Additional limit"
		}
		id := b.ID
		if id == "" {
			id = fmt.Sprintf("additional:%d", i)
		}
		add(id, name, b.Rate)
	}
	sortWindows(ws)
	return data.Plan, ws, nil
}

func parseClaude(raw []byte) ([]Window, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, errors.New("Unrecognized provider response")
	}
	ws := []Window{}
	var limits []struct {
		Kind    string   `json:"kind"`
		Group   string   `json:"group"`
		Percent *float64 `json:"percent"`
		Reset   string   `json:"resets_at"`
		Scope   *struct {
			Model *struct {
				Name string `json:"display_name"`
			} `json:"model"`
			Surface *struct {
				Name string `json:"display_name"`
			} `json:"surface"`
		} `json:"scope"`
	}
	if b, ok := fields["limits"]; ok && string(b) != "null" {
		if json.Unmarshal(b, &limits) != nil {
			return nil, errors.New("Unrecognized provider limits")
		}
	}
	seen := map[string]bool{}
	for _, l := range limits {
		var seconds int64
		switch l.Group {
		case "weekly":
			seconds = 604800
		case "session":
			seconds = 18000
		default:
			continue
		}
		label := durationLabel(seconds)
		if l.Scope != nil {
			parts := []string{}
			if l.Scope.Model != nil && l.Scope.Model.Name != "" {
				parts = append(parts, l.Scope.Model.Name)
			}
			if l.Scope.Surface != nil && l.Scope.Surface.Name != "" {
				parts = append(parts, l.Scope.Surface.Name)
			}
			if len(parts) > 0 {
				label = strings.Join(parts, " / ") + " · " + label
			}
		}
		id := l.Kind + ":" + label
		if seen[id] {
			continue
		}
		seen[id] = true
		t, _ := time.Parse(time.RFC3339Nano, l.Reset)
		var reset int64
		if !t.IsZero() {
			reset = t.UnixMilli()
		}
		ws = append(ws, Window{ID: id, Label: label, UsedPercent: validPercent(l.Percent), WindowSeconds: seconds, ResetsAt: reset})
	}
	// Named limits are authoritative. Fill only missing legacy equivalents.
	for _, key := range []string{"five_hour", "seven_day", "seven_day_opus", "seven_day_sonnet", "seven_day_fable", "seven_day_cowork", "seven_day_oauth_apps"} {
		b, ok := fields[key]
		if !ok || string(b) == "null" {
			continue
		}
		seconds := int64(604800)
		label := "Weekly"
		if key == "five_hour" {
			seconds = 18000
			label = "5-hour"
		} else if key != "seven_day" {
			name := map[string]string{"seven_day_opus": "Opus", "seven_day_sonnet": "Sonnet", "seven_day_fable": "Fable", "seven_day_cowork": "Cowork", "seven_day_oauth_apps": "OAuth apps"}[key]
			label = name + " · Weekly"
		}
		found := false
		for _, w := range ws {
			if w.Label == label {
				found = true
			}
		}
		if found {
			continue
		}
		var w struct {
			Used  *float64 `json:"utilization"`
			Reset string   `json:"resets_at"`
		}
		if json.Unmarshal(b, &w) != nil {
			return nil, errors.New("Unrecognized provider limits")
		}
		t, _ := time.Parse(time.RFC3339Nano, w.Reset)
		var reset int64
		if !t.IsZero() {
			reset = t.UnixMilli()
		}
		ws = append(ws, Window{ID: key, Label: label, UsedPercent: validPercent(w.Used), WindowSeconds: seconds, ResetsAt: reset})
	}
	sortWindows(ws)
	return ws, nil
}
