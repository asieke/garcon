package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"garcon/internal/codexrouting"
	"garcon/internal/limits"
	"garcon/internal/usage"
)

func emptyCodexPool(t *testing.T) *codexrouting.Router {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", "")
	return codexrouting.New(t.TempDir(), func() limits.Snapshot { return limits.Snapshot{} })
}

func TestRealtimeKeepsCallOwnerOutsideAccountPool(t *testing.T) {
	router := emptyCodexPool(t)
	const path = "/codex/backend-api/codex/realtime/calls"
	const query = "intent=quicksilver&architecture=avas"
	for _, body := range []string{
		`{"session":{"model":"gpt-live-1-codex"},"sdp":"unchanged"}`,
		"--boundary\r\nContent-Disposition: form-data; name=\"sdp\"\r\n\r\nv=0\r\n--boundary--\r\n",
	} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got, _ := io.ReadAll(r.Body)
			if string(got) != body || r.URL.Path != "/backend-api/codex/realtime/calls" || r.URL.RawQuery != query || r.Header.Get("Authorization") != "Bearer caller" || r.Header.Get("ChatGPT-Account-Id") != "caller-account" {
				t.Error("voice call request or owner changed")
			}
			w.Header().Set("Location", "/v1/realtime/calls/rtc_test")
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, "SDP answer")
		}))
		target, _ := url.Parse(upstream.URL)
		var saved usage.Record
		p := Proxy{Codex: router, Save: func(r usage.Record) { saved = r }}
		rt, _ := ParseRoute(path)
		rt.Target = target
		r := httptest.NewRequest(http.MethodPost, path+"?"+query, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer caller")
		r.Header.Set("ChatGPT-Account-Id", "caller-account")
		w := httptest.NewRecorder()
		p.Serve(w, r, rt)
		upstream.Close()
		if w.Code != http.StatusCreated || w.Body.String() != "SDP answer" || w.Header().Get("Location") != "/v1/realtime/calls/rtc_test" || w.Header().Get("X-Garcon-Account-Id") != "" {
			t.Fatalf("voice response changed: %d %s", w.Code, w.Body.String())
		}
		if saved.AccountID != "caller-account" || saved.State != "complete" || saved.Kind != "request" {
			t.Fatalf("lost voice attribution: %+v", saved)
		}
		if strings.HasPrefix(body, "{") && saved.Model != "gpt-live-1-codex" {
			t.Fatalf("missing voice model: %q", saved.Model)
		}
	}
}

func TestRealtimeWebSocketPreservesBidirectionalStream(t *testing.T) {
	router := emptyCodexPool(t)
	const path = "/codex/backend-api/codex/realtime"
	// Actual WebSocket frames: a masked client text frame and an unmasked reply.
	// Garcon must copy these bytes without decoding them as JSON or SSE.
	clientFrame := []byte{0x81, 0x82, 1, 2, 3, 4, 'h' ^ 1, 'i' ^ 2}
	serverFrame := []byte{0x81, 2, 'o', 'k'}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/backend-api/codex/realtime" || r.URL.Query().Get("call_id") != "rtc_test" || r.Header.Get("Authorization") != "Bearer caller" || r.Header.Get("ChatGPT-Account-Id") != "caller-account" || r.Header.Get("OpenAI-Beta") != "realtime=v1" {
			t.Error("websocket request or caller changed")
		}
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		fmt.Fprint(rw, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=\r\n\r\n")
		rw.Flush()
		b := make([]byte, len(clientFrame))
		if _, err := io.ReadFull(rw, b); err != nil || string(b) != string(clientFrame) {
			t.Error("client frames not forwarded", err)
			return
		}
		rw.Write(serverFrame)
		rw.Flush()
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)
	records := make(chan usage.Record, 4)
	p := Proxy{Codex: router, Start: func(r usage.Record) { records <- r }, Save: func(r usage.Record) { records <- r }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rt, _ := ParseRoute(r.URL.Path)
		rt.Target = target
		p.Serve(w, r, rt)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	conn, err := net.DialTimeout("tcp", u.Host, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(conn, "GET %s?call_id=rtc_test&model=gpt-live-1-codex HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nAuthorization: Bearer caller\r\nChatGPT-Account-Id: caller-account\r\nOpenAI-Beta: realtime=v1\r\n\r\n", path, u.Host)
	rd := bufio.NewReader(conn)
	res, err := http.ReadResponse(rd, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 101 {
		t.Fatalf("upgrade failed: %s", res.Status)
	}
	if _, err := conn.Write(clientFrame); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, len(serverFrame))
	if _, err := io.ReadFull(rd, b); err != nil || string(b) != string(serverFrame) {
		t.Fatal("server frames not forwarded", err)
	}
	conn.Close()
	for i := 0; i < 2; i++ {
		select {
		case rec := <-records:
			if rec.AccountID != "caller-account" || rec.Model != "gpt-live-1-codex" || rec.Input != 0 || rec.Output != 0 {
				t.Fatalf("bad metadata: %+v", rec)
			}
			if i == 1 && (rec.Status != 101 || rec.State != "complete") {
				t.Fatalf("bad terminal state: %+v", rec)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("missing websocket lifecycle record")
		}
	}
}

func TestVoiceExemptionDoesNotBypassCodingAccountPool(t *testing.T) {
	p := Proxy{Codex: emptyCodexPool(t)}
	for _, path := range []string{
		"/codex/backend-api/codex/responses",
		"/codex/backend-api/codex/realtime-lookalike",
	} {
		rt, _ := ParseRoute(path)
		if isCodexRealtime(rt) {
			t.Fatalf("unexpected voice exemption: %s", path)
		}
		r := httptest.NewRequest("GET", "http://127.0.0.1:4141"+path, nil)
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("Upgrade", "websocket")
		w := httptest.NewRecorder()
		p.Serve(w, r, rt)
		if w.Code != http.StatusUpgradeRequired {
			t.Fatalf("coding socket bypassed pool: %d", w.Code)
		}
	}
}
