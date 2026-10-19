package telemetry

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func policy(t *testing.T, c Config) *Policy {
	t.Helper()
	p, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPolicyDefaultOffAndHostBoundaries(t *testing.T) {
	off := policy(t, Config{})
	on := policy(t, Config{Enabled: true, BlockHosts: []string{"metrics.example.org"}, AllowHosts: []string{"http-intake.logs.us3.datadoghq.com"}})
	for host, want := range map[string]bool{
		"http-intake.logs.us5.datadoghq.com":             true,
		"HTTP-INTAKE.LOGS.US5.DATADOGHQ.COM.:443":        true,
		"http-intake.logs.datadoghq.eu":                  true,
		"http-intake.logs.us3.datadoghq.com":             false,
		"http-intake.logs.a.b.datadoghq.com":             false,
		"http-intake.logs.us5.datadoghq.com.example.org": false,
		"metrics.example.org":                            true,
		"api.anthropic.com":                              false,
	} {
		if got := on.Blocked(host); got != want {
			t.Errorf("host %s got %v want %v", host, got, want)
		}
		if off.Blocked(host) {
			t.Errorf("default-off blocked %s", host)
		}
	}
	for _, host := range []string{"*", "*.com", "example.com/path", "https://example.com", "user@example.com", "*bad.example.com", "a..com"} {
		if _, err := New(Config{BlockHosts: []string{host}}); err == nil {
			t.Errorf("accepted pattern %q", host)
		}
	}
	for _, addr := range []string{":4142", "0.0.0.0:4142", "example.org:4142", "localhost:4142", "[::]:4142"} {
		if _, err := New(Config{Enabled: true, Listen: addr}); err == nil {
			t.Errorf("accepted nonliteral/nonloopback %q", addr)
		}
	}
}

func TestConfigReadAndDisabledListener(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.json")
	p, err := Load(path)
	if err != nil || p.Enabled {
		t.Fatalf("missing config: %v %v", p, err)
	}
	stop, err := Start(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	stop()
	for _, body := range []string{`{"enabled":true,"unknown":1}`, `{"enabled":true} {}`, `{"enabled":"yes"}`} {
		os.WriteFile(path, []byte(body), 0600)
		if _, err := Load(path); err == nil {
			t.Fatalf("accepted invalid config %s", body)
		}
	}
	os.WriteFile(path, []byte(`{"enabled":true,"block_hosts":["metrics.example.org"]}`), 0600)
	p, err = Load(path)
	if err != nil || !p.Blocked("metrics.example.org") || p.Address() != "http://127.0.0.1:4142" {
		t.Fatalf("config: %+v %v", p, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestForwardBlocksBeforeNetworkAndOmitsSecrets(t *testing.T) {
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previous)
	f := &Forward{Policy: policy(t, Config{Enabled: true}), Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("blocked host contacted upstream")
		return nil, nil
	})}
	r := httptest.NewRequest("POST", "http://http-intake.logs.us5.datadoghq.com/api/v2/logs?api_key=QUERY_SECRET", strings.NewReader("BODY_SECRET"))
	r.RemoteAddr = "127.0.0.1:1234"
	r.SetBasicAuth("claude", "PASSWORD_SECRET")
	r.Header.Set("Proxy-Authorization", r.Header.Get("Authorization"))
	r.Header.Set("Authorization", "Bearer HEADER_SECRET")
	w := httptest.NewRecorder()
	f.ServeHTTP(w, r)
	if w.Code != 403 || w.Header().Get("X-Garcon-Telemetry-Blocked") != "true" {
		t.Fatalf("blocked response: %d %s", w.Code, w.Body.String())
	}
	for _, want := range []string{`client="claude"`, `host="http-intake.logs.us5.datadoghq.com"`, `path="/api/v2/logs"`} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("missing %s: %s", want, logs.String())
		}
	}
	for _, secret := range []string{"QUERY_SECRET", "BODY_SECRET", "PASSWORD_SECRET", "HEADER_SECRET"} {
		if strings.Contains(logs.String(), secret) {
			t.Errorf("logged %s", secret)
		}
	}
}

func TestForwardAllowOverridePreservesBodyAndStripsProxyAuth(t *testing.T) {
	var calls int
	f := &Forward{Policy: policy(t, Config{Enabled: true, AllowHosts: []string{"http-intake.logs.us5.datadoghq.com"}}), Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if string(body) != "original body" || r.URL.RawQuery != "a=1;b=2&x=1" || r.Header.Get("Authorization") != "Bearer provider-token" || r.Header.Get("Proxy-Authorization") != "" {
			t.Errorf("request changed or leaked proxy credentials: body=%q", body)
		}
		return &http.Response{StatusCode: 202, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("accepted"))}, nil
	})}
	r := httptest.NewRequest("POST", "http://http-intake.logs.us5.datadoghq.com/api/v2/logs?a=1;b=2&x=1", strings.NewReader("original body"))
	r.RemoteAddr = "127.0.0.1:1234"
	r.SetBasicAuth("claude", "label-only")
	r.Header.Set("Proxy-Authorization", r.Header.Get("Authorization"))
	r.Header.Set("Authorization", "Bearer provider-token")
	w := httptest.NewRecorder()
	f.ServeHTTP(w, r)
	if calls != 1 || w.Code != 202 || w.Body.String() != "accepted" {
		t.Fatalf("allow: calls=%d status=%d body=%s", calls, w.Code, w.Body.String())
	}
}

func TestForwardRejectsBrowserRemoteAndNonProxyRequests(t *testing.T) {
	f := &Forward{Policy: policy(t, Config{Enabled: true}), Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid request reached upstream")
		return nil, nil
	})}
	for _, tc := range []struct{ url, remote, origin, method string }{
		{"http://example.org/", "192.0.2.1:1234", "", "GET"},
		{"http://example.org/", "127.0.0.1:1234", "https://evil.example", "GET"},
		{"/", "127.0.0.1:1234", "", "GET"},
		{"http://example.org:22/", "127.0.0.1:1234", "", "GET"},
		{"https://example.org/", "127.0.0.1:1234", "", "GET"},
		{"http://user:secret@example.org/", "127.0.0.1:1234", "", "GET"},
		{"example.org:22", "127.0.0.1:1234", "", "CONNECT"},
	} {
		r := httptest.NewRequest(tc.method, tc.url, nil)
		r.RemoteAddr = tc.remote
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		f.ServeHTTP(w, r)
		if w.Code != 400 && w.Code != 403 {
			t.Errorf("%+v accepted: %d", tc, w.Code)
		}
	}
}

func TestConnectBlockDoesNotDial(t *testing.T) {
	f := &Forward{Policy: policy(t, Config{Enabled: true}), DialContext: func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("blocked CONNECT dialed")
		return nil, nil
	}}
	r := httptest.NewRequest("CONNECT", "http-intake.logs.us5.datadoghq.com:443", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	f.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("CONNECT status=%d", w.Code)
	}
}

func TestConnectPreservesBufferedBytesAndClosesOnShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &Forward{Policy: policy(t, Config{Enabled: true}), Context: ctx, DialContext: func(_ context.Context, _, addr string) (net.Conn, error) {
		if addr != "provider.example:443" {
			return nil, fmt.Errorf("unexpected destination")
		}
		a, b := net.Pipe()
		go func() { defer b.Close(); io.Copy(b, b) }()
		return a, nil
	}}
	s := httptest.NewServer(f)
	defer s.Close()
	c, err := net.DialTimeout("tcp", strings.TrimPrefix(s.URL, "http://"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	io.WriteString(c, "CONNECT provider.example:443 HTTP/1.1\r\nHost: provider.example:443\r\n\r\nfirst TLS bytes")
	reader := bufio.NewReader(c)
	res, err := http.ReadResponse(reader, &http.Request{Method: "CONNECT"})
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	data := make([]byte, len("first TLS bytes"))
	if _, err = io.ReadFull(reader, data); err != nil {
		t.Fatal(err)
	}
	if string(data) != "first TLS bytes" {
		t.Fatalf("lost buffered tunnel bytes: %q", data)
	}
	cancel()
	if _, err = reader.ReadByte(); err == nil {
		t.Fatal("tunnel survived shutdown")
	}
}

type closeListener struct {
	net.Listener
	closed chan struct{}
	once   sync.Once
}

func (l *closeListener) Close() error {
	err := l.Listener.Close()
	l.once.Do(func() { close(l.closed) })
	return err
}

func TestConnectHalfCloseAndGracefulDrain(t *testing.T) {
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	upstreamDone := make(chan string, 1)
	go func() {
		conn, err := upstream.Accept()
		if err != nil {
			upstreamDone <- err.Error()
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		body, err := io.ReadAll(conn)
		if err != nil {
			upstreamDone <- err.Error()
			return
		}
		io.WriteString(conn, "response after upload EOF")
		upstreamDone <- string(body)
	}()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln := &closeListener{Listener: listener, closed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := serve(ctx, policy(t, Config{Enabled: true}), ln, func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, upstream.Addr().String())
	})
	defer stop()
	c, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	io.WriteString(c, "CONNECT provider.example:443 HTTP/1.1\r\nHost: provider.example:443\r\n\r\n")
	reader := bufio.NewReader(c)
	res, err := http.ReadResponse(reader, &http.Request{Method: "CONNECT"})
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("CONNECT: %v %v", res, err)
	}
	cancel()
	select {
	case <-ln.closed:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not stop admission")
	}
	// A signal has closed the listener, but the established tunnel must finish.
	io.WriteString(c, "upload finishes during drain")
	if err := c.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(reader)
	if err != nil || string(body) != "response after upload EOF" {
		t.Fatalf("half-close/drain lost response: %q %v", body, err)
	}
	if body := <-upstreamDone; body != "upload finishes during drain" {
		t.Fatalf("upload: %q", body)
	}
	stop()
}
