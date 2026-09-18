// Garcon: a local pass-through proxy for coding agents that records usage.
//
// Point a harness at http://127.0.0.1:4141/<harness>/<provider>/ and
// every request is forwarded verbatim to that provider. Completion calls are
// recorded with the model and tokens the provider reports. The dashboard is
// served at /, and Settings can sync the log with other machines through a
// Supabase project the user owns.
//
// There is no authentication. What keeps all of this private is that the server
// listens on loopback and answers only requests addressed to this machine, so
// neither the network nor a DNS-rebound web page can reach it (internal/local).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"garcon/internal/claude"
	"garcon/internal/dashboard"
	"garcon/internal/local"
	"garcon/internal/onboarding"
	"garcon/internal/prices"
	"garcon/internal/proxy"
	"garcon/internal/service"
	"garcon/internal/setup"
	"garcon/internal/store"
	"garcon/internal/syncer"
)

// version is stamped at build time: go build -ldflags "-X main.version=1.2.3".
var version = "dev"

// config is what the dashboard's Settings view shows: how this instance is wired.
type config struct {
	Listen    string            `json:"listen"`
	Data      string            `json:"data"`
	Rows      int               `json:"rows"`
	Bytes     int64             `json:"bytes"`
	Started   int64             `json:"started"` // unix milliseconds
	Version   string            `json:"version"`
	Providers map[string]string `json:"providers"`
	Implicit  map[string]string `json:"implicit_harnesses"`
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "help", "-h", "--help":
			fmt.Println("usage: garcon [-listen ADDR] [-allow-remote] [-data FILE] [-config FILE]\n       garcon setup [--no-service] | doctor [--url URL]\n       garcon claude [--config-dir DIR] [--url URL] [-- claude arguments]\n       garcon update (npm installs)\n       garcon service install|uninstall|restart|status\n       garcon connect-supabase --create-project | --project-ref REF [--skip-schema]\n       garcon connect-supabase --project-url URL --key-stdin [--name LABEL]\n       garcon connect-supabase --print-sql\n       garcon version")
			return
		case "setup", "doctor":
			onboarding.Main(os.Args[1], os.Args[2:], version)
			return
		case "update":
			fmt.Fprintln(os.Stderr, "For npm installs: npm install -g ai-garcon@latest && garcon service install && garcon doctor\nFor source installs: scripts/install.sh --update")
			os.Exit(1)
		case "service":
			service.Main(os.Args[2:])
			return
		case "connect-supabase":
			setup.Main(os.Args[2:])
			return
		case "claude":
			claude.Main(os.Args[2:])
			return
		case "version", "-version", "--version":
			fmt.Println("garcon", version)
			return
		}
	}
	home, _ := os.UserHomeDir()
	listen := flag.String("listen", "127.0.0.1:4141", "address to listen on")
	allowRemote := flag.Bool("allow-remote", false, "serve on a non-loopback -listen address; there is no authentication, so the dashboard, the settings and the relay are then open to that network")
	data := flag.String("data", filepath.Join(home, ".local/share/garcon/usage.jsonl"), "usage log")
	configPath := flag.String("config", filepath.Join(home, ".config/garcon/config.json"), "settings file")
	flag.Parse()
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "unknown command %q; run garcon --help\n", flag.Arg(0))
		os.Exit(2)
	}
	if !local.Addr(*listen) && !*allowRemote {
		fmt.Fprintf(os.Stderr, "refusing to listen on %s: Garcon has no authentication, so the dashboard, the settings and the relay would be open to that network.\nUse a loopback address (the default is 127.0.0.1:4141), or pass -allow-remote to do it anyway.\n", *listen)
		os.Exit(2)
	}

	st, err := store.Open(*data)
	if err != nil {
		log.Fatal(err)
	}
	sy := syncer.New(st, *configPath)
	go sy.Run()
	started := time.Now()
	px := &proxy.Proxy{Save: st.Save}
	static := own(dashboard.Handler(), false)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if rt, ok := proxy.ParseRoute(r.URL.Path); ok {
			px.Serve(w, r, rt)
			return
		}
		static.ServeHTTP(w, r)
	})
	mux.Handle("/api/usage", readOnly(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(st.All())
	}))
	mux.Handle("/api/settings", own(sy, true))
	// Model list prices for the cost estimates, from OpenRouter's public catalogue; fetched when
	// the dashboard first asks and at most daily after that, cached next to the usage log.
	mux.Handle("/api/prices", own(prices.New(st.Dir()), true))
	mux.Handle("/api/config", readOnly(func(w http.ResponseWriter, r *http.Request) {
		hosts := map[string]string{}
		for name, u := range proxy.Providers {
			hosts[name] = u.String()
		}
		var size int64
		if fi, err := os.Stat(*data); err == nil {
			size = fi.Size()
		}
		json.NewEncoder(w).Encode(config{Listen: *listen, Data: *data, Rows: st.Len(), Bytes: size, Started: started.UnixMilli(),
			Version: version, Providers: hosts, Implicit: proxy.ImplicitProvider})
	}))

	if *allowRemote {
		log.Printf("WARNING: -allow-remote: anyone who can reach %s can read the usage log, change the sync settings and relay through this proxy", *listen)
	}
	log.Printf("garcon %s listening on http://%s, logging to %s", version, *listen, *data)
	srv := &http.Server{
		Addr:              *listen,
		Handler:           local.Guard(mux, !*allowRemote),
		ReadHeaderTimeout: 10 * time.Second, // completions stream for minutes, so no write or whole-request timeout
		IdleTimeout:       2 * time.Minute,
	}
	log.Fatal(srv.ListenAndServe())
}

// own marks a response as Garcon's own (the dashboard or the API) rather than a
// relayed upstream reply, which passes through untouched: no content sniffing,
// no framing by other pages, no referrer, and for the API no caching.
func own(next http.Handler, api bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		if api {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// readOnly is a GET/HEAD JSON endpoint; every other method is refused.
func readOnly(get func(http.ResponseWriter, *http.Request)) http.Handler {
	return own(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		get(w, r)
	}), true)
}
