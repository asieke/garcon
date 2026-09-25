package codexrouting

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
)

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if req.Method == http.MethodPut {
		if !localRequest(req) {
			WriteError(w, &routingError{403, "Routing changes require a same-origin local client"})
			return
		}
		var config struct {
			Enabled  *bool    `json:"enabled"`
			Accounts []string `json:"accounts"`
		}
		d := json.NewDecoder(io.LimitReader(req.Body, 1024))
		d.DisallowUnknownFields()
		if d.Decode(&config) != nil || config.Enabled == nil {
			WriteError(w, &routingError{400, "Expected an enabled boolean"})
			return
		}
		if err := r.ConfigureAccounts(*config.Enabled, config.Accounts); err != nil {
			WriteError(w, &routingError{400, "Could not save routing configuration or no Codex OAuth logins were found"})
			return
		}
		go r.Refresh(context.Background())
	} else if req.Method != http.MethodGet && req.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD, PUT")
		w.WriteHeader(405)
		return
	}
	json.NewEncoder(w).Encode(r.Status())
}
