// The development dashboard uses the running service's API and reads connection
// configuration without opening a second store or starting collectors.
package main

import (
	"flag"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"garcon/internal/connections"
	"garcon/internal/dashboard"
	"garcon/internal/local"
	"garcon/internal/onboarding"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:4242", "development dashboard address")
	backend := flag.String("backend", onboarding.DefaultURL, "running local Garcon service")
	flag.Parse()
	if !local.Addr(*listen) {
		log.Fatal("development dashboard requires a loopback address")
	}
	base, err := onboarding.LocalURL(*backend)
	if err != nil {
		log.Fatal(err)
	}
	_, err = onboarding.Health(base)
	if err != nil {
		log.Fatal(err)
	}
	target, _ := url.Parse(base)
	proxy := httputil.NewSingleHostReverseProxy(target)
	direct := proxy.Director
	proxy.Director = func(r *http.Request) {
		// Translate only same-origin browser requests; keep foreign origins so
		// the live backend can reject them with its normal mutation guard.
		if r.Header.Get("Origin") == "http://"+r.Host {
			r.Header.Set("Origin", base)
		}
		direct(r)
		r.Host = target.Host
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", proxy)
	// This endpoint is also available while previewing against older releases.
	mux.Handle("/api/connections", connections.Handler(base))
	mux.Handle("/", dashboard.Handler())
	log.Printf("Development dashboard: http://%s (live API: %s; settings change that service)", *listen, base)
	srv := &http.Server{Addr: *listen, Handler: local.Guard(mux, true), ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
