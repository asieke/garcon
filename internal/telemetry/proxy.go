package telemetry

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"
	"time"
)

// Forward is a standard HTTP proxy for clients that honor HTTP(S)_PROXY.
// HTTPS uses an opaque CONNECT tunnel: only its host is visible to Garcon.
type Forward struct {
	Policy      *Policy
	Context     context.Context
	Transport   http.RoundTripper
	DialContext func(context.Context, string, string) (net.Conn, error)
	active      sync.WaitGroup
	admission   sync.Mutex
	closing     bool
}

func (f *Forward) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.admission.Lock()
	if f.closing {
		f.admission.Unlock()
		http.Error(w, "telemetry proxy is shutting down", http.StatusServiceUnavailable)
		return
	}
	f.active.Add(1)
	f.admission.Unlock()
	defer f.active.Done()
	remote, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(remote).IsLoopback() || r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "" {
		http.Error(w, "telemetry proxy accepts local proxy clients only", http.StatusForbidden)
		return
	}
	if f.Policy == nil || !f.Policy.Enabled {
		http.Error(w, "telemetry proxy is disabled", http.StatusServiceUnavailable)
		return
	}
	client := "proxy client"
	// The optional proxy username is a client label, not a credential.
	if auth := r.Header.Get("Proxy-Authorization"); auth != "" {
		copy := &http.Request{Header: http.Header{"Authorization": []string{auth}}}
		if name, _, ok := copy.BasicAuth(); ok && name != "" {
			client = clean(name, 64)
		}
	}
	host, path := r.URL.Hostname(), r.URL.EscapedPath()
	if r.Method == http.MethodConnect {
		var port string
		host, port, err = net.SplitHostPort(r.Host)
		if err != nil || port != "443" || r.URL.User != nil || r.URL.RawQuery != "" {
			http.Error(w, "CONNECT requires a host on port 443", http.StatusBadRequest)
			return
		}
		path = "(encrypted HTTPS path)"
	} else if r.URL.Scheme != "http" || host == "" || r.URL.User != nil || (r.URL.Port() != "" && r.URL.Port() != "80") {
		http.Error(w, "use an absolute HTTP URL on port 80, or HTTPS CONNECT on port 443", http.StatusBadRequest)
		return
	}
	if f.Policy.Reject(client, host, path) {
		w.Header().Set("X-Garcon-Telemetry-Blocked", "true")
		http.Error(w, "Garcon blocked this host by the local telemetry policy", http.StatusForbidden)
		return
	}
	if r.Method == http.MethodConnect {
		f.tunnel(w, r)
		return
	}
	(&httputil.ReverseProxy{
		Transport: f.Transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.RawQuery = pr.In.URL.RawQuery
			pr.Out.Host = pr.Out.URL.Host
			pr.Out.Header.Del("Proxy-Authorization")
			pr.Out.Header.Del("Proxy-Connection")
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "upstream connection failed", http.StatusBadGateway)
		},
	}).ServeHTTP(w, r)
}

func (f *Forward) tunnel(w http.ResponseWriter, r *http.Request) {
	dial := f.DialContext
	if dial == nil {
		dial = (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	}
	upstream, err := dial(r.Context(), "tcp", r.Host)
	if err != nil {
		http.Error(w, "upstream connection failed", http.StatusBadGateway)
		return
	}
	defer upstream.Close()
	client, buf, err := http.NewResponseController(w).Hijack()
	if err != nil {
		http.Error(w, "CONNECT requires HTTP/1", http.StatusInternalServerError)
		return
	}
	defer client.Close()
	ctx := f.Context
	if ctx == nil {
		ctx = context.Background()
	}
	stop := context.AfterFunc(ctx, func() { client.Close(); upstream.Close() })
	defer stop()
	if _, err = buf.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if err = buf.Flush(); err != nil {
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := io.Copy(upstream, buf); err != nil {
			upstream.Close()
			client.Close()
			return
		}
		closeWrite(upstream)
	}()
	if _, err := io.Copy(client, upstream); err != nil {
		client.Close()
		upstream.Close()
	} else {
		closeWrite(client)
	}
	<-done
}

func closeWrite(conn net.Conn) {
	if half, ok := conn.(interface{ CloseWrite() error }); ok {
		half.CloseWrite()
	} else {
		conn.Close()
	}
}

// Start binds only an explicit loopback interface. Missing/disabled policy opens no port.
func Start(ctx context.Context, policy *Policy) (func(), error) {
	if policy == nil || !policy.Enabled {
		return func() {}, nil
	}
	ln, err := net.Listen("tcp", policy.Listen)
	if err != nil {
		return nil, fmt.Errorf("telemetry proxy: %w", err)
	}
	return serve(ctx, policy, ln, nil), nil
}

func serve(ctx context.Context, policy *Policy, ln net.Listener, dial func(context.Context, string, string) (net.Conn, error)) func() {
	// Signals stop admission first; established HTTPS tunnels get the same
	// thirty-second drain window as the main relay.
	tunnels, cancel := context.WithCancel(context.Background())
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil // never recurse through this process's HTTP(S)_PROXY
	forward := &Forward{Policy: policy, Context: tunnels, Transport: transport, DialContext: dial}
	server := &http.Server{Handler: forward, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	done := make(chan struct{})
	go func() { defer close(done); server.Serve(ln) }()
	stopped := make(chan struct{})
	var once sync.Once
	stop := func() {
		once.Do(func() {
			defer close(stopped)
			forward.admission.Lock()
			forward.closing = true
			forward.admission.Unlock()
			drain, end := context.WithTimeout(context.Background(), 30*time.Second)
			defer end()
			server.Shutdown(drain)
			finished := make(chan struct{})
			go func() { forward.active.Wait(); close(finished) }()
			select {
			case <-finished:
			case <-drain.Done():
			}
			cancel()
			server.Close()
			transport.CloseIdleConnections()
			<-done
		})
	}
	go func() {
		select {
		case <-ctx.Done():
			stop()
		case <-stopped:
		}
	}()
	return stop
}

// Address describes the listener without suggesting it covers bypassing clients.
func (p *Policy) Address() string {
	if p == nil || !p.Enabled {
		return ""
	}
	return "http://" + strings.TrimSpace(p.Listen)
}
