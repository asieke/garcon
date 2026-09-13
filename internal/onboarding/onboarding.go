// Package onboarding connects installation to a verified, usable dashboard.
package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"garcon/internal/service"
)

const DefaultURL = "http://127.0.0.1:4141"

// LocalURL prevents setup credentials from being sent to a remote server.
func LocalURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("invalid Garcon URL")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "http" || u.User != nil || (u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback())) || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("Garcon URL must be a loopback HTTP address, such as http://127.0.0.1:4141")
	}
	return "http://" + u.Host, nil
}

var Client = &http.Client{Timeout: 35 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}

func Read(base, endpoint string, out any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", base+endpoint, nil)
	if err != nil {
		return err
	}
	res, err := Client.Do(req)
	if err != nil {
		return fmt.Errorf("Garcon is not answering at %s; run garcon setup (or garcon in another terminal): %w", base, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned HTTP %d", endpoint, res.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out); err != nil {
		return fmt.Errorf("%s did not return Garcon JSON: %w", base, err)
	}
	return nil
}

type Config struct {
	Version string `json:"version"`
	Started int64  `json:"started"`
	Rows    int    `json:"rows"`
	Data    string `json:"data"`
}

func Health(base string) (Config, error) {
	var c Config
	err := Read(base, "/api/config", &c)
	if err == nil && (c.Started == 0 || c.Data == "" || c.Version == "") {
		err = errors.New("the address is occupied by a server that is not Garcon")
	}
	return c, err
}

func Main(command string, args []string, version string) {
	fs := flag.NewFlagSet("garcon "+command, flag.ContinueOnError)
	raw := fs.String("url", DefaultURL, "local Garcon address")
	wait := fs.Duration("wait", 0, "wait for the expected server version, e.g. 10s (doctor)")
	noService := fs.Bool("no-service", false, "use an already running foreground Garcon (setup only)")
	err := fs.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err == nil && fs.NArg() != 0 {
		err = errors.New("unexpected arguments; use --help")
	}
	var base string
	if err == nil {
		base, err = LocalURL(*raw)
	}
	if err == nil && command == "setup" {
		if !*noService {
			if base != DefaultURL {
				err = errors.New("custom addresses require garcon setup --no-service --url URL; start garcon -listen ADDR first")
			} else {
				// A listener on this port must be identified before touching the service.
				conn, dialErr := net.DialTimeout("tcp", "127.0.0.1:4141", time.Second)
				if dialErr == nil {
					conn.Close()
					_, err = Health(base)
					if err == nil && !service.Installed() {
						err = errors.New("Garcon is already running in the foreground; use garcon setup --no-service, or stop it before installing the service")
					}
				}
				if err == nil {
					err = service.Install()
				}
			}
		}
		if err == nil {
			for i := 0; i < 20; i++ {
				var c Config
				c, err = Health(base)
				if err == nil && c.Version == version {
					break
				}
				if err == nil {
					err = fmt.Errorf("running version %s differs from installed %s; run garcon service install", c.Version, version)
				}
				time.Sleep(250 * time.Millisecond)
			}
		}
	}
	if err == nil && command == "doctor" && *wait > 0 {
		deadline := time.Now().Add(*wait)
		for {
			c, healthErr := Health(base)
			if healthErr == nil && c.Version == version {
				break
			}
			if time.Now().After(deadline) {
				break
			}
			time.Sleep(250 * time.Millisecond)
		}
	}
	if err == nil {
		err = Doctor(base, version, os.Stdout)
	}
	if err == nil && command == "setup" {
		fmt.Printf("\nNext: open %s/?view=settings\n1. In Connect a harness, choose your tool and account label, then copy its configuration.\n2. Restart that tool and make one short request. Check Logs for the new row.\n3. Optional: Settings > Sync connects your Supabase project. On another machine, reuse that project and choose a different device name.\n\nRun garcon doctor whenever you need to check this installation.\n", base)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func Doctor(base, version string, out io.Writer) error {
	c, err := Health(base)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Dashboard: %s\nVersion: installed %s, running %s\nLocal usage: %d rows (%s)\n", base, version, c.Version, c.Rows, c.Data)
	if c.Version != version {
		return errors.New("version mismatch; run garcon service install to refresh the service, or restart your foreground process")
	}
	var s struct {
		Settings struct {
			Device  string `json:"device_name"`
			Enabled bool   `json:"sync_enabled"`
		} `json:"settings"`
		Status struct {
			Pending   int    `json:"pending"`
			Remote    int    `json:"remote_rows"`
			PushError string `json:"last_push_error"`
			PullError string `json:"last_pull_error"`
		} `json:"status"`
	}
	if err := Read(base, "/api/settings", &s); err != nil {
		return err
	}
	fmt.Fprintf(out, "Device: %s\n", s.Settings.Device)
	if !s.Settings.Enabled {
		fmt.Fprintln(out, "Sync: off (optional; local recording is ready)")
	} else {
		fmt.Fprintf(out, "Sync: on; %d pending local rows, %d remote rows\n", s.Status.Pending, s.Status.Remote)
		if s.Status.PushError != "" || s.Status.PullError != "" {
			return fmt.Errorf("sync needs attention: %s %s; open Settings > Sync", s.Status.PushError, s.Status.PullError)
		}
	}
	if c.Rows == 0 {
		fmt.Fprintln(out, "First request: still waiting. Configure a harness in Settings, then send one request.")
	}
	return nil
}
