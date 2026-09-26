package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"garcon/internal/control"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const managementHelp = `Local administration (run on the Garcon host; never writes over TCP):
  garcon providers list
  garcon providers add NAME HTTPS_ORIGIN | remove NAME
  garcon accounts list | refresh
  garcon accounts login codex|claude --profile NAME
  garcon accounts logout codex|claude --profile NAME
  garcon accounts key openrouter --key-stdin | remove-key openrouter
  garcon accounts nickname EMAIL NAME
  garcon routing show | set --json-stdin
  garcon prices refresh
Append --data FILE for a non-default database. Routing JSON uses enabled,
accounts (IDs), and priorities (ID to 1–99). Existing sessions stay pinned.
`

// manage uses a Unix socket only. There is no TCP URL or HTTP-header escape hatch.
func manage(args []string, in io.Reader, out, stderr io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	data := filepath.Join(home, ".local/share/garcon/usage.db")
	// Allow --data anywhere without making action arguments order-dependent.
	rest := []string{}
	for i := 0; i < len(args); i++ {
		if args[i] == "--data" {
			i++
			if i == len(args) {
				return errors.New("--data requires a path")
			}
			data = args[i]
		} else {
			rest = append(rest, args[i])
		}
	}
	if len(rest) < 2 {
		return errors.New(managementHelp)
	}
	group, action := rest[0], rest[1]
	values := rest[2:]
	method, path := "GET", ""
	var payload any
	switch group + " " + action {
	case "providers list":
		path = "/api/providers"
	case "providers add":
		if len(values) != 2 {
			return errors.New("usage: garcon providers add NAME HTTPS_ORIGIN")
		}
		method, path, payload = "PUT", "/api/providers", map[string]string{"name": values[0], "url": values[1]}
		values = nil
	case "providers remove":
		if len(values) != 1 {
			return errors.New("usage: garcon providers remove NAME")
		}
		method, path, payload = "DELETE", "/api/providers", map[string]string{"name": values[0]}
		values = nil
	case "accounts list":
		path = "/api/accounts"
	case "accounts refresh":
		method, path = "POST", "/api/limits/refresh"
	case "accounts login", "accounts logout":
		return accountLogin(action, values, home, in, out, stderr)
	case "accounts key":
		if len(values) != 2 || values[0] != "openrouter" || values[1] != "--key-stdin" {
			return errors.New("usage: garcon accounts key openrouter --key-stdin")
		}
		key, err := io.ReadAll(io.LimitReader(in, 4097))
		if err != nil {
			return err
		}
		if len(key) > 4096 {
			return errors.New("key too long")
		}
		method, path, payload = "PUT", "/api/accounts/openrouter", map[string]string{"key": strings.TrimSpace(string(key))}
		values = nil
	case "accounts remove-key":
		if len(values) != 1 || values[0] != "openrouter" {
			return errors.New("usage: garcon accounts remove-key openrouter")
		}
		method, path = "DELETE", "/api/accounts/openrouter"
		values = nil
	case "accounts nickname":
		if len(values) != 2 {
			return errors.New("usage: garcon accounts nickname EMAIL NAME")
		}
		method, path, payload = "PUT", "/api/nickname?account="+url.QueryEscape(values[0]), map[string]string{"nickname": values[1]}
		values = nil
	case "routing show":
		path = "/api/routing/codex"
	case "routing set":
		if len(values) != 1 || values[0] != "--json-stdin" {
			return errors.New("usage: garcon routing set --json-stdin")
		}
		body, err := io.ReadAll(io.LimitReader(in, 32769))
		if err != nil {
			return err
		}
		if len(body) > 32768 || !json.Valid(body) {
			return errors.New("expected routing JSON (maximum 32 KiB)")
		}
		method, path, payload = "PUT", "/api/routing/codex", json.RawMessage(body)
		values = nil
	case "prices refresh":
		method, path = "POST", "/api/prices"
	default:
		return errors.New(managementHelp)
	}
	if len(values) != 0 {
		return errors.New("unexpected arguments; run garcon help")
	}
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	// Legacy data paths use the migrated database's socket.
	if strings.HasSuffix(data, ".jsonl") {
		data = strings.TrimSuffix(data, ".jsonl") + ".db"
	}
	req, err := http.NewRequest(method, "http://localhost"+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := control.Client(control.SocketPath(data))
	defer client.CloseIdleConnections()
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach local Garcon control socket; run this on the server with its --data path: %w", err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return err
	}
	if res.StatusCode >= 400 {
		return fmt.Errorf("Garcon: %s: %s", res.Status, strings.TrimSpace(string(b)))
	}
	_, err = out.Write(b)
	return err
}

var profilePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

func accountLogin(action string, args []string, home string, in io.Reader, out, stderr io.Writer) error {
	if len(args) < 1 || (args[0] != "codex" && args[0] != "claude") {
		return errors.New("choose codex or claude and --profile NAME")
	}
	provider := args[0]
	flags := flag.NewFlagSet("accounts "+action, flag.ContinueOnError)
	flags.SetOutput(stderr)
	profile := flags.String("profile", "", "separate local login profile")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || !profilePattern.MatchString(*profile) {
		return errors.New("--profile must be 1–32 lowercase letters, digits, or hyphens, starting with a letter or digit")
	}
	dir := filepath.Join(home, "."+provider+"-garcon-"+*profile)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	command := []string{"-c", `cli_auth_credentials_store="file"`, action}
	envName := "CODEX_HOME"
	if provider == "claude" {
		command = []string{"auth", action}
		envName = "CLAUDE_CONFIG_DIR"
	}
	cmd := exec.Command(provider, command...)
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, envName+"=") {
			cmd.Env = append(cmd.Env, v)
		}
	}
	cmd.Env = append(cmd.Env, envName+"="+dir)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = in, out, stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s failed: %w", provider, action, err)
	}
	fmt.Fprintln(out, "Run garcon accounts refresh, then garcon accounts list to verify the account. Routing enrollment is separate.")
	return nil
}
