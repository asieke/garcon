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
	"flag"
	"fmt"
	"garcon/internal/control"
	"garcon/internal/openrouter"
	"garcon/internal/providers"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"garcon/internal/claude"
	"garcon/internal/codexmetadata"
	"garcon/internal/codexrouting"
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
	RemoteReadOnly bool              `json:"remote_read_only"`
	Listen         string            `json:"listen"`
	Data           string            `json:"data"`
	Rows           int               `json:"rows"`
	Bytes          int64             `json:"bytes"`
	Started        int64             `json:"started"` // unix milliseconds
	Version        string            `json:"version"`
	Providers      map[string]string `json:"providers"`
	Implicit       map[string]string `json:"implicit_harnesses"`
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "help", "-h", "--help":
			fmt.Println("usage: garcon [-listen ADDR] [-allow-remote] [-data FILE]\n       garcon setup [--no-service] | doctor [--url URL]\n       garcon claude [--config-dir DIR] [--url URL] [-- claude arguments]\n       garcon update (npm installs)\n       garcon service install|uninstall|restart|status\n       garcon version")
			fmt.Print(managementHelp)
			return
		case "providers", "accounts", "routing", "prices":
			if err := manage(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
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
	allowRemote := flag.Bool("allow-remote", false, "serve a view-only dashboard on the network; manage locally with the CLI; relay remains unauthenticated")
	data := flag.String("data", filepath.Join(home, ".local/share/garcon/usage.db"), "local SQLite database (legacy .jsonl paths are migrated)")
	flag.Parse()
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "unknown command %q; run garcon --help\n", flag.Arg(0))
		os.Exit(2)
	}
	if !local.Addr(*listen) && !*allowRemote {
		fmt.Fprintf(os.Stderr, "refusing to listen on %s: Garcon has no authentication, so the dashboard, the settings and the relay would be open to that network.\nUse a loopback address (the default is 127.0.0.1:4141), or pass -allow-remote to do it anyway.\n", *listen)
		os.Exit(2)
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
	go li.Run(context.Background())
	started := time.Now()
	routing := codexrouting.New(st.Dir(), li.Snapshot, st.DB)
	go routing.Run(context.Background())
	handler, admin, err := newHandlers(st, li, routing, *listen, *allowRemote, started)
	if err != nil {
		log.Fatal(err)
	}
	controlServer, controlListener, err := control.Listen(control.SocketPath(st.Path()), admin)
	if err != nil {
		log.Fatal(err)
	}
	defer controlServer.Close()
	go func() {
		if err := controlServer.Serve(controlListener); err != nil && err != http.ErrServerClosed {
			log.Printf("local control: %v", err)
		}
	}()
	if *allowRemote {
		log.Printf("WARNING: -allow-remote: anyone who can reach %s can inspect the view-only dashboard and relay through this proxy; manage with the local CLI", *listen)
	}
	log.Printf("garcon %s listening on http://%s, logging to %s", version, *listen, st.Path())
	srv := &http.Server{
		Addr:              *listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second, // completions stream for minutes, so no write or whole-request timeout
		IdleTimeout:       2 * time.Minute,
	}
	log.Fatal(srv.Serve(listener))
}

// newHandlers keeps TCP dashboard policy and local management structurally separate.
func newHandlers(st *store.Store, li *limits.Service, routing *codexrouting.Router, listen string, allowRemote bool, started time.Time) (http.Handler, http.Handler, error) {
	px := &proxy.Proxy{Save: st.Save, Start: st.Save, Codex: routing}
	static := own(dashboard.Handler(), false)
	providerRegistry, err := providers.New(st.DB)
	if err != nil {
		return nil, nil, err
	}
	priceService := prices.New(st.Dir(), st.DB)
	openRouter := openrouter.New(st.Dir())

	mux := http.NewServeMux()
	mux.Handle("/", static)
	mux.Handle("/api/usage", readOnly(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(st.All())
	}))
	mux.Handle("/api/usage/recent", readOnly(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(st.RequestFeed())
	}))
	for _, path := range []string{"/api/sessions", "/api/logs", "/api/events", "/api/analytics"} {
		mux.Handle(path, own(st, true))
	}
	mux.Handle("/api/nickname", own(http.HandlerFunc(st.Nickname), true))
	mux.Handle("/api/limits", own(li, true))
	mux.Handle("/api/limits/refresh", own(li, true))
	mux.Handle("/api/routing/codex", own(routing, true))
	// Model list prices for the cost estimates, from OpenRouter's public catalogue; fetched when
	// the dashboard first asks and at most daily after that, cached next to the usage log.
	mux.Handle("/api/prices", own(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if allowRemote {
			readOnly(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(priceService.Catalog()) }).ServeHTTP(w, r)
			return
		}
		priceService.ServeHTTP(w, r)
	}), true))
	accounts := readOnly(func(w http.ResponseWriter, r *http.Request) {
		configured, err := openRouter.Configured()
		if err != nil {
			http.Error(w, "could not read OpenRouter configuration", 500)
			return
		}
		json.NewEncoder(w).Encode(struct {
			limits.Snapshot
			OpenRouterConfigured bool `json:"openrouter_configured"`
		}{li.Snapshot(), configured})
	})
	mux.Handle("/api/accounts", accounts)
	mux.Handle("/api/providers", readOnly(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(providerRegistry.Snapshot()) }))
	mux.Handle("/api/config", readOnly(func(w http.ResponseWriter, r *http.Request) {
		hosts := providerRegistry.Snapshot()
		var size int64
		if fi, err := os.Stat(st.Path()); err == nil {
			size = fi.Size()
		}
		json.NewEncoder(w).Encode(config{RemoteReadOnly: allowRemote, Listen: listen, Data: st.Path(), Rows: st.Len(), Bytes: size, Started: started.UnixMilli(),
			Version: version, Providers: hosts, Implicit: proxy.ImplicitProvider})
	}))

	// Administration is reachable only through a private filesystem socket.
	admin := http.NewServeMux()
	admin.Handle("/api/accounts", accounts)
	admin.Handle("/api/accounts/openrouter", openRouter)
	admin.Handle("/api/providers", providerRegistry)
	admin.Handle("/api/routing/codex", routing)
	admin.Handle("/api/limits", li)
	admin.Handle("/api/limits/refresh", li)
	admin.Handle("/api/nickname", http.HandlerFunc(st.Nickname))
	admin.Handle("/api/prices", priceService)
	dashboardHandler := local.DashboardAccess(mux, allowRemote)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reserve /api, even when the second path segment is a provider name.
		if r.URL.Path != "/api" && !strings.HasPrefix(r.URL.Path, "/api/") {
			if rt, ok := providerRegistry.ParseRoute(r.URL.Path); ok {
				if rt.Provider == "openrouter" {
					if status, err := openRouter.Authorize(r); err != nil {
						http.Error(w, err.Error(), status)
						return
					}
				}
				px.Serve(w, r, rt)
				return
			}
		}
		dashboardHandler.ServeHTTP(w, r)
	})
	return local.Guard(handler, !allowRemote), admin, nil
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
