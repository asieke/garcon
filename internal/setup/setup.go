// Package setup is `garcon connect-supabase`: the first-machine path for sync.
// It drives the Supabase CLI to create a project, apply the schema and fetch the
// secret key, then hands everything to the running proxy over /api/settings so
// the key is never printed.
package setup

import (
	"bytes"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"garcon/internal/onboarding"
)

//go:embed schema.sql
var Schema string

// Main runs the subcommand and exits non-zero on failure.
func Main(args []string) {
	fs := flag.NewFlagSet("garcon connect-supabase", flag.ExitOnError)
	hostname, _ := os.Hostname()
	name := fs.String("name", hostname, "this machine's label in the dashboard")
	projectURL := fs.String("project-url", "", "join an existing project without the Supabase CLI (requires --key-stdin)")
	keyStdin := fs.Bool("key-stdin", false, "read the secret key from stdin, not command arguments")
	skipSchema := fs.Bool("skip-schema", false, "join an already configured project without applying SQL (requires --project-ref)")
	create := fs.Bool("create-project", false, "create a new Supabase project; otherwise choose an existing project")
	ref := fs.String("project-ref", "", "reuse an existing project instead of creating one")
	org := fs.String("org-id", "", "organisation to create the project in (needed when you belong to several)")
	region := fs.String("region", "us-east-1", "region for a new project")
	garcon := fs.String("url", "http://127.0.0.1:4141", "the running garcon")
	printSQL := fs.Bool("print-sql", false, "print the table schema and exit")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "usage: garcon connect-supabase --name \"work laptop\" [--create-project | --project-ref REF] [--org-id ID] [--region R]\n       garcon connect-supabase --project-url URL --key-stdin [--name LABEL]\n       garcon connect-supabase --print-sql\n\n")
		fs.PrintDefaults()
	}
	fs.Parse(args)
	if *printSQL {
		fmt.Print(Schema)
		return
	}
	if fs.NArg() != 0 || strings.TrimSpace(*name) == "" {
		fs.Usage()
		os.Exit(2)
	}
	base, err := onboarding.LocalURL(*garcon)
	if err == nil && ((*projectURL != "") != *keyStdin) {
		err = errors.New("use --project-url URL and --key-stdin together")
	}
	if err == nil && *projectURL != "" && (*ref != "" || *create || *skipSchema || *org != "") {
		err = errors.New("--project-url cannot be combined with project creation or CLI project flags")
	}
	if err == nil && *skipSchema && *ref == "" {
		err = errors.New("--skip-schema requires --project-ref")
	}
	if err == nil && *create && *ref != "" {
		err = errors.New("choose --create-project or --project-ref, not both")
	}
	if err == nil && *projectURL == "" && *ref == "" && !*create {
		err = errors.New("choose --create-project for a new Supabase project, --project-ref REF to reuse one, or --project-url URL --key-stdin to join without the CLI; sync is optional")
	}
	if err == nil {
		if *projectURL != "" {
			if _, err = onboarding.Health(base); err == nil {
				var key []byte
				key, err = io.ReadAll(io.LimitReader(os.Stdin, 8193))
				if err == nil && (len(key) > 8192 || strings.TrimSpace(string(key)) == "") {
					err = errors.New("stdin must contain a nonempty Supabase secret key (at most 8192 bytes)")
				}
				if err == nil {
					err = save(*name, *projectURL, strings.TrimSpace(string(key)), base)
				}
			}
		} else {
			err = run(*name, *ref, *org, *region, base, *skipSchema)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// cli runs the Supabase CLI, returning stdout; stderr streams to the terminal.
func cli(args ...string) ([]byte, error) {
	c := exec.Command("supabase", args...)
	c.Stderr = os.Stderr
	var out bytes.Buffer
	c.Stdout = &out
	err := c.Run()
	return out.Bytes(), err
}

func run(name, ref, org, region, garcon string, skipSchema bool) error {
	if _, err := exec.LookPath("supabase"); err != nil {
		return errors.New("the Supabase CLI is not installed: https://supabase.com/docs/guides/cli")
	}
	if _, err := onboarding.Health(garcon); err != nil {
		return err
	}
	if !skipSchema {
		// Check capabilities before creating any remote resource.
		help, err := cli("db", "query", "--help")
		if err != nil || !bytes.Contains(help, []byte("--project-ref")) {
			return errors.New("update the Supabase CLI to a version supporting db query --project-ref, or run garcon connect-supabase --print-sql in the SQL editor and join with --project-url URL --key-stdin")
		}
	}

	orgs, err := cli("orgs", "list", "-o", "json")
	if err != nil {
		return errors.New("not logged in to the Supabase CLI; run: supabase login")
	}

	if ref == "" {
		if org == "" {
			org, err = pickOrg(orgs)
			if err != nil {
				return err
			}
		}
		password := newPassword()
		fmt.Printf("creating project 'garcon' in %s…\n", region)
		out, err := cli("projects", "create", "garcon", "--org-id", org, "--region", region, "--db-password", password, "-o", "json")
		if err != nil {
			return fmt.Errorf("project creation failed: %w", err)
		}
		if ref = field(out, "id", "ref", "project_ref"); ref == "" {
			return fmt.Errorf("could not read the project ref from: %s", out)
		}
		fmt.Printf("database password (garcon never needs it; keep it in your password manager): %s\n", password)
		fmt.Printf("waiting for %s to become healthy", ref)
		if err := waitHealthy(ref); err != nil {
			return err
		}
	}

	if !skipSchema {
		fmt.Printf("applying the garcon_usage schema to %s…\n", ref)
		tmp, err := os.CreateTemp("", "garcon-*.sql")
		if err != nil {
			return err
		}
		defer os.Remove(tmp.Name())
		if _, err := tmp.WriteString(Schema); err != nil {
			tmp.Close()
			return err
		}
		if err := tmp.Close(); err != nil {
			return err
		}
		if _, err := cli("db", "query", "--linked", "--project-ref", ref, "-f", tmp.Name()); err != nil {
			return fmt.Errorf("schema failed: %w; run garcon connect-supabase --print-sql in the project SQL editor, then retry with --project-ref %s --skip-schema", err, ref)
		}

	}
	fmt.Println("fetching the secret key and handing it to garcon…")
	keys, err := cli("projects", "api-keys", "--project-ref", ref, "--reveal", "-o", "json")
	if err != nil {
		return fmt.Errorf("could not list API keys: %w", err)
	}
	key := secretKey(keys)
	if key == "" {
		return errors.New("no sb_secret_ key found; create one under Project Settings > API Keys and rerun")
	}
	url := "https://" + ref + ".supabase.co"
	if err := save(name, url, key, garcon); err != nil {
		return err
	}
	fmt.Printf("Another machine: garcon connect-supabase --name \"<its label>\" --project-ref %s --skip-schema\n", ref)
	return nil
}

func save(name, projectURL, key, garcon string) error {
	body, _ := json.Marshal(map[string]any{"sync_enabled": true, "device_name": name, "url": projectURL, "key": key})
	req, err := http.NewRequest(http.MethodPut, garcon+"/api/settings", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := onboarding.Client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 8192))
		// Even an unexpected server response must not echo the submitted secret.
		detail := strings.ReplaceAll(strings.TrimSpace(string(msg)), key, "[redacted]")
		return fmt.Errorf("settings were not saved (HTTP %d): %s; check the project URL, secret key and schema in Settings > Sync", res.StatusCode, detail)
	}
	fmt.Printf("Connected: sync is ON for %q. Table access verified; transfer runs in the background.\nProgress: %s/?view=settings (or garcon doctor)\n", name, garcon)
	return nil
}

// pickOrg returns the only organisation, or lists them when there are several.
func pickOrg(list []byte) (string, error) {
	var orgs []map[string]any
	if err := json.Unmarshal(list, &orgs); err != nil || len(orgs) == 0 {
		return "", errors.New("could not list organisations; run: supabase orgs list")
	}
	if len(orgs) == 1 {
		return str(orgs[0]["id"]), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "you belong to %d organisations; pass --org-id with one of:\n", len(orgs))
	for _, o := range orgs {
		fmt.Fprintf(&b, "  %s  %s\n", str(o["id"]), str(o["name"]))
	}
	return "", errors.New(strings.TrimRight(b.String(), "\n"))
}

// waitHealthy polls the project list until the new project reports ACTIVE_HEALTHY.
func waitHealthy(ref string) error {
	for i := 0; i < 60; i++ {
		out, _ := cli("projects", "list", "-o", "json")
		var projects []map[string]any
		json.Unmarshal(out, &projects)
		for _, p := range projects {
			if (str(p["id"]) == ref || str(p["ref"]) == ref) && str(p["status"]) == "ACTIVE_HEALTHY" {
				fmt.Println()
				return nil
			}
		}
		fmt.Print(".")
		time.Sleep(5 * time.Second)
	}
	fmt.Println()
	return fmt.Errorf("project is still starting; rerun with --project-ref %s once it is ready", ref)
}

// secretKey picks the sb_secret_ key out of `projects api-keys --reveal -o json`.
func secretKey(list []byte) string {
	var keys []map[string]any
	json.Unmarshal(list, &keys)
	for _, k := range keys {
		if v := str(k["api_key"]); strings.HasPrefix(v, "sb_secret_") {
			return v
		}
	}
	return ""
}

// field returns the first of the named keys present in a JSON object.
func field(obj []byte, names ...string) string {
	var m map[string]any
	json.Unmarshal(obj, &m)
	for _, n := range names {
		if v := str(m[n]); v != "" {
			return v
		}
	}
	return ""
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func newPassword() string {
	b := make([]byte, 18)
	rand.Read(b)
	return strings.NewReplacer("/", "", "+", "", "=", "").Replace(base64.StdEncoding.EncodeToString(b))
}
