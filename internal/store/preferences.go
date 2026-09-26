package store

import (
	"database/sql"
	"encoding/json"
	"garcon/internal/local"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// Nicknames are shared by the compact widget and future account displays.
// This narrow API never accepts credential fields or arbitrary settings keys.
func (s *Store) Nickname(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	account := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("account")))
	if account == "" || len(account) > 256 {
		http.Error(w, "invalid account", 400)
		return
	}
	switch r.Method {
	case "GET", "HEAD":
		var nickname string
		err := s.DB.Get("nickname:"+account, &nickname)
		if err != nil && err != sql.ErrNoRows {
			http.Error(w, "could not read nickname", 500)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"nickname": nickname, "exists": err == nil})
	case "PUT":
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || !local.Host(host) || !local.Host(r.Host) {
			http.Error(w, "local client required", 403)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host {
				http.Error(w, "cross-origin request refused", 403)
				return
			}
		}
		var v struct {
			Nickname string `json:"nickname"`
		}
		dec := json.NewDecoder(io.LimitReader(r.Body, 1024))
		dec.DisallowUnknownFields()
		if dec.Decode(&v) != nil || len([]rune(v.Nickname)) > 40 {
			http.Error(w, "invalid nickname", 400)
			return
		}
		v.Nickname = strings.TrimSpace(v.Nickname)
		if err = s.DB.Put("nickname:"+account, v.Nickname); err != nil {
			http.Error(w, "could not save nickname", 500)
			return
		}
		json.NewEncoder(w).Encode(v)
	default:
		w.Header().Set("Allow", "GET, HEAD, PUT")
		http.Error(w, "method not allowed", 405)
	}
}
