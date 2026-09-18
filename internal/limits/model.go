// Package limits reads provider subscription allowances independently of proxy traffic.
// Only normalized snapshots are stored; login stores are always read-only.
package limits

import (
	"crypto/sha256"
	"fmt"
)

const refreshSeconds = 300

type Window struct {
	ID            string   `json:"id"`
	Label         string   `json:"label"`
	UsedPercent   *float64 `json:"used_percent"`
	WindowSeconds int64    `json:"window_seconds"`
	ResetsAt      int64    `json:"resets_at"` // Unix milliseconds; zero means unknown.
	Expired       bool     `json:"expired"`
}

type Account struct {
	ID           string        `json:"id"`
	Provider     string        `json:"provider"`
	Email        string        `json:"email"`
	Workspace    string        `json:"workspace,omitempty"`
	Plan         string        `json:"plan,omitempty"`
	Status       string        `json:"status"` // fresh, stale, needs_login, unavailable
	Error        string        `json:"error,omitempty"`
	FetchedAt    int64         `json:"fetched_at"`
	Windows      []Window      `json:"windows"`
	ResetCredits *ResetCredits `json:"reset_credits,omitempty"`
}

type ResetCredit struct {
	ExpiresAt int64 `json:"expires_at"` // Unix milliseconds; zero means not reported.
}

type ResetCredits struct {
	AvailableCount *int          `json:"available_count"`
	Credits        []ResetCredit `json:"credits"`
	Status         string        `json:"status"` // fresh, stale, unavailable
	FetchedAt      int64         `json:"fetched_at"`
	Error          string        `json:"error,omitempty"`
}

type Snapshot struct {
	Accounts   []Account `json:"accounts"`
	Refreshing bool      `json:"refreshing"`
	UpdatedAt  int64     `json:"updated_at"`
	Error      string    `json:"error,omitempty"`
}

// The quota identity includes the person and workspace, not the harness or token.
func accountID(provider, person, workspace string) string {
	d := sha256.Sum256([]byte(provider + "\x00" + person + "\x00" + workspace))
	return fmt.Sprintf("%s:%x", provider, d[:16])
}
