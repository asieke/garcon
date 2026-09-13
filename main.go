// Garcon: a local pass-through proxy for coding agents that records usage.
//
// Point a harness at http://127.0.0.1:4141/<harness>/<account>/<provider>/ and
// every request is forwarded verbatim to that provider. Claude Code and Codex
// each talk to exactly one provider, so their URLs omit the provider segment:
// /claude/<account>/ goes to Anthropic and /codex/<account>/ to chatgpt.com.
// Completion calls are recorded with the model and tokens the provider reports.
// The dashboard is served at /.
package main

import (
	"bytes"
	"crypto/tls"
	"embed"
	"encoding/json"
	"flag"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptrace"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed all:web/build
var web embed.FS

var providers = map[string]*url.URL{
	"anthropic":  {Scheme: "https", Host: "api.anthropic.com"},
	"openai":     {Scheme: "https", Host: "api.openai.com"},
	"openrouter": {Scheme: "https", Host: "openrouter.ai"},
	"chatgpt":    {Scheme: "https", Host: "chatgpt.com"},
}

// Harnesses whose URLs predate the provider segment and imply it.
var implicitProvider = map[string]string{"claude": "anthropic", "codex": "chatgpt"}

type route struct {
	harness, account, provider, rest string
	target                           *url.URL
}

// parseRoute splits /<harness>/<account>/[<provider>/]<rest>. Anything that does not
// resolve to a known provider is not a proxy request (it is a dashboard asset).
func parseRoute(path string) (route, bool) {
	harness, rest, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
	account, rest, _ := strings.Cut(rest, "/")
	if harness == "" || account == "" {
		return route{}, false
	}
	provider, ok := implicitProvider[harness]
	if !ok {
		provider, rest, _ = strings.Cut(rest, "/")
	}
	target := providers[provider]
	if target == nil {
		return route{}, false
	}
	return route{harness: harness, account: account, provider: provider, rest: rest, target: target}, true
}

// isCompletion reports whether an upstream path is a model call worth recording:
// Anthropic Messages, OpenAI Responses (also Codex's chatgpt.com backend) or
// OpenAI-compatible chat completions (OpenAI, OpenRouter, Hermes, OpenClaw).
func isCompletion(rest string) bool {
	return strings.HasSuffix(rest, "/messages") || strings.HasSuffix(rest, "/responses") || strings.HasSuffix(rest, "/chat/completions")
}

type record struct {
	Time    int64  `json:"time"` // unix milliseconds
	Harness string `json:"harness"`
	Account string `json:"account"`
	// Provider is omitted on rows recorded before harnesses other than Claude Code
	// and Codex were supported; those imply anthropic and chatgpt respectively.
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model"`
	Status   int    `json:"status"`
	Ms       int64  `json:"ms"`

	// The fields below isolate every part of Ms that garcon itself controls,
	// via net/http/httptrace, so total latency (almost entirely the provider
	// generating a response) is never confused with what the proxy adds.
	// All are omitted (never a measured zero) on requests recorded before this
	// instrumentation shipped.

	// QueueUs is request start to the transport asking for a connection: pure
	// proxy-side Go work (URL rewrite, request cloning, scheduling) with no
	// network involved yet. Microseconds, because it is expected to be tiny -
	// reported to prove that, not just assume it.
	QueueUs *int64 `json:"queue_us,omitempty"`
	// Reused is true when an already-open pooled connection served this
	// request, false when a fresh connection had to be established.
	Reused *bool `json:"reused,omitempty"`
	// ConnectMs is the time spent acquiring the upstream connection: near-zero
	// when Reused, or the full DNS+TCP+TLS span otherwise. Always present
	// alongside Reused, whichever case applies.
	ConnectMs *int64 `json:"connect_ms,omitempty"`
	// DnsMs, TcpMs and TlsMs break ConnectMs down by phase. Present only when
	// Reused is false, since none of them run on a reused connection.
	DnsMs *int64 `json:"dns_ms,omitempty"`
	TcpMs *int64 `json:"tcp_ms,omitempty"`
	TlsMs *int64 `json:"tls_ms,omitempty"`
	// FirstByteMs is request start to the first response byte from upstream.
	// It is dominated by the provider's own time-to-first-token, not proxy
	// overhead, but gives the numbers above context.
	FirstByteMs *int64 `json:"first_byte_ms,omitempty"`

	Input      int64 `json:"input"` // uncached input tokens
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
	Output     int64 `json:"output"`
}

// usage matches the Anthropic Messages, OpenAI Responses and OpenAI chat
// completions usage objects (OpenRouter and other compatible servers use the
// chat completions shape).
type usage struct {
	Input      int64 `json:"input_tokens"`
	Output     int64 `json:"output_tokens"`
	CacheRead  int64 `json:"cache_read_input_tokens"`
	CacheWrite int64 `json:"cache_creation_input_tokens"`
	Details    struct {
		Cached int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	// Chat completions naming. Reasoning tokens are billed as, and included in,
	// completion_tokens, so no separate field is needed.
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	PromptDetails    struct {
		Cached int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

// event is one response body or SSE data line. Model and usage sit at the top
// level (Anthropic message_delta, non-streaming replies), under message
// (Anthropic message_start) or under response (OpenAI response.* events).
type event struct {
	Model    string `json:"model"`
	Usage    *usage `json:"usage"`
	Message  *event `json:"message"`
	Response *event `json:"response"`
}

// fold reads every event in a body into rec, keeping the largest value seen per
// usage field so partial streaming counts never win.
func fold(body []byte, rec *record) {
	for _, line := range bytes.Split(body, []byte("\n")) {
		line = bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(line), []byte("data:")))
		var ev event
		if !bytes.HasPrefix(line, []byte("{")) || json.Unmarshal(line, &ev) != nil {
			continue
		}
		for _, e := range []*event{&ev, ev.Message, ev.Response} {
			if e == nil {
				continue
			}
			if e.Model != "" {
				rec.Model = e.Model
			}
			if u := e.Usage; u != nil {
				rec.Input = max(rec.Input, u.Input-u.Details.Cached, u.PromptTokens-u.PromptDetails.Cached)
				rec.Output = max(rec.Output, u.Output, u.CompletionTokens)
				rec.CacheRead = max(rec.CacheRead, u.CacheRead, u.Details.Cached, u.PromptDetails.Cached)
				rec.CacheWrite = max(rec.CacheWrite, u.CacheWrite)
			}
		}
	}
}

// tap copies a response body as it streams through and hands the whole thing to
// done when the proxy closes it.
type tap struct {
	io.ReadCloser
	buf  bytes.Buffer
	done func([]byte)
}

func (t *tap) Read(p []byte) (int, error) {
	n, err := t.ReadCloser.Read(p)
	t.buf.Write(p[:n])
	return n, err
}

func (t *tap) Close() error {
	t.done(t.buf.Bytes())
	return t.ReadCloser.Close()
}

var (
	mu      sync.Mutex
	records = []record{}
	logFile *os.File
	started = time.Now()
)

// config is what the dashboard's Settings tab shows: how this instance is wired.
type config struct {
	Listen    string            `json:"listen"`
	Data      string            `json:"data"`
	Rows      int               `json:"rows"`
	Bytes     int64             `json:"bytes"`
	Started   int64             `json:"started"` // unix milliseconds
	Providers map[string]string `json:"providers"`
	Implicit  map[string]string `json:"implicit_harnesses"`
}

func save(rec record) {
	line, _ := json.Marshal(rec)
	mu.Lock()
	defer mu.Unlock()
	records = append(records, rec)
	if _, err := logFile.Write(append(line, '\n')); err != nil {
		log.Print(err)
	}
}

func load(path string) {
	os.MkdirAll(filepath.Dir(path), 0o700)
	data, _ := os.ReadFile(path)
	for _, line := range bytes.Split(data, []byte("\n")) {
		var rec record
		if json.Unmarshal(line, &rec) == nil {
			records = append(records, rec)
		}
	}
	var err error
	if logFile, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err != nil {
		log.Fatal(err)
	}
}

func proxy(w http.ResponseWriter, r *http.Request, rt route) {
	target, rest := rt.target, rt.rest
	completion := isCompletion(rest)
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
	(&httputil.ReverseProxy{
		FlushInterval: -1,
		Rewrite: func(p *httputil.ProxyRequest) {
			p.Out.URL.Path, p.Out.URL.RawPath = "/"+rest, ""
			p.SetURL(target)
			p.Out.Header.Del("Accept-Encoding") // Go's transport then decompresses, so the tap sees plain text
			p.Out = p.Out.WithContext(httptrace.WithClientTrace(p.Out.Context(), trace))
		},
		ModifyResponse: func(res *http.Response) error {
			if !completion {
				return nil
			}
			res.Body = &tap{ReadCloser: res.Body, done: func(b []byte) {
				rec := record{Time: start.UnixMilli(), Harness: rt.harness, Account: rt.account, Provider: rt.provider,
					Status: res.StatusCode, Ms: time.Since(start).Milliseconds()}
				if !tGetConn.IsZero() {
					us := tGetConn.Sub(start).Microseconds()
					rec.QueueUs = &us
				}
				if !tGetConn.IsZero() && !tGotConn.IsZero() {
					rec.Reused = &reused
					ms := tGotConn.Sub(tGetConn).Milliseconds()
					rec.ConnectMs = &ms
				}
				if !tDNSStart.IsZero() && !tDNSDone.IsZero() {
					ms := tDNSDone.Sub(tDNSStart).Milliseconds()
					rec.DnsMs = &ms
				}
				if !tTCPStart.IsZero() && !tTCPDone.IsZero() {
					ms := tTCPDone.Sub(tTCPStart).Milliseconds()
					rec.TcpMs = &ms
				}
				if !tTLSStart.IsZero() && !tTLSDone.IsZero() {
					ms := tTLSDone.Sub(tTLSStart).Milliseconds()
					rec.TlsMs = &ms
				}
				if !tFirstByte.IsZero() {
					ms := tFirstByte.Sub(start).Milliseconds()
					rec.FirstByteMs = &ms
				}
				fold(b, &rec)
				save(rec)
			}}
			return nil
		},
	}).ServeHTTP(w, r)
}

func main() {
	listen := flag.String("listen", "127.0.0.1:4141", "address to listen on")
	data := flag.String("data", filepath.Join(os.Getenv("HOME"), ".local/share/garcon/usage.jsonl"), "usage log")
	flag.Parse()
	load(*data)

	site, _ := fs.Sub(web, "web/build")
	static := http.FileServerFS(site)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if rt, ok := parseRoute(r.URL.Path); ok {
			proxy(w, r, rt)
			return
		}
		static.ServeHTTP(w, r)
	})
	http.HandleFunc("/api/usage", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		defer mu.Unlock()
		json.NewEncoder(w).Encode(records)
	})
	http.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		hosts := map[string]string{}
		for name, u := range providers {
			hosts[name] = u.String()
		}
		var size int64
		if st, err := os.Stat(*data); err == nil {
			size = st.Size()
		}
		mu.Lock()
		rows := len(records)
		mu.Unlock()
		json.NewEncoder(w).Encode(config{Listen: *listen, Data: *data, Rows: rows, Bytes: size,
			Started: started.UnixMilli(), Providers: hosts, Implicit: implicitProvider})
	})
	log.Printf("garcon listening on http://%s, logging to %s", *listen, *data)
	log.Fatal(http.ListenAndServe(*listen, nil))
}
