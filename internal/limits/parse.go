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

func parseCodex(raw []byte) (string, []Window, *UsageCredits, error) {
	var data struct {
		Plan       string          `json:"plan_type"`
		Rate       *codexBucket    `json:"rate_limit"`
		Review     *codexBucket    `json:"code_review_rate_limit"`
		Credits    json.RawMessage `json:"credits"`
		Additional []struct {
			Name    string       `json:"limit_name"`
			ID      string       `json:"limit_id"`
			Feature string       `json:"metered_feature"`
			Rate    *codexBucket `json:"rate_limit"`
		} `json:"additional_rate_limits"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return "", nil, nil, errors.New("Unrecognized provider response")
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
	return data.Plan, ws, parseUsageCredits(data.Credits), nil
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

func parseAGY(raw []byte) ([]Window, error) {
	var data struct {
		Command struct {
			Name string `json:"name"`
			Data struct {
				Groups []struct {
					Name    string `json:"name"`
					Buckets []struct {
						ID                string   `json:"id"`
						Name              string   `json:"name"`
						Window            string   `json:"window"`
						RemainingFraction *float64 `json:"remaining_fraction"`
						ResetTime         string   `json:"reset_time"`
					} `json:"buckets"`
				} `json:"groups"`
			} `json:"data"`
		} `json:"command"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, errors.New("Unrecognized provider response")
	}
	ws := []Window{}
	for _, g := range data.Command.Data.Groups {
		prefix := strings.TrimSpace(g.Name)
		switch strings.ToLower(prefix) {
		case "", "gemini models", "gemini":
			prefix = ""
		case "claude and gpt models":
			prefix = "Claude & GPT"
		}
		for _, b := range g.Buckets {
			var seconds int64
			switch strings.ToLower(b.Window) {
			case "5h", "5-hour", "session":
				seconds = 18000
			case "weekly", "7d":
				seconds = 604800
			default:
				continue
			}
			label := durationLabel(seconds)
			if prefix != "" {
				label = prefix + " · " + label
			}
			var usedPtr *float64
			if b.RemainingFraction != nil && !math.IsNaN(*b.RemainingFraction) && !math.IsInf(*b.RemainingFraction, 0) {
				u := math.Max(0, math.Min(100, (1.0-*b.RemainingFraction)*100.0))
				usedPtr = validPercent(&u)
			}
			t, _ := time.Parse(time.RFC3339Nano, b.ResetTime)
			var reset int64
			if !t.IsZero() {
				reset = t.UnixMilli()
			}
			id := b.ID
			if id == "" {
				id = "agy:" + label
			}
			ws = append(ws, Window{ID: id, Label: label, UsedPercent: usedPtr, WindowSeconds: seconds, ResetsAt: reset})
		}
	}
	sortWindows(ws)
	return ws, nil
}

// museWindow accepts Muse's protocol (camelCase) and stored (snake_case)
// spellings of one subscription window. Percentages are verbatim and may
// exceed 100 when the account is over quota.
type museWindow struct {
	UsedPercent       *float64 `json:"used_percent"`
	UsedPercentCamel  *float64 `json:"usedPercent"`
	DurationMins      float64  `json:"window_duration_mins"`
	DurationMinsCamel float64  `json:"windowDurationMins"`
	ResetsAt          any      `json:"resets_at"`
	ResetsAtMs        any      `json:"resets_at_ms"`
	ResetsAtMsCamel   any      `json:"resetsAtMs"`
}

type museUsage struct {
	ObservedAtMs int64       `json:"observedAtMs"`
	Tier         string      `json:"tier"`
	Window       *museWindow `json:"window"`
	Weekly       *museWindow `json:"weekly"`
}

func (w *museWindow) used() *float64 {
	if w.UsedPercentCamel != nil {
		return validPercent(w.UsedPercentCamel)
	}
	return validPercent(w.UsedPercent)
}

func (w *museWindow) reset() int64 {
	for _, v := range []any{w.ResetsAtMsCamel, w.ResetsAtMs, w.ResetsAt} {
		if ms := parseMuseReset(v); ms > 0 {
			return ms
		}
	}
	return 0
}

func (w *museWindow) seconds(fallback int64) int64 {
	for _, mins := range []float64{w.DurationMinsCamel, w.DurationMins} {
		if mins > 0 && mins < 60*24*31 {
			return int64(mins) * 60
		}
	}
	return fallback
}

// parseMuseReset normalizes an epoch in seconds, milliseconds or
// microseconds, or an RFC 3339 string, to milliseconds.
func parseMuseReset(v any) int64 {
	switch x := v.(type) {
	case float64:
		return epochMillis(int64(x))
	case string:
		if t, err := time.Parse(time.RFC3339Nano, x); err == nil && !t.IsZero() {
			return t.UnixMilli()
		}
	}
	return 0
}

func epochMillis(v int64) int64 {
	switch {
	case v <= 0:
		return 0
	case v >= 1e17:
		return v / 1e6
	case v >= 1e14:
		return v / 1e3
	case v >= 1e11:
		return v
	default:
		return v * 1000
	}
}

// parseMuseWindows returns no windows when Muse has not reported usage; a
// blank window must never look like an observed zero.
func parseMuseWindows(u *museUsage) []Window {
	if u == nil {
		return nil
	}
	ws := []Window{}
	if u.Window != nil && (u.Window.used() != nil || u.Window.reset() > 0) {
		sec := u.Window.seconds(18000)
		ws = append(ws, Window{ID: "muse:window", Label: durationLabel(sec), UsedPercent: u.Window.used(), WindowSeconds: sec, ResetsAt: u.Window.reset()})
	}
	if u.Weekly != nil && (u.Weekly.used() != nil || u.Weekly.reset() > 0) {
		ws = append(ws, Window{ID: "muse:weekly", Label: "Weekly", UsedPercent: u.Weekly.used(), WindowSeconds: 604800, ResetsAt: u.Weekly.reset()})
	}
	sortWindows(ws)
	return ws
}
