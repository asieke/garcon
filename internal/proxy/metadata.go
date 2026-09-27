package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"unicode"
)

// requestMetadata extracts attribution only. It never changes the forwarded
// bytes, rejects a request, or persists prompts or the full metadata.user_id.
// Headers work for any harness; body adapters cover clients without headers.
func requestMetadata(r *http.Request, harness string) (session, model string) {
	route, _ := ParseRoute(r.URL.Path)
	realtime := isCodexRealtime(route)
	if realtime {
		model = metadataID(r.URL.Query().Get("model"))
	}
	if harness == "claude" {
		session = metadataID(r.Header.Get("X-Claude-Code-Session-Id"))
	}
	for _, name := range []string{"Session_id", "Session-Id", "Thread-Id"} {
		if session == "" {
			session = metadataID(r.Header.Get(name))
		}
	}
	if r.Body == nil || r.Body == http.NoBody {
		return
	}
	const limit = 32 << 20
	body := r.Body
	b, err := io.ReadAll(io.LimitReader(body, limit+1))
	r.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(b), body), body}
	if err != nil || len(b) > limit {
		return
	}
	var payload struct {
		Model   string `json:"model"`
		Session struct {
			Model string `json:"model"`
		} `json:"session"`
		CacheKey       string                     `json:"prompt_cache_key"`
		Metadata       map[string]json.RawMessage `json:"metadata"`
		ClientMetadata map[string]json.RawMessage `json:"client_metadata"`
	}
	if json.Unmarshal(b, &payload) != nil {
		return
	}
	if payload.Model != "" {
		model = metadataID(payload.Model)
	} else if realtime && payload.Session.Model != "" {
		model = metadataID(payload.Session.Model)
	}
	value := func(m map[string]json.RawMessage, key string) string {
		var s string
		_ = json.Unmarshal(m[key], &s)
		return metadataID(s)
	}
	for _, m := range []map[string]json.RawMessage{payload.ClientMetadata, payload.Metadata} {
		for _, key := range []string{"session_id", "thread_id"} {
			if session == "" {
				session = value(m, key)
			}
		}
	}
	if session == "" && harness == "claude" {
		var user string
		_ = json.Unmarshal(payload.Metadata["user_id"], &user)
		var identity struct {
			SessionID string `json:"session_id"`
		}
		if json.Unmarshal([]byte(user), &identity) == nil {
			session = metadataID(identity.SessionID)
		} else if strings.HasPrefix(user, "user_") && strings.Contains(user, "_account_") {
			// Older Claude Code versions pack identity and session into a string.
			if i := strings.LastIndex(user, "_session_"); i >= 0 {
				session = metadataID(user[i+len("_session_"):])
			}
		}
	}
	if session == "" && harness == "codex" {
		session = metadataID(payload.CacheKey)
	}
	return
}

func metadataID(s string) string {
	if len(s) > 512 || strings.ContainsFunc(s, unicode.IsControl) {
		return ""
	}
	return strings.TrimSpace(s)
}
