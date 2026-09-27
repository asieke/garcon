// Package connections reports saved harness connections, independent of traffic.
package connections

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"garcon/internal/local"
	"github.com/pelletier/go-toml/v2"
)

type Status struct {
	Client        string `json:"client"`
	Connected     *bool  `json:"connected"`
	Mode          string `json:"mode,omitempty"`
	Profiles      int    `json:"profiles,omitempty"`
	RemoteControl bool   `json:"remote_control_at_startup,omitempty"`
}

// Codex checks the default user configuration. Per-launch command-line overrides
// and managed configuration are outside the scope of this saved connection.
func Codex(path, base string) Status {
	status := Status{Client: "codex"}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		value := false
		status.Connected = &value
		return status
	}
	if err != nil {
		return status
	}
	var config struct {
		Provider string `toml:"model_provider"`
		Profile  string `toml:"profile"`
		Profiles map[string]struct {
			Provider string `toml:"model_provider"`
		} `toml:"profiles"`
		Providers map[string]struct {
			Base string `toml:"base_url"`
			Wire string `toml:"wire_api"`
		} `toml:"model_providers"`
	}
	if toml.Unmarshal(b, &config) != nil {
		return status
	}
	provider := config.Provider
	if p := config.Profiles[config.Profile]; p.Provider != "" {
		provider = p.Provider
	}
	p := config.Providers[provider]
	value := (p.Wire == "" || p.Wire == "responses") && sameEndpoint(p.Base, strings.TrimRight(base, "/")+"/codex/backend-api/codex")
	status.Connected = &value
	return status
}

func sameEndpoint(raw, expected string) bool {
	a, err := url.Parse(raw)
	if err != nil {
		return false
	}
	b, err := url.Parse(expected)
	if err != nil {
		return false
	}
	return a.Scheme == b.Scheme && a.User == nil && a.RawQuery == "" && a.Fragment == "" &&
		a.Port() == b.Port() && strings.TrimRight(a.Path, "/") == strings.TrimRight(b.Path, "/") &&
		(strings.EqualFold(a.Hostname(), b.Hostname()) || (local.Host(a.Hostname()) && local.Host(b.Hostname())))
}

func Handler(base string) http.Handler {
	userHome, _ := os.UserHomeDir()
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		home = filepath.Join(userHome, ".codex")
	}
	claudeHome := os.Getenv("CLAUDE_CONFIG_DIR")
	if claudeHome == "" {
		claudeHome = filepath.Join(userHome, ".claude")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode([]Status{Codex(filepath.Join(home, "config.toml"), base), ClaudeConfigured(userHome, filepath.Join(claudeHome, "settings.json"), base)})
	})
}
