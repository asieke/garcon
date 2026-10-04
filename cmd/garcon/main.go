// Garcon: a local pass-through proxy for coding agents that records usage.
//
// Point a harness at http://127.0.0.1:4141/<harness>/<provider>/ and
// every request is forwarded verbatim to that provider. Completion calls are
// recorded with the model and tokens the provider reports. The dashboard is
// served at / and shows this machine's locally recorded usage.
//
// There is no authentication. What keeps all of this private is that the server
// listens on loopback and answers only requests addressed to this machine, so
// neither the network nor a DNS-rebound web page can reach it (internal/local).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"garcon/internal/claude"
	"garcon/internal/clauderouting"
	"garcon/internal/codexmetadata"
	"garcon/internal/codexrouting"
	"garcon/internal/connections"
	"garcon/internal/dashboard"
	"garcon/internal/limits"
	"garcon/internal/local"
	"garcon/internal/onboarding"
	"garcon/internal/prices"
	"garcon/internal/proxy"
	"garcon/internal/service"
	"garcon/internal/store"
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
			fmt.Println("usage: garcon [-listen ADDR] [-allow-remote] [-data FILE] [-claude-gateway-config FILE]\n       garcon setup [--no-service] | doctor [--url URL]\n       garcon claude [rc] [--config-dir DIR] [--url URL] [-- claude arguments]\n       garcon codex status|pin|unpin [--help]\n       garcon update (npm installs)\n       garcon service install|uninstall|restart|status\n       garcon version")
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
		case "codex":
			if err := codexrouting.RunCLI(os.Args[2:], os.Stdout, os.Stderr); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
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
	data := flag.String("data", filepath.Join(home, ".local/share/garcon/usage.db"), "local SQLite database (legacy .jsonl paths are migrated)")
	claudeGatewayConfig := flag.String("claude-gateway-config", filepath.Join(home, ".config/garcon/claude-gateway.json"), "optional Claude Desktop gateway key/profile configuration (absent disables the gateway)")
	flag.Parse()
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "unknown command %q; run garcon --help\n", flag.Arg(0))
		os.Exit(2)
	}
	if !local.Addr(*listen) && !*allowRemote {
		fmt.Fprintf(os.Stderr, "refusing to listen on %s: Garcon has no authentication, so the dashboard, the settings and the relay would be open to that network.\nUse a loopback address (the default is 127.0.0.1:4141), or pass -allow-remote to do it anyway.\n", *listen)
		os.Exit(2)
	}

	// Updates restart the service after every deploy, so SIGTERM/SIGINT must drain
	// in-flight streams instead of severing them mid-response: completions stream
	// for minutes, and the systemd unit and the macOS LaunchAgent both send
	// SIGTERM. The 30s drain below fits inside systemd's default 90s stop window.
	shutdownCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	claudeGateway, err := claude.LoadGateway(*claudeGatewayConfig)
	if err != nil {
		log.Fatal(err)
	}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()
	st, err := store.Open(*data)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	st.Tasks = codexmetadata.New()
	log.SetOutput(io.MultiWriter(os.Stderr, st.DB))
	li := limits.New(st.Dir(), st.DB)
	go li.Run(shutdownCtx)
	started := time.Now()
	routing := codexrouting.New(st.Dir(), li.Snapshot, st.DB)
	go routing.Run(shutdownCtx)
	px := &proxy.Proxy{Save: st.Save, Start: st.Save, Codex: routing}
	static := own(dashboard.Handler(), false)

	mux := http.NewServeMux()
	if claudeGateway != nil {
		claudeRouter, err := clauderouting.New(st.DB, li.Snapshot, claudeGateway.Profile())
		if err != nil {
			log.Fatal(err)
		}
		claudeGateway.Prepare = claudeRouter.Prepare
		go claudeRouter.Run(shutdownCtx)
		mux.Handle("/api/routing/claude", readOnly(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(claudeRouter.Status()) }))
	}

	claudeGatewayHandler := claudeGateway.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rt, ok := proxy.ParseRoute(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		px.Serve(w, r, rt)
	}))
	mux.Handle("/claude-gateway", claudeGatewayHandler)
	mux.Handle("/claude-gateway/", claudeGatewayHandler)
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
	mux.Handle("/api/usage/recent", readOnly(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(st.RequestFeed())
	}))
	mux.Handle("/api/connections", own(connections.Handler("http://"+*listen), true))
	for _, path := range []string{"/api/sessions", "/api/logs", "/api/events", "/api/analytics"} {
		mux.Handle(path, own(st, true))
	}
	mux.Handle("/api/nickname", own(http.HandlerFunc(st.Nickname), true))
	mux.Handle("/api/limits", own(li, true))
	mux.Handle("/api/limits/refresh", own(li, true))
	mux.Handle("/api/routing/codex", own(routing, true))
	// Model list prices for the cost estimates, from OpenRouter's public catalogue; fetched when
	// the dashboard first asks and at most daily after that, cached next to the usage log.
	mux.Handle("/api/prices", own(prices.New(st.Dir(), st.DB), true))
	mux.Handle("/api/config", readOnly(func(w http.ResponseWriter, r *http.Request) {
		hosts := map[string]string{}
		for name, u := range proxy.Providers {
			hosts[name] = u.String()
		}
		var size int64
		if fi, err := os.Stat(st.Path()); err == nil {
			size = fi.Size()
		}
		json.NewEncoder(w).Encode(config{Listen: *listen, Data: st.Path(), Rows: st.Len(), Bytes: size, Started: started.UnixMilli(),
			Version: version, Providers: hosts, Implicit: proxy.ImplicitProvider})
	}))

	if *allowRemote {
		log.Printf("WARNING: -allow-remote: anyone who can reach %s can read the usage log, refresh subscription limits and relay through this proxy", *listen)
	}
	log.Printf("garcon %s listening on http://%s, logging to %s", version, *listen, st.Path())
	srv := &http.Server{
		Addr:              *listen,
		Handler:           local.Guard(mux, !*allowRemote),
		ReadHeaderTimeout: 10 * time.Second, // completions stream for minutes, so no write or whole-request timeout
		IdleTimeout:       2 * time.Minute,
	}
	// Serve in the background so a SIGTERM/SIGINT stops accepting new
	// connections and drains in-flight streams before the deferred store close
	// runs and the process exits.
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(listener) }()
	select {
	case err := <-serveErr:
		// Serve only returns on its own for a real failure.
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("serve: %v", err)
		}
	case <-shutdownCtx.Done():
		log.Printf("received shutdown signal; draining in-flight requests (up to 30s)")
		drain, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := srv.Shutdown(drain); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}
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
