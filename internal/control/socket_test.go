package control

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivateSocket(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "gc-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := SocketPath(filepath.Join(dir, "usage.db"))
	calls := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Host != "localhost" || r.RemoteAddr != "127.0.0.1:0" {
			t.Error("not a local transport request")
		}
		w.Write([]byte("ok"))
	})
	server, listener, err := Listen(path, handler)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	go server.Serve(listener)
	for p, mode := range map[string]os.FileMode{path: 0600, filepath.Dir(path): 0700} {
		info, err := os.Stat(p)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("permissions %s: %v", p, err)
		}
	}
	client := Client(path)
	defer client.CloseIdleConnections()
	req, _ := http.NewRequest("PUT", "http://localhost/api/providers", strings.NewReader(`{}`))
	req.Header.Set("Origin", "http://localhost")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if res.StatusCode != 403 || calls != 0 {
		t.Fatal("browser origin reached control")
	}
	res, err = client.Get("http://localhost/api/providers")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || calls != 1 {
		t.Fatal("CLI not accepted")
	}
	if s, l, err := Listen(path, handler); err == nil {
		s.Close()
		l.Close()
		t.Fatal("replaced active listener")
	}
}
func TestRejectsUnsafeSocketDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unsafe", "admin.sock")
	if err := os.Mkdir(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if s, l, err := Listen(path, http.NotFoundHandler()); err == nil {
		s.Close()
		l.Close()
		t.Fatal("accepted public directory")
	}
}
