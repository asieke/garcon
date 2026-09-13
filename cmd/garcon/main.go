// Garcon: a local pass-through proxy for coding agents that records usage.
//
// Point a harness at http://127.0.0.1:4141/<harness>/<account>/<provider>/ and
// every request is forwarded verbatim to that provider. Completion calls are
// recorded with the model and tokens the provider reports. The dashboard is
// served at /, and Settings can sync the log with other machines through a
// Supabase project the user owns.
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

	"garcon/internal/dashboard"
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
			fmt.Println("usage: garcon [-listen ADDR] [-data FILE] [-config FILE]\n       garcon service install|uninstall|restart|status\n       garcon connect-supabase --name LABEL [--project-ref REF] | --print-sql\n       garcon version")
			return
		case "service":
			service.Main(os.Args[2:])
			return
		case "connect-supabase":
			setup.Main(os.Args[2:])
			return
		case "version", "-version", "--version":
			fmt.Println("garcon", version)
			return
		}
	}
	home, _ := os.UserHomeDir()
	listen := flag.String("listen", "127.0.0.1:4141", "address to listen on")
	data := flag.String("data", filepath.Join(home, ".local/share/garcon/usage.jsonl"), "usage log")
	configPath := flag.String("config", filepath.Join(home, ".config/garcon/config.json"), "settings file")
	flag.Parse()

	st, err := store.Open(*data)
	if err != nil {
		log.Fatal(err)
	}
	sy := syncer.New(st, *configPath, *listen)
	go sy.Run()
	started := time.Now()
	px := &proxy.Proxy{Save: st.Save}
	static := dashboard.Handler()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if rt, ok := proxy.ParseRoute(r.URL.Path); ok {
			px.Serve(w, r, rt)
			return
		}
		static.ServeHTTP(w, r)
	})
	http.HandleFunc("/api/usage", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(st.All())
	})
	http.Handle("/api/settings", sy)
	http.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
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
	})
	log.Printf("garcon %s listening on http://%s, logging to %s", version, *listen, *data)
	log.Fatal(http.ListenAndServe(*listen, nil))
}
