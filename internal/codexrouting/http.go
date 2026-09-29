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
			PinnedAccount *string        `json:"pinned_account"`
			Enabled       *bool          `json:"enabled"`
			Accounts      []string       `json:"accounts"`
			Priorities    map[string]int `json:"priorities"`
		}
		d := json.NewDecoder(io.LimitReader(req.Body, 32<<10))
		d.DisallowUnknownFields()
		if d.Decode(&config) != nil {
			WriteError(w, &routingError{400, "Expected an account selection"})
			return
		}
		// Account-only updates preserve the pool when the field is omitted.
		if err := r.configure(false, config.Accounts, config.Priorities, config.PinnedAccount); err != nil {
			WriteError(w, &routingError{400, "Could not save configuration: check the account pool, local login, and priorities; unpin before removing the pinned account"})
			return
		}
		if config.PinnedAccount == nil || config.Accounts != nil {
			go r.Refresh(context.Background())
		}
	} else if req.Method != http.MethodGet && req.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD, PUT")
		w.WriteHeader(405)
		return
	}
	json.NewEncoder(w).Encode(r.Status())
}
