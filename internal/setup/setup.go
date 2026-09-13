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
)

//go:embed schema.sql
var Schema string

// Main runs the subcommand and exits non-zero on failure.
func Main(args []string) {
	fs := flag.NewFlagSet("garcon connect-supabase", flag.ExitOnError)
	name := fs.String("name", "", "this machine's label in the dashboard (required)")
	ref := fs.String("project-ref", "", "reuse an existing project instead of creating one")
	org := fs.String("org-id", "", "organisation to create the project in (needed when you belong to several)")
	region := fs.String("region", "us-east-1", "region for a new project")
	garcon := fs.String("url", "http://127.0.0.1:4141", "the running garcon")
	printSQL := fs.Bool("print-sql", false, "print the table schema and exit")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "usage: garcon connect-supabase --name \"work laptop\" [--project-ref REF] [--org-id ID] [--region R]\n       garcon connect-supabase --print-sql\n\n")
		fs.PrintDefaults()
	}
	fs.Parse(args)
	if *printSQL {
		fmt.Print(Schema)
		return
	}
	if *name == "" {
		fs.Usage()
		os.Exit(2)
	}
	if err := run(*name, *ref, *org, *region, *garcon); err != nil {
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

func run(name, ref, org, region, garcon string) error {
	if _, err := exec.LookPath("supabase"); err != nil {
		return errors.New("the Supabase CLI is not installed: https://supabase.com/docs/guides/cli")
	}
	if res, err := http.Get(garcon + "/api/settings"); err != nil || res.StatusCode != 200 {
		return fmt.Errorf("garcon is not answering at %s (garcon service install)", garcon)
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

	fmt.Printf("applying the garcon_usage schema to %s…\n", ref)
	tmp, err := os.CreateTemp("", "garcon-*.sql")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	tmp.WriteString(Schema)
	tmp.Close()
	if _, err := cli("db", "query", "--linked", "--project-ref", ref, "-f", tmp.Name()); err != nil {
		return fmt.Errorf("schema failed: %w", err)
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
	body, _ := json.Marshal(map[string]any{"sync_enabled": true, "device_name": name, "url": url, "key": key})
	req, _ := http.NewRequest(http.MethodPut, garcon+"/api/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		msg, _ := io.ReadAll(res.Body)
		return fmt.Errorf("garcon refused the settings (HTTP %d): %s", res.StatusCode, strings.TrimSpace(string(msg)))
	}
	fmt.Printf(`connected: sync is ON for %q -> %s
  progress: %s/?view=settings
  another machine: garcon connect-supabase --name "<its label>" --project-ref %s
                   or paste the URL and the sb_secret_ key (Project Settings > API Keys)
                   into Settings > Sync in its dashboard.
`, name, url, garcon, ref)
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
