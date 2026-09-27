package connections

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Registration records an explicitly configured launcher, not a live session.
// Checking the file digest lets the read-only dashboard detect removal or edits
// without running shell scripts or assuming every Claude login uses Garcon.
type claudeLauncher struct {
	Path    string `json:"path"`
	Profile string `json:"profile"`
	Base    string `json:"base_url"`
	Digest  string `json:"sha256"`
}

func launcherRegistry(home string) string {
	return filepath.Join(home, ".config", "garcon", "claude-launchers.json")
}

func launcherDigest(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Size() > 1<<20 {
		return "", fmt.Errorf("launcher must be an executable file smaller than 1 MiB")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(b)), nil
}

func RegisterClaudeLauncher(home, launcher, profile, base string) error {
	launcher, err := filepath.Abs(launcher)
	if err != nil {
		return err
	}
	profile, err = filepath.Abs(profile)
	if err != nil {
		return err
	}
	if info, err := os.Stat(profile); err != nil || !info.IsDir() {
		return fmt.Errorf("Claude profile directory does not exist")
	}
	digest, err := launcherDigest(launcher)
	if err != nil {
		return err
	}
	path := launcherRegistry(home)
	var entries []claudeLauncher
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &entries); err != nil {
			return fmt.Errorf("invalid launcher registry: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	entry := claudeLauncher{launcher, profile, base, digest}
	found := false
	for i := range entries {
		if entries[i].Path == launcher {
			entries[i] = entry
			found = true
		}
	}
	if !found {
		entries = append(entries, entry)
	}
	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".claude-launchers-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func ClaudeConfigured(home, settings, base string) Status {
	status := Claude(settings, base)
	if status.Connected != nil && *status.Connected {
		status.Mode = "http"
	}
	b, err := os.ReadFile(launcherRegistry(home))
	if err != nil {
		return status
	}
	var entries []claudeLauncher
	if json.Unmarshal(b, &entries) != nil {
		return status
	}
	profiles := map[string]bool{}
	rc := true
	for _, entry := range entries {
		if !sameEndpoint(entry.Base, base) {
			continue
		}
		digest, err := launcherDigest(entry.Path)
		if err != nil || digest != entry.Digest {
			continue
		}
		if info, err := os.Stat(entry.Profile); err != nil || !info.IsDir() {
			continue
		}
		var config struct {
			RemoteControl bool `json:"remoteControlAtStartup"`
		}
		b, err := os.ReadFile(filepath.Join(entry.Profile, "settings.json"))
		if err != nil || json.Unmarshal(b, &config) != nil {
			rc = false
		} else {
			rc = rc && config.RemoteControl
		}
		profiles[entry.Profile] = true
	}
	if len(profiles) > 0 {
		configured := true
		status.Connected = &configured
		if status.Mode == "http" {
			status.Mode = "http+socket"
		} else {
			status.Mode = "socket"
		}
		status.Profiles = len(profiles)
		status.RemoteControl = rc
	}
	return status
}
