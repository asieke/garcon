package claude

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// routed returns the path Garcon expects and whether the route had to be added. Claude Code addresses
// its API calls with ANTHROPIC_BASE_URL, so they already carry /claude; only its org policy
// poll (GET /api/claude_code/policy_limits) arrives bare and expects the socket's owner to route it.
func routed(path string) (string, bool) {
	const prefix = "/claude"
	if path == prefix || strings.HasPrefix(path, prefix+"/") {
		return path, false
	}
	return prefix + path, true
}

// handler relays one Claude Code session's socket to Garcon. Requests pass through unchanged except
// that the route is added when missing and the account's login when no Authorization is present.
func handler(garcon *url.URL, token func() string) http.Handler {
	return &httputil.ReverseProxy{
		FlushInterval: -1, // completions stream
		Transport: &http.Transport{
			Proxy:           nil,              // never let HTTP_PROXY divert a loopback call
			IdleConnTimeout: 90 * time.Second, // under Garcon's two-minute idle timeout
		},
		ErrorLog: log.New(io.Discard, "", 0), // the terminal belongs to Claude Code
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Path, _ = routed(pr.In.URL.Path)
			pr.Out.URL.RawPath = ""
			// SetURL clears Out.Host, so the request reaches Garcon addressed to 127.0.0.1 and passes
			// its loopback guard. A Director-style proxy would keep the inbound Host and be refused.
			pr.SetURL(garcon)
			if pr.Out.Header.Get("Authorization") == "" {
				pr.Out.Header.Set("Authorization", "Bearer "+token())
			}
		},
	}
}

// listen binds this invocation's socket, readable only by the user. The name stays short because a
// unix socket path is limited to about a hundred bytes and macOS puts $TMPDIR half way there already.
func listen() (net.Listener, string, error) {
	dir := os.TempDir()
	if runtime := os.Getenv("XDG_RUNTIME_DIR"); runtime != "" {
		dir = runtime
	}
	dir = filepath.Join(dir, "garcon")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("claude-%d.sock", os.Getpid()))
	os.Remove(path) // left by a crashed run whose pid was reused
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, "", fmt.Errorf("cannot bind %s: %w", path, err)
	}
	os.Chmod(path, 0o600)
	return ln, path, nil
}
