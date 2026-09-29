package codexrouting

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCLIPinLifecycleUsesRunningRouter(t *testing.T) {
	r, _, _ := setup(t)
	server := httptest.NewServer(r)
	defer server.Close()
	run := func(command string, tail ...string) string {
		t.Helper()
		var out, diagnostics bytes.Buffer
		args := append([]string{command, "--url", server.URL}, tail...)
		if err := RunCLI(args, &out, &diagnostics); err != nil {
			t.Fatalf("%v: %v (%s)", args, err, diagnostics.String())
		}
		return out.String()
	}
	routeTo(t, r, "existing", "b")
	if out := run("status"); !strings.Contains(out, "automatic") || !strings.Contains(out, "b@example.com") || !strings.Contains(out, "UP NEXT") {
		t.Fatalf("incomplete status: %s", out)
	}
	if out := run("pin", "A@EXAMPLE.COM"); !strings.Contains(out, "PINNED") {
		t.Fatalf("pin not confirmed: %s", out)
	}
	routeTo(t, r, "existing", "a")
	if len(r.config.Accounts) != 3 {
		t.Fatal("CLI pin changed the account pool")
	}
	var status Status
	if err := json.Unmarshal([]byte(run("status", "--json")), &status); err != nil || status.PinnedAccount != "a" {
		t.Fatalf("invalid JSON status: %+v %v", status, err)
	}
	run("pin", "c")
	routeTo(t, r, "new", "c")
	if err := json.Unmarshal([]byte(run("unpin", "--json")), &status); err != nil || status.PinnedAccount != "" || status.NextAccount != "b" {
		t.Fatalf("unpin failed: %+v %v", status, err)
	}
	routeTo(t, r, "automatic", "b")
}

func TestCLIInvalidSelectionsDoNotMutateRouter(t *testing.T) {
	r, _, _ := setup(t)
	if err := r.Pin("a"); err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigureAccounts(false, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(r)
	defer server.Close()
	for _, selector := range []string{"unknown", "c", "c@example.com"} {
		var out bytes.Buffer
		if err := RunCLI([]string{"pin", "--url", server.URL, selector}, &out, &out); err == nil {
			t.Fatalf("accepted %q", selector)
		}
		if r.Status().PinnedAccount != "a" {
			t.Fatal("invalid selection changed the pin")
		}
	}
	accounts := []Account{{ID: "a", Email: "shared@example.com", Enrolled: true}, {ID: "b", Email: "shared@example.com", Enrolled: true}}
	if _, err := resolvePinAccount(accounts, "shared@example.com"); err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("ambiguous email accepted: %v", err)
	}
	if id, err := resolvePinAccount(accounts, "b"); err != nil || id != "b" {
		t.Fatalf("ID did not disambiguate: %s %v", id, err)
	}
}

func TestCLIRejectsBadResponsesAndOldServers(t *testing.T) {
	for _, test := range []struct {
		name       string
		code       int
		body, want string
	}{
		{"old server", 200, `{"enabled":true,"accounts":[]}`, "does not support pinning"},
		{"malformed", 200, `not-json`, "invalid Garcon routing response"},
		{"server error", 503, `{"error":{"message":"router unavailable"}}`, "router unavailable"},
		{"non JSON error", 403, `Forbidden`, "HTTP 403"},
	} {
		t.Run(test.name, func(t *testing.T) {
			writes := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Method != http.MethodGet {
					writes++
				}
				w.WriteHeader(test.code)
				w.Write([]byte(test.body))
			}))
			defer server.Close()
			var out bytes.Buffer
			err := RunCLI([]string{"unpin", "--url", server.URL}, &out, &out)
			if err == nil || !strings.Contains(err.Error(), test.want) || writes != 0 {
				t.Fatalf("error=%v writes=%d", err, writes)
			}
		})
	}
}

func TestCLIPreservesServerWriteErrorsAndChecksConfirmation(t *testing.T) {
	for _, reject := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Method == http.MethodPut && reject {
				w.WriteHeader(400)
				w.Write([]byte(`{"error":{"message":"cannot save pin"}}`))
				return
			}
			// Simulate a service that fails to apply the requested unpin.
			w.Write([]byte(`{"pinned_account":"a","next_account":"a","accounts":[]}`))
		}))
		var out bytes.Buffer
		err := RunCLI([]string{"unpin", "--url", server.URL}, &out, &out)
		server.Close()
		want := "did not confirm"
		if reject {
			want = "cannot save pin"
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("unexpected write result: %v", err)
		}
	}
}

func TestCLIArgumentValidationAndHelp(t *testing.T) {
	for _, args := range [][]string{{"pin"}, {"pin", ""}, {"status", "extra"}, {"unpin", "a"}, {"unknown"}, {"status", "--url", "https://example.com"}, {"pin", "--unknown"}} {
		var out bytes.Buffer
		if err := RunCLI(args, &out, &out); err == nil {
			t.Fatalf("accepted invalid arguments %v", args)
		}
	}
	for _, args := range [][]string{nil, {"--help"}, {"pin", "--help"}} {
		var out bytes.Buffer
		if err := RunCLI(args, &out, &out); err != nil || !strings.Contains(out.String(), "garcon codex pin") {
			t.Fatalf("help failed: %v %s", err, out.String())
		}
	}
}
