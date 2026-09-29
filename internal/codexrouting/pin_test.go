package codexrouting

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"garcon/internal/database"
)

func routeTo(t *testing.T, r *Router, session, want string) {
	t.Helper()
	req := request(session)
	done, err := r.Prepare(req)
	if err != nil {
		t.Fatal(err)
	}
	done()
	if got := req.Header.Get("ChatGPT-Account-Id"); got != want {
		t.Fatalf("routed to %q, want %q", got, want)
	}
}

func TestManualPinOverridesConversationsAndSurvivesRestart(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		name := "json"
		if sqlite {
			name = "sqlite"
		}
		t.Run(name, func(t *testing.T) {
			r, _, _ := setup(t)
			var db *database.DB
			if sqlite {
				var err error
				db, err = database.Open(filepath.Join(t.TempDir(), "usage.db"))
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				r.db = db
			}
			routeTo(t, r, "existing", "b")
			if err := r.Pin("a"); err != nil {
				t.Fatal(err)
			}
			if s := r.Status(); s.PinnedAccount != "a" || s.NextAccount != "a" {
				t.Fatalf("wrong status: %+v", s)
			}
			routeTo(t, r, "existing", "a")
			routeTo(t, r, "new", "a")
			if db != nil {
				var id string
				if err := db.QueryRow("SELECT account_id FROM sessions WHERE session_id='existing'").Scan(&id); err != nil || id != "a" {
					t.Fatalf("assignment not updated: %s %v", id, err)
				}
			}
			var restarted *Router
			if db == nil {
				restarted = New(r.dir, r.snapshot)
			} else {
				restarted = New(r.dir, r.snapshot, db)
			}
			restarted.now, restarted.health, restarted.discover = r.now, r.health, r.discover
			routeTo(t, restarted, "after-restart", "a")
			if err := restarted.Pin("c"); err != nil {
				t.Fatal(err)
			}
			routeTo(t, restarted, "existing", "c")
			if err := restarted.Pin(""); err != nil {
				t.Fatal(err)
			}
			if s := restarted.Status(); s.PinnedAccount != "" || s.NextAccount != "b" {
				t.Fatalf("automatic routing not restored: %+v", s)
			}
			routeTo(t, restarted, "automatic", "b")
			routeTo(t, restarted, "existing", "c")
		})
	}
}

func TestManualPinNoFallback(t *testing.T) {
	for _, condition := range []string{"exhausted", "stale", "unknown", "model", "cooldown", "missing-login"} {
		t.Run(condition, func(t *testing.T) {
			r, snapshot, home := setup(t)
			if err := r.Pin("a"); err != nil {
				t.Fatal(err)
			}
			switch condition {
			case "exhausted":
				used := 100.0
				snapshot.Accounts[0].Windows[0].UsedPercent = &used
			case "stale":
				snapshot.Accounts[0].Status = "stale"
			case "unknown":
				snapshot.Accounts[0].Windows[0].UsedPercent = nil
			case "model":
				r.health["a"] = health{models: map[string]bool{"other": true}, checked: r.now()}
			case "cooldown":
				r.cooldown["a"] = r.now().Add(time.Minute)
			case "missing-login":
				if err := os.RemoveAll(filepath.Join(home, ".codex-a")); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := r.Prepare(request("new")); err == nil || !strings.Contains(err.Error(), "pinned Codex account") {
				t.Fatalf("expected explicit pin failure, got %v", err)
			}
			if s := r.Status(); s.PinnedAccount != "a" || (condition != "model" && s.NextAccount != "") {
				t.Fatalf("wrong unavailable status: %+v", s)
			}
			if err := r.Pin(""); err != nil {
				t.Fatal(err)
			}
			routeTo(t, r, "new", "b")
		})
	}
}

func TestPinValidationAndPersistenceFailure(t *testing.T) {
	r, _, _ := setup(t)
	if err := r.Pin("missing"); err == nil {
		t.Fatal("accepted unknown account")
	}
	if err := r.ConfigureAccounts(false, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Pin("c"); err == nil {
		t.Fatal("accepted account outside pool")
	}
	if err := r.Pin("a"); err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigureAccounts(false, []string{"b"}); err == nil {
		t.Fatal("silently removed pinned account")
	}
	if s := r.Status(); s.PinnedAccount != "a" || !s.Accounts[0].Enrolled {
		t.Fatal("failed config changed state")
	}
	// Failed saves must leave the previous override active.
	if err := os.Remove(filepath.Join(r.dir, "codex-routing.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(r.dir, "codex-routing.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := r.Pin("b"); err == nil {
		t.Fatal("ignored persistence failure")
	}
	if r.Status().PinnedAccount != "a" {
		t.Fatal("unsaved pin became active")
	}
}

func TestNextAccountMatchesSelectionIncludingTies(t *testing.T) {
	r, snapshot, _ := setup(t)
	for i := range snapshot.Accounts {
		used := 20.0
		snapshot.Accounts[i].Windows[0].UsedPercent = &used
	}
	original := r.discover
	r.discover = func() []credential { all := original(); all[0], all[2] = all[2], all[0]; return all }
	if got := r.Status().NextAccount; got != "a" {
		t.Fatalf("tie-break selected %s", got)
	}
	routeTo(t, r, "tie", "a")
	r.cooldown["a"] = r.now().Add(time.Minute)
	if got := r.Status().NextAccount; got != "b" {
		t.Fatalf("unavailable account shown as next: %s", got)
	}
	routeTo(t, r, "healthy", "b")
	r.problem = "configuration unreadable"
	if r.Status().NextAccount != "" {
		t.Fatal("reported next account while router blocked")
	}
}

func TestManualPinOverridesContinuationOwner(t *testing.T) {
	r, _, _ := setup(t)
	if err := r.Pin("b"); err != nil {
		t.Fatal(err)
	}
	req := request("continuation")
	req.Header.Set("X-Codex-Turn-State", "opaque-state")
	req.Header.Del("ChatGPT-Account-Id")
	done, err := r.Prepare(req)
	if err != nil {
		t.Fatal(err)
	}
	done()
	if req.Header.Get("ChatGPT-Account-Id") != "b" {
		t.Fatal("manual pin ignored for continuation")
	}
}

func TestPinHTTPUpdatesAndLocalBoundary(t *testing.T) {
	r, _, _ := setup(t)
	send := func(body, remote, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "http://127.0.0.1:4141/api/routing/codex", strings.NewReader(body))
		req.RemoteAddr = remote
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	for _, client := range []struct{ remote, origin string }{{"192.0.2.1:123", ""}, {"127.0.0.1:123", "https://evil.example"}} {
		if w := send(`{"pinned_account":"a"}`, client.remote, client.origin); w.Code != 403 {
			t.Fatalf("allowed external pin: %d", w.Code)
		}
	}
	if r.Status().PinnedAccount != "" {
		t.Fatal("unauthorized pin persisted")
	}
	if w := send(`{"pinned_account":"a"}`, "127.0.0.1:123", "http://127.0.0.1:4141"); w.Code != 200 || !strings.Contains(w.Body.String(), `"pinned_account":"a"`) {
		t.Fatalf("pin failed: %s", w.Body.String())
	}
	if len(r.config.Accounts) != 3 {
		t.Fatal("pin replaced pool")
	}
	if w := send(`{"pinned_account":"missing"}`, "127.0.0.1:123", ""); w.Code != 400 {
		t.Fatal("invalid pin accepted")
	}
	if r.Status().PinnedAccount != "a" {
		t.Fatal("invalid update cleared pin")
	}
	if w := send(`{"pinned_account":""}`, "127.0.0.1:123", ""); w.Code != 200 {
		t.Fatal("unpin failed")
	}
	if r.Status().NextAccount != "b" {
		t.Fatal("unpin did not restore automatic routing")
	}
}

func TestPinChangeLeavesInflightRequestAlone(t *testing.T) {
	r, _, _ := setup(t)
	old := request("ongoing")
	release, err := r.Prepare(old)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := r.Pin("a"); err != nil {
		t.Fatal(err)
	}
	routeTo(t, r, "ongoing", "a")
	if old.Header.Get("ChatGPT-Account-Id") != "b" || r.active["b"] != 1 || r.active["a"] != 0 {
		t.Fatal("pin disrupted in-flight identity or accounting")
	}
}

func TestFailedReassignmentPreservesPreviousConversation(t *testing.T) {
	r, _, _ := setup(t)
	routeTo(t, r, "existing", "b")
	if err := r.Pin("a"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(r.dir, "codex-routing-pins.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Prepare(request("existing")); err == nil {
		t.Fatal("forwarded without saving reassignment")
	}
	for _, id := range r.pins {
		if id != "b" {
			t.Fatal("failed reassignment replaced original identity")
		}
	}
}
