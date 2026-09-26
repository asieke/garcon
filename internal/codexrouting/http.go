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
			Enabled    *bool          `json:"enabled"`
			Accounts   []string       `json:"accounts"`
			Priorities map[string]int `json:"priorities"`
		}
		d := json.NewDecoder(io.LimitReader(req.Body, 32<<10))
		d.DisallowUnknownFields()
		if d.Decode(&config) != nil || config.Enabled == nil {
			WriteError(w, &routingError{400, "Expected an enabled boolean"})
			return
		}
		if err := r.ConfigurePriorities(*config.Enabled, config.Accounts, config.Priorities); err != nil {
			WriteError(w, &routingError{400, "Could not save configuration: check account selection and priorities (1–99)"})
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
