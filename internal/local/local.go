// Package local decides whether an address or a request stays on this machine.
// Garcon has no authentication: what keeps the dashboard, the settings and the
// relay private is that they answer on loopback alone and refuse any request
// addressed to another name, so neither the network nor a DNS-rebound web page
// can reach them.
package local

import (
	"net"
	"net/http"
	"strconv"
	"strings"
)

// Addr reports whether a listen address names a loopback interface. An empty
// host (":4141") means every interface and is not loopback.
func Addr(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		host = listen
	}
	return Host(host)
}

// Host reports whether a Host header, or a bare host, names this machine:
// localhost or a loopback IP, with or without a port.
func Host(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Guard rejects requests that are not addressed to this machine. A page on
// evil.example whose name is rebound to 127.0.0.1 still arrives with
// Host: evil.example, so wrapping every route defeats DNS rebinding. With
// enforce false (-allow-remote) everything passes through.
func Guard(next http.Handler, enforce bool) http.Handler {
	if !enforce {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !Host(r.Host) {
			http.Error(w, "Garcon only answers requests addressed to this machine (Host 127.0.0.1, localhost or [::1]), not "+strconv.Quote(r.Host), http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
