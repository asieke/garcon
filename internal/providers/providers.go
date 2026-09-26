// Package providers manages explicit additional relay destinations. Built-in
// destinations are immutable so Codex cannot be redirected to another provider.
package providers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"garcon/internal/database"
	"garcon/internal/proxy"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sync"
)

type Registry struct {
	mu     sync.RWMutex
	db     *database.DB
	custom map[string]string
}

func New(db *database.DB) (*Registry, error) {
	r := &Registry{db: db, custom: map[string]string{}}
	if err := db.Get("providers", &r.custom); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	return r, nil
}
func (r *Registry) Snapshot() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := map[string]string{}
	for name, u := range proxy.Providers {
		result[name] = u.String()
	}
	for name, u := range r.custom {
		result[name] = u
	}
	return result
}
func (r *Registry) ParseRoute(path string) (proxy.Route, bool) {
	return proxy.ParseRouteWithProviders(path, r.Snapshot())
}

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

func (r *Registry) Set(name, target string) error {
	if !namePattern.MatchString(name) || proxy.Providers[name] != nil {
		return fmt.Errorf("choose a new provider name; built-in providers cannot be changed")
	}
	if target != "" {
		u, err := url.Parse(target)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return fmt.Errorf("provider URL must be an HTTPS origin without credentials, path, query, or fragment")
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	next := map[string]string{}
	for k, v := range r.custom {
		next[k] = v
	}
	if target == "" {
		if _, ok := next[name]; !ok {
			return fmt.Errorf("unknown custom provider")
		}
		delete(next, name)
	} else {
		next[name] = target
	}
	if err := r.db.Put("providers", next); err != nil {
		return err
	}
	r.custom = next
	return nil
}
func (r *Registry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method == "PUT" || req.Method == "DELETE" {
		var v struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, req.Body, 4096))
		d.DisallowUnknownFields()
		var extra any
		if d.Decode(&v) != nil || d.Decode(&extra) != io.EOF {
			http.Error(w, "invalid provider", 400)
			return
		}
		if req.Method == "DELETE" {
			v.URL = ""
		} else if v.URL == "" {
			http.Error(w, "provider URL required", 400)
			return
		}
		if err := r.Set(v.Name, v.URL); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
	} else if req.Method != "GET" && req.Method != "HEAD" {
		http.Error(w, "method not allowed", 405)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(r.Snapshot())
}
