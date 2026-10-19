// Package telemetry implements opt-in, local host-based telemetry blocking.
// It never inspects request bodies or decrypts HTTPS traffic.
package telemetry

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
)

// Defaults is deliberately small. Wildcards match one complete DNS label.
var Defaults = []string{"http-intake.logs.*.datadoghq.com", "http-intake.logs.datadoghq.com", "http-intake.logs.datadoghq.eu"}

type Config struct {
	Enabled    bool     `json:"enabled"`
	Listen     string   `json:"listen,omitempty"`
	BlockHosts []string `json:"block_hosts,omitempty"`
	AllowHosts []string `json:"allow_hosts,omitempty"`
}

type Policy struct {
	Config
	blocked []string
	allowed []string
}

// Load reads an optional owner-managed configuration; absence disables blocking.
func Load(path string) (*Policy, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return New(Config{})
	}
	if err != nil {
		return nil, fmt.Errorf("telemetry configuration: %w", err)
	}
	defer f.Close()
	var c Config
	d := json.NewDecoder(io.LimitReader(f, 65537))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return nil, fmt.Errorf("invalid telemetry configuration: %w", err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("telemetry configuration must contain one JSON object")
	}
	return New(c)
}

func New(c Config) (*Policy, error) {
	if c.Enabled && c.Listen == "" {
		c.Listen = "127.0.0.1:4142"
	}
	if c.Listen != "" {
		host, port, err := net.SplitHostPort(c.Listen)
		ip := net.ParseIP(host)
		if err != nil || ip == nil || !ip.IsLoopback() || port == "" {
			return nil, fmt.Errorf("telemetry listen must be a literal loopback address and port")
		}
	}
	p := &Policy{Config: c}
	for _, list := range []struct {
		in  []string
		out *[]string
	}{{append(append([]string{}, Defaults...), c.BlockHosts...), &p.blocked}, {c.AllowHosts, &p.allowed}} {
		for _, raw := range list.in {
			h := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
			if !validHost(h, true) {
				return nil, fmt.Errorf("invalid telemetry host pattern %q: use a DNS host, with optional whole-label wildcards", raw)
			}
			*list.out = append(*list.out, h)
		}
	}
	return p, nil
}

func validHost(h string, wildcard bool) bool {
	if len(h) == 0 || len(h) > 253 {
		return false
	}
	labels := strings.Split(h, ".")
	if len(labels) < 2 {
		return false
	}
	concrete := 0
	for _, label := range labels {
		if wildcard && label == "*" {
			continue
		}
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
				return false
			}
		}
		concrete++
	}
	return concrete >= 2
}

func match(pattern, host string) bool {
	a, b := strings.Split(pattern, "."), strings.Split(host, ".")
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != "*" && a[i] != b[i] {
			return false
		}
	}
	return true
}

func (p *Policy) Blocked(host string) bool {
	if p == nil || !p.Enabled {
		return false
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, h := range p.allowed {
		if match(h, host) {
			return false
		}
	}
	for _, h := range p.blocked {
		if match(h, host) {
			return true
		}
	}
	return false
}

func clean(s string, max int) string {
	if len(s) > max {
		s = s[:max]
	}
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, s)
}

// Reject logs only attribution and destination metadata to the existing events view.
func (p *Policy) Reject(client, host, path string) bool {
	if !p.Blocked(host) {
		return false
	}
	log.Printf("telemetry blocked: client=%q host=%q path=%q", clean(client, 64), clean(host, 253), clean(path, 256))
	return true
}
