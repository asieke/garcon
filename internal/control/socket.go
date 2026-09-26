// Package control provides local administration over a filesystem-protected
// Unix socket. The TCP listener never selects this handler based on headers.
package control

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func SocketPath(data string) string { return data + ".control/admin.sock" }

func Listen(path string, handler http.Handler) (*http.Server, net.Listener, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 {
		return nil, nil, fmt.Errorf("control directory must be a real directory with mode 0700: %s", dir)
	}
	// A stale socket may remain after a crash. Never unlink an active listener,
	// a symlink, or another kind of file.
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, nil, fmt.Errorf("control path is not a socket: %s", path)
		}
		conn, err := net.DialTimeout("unix", path, time.Second)
		if err == nil {
			conn.Close()
			return nil, nil, fmt.Errorf("control socket already in use: %s", path)
		}
		if !errors.Is(err, syscall.ECONNREFUSED) {
			return nil, nil, fmt.Errorf("cannot verify stale control socket: %w", err)
		}
		if err := os.Remove(path); err != nil {
			return nil, nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		listener.Close()
		return nil, nil, err
	}
	server := &http.Server{ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Browser origins are never accepted, even over the private socket.
		if r.Header.Get("Origin") != "" {
			http.Error(w, "local CLI required", 403)
			return
		}
		r.Host = "localhost"
		r.RemoteAddr = "127.0.0.1:0"
		handler.ServeHTTP(w, r)
	})}
	return server, listener, nil
}

func Client(path string) *http.Client {
	return &http.Client{Timeout: 2 * time.Minute, Transport: &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", path)
		},
	}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
