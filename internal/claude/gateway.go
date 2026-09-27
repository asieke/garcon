package claude

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"garcon/internal/local"
)

// Gateway is an optional loopback-only bridge for Desktop's static gateway key.
// Authentication is independent of the optional session router.
type Gateway struct {
	Prepare func(*http.Request) (func(), error)
	keyHash [32]byte
	profile string
	load    func(context.Context, string) (Credentials, error)
	renew   func(context.Context, string) (bool, error)
}

// LoadGateway returns nil when the config is absent: credential injection is off
// by default. Existing /claude requests are never changed by this gateway.
func LoadGateway(path string) (*Gateway, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Claude gateway configuration: %w", err)
	}
	var c struct {
		Key     string `json:"key"`
		Profile string `json:"profile"`
	}
	if json.Unmarshal(b, &c) != nil {
		return nil, fmt.Errorf("invalid Claude gateway configuration JSON")
	}
	if strings.TrimSpace(c.Key) == "" || !filepath.IsAbs(c.Profile) {
		return nil, fmt.Errorf("Claude gateway requires a nonempty key and an absolute profile path")
	}
	renewer := NewRenewer()
	return &Gateway{keyHash: sha256.Sum256([]byte(c.Key)), profile: c.Profile, load: LoadContext, renew: renewer.Refresh}, nil
}

// Handler accepts only inference API paths. It replaces the local credential
// before handing the request to Garcon's normal proxy and usage recorder.
func (g *Gateway) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if g == nil {
			http.NotFound(w, r)
			return
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() || !local.Host(r.Host) || r.Header.Get("Origin") != "" {
			http.Error(w, "Claude gateway accepts local application requests only", http.StatusForbidden)
			return
		}
		key := r.Header.Get("X-Api-Key")
		if auth := r.Header.Get("Authorization"); auth != "" {
			kind, value, ok := strings.Cut(auth, " ")
			if !ok || !strings.EqualFold(kind, "Bearer") || key != "" {
				http.Error(w, "invalid gateway credential", http.StatusUnauthorized)
				return
			}
			key = value
		}
		hash := sha256.Sum256([]byte(key))
		if subtle.ConstantTimeCompare(hash[:], g.keyHash[:]) != 1 {
			http.Error(w, "invalid gateway credential", http.StatusUnauthorized)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/claude-gateway")
		allowed := r.Method == "GET" && path == "/v1/models" || r.Method == "POST" && (path == "/v1/messages" || path == "/v1/messages/count_tokens")
		if !allowed {
			http.NotFound(w, r)
			return
		}
		out := r.Clone(r.Context())
		out.URL.Path, out.URL.RawPath = "/claude"+path, ""
		if g.Prepare != nil {
			release, err := g.Prepare(out)
			if err != nil {
				status := http.StatusServiceUnavailable
				if e, ok := err.(interface{ HTTPStatus() int }); ok {
					status = e.HTTPStatus()
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"type": "garcon_routing_error", "message": err.Error()}})
				return
			}
			defer release()
		} else {
			if _, err := g.renew(r.Context(), g.profile); err != nil {
				http.Error(w, "Claude login renewal failed; sign in to the configured profile", http.StatusServiceUnavailable)
				return
			}
			creds, err := g.load(r.Context(), g.profile)
			if err != nil || creds.AccessToken == "" || creds.ExpiresAt > 0 && creds.ExpiresAt <= time.Now().UnixMilli() {
				http.Error(w, "Claude login unavailable or expired; sign in to the configured profile", http.StatusServiceUnavailable)
				return
			}
			out.Header.Set("Authorization", "Bearer "+creds.AccessToken)
		}
		out.Header.Del("X-Api-Key")
		out.Header.Del("Cookie")
		beta := out.Header.Get("Anthropic-Beta")
		found := false
		for _, value := range strings.Split(beta, ",") {
			if strings.TrimSpace(value) == "oauth-2025-04-20" {
				found = true
			}
		}
		if !found {
			if beta != "" {
				beta += ","
			}
			out.Header.Set("Anthropic-Beta", beta+"oauth-2025-04-20")
		}
		if out.Header.Get("Anthropic-Version") == "" {
			out.Header.Set("Anthropic-Version", "2023-06-01")
		}
		next.ServeHTTP(w, out)
	})
}

// Profile is the configured legacy profile, also included during initial discovery.
func (g *Gateway) Profile() string { return g.profile }

type gatewayDetailKey struct{}
type GatewayDetails struct {
	Session, Model, Account, AccountID string
	Observe                            func(*http.Response)
}

func WithGatewayDetails(req *http.Request, d GatewayDetails) {
	*req = *req.WithContext(context.WithValue(req.Context(), gatewayDetailKey{}, d))
}
func Details(req *http.Request) GatewayDetails {
	d, _ := req.Context().Value(gatewayDetailKey{}).(GatewayDetails)
	return d
}
