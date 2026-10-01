// Package proxy routes harness URLs to their provider, with automatic account attribution,
// forwarding every request unchanged and recording completion calls.
package proxy

import (
	"crypto/rand"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptrace"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"garcon/internal/accounts"
	"garcon/internal/claude"
	"garcon/internal/codexrouting"
	"garcon/internal/usage"
)

// Providers maps the URL segment to the upstream.
var Providers = map[string]*url.URL{
	"anthropic":  {Scheme: "https", Host: "api.anthropic.com"},
	"openai":     {Scheme: "https", Host: "api.openai.com"},
	"openrouter": {Scheme: "https", Host: "openrouter.ai"},
	"chatgpt":    {Scheme: "https", Host: "chatgpt.com"},
}

// ImplicitProvider lists harnesses whose URLs predate the provider segment and imply it.
var ImplicitProvider = map[string]string{"claude": "anthropic", "codex": "chatgpt"}

// ProviderOf mirrors the dashboard: rows from before the provider segment
// existed imply it from the harness.
func ProviderOf(r usage.Record) string {
	if r.Provider != "" {
		return r.Provider
	}
	if p, ok := ImplicitProvider[r.Harness]; ok {
		return p
	}
	return "anthropic"
}

// Route is a parsed proxy path.
type Route struct {
	Harness, Provider, Rest string
	Target                  *url.URL
}

// ParseRoute splits /<harness>/[<provider>/]<rest>. Claude and Codex
// imply their provider. Account labels are not accepted in routing paths.
func ParseRoute(path string) (Route, bool) {
	harness, rest, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
	if harness == "" {
		return Route{}, false
	}
	provider, implicit := ImplicitProvider[harness]
	if implicit {
		first, _, _ := strings.Cut(rest, "/")
		if harness == "claude" && first != "" && first != "v1" && first != "api" {
			return Route{}, false
		}
		if harness == "codex" && first != "" && first != "backend-api" && first != "v1" {
			return Route{}, false
		}
	} else {
		provider, rest, _ = strings.Cut(rest, "/")
	}
	target := Providers[provider]
	if target == nil {
		return Route{}, false
	}
	return Route{Harness: harness, Provider: provider, Rest: rest, Target: target}, true
}

// IsCompletion reports whether an upstream path is a model call worth recording:
// Anthropic Messages, OpenAI Responses (also Codex's chatgpt.com backend) or
// OpenAI-compatible chat completions (OpenAI, OpenRouter, Hermes, OpenClaw).
func IsCompletion(rest string) bool {
	return strings.HasSuffix(rest, "/messages") || strings.HasSuffix(rest, "/responses") || strings.HasSuffix(rest, "/chat/completions")
}

// Codex creates voice calls through its model provider, but may join the call's
// sideband directly with its own login. Never pool either half of that session:
// changing the call owner makes the sideband handshake fail with a 404.
func isCodexRealtime(rt Route) bool {
	if rt.Harness != "codex" || rt.Provider != "chatgpt" {
		return false
	}
	for _, root := range []string{"backend-api/codex/realtime", "v1/realtime", "v1/live"} {
		if rt.Rest == root || strings.HasPrefix(rt.Rest, root+"/") {
			return true
		}
	}
	return false
}

// Built-in image calls do not carry a coding conversation ID, and image edits
// may use multipart bodies. Keep the caller's image entitlement and payload;
// coding-session assignments, model checks, and pins do not apply here.
func isCodexImage(rt Route) bool {
	return rt.Harness == "codex" && rt.Provider == "chatgpt" &&
		(rt.Rest == "backend-api/codex/images/generations" || rt.Rest == "backend-api/codex/images/edits")
}

// Proxy forwards routed requests and hands each completion's Record to Save.
type Proxy struct {
	Save     func(usage.Record)
	Start    func(usage.Record)
	Accounts accounts.Resolver
	Codex    *codexrouting.Router
}

// Serve proxies one routed request.
func (p *Proxy) Serve(w http.ResponseWriter, r *http.Request, rt Route) {
	start := time.Now()
	rec := usage.Record{RequestID: fmt.Sprintf("%x", randomID()), Time: start.UnixMilli(), Harness: rt.Harness, Provider: rt.Provider, Method: r.Method, Path: r.URL.Path, Kind: "request", State: "streaming"}
	if IsCompletion(rt.Rest) {
		rec.Kind = "completion"
	}
	capture := func() {
		d := codexrouting.Details(r)
		if d.Session != "" {
			rec.SessionID = d.Session
		}
		if d.Model != "" {
			rec.Model = d.Model
		}
		rec.AccountID = d.AccountID
		rec.Account = d.Account
		if rec.Account == "" {
			rec.Account = p.Accounts.Resolve(rt.Provider, r.Header)
		}
	}
	routed := p.Codex != nil && rt.Harness == "codex" && rt.Provider == "chatgpt" && p.Codex.Enabled() && !isCodexRealtime(rt) && !isCodexImage(rt)
	if routed {
		// Clone before replacing identity so callers and other middleware keep
		// their original request. The actual upstream identity drives attribution.
		r = r.Clone(r.Context())
		release, err := p.Codex.Prepare(r)
		if err != nil {
			capture()
			rec.Status = codexrouting.ErrorStatus(err)
			rec.State = "failed"
			rec.Error = err.Error()
			rec.Ms = time.Since(start).Milliseconds()
			if p.Save != nil {
				p.Save(rec)
			}
			codexrouting.WriteError(w, err)
			return
		}
		defer release()
	} else {
		rec.SessionID, rec.Model = requestMetadata(r, rt.Harness)
	}
	capture()
	if d := claude.Details(r); d.AccountID != "" {
		rec.AccountID, rec.Account, rec.SessionID, rec.Model = d.AccountID, d.Account, d.Session, d.Model
	}
	if !routed && rt.Provider == "chatgpt" {
		rec.AccountID = metadataID(r.Header.Get("ChatGPT-Account-Id"))
	}
	if p.Start != nil {
		p.Start(rec)
	}

	var tGetConn, tGotConn, tDNSStart, tDNSDone, tTCPStart, tTCPDone, tTLSStart, tTLSDone, tFirstByte time.Time
	var reused bool
	trace := &httptrace.ClientTrace{
		GetConn:              func(string) { tGetConn = time.Now() },
		GotConn:              func(info httptrace.GotConnInfo) { tGotConn = time.Now(); reused = info.Reused },
		DNSStart:             func(httptrace.DNSStartInfo) { tDNSStart = time.Now() },
		DNSDone:              func(httptrace.DNSDoneInfo) { tDNSDone = time.Now() },
		ConnectStart:         func(string, string) { tTCPStart = time.Now() },
		ConnectDone:          func(string, string, error) { tTCPDone = time.Now() },
		TLSHandshakeStart:    func() { tTLSStart = time.Now() },
		TLSHandshakeDone:     func(tls.ConnectionState, error) { tTLSDone = time.Now() },
		GotFirstResponseByte: func() { tFirstByte = time.Now() },
	}
	span := func(a, b time.Time) *int64 {
		if a.IsZero() || b.IsZero() {
			return nil
		}
		ms := b.Sub(a).Milliseconds()
		return &ms
	}
	finished := false
	var upgradeDone func()
	defer func() {
		if !finished && upgradeDone != nil {
			upgradeDone()
		}
		if !finished && p.Save != nil {
			rec.State = "interrupted"
			rec.Error = "Request ended before the response completed"
			rec.Ms = time.Since(start).Milliseconds()
			p.Save(rec)
		}
	}()
	(&httputil.ReverseProxy{
		ErrorHandler: func(w http.ResponseWriter, req *http.Request, err error) {
			rec.Status = 502
			rec.State = "failed"
			rec.Error = "Upstream connection failed"
			rec.Ms = time.Since(start).Milliseconds()
			finished = true
			if p.Save != nil {
				p.Save(rec)
			}
			http.Error(w, rec.Error, 502)
		},
		FlushInterval: -1,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Path, pr.Out.URL.RawPath = "/"+rt.Rest, ""
			pr.SetURL(rt.Target)
			pr.Out.Header.Del("Accept-Encoding") // Go's transport then decompresses, so the tap sees plain text
			pr.Out = pr.Out.WithContext(httptrace.WithClientTrace(pr.Out.Context(), trace))
		},
		ModifyResponse: func(res *http.Response) error {
			if d := claude.Details(r); d.AccountID != "" {
				res.Header.Set("X-Garcon-Account-Id", d.AccountID)
				if d.Observe != nil {
					d.Observe(res)
				}
			}
			if routed {
				id := r.Header.Get("ChatGPT-Account-Id")
				p.Codex.Observe(id, res)
				res.Header.Set("X-Garcon-Account-Id", id)
			}

			done := func(measured usage.Record, failure string, completed bool, streamError error) {
				rec.Status = res.StatusCode
				rec.Ms = time.Since(start).Milliseconds()
				rec.State = "complete"
				if res.StatusCode >= 400 {
					rec.State = "failed"
					rec.Error = http.StatusText(res.StatusCode)
				}
				finished = true
				if !tGetConn.IsZero() {
					us := tGetConn.Sub(start).Microseconds()
					rec.QueueUs = &us
				}
				if rec.ConnectMs = span(tGetConn, tGotConn); rec.ConnectMs != nil {
					rec.Reused = &reused
				}
				rec.DnsMs, rec.TcpMs, rec.TlsMs = span(tDNSStart, tDNSDone), span(tTCPStart, tTCPDone), span(tTLSStart, tTLSDone)
				rec.FirstByteMs = span(start, tFirstByte)
				if IsCompletion(rt.Rest) {
					if measured.Model != "" {
						rec.Model = measured.Model
					}
					rec.Input, rec.CacheRead, rec.CacheWrite, rec.Output = measured.Input, measured.CacheRead, measured.CacheWrite, measured.Output
					if failure != "" {
						rec.State = "failed"
						rec.Error = failure
					}
				}
				// Provider failures take precedence. Once a completion event has
				// arrived, connection teardown does not undo that response.
				if rec.State != "failed" && !(IsCompletion(rt.Rest) && completed) {
					if r.Context().Err() != nil {
						rec.State = "interrupted"
						rec.Error = "Client disconnected or canceled the request"
					} else if streamError != nil {
						rec.State = "interrupted"
						rec.Error = "Upstream response stream ended unexpectedly"
					}
				}
				if rec.Account == "" {
					rec.Account = p.Accounts.Resolve(rt.Provider, r.Header)
				}
				if p.Save != nil {
					p.Save(rec)
				}
			}
			if res.StatusCode == http.StatusSwitchingProtocols {
				// ReverseProxy needs the original bidirectional stream for Upgrade.
				// An SSE tap drops Write and would break an accepted WebSocket.
				// Record its lifecycle only; never inspect audio or control frames.
				upgradeDone = func() { done(usage.Record{}, "", false, nil) }
			} else {
				res.Body = &tap{ReadCloser: res.Body, done: done}
			}
			return nil
		},
	}).ServeHTTP(w, r)
}

func randomID() []byte {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}
