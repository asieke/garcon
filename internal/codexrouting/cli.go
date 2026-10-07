package codexrouting

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/tabwriter"
	"time"

	"garcon/internal/onboarding"
)

const cliUsage = `usage: garcon codex status [--url URL] [--json]
       garcon codex pin [--url URL] [--json] <account-id-or-email>
       garcon codex unpin [--url URL] [--json]
`

// RunCLI controls the running router through the same API as the Usage widget.
// It never writes the service's database behind the router's in-memory state.
func RunCLI(args []string, out, diagnostics io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, err := io.WriteString(out, cliUsage)
		return err
	}
	command := args[0]
	if command != "status" && command != "pin" && command != "unpin" {
		return fmt.Errorf("unknown Codex command %q; run garcon codex --help", command)
	}
	fs := flag.NewFlagSet("garcon codex "+command, flag.ContinueOnError)
	fs.SetOutput(diagnostics)
	rawURL := fs.String("url", onboarding.DefaultURL, "running local Garcon service")
	asJSON := fs.Bool("json", false, "print routing status as JSON")
	fs.Usage = func() { fmt.Fprint(diagnostics, cliUsage); fs.PrintDefaults() }
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	expected := 0
	if command == "pin" {
		expected = 1
	}
	if fs.NArg() != expected || (expected == 1 && strings.TrimSpace(fs.Arg(0)) == "") {
		return fmt.Errorf("invalid arguments; %s", strings.TrimSpace(cliUsage))
	}
	base, err := onboarding.LocalURL(*rawURL)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	status, err := cliRequest(client, base, nil)
	if err != nil {
		return err
	}
	if command != "status" {
		id := ""
		if command == "pin" {
			id, err = resolvePinAccount(status.Accounts, strings.TrimSpace(fs.Arg(0)))
			if err != nil {
				return err
			}
		}
		status, err = cliRequest(client, base, &id)
		if err != nil {
			return err
		}
		if status.PinnedAccount != id {
			return errors.New("Garcon did not confirm the requested pin; check garcon codex status")
		}
	}
	if *asJSON {
		return json.NewEncoder(out).Encode(status)
	}
	return printRouting(out, status)
}

func cliRequest(client *http.Client, base string, pin *string) (Status, error) {
	method := http.MethodGet
	var body io.Reader
	if pin != nil {
		method = http.MethodPut
		encoded, _ := json.Marshal(map[string]string{"pinned_account": *pin})
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, base+"/api/routing/codex", body)
	if err != nil {
		return Status{}, err
	}
	if pin != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := client.Do(req)
	if err != nil {
		return Status{}, fmt.Errorf("cannot reach Garcon at %s: %w", base, err)
	}
	defer res.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode != http.StatusOK {
		var failure struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if decoder.Decode(&failure) == nil && failure.Error.Message != "" {
			return Status{}, fmt.Errorf("Garcon returned HTTP %d: %s", res.StatusCode, failure.Error.Message)
		}
		return Status{}, fmt.Errorf("Garcon returned HTTP %d", res.StatusCode)
	}
	var wire struct {
		Status
		Pin *string `json:"pinned_account"`
	}
	if err := decoder.Decode(&wire); err != nil {
		return Status{}, fmt.Errorf("invalid Garcon routing response: %w", err)
	}
	if wire.Pin == nil {
		return Status{}, errors.New("the running Garcon service does not support pinning; update and restart it")
	}
	wire.Status.PinnedAccount = *wire.Pin
	return wire.Status, nil
}

func resolvePinAccount(accounts []Account, selector string) (string, error) {
	// An exact account ID wins even if another account happens to use it as an email.
	for _, account := range accounts {
		if account.ID == selector {
			return enrolledPinAccount(account)
		}
	}
	var matches []Account
	for _, account := range accounts {
		if strings.EqualFold(account.Email, selector) {
			matches = append(matches, account)
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no Codex account matches %q; run garcon codex status for account IDs", selector)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("multiple Codex accounts use %q; use an account ID from garcon codex status", selector)
	}
	return enrolledPinAccount(matches[0])
}

func enrolledPinAccount(account Account) (string, error) {
	if !account.Enrolled {
		return "", errors.New("account is not in the Codex routing pool; enroll it before pinning")
	}
	return account.ID, nil
}

func printRouting(out io.Writer, status Status) error {
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if status.PinnedAccount != "" {
		fmt.Fprintf(table, "Pinned Codex account for new conversations: %s\n", status.PinnedAccount)
	} else {
		fmt.Fprintln(table, "Codex routing: automatic")
	}
	if status.NextAccount != "" {
		fmt.Fprintf(table, "Up next: %s (model access may affect eligibility)\n", status.NextAccount)
	} else {
		fmt.Fprintln(table, "Up next: no eligible account")
	}
	if status.Error != "" {
		fmt.Fprintf(table, "Routing error: %s\n", status.Error)
	}
	fmt.Fprintln(table, "\nACCOUNT ID\tEMAIL\tIN POOL\tSTATUS\tROUTING")
	for _, account := range status.Accounts {
		marker := ""
		if account.ID == status.PinnedAccount {
			marker = "PINNED"
		} else if account.ID == status.NextAccount {
			marker = "UP NEXT"
		}
		fmt.Fprintf(table, "%s\t%s\t%t\t%s\t%s\n", account.ID, account.Email, account.Enrolled, account.Status, marker)
	}
	return table.Flush()
}
