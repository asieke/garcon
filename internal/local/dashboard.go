package local

import (
	"net/http"
	"strings"
)

// DashboardAccess is a process policy, never a client-address or header policy.
// Only audited read endpoints are exposed remotely. New APIs fail closed,
// including action endpoints invoked with GET, HEAD, or a method override.
func DashboardAccess(next http.Handler, remote bool) http.Handler {
	if !remote {
		return next
	}
	reads := map[string]bool{
		"/api/config": true, "/api/accounts": true, "/api/usage": true, "/api/usage/recent": true,
		"/api/sessions": true, "/api/logs": true, "/api/events": true,
		"/api/analytics": true, "/api/nickname": true, "/api/limits": true,
		"/api/routing/codex": true, "/api/providers": true, "/api/prices": true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		read := r.Method == http.MethodGet || r.Method == http.MethodHead
		api := r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/")
		if !read || (api && !reads[r.URL.Path]) {
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "Remote mode: view-only. Dashboard changes are disabled. Run garcon providers, garcon accounts, or garcon routing locally on the server (garcon help for commands).", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
