package setup

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParsers(t *testing.T) {
	keys := []byte(`[{"name":"anon","api_key":"eyJ...","type":"legacy"},{"name":"default","api_key":"sb_publishable_x","type":"publishable"},{"name":"default","api_key":"sb_secret_y","type":"secret"}]`)
	if secretKey(keys) != "sb_secret_y" {
		t.Errorf("secretKey: %q", secretKey(keys))
	}
	if secretKey([]byte(`[]`)) != "" {
		t.Error("no key must be empty")
	}
	if field([]byte(`{"ref":"abc"}`), "id", "ref") != "abc" || field([]byte(`{"id":"x","ref":"y"}`), "id", "ref") != "x" {
		t.Error("field precedence")
	}
	if org, err := pickOrg([]byte(`[{"id":"one","name":"Only"}]`)); err != nil || org != "one" {
		t.Errorf("single org: %q %v", org, err)
	}
	if _, err := pickOrg([]byte(`[{"id":"a","name":"A"},{"id":"b","name":"B"}]`)); err == nil || !strings.Contains(err.Error(), "--org-id") {
		t.Errorf("several orgs: %v", err)
	}
	if !strings.Contains(Schema, "create table if not exists public.garcon_usage") {
		t.Error("schema not embedded")
	}
	if p := newPassword(); len(p) < 20 || strings.ContainsAny(p, "/+=") {
		t.Errorf("password: %q", p)
	}
}

func TestSave(t *testing.T) {
	for _, status := range []int{200, 400} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				json.NewDecoder(r.Body).Decode(&body)
				if r.Method != "PUT" || body["key"] != "sb_secret_test" || body["device_name"] != "desk" || body["sync_enabled"] != true {
					t.Errorf("unexpected request: %v", body)
				}
				w.WriteHeader(status)
				if status != 200 {
					w.Write([]byte("refused sb_secret_test"))
				}
			}))
			defer s.Close()
			err := save("desk", "https://example.supabase.co", "sb_secret_test", s.URL)
			if status == 200 && err != nil {
				t.Fatal(err)
			}
			if status != 200 && (err == nil || strings.Contains(err.Error(), "sb_secret_test")) {
				t.Fatalf("unsafe error: %v", err)
			}
		})
	}
}
