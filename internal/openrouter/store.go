// Package openrouter manages a local key without including it in the usage ledger.
package openrouter

import (
	"encoding/json"
	"errors"
	"garcon/internal/local"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Store struct {
	path string
	mu   sync.Mutex
}

func New(dir string) *Store { return &Store{path: filepath.Join(dir, "openrouter-key")} }
func localRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !local.Host(host) || !local.Host(r.Host) {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, e := url.Parse(origin)
		if e != nil || u.Host != r.Host || (u.Scheme != "http" && u.Scheme != "https") {
			return false
		}
	}
	return true
}
func (s *Store) key() (string, error) {
	b, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", errors.New("Could not read saved OpenRouter key")
	}
	return string(b), nil
}
func (s *Store) Authorize(r *http.Request) (int, error) {
	if !localRequest(r) {
		return 403, errors.New("OpenRouter requires a same-origin local client")
	}
	key, err := s.key()
	if err != nil {
		return 500, err
	}
	if key == "" {
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") && len(auth) > 7 && auth != "Bearer garcon-local" {
			r.Header.Del("ChatGPT-Account-Id")
			r.Header.Del("Cookie")
			return 0, nil
		}
		return 401, errors.New("Run garcon accounts key openrouter --key-stdin locally to add an OpenRouter key")
	}
	r.Header.Set("Authorization", "Bearer "+key)
	r.Header.Del("ChatGPT-Account-Id")
	r.Header.Del("Cookie")
	return 0, nil
}
func (s *Store) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if !localRequest(r) {
		http.Error(w, "local same-origin client required", 403)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.Method {
	case "GET", "HEAD":
		key, err := s.key()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"configured": key != ""})
	case "PUT":
		var v struct {
			Key string `json:"key"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		dec.DisallowUnknownFields()
		var extra any
		if dec.Decode(&v) != nil || dec.Decode(&extra) != io.EOF {
			http.Error(w, "invalid key", 400)
			return
		}
		key := strings.TrimSpace(v.Key)
		if len(key) < 8 || len(key) > 1024 || strings.ContainsAny(key, " \r\n\t") {
			http.Error(w, "invalid key", 400)
			return
		}
		if err := s.save(key); err != nil {
			http.Error(w, "Could not save OpenRouter key", 500)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"configured": true})
	case "DELETE":
		if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
			http.Error(w, "Could not remove OpenRouter key", 500)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"configured": false})
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		http.Error(w, "method not allowed", 405)
	}
}
func (s *Store) save(key string) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".openrouter-key-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.WriteString(key); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), s.path)
}

// Configured reveals presence only, never credentials.
func (s *Store) Configured() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, err := s.key()
	return key != "", err
}
