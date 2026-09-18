// Package proxy routes harness URLs to their provider, with automatic account attribution,
// forwarding every request unchanged and recording completion calls.
package proxy

import (
	"crypto/tls"
	"net/http"
	"net/http/httptrace"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"garcon/internal/accounts"
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

// Proxy forwards routed requests and hands each completion's Record to Save.
type Proxy struct {
	Save     func(usage.Record)
	Accounts accounts.Resolver
}

// Serve proxies one routed request.
func (p *Proxy) Serve(w http.ResponseWriter, r *http.Request, rt Route) {
	completion := IsCompletion(rt.Rest)
	start := time.Now()

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
	(&httputil.ReverseProxy{
		FlushInterval: -1,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Path, pr.Out.URL.RawPath = "/"+rt.Rest, ""
			pr.SetURL(rt.Target)
			pr.Out.Header.Del("Accept-Encoding") // Go's transport then decompresses, so the tap sees plain text
			pr.Out = pr.Out.WithContext(httptrace.WithClientTrace(pr.Out.Context(), trace))
		},
		ModifyResponse: func(res *http.Response) error {
			if !completion {
				return nil
			}
			res.Body = &tap{ReadCloser: res.Body, done: func(b []byte) {
				rec := usage.Record{Time: start.UnixMilli(), Harness: rt.Harness, Provider: rt.Provider,
					Status: res.StatusCode, Ms: time.Since(start).Milliseconds()}
				if !tGetConn.IsZero() {
					us := tGetConn.Sub(start).Microseconds()
					rec.QueueUs = &us
				}
				if rec.ConnectMs = span(tGetConn, tGotConn); rec.ConnectMs != nil {
					rec.Reused = &reused
				}
				rec.DnsMs, rec.TcpMs, rec.TlsMs = span(tDNSStart, tDNSDone), span(tTCPStart, tTCPDone), span(tTLSStart, tTLSDone)
				rec.FirstByteMs = span(start, tFirstByte)
				usage.Fold(b, &rec)
				rec.Account = p.Accounts.Resolve(rt.Provider, r.Header)
				p.Save(rec)
			}}
			return nil
		},
	}).ServeHTTP(w, r)
}
