package local

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAddr(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:4141": true, "localhost:4141": true, "[::1]:4141": true, "127.0.0.2:80": true,
		"0.0.0.0:4141": false, ":4141": false, "[::]:4141": false, "192.168.1.5:4141": false, "": false, "garcon.local:4141": false,
	} {
		if got := Addr(addr); got != want {
			t.Errorf("Addr(%q) = %v", addr, got)
		}
	}
}

func TestHost(t *testing.T) {
	for host, want := range map[string]bool{
		"127.0.0.1:4141": true, "127.0.0.1": true, "localhost": true, "LOCALHOST:4141": true, "[::1]:4141": true, "[::1]": true, "::1": true,
		"evil.example:4141": false, "evil.example": false, "localhost.evil.example": false, "127.0.0.1.evil.example": false,
		"": false, "0.0.0.0:4141": false, "192.168.1.5:4141": false, "[::ffff:10.0.0.1]:4141": false,
	} {
		if got := Host(host); got != want {
			t.Errorf("Host(%q) = %v", host, got)
		}
	}
}

func TestGuard(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	for _, tc := range []struct {
		host    string
		enforce bool
		want    int
	}{
		{"127.0.0.1:4141", true, 204}, {"localhost:4141", true, 204}, {"evil.example:4141", true, 403}, {"", true, 403},
		{"evil.example:4141", false, 204}, {"192.168.1.5:4141", false, 204},
	} {
		req := httptest.NewRequest(http.MethodGet, "http://"+tc.host+"/api/usage", nil)
		req.Host = tc.host
		w := httptest.NewRecorder()
		Guard(ok, tc.enforce).ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Errorf("Host %q enforce %v: %d %s", tc.host, tc.enforce, w.Code, w.Body)
		}
	}
}
